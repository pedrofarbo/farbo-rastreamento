package vehicles

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// OwnerIndex responde, em memória, de qual cliente é cada veículo e cada
// rastreador. O WebSocket consulta o índice a cada mensagem para entregar ao
// cliente só o que é dele, então a consulta não pode ir ao banco.
//
// Guarda também quem recebeu do dono acesso ao veículo (shares): essas
// pessoas acompanham a posição dele ao vivo. E, para a equipe restrita a
// grupos, o grupo de cada veículo e os grupos de cada pessoa.
//
// O índice se recarrega sozinho a cada intervalo (o que cobre alterações
// feitas em outras instâncias) e na hora quando esta instância altera um
// veículo ou um acesso (Refresh).
type OwnerIndex struct {
	db  *database.DB
	log *slog.Logger

	mu        sync.RWMutex
	byVehicle map[uuid.UUID]uuid.UUID
	byDevice  map[uuid.UUID]uuid.UUID
	// Quem tem acesso (de terceiro) a cada veículo e rastreador.
	guestsByVehicle map[uuid.UUID]map[uuid.UUID]bool
	guestsByDevice  map[uuid.UUID]map[uuid.UUID]bool
	// O grupo de cada veículo e rastreador (sem grupo: fora do mapa) e os
	// grupos de quem da equipe está restrito (sem nenhum: mapa vazio).
	groupByVehicle map[uuid.UUID]uuid.UUID
	groupByDevice  map[uuid.UUID]uuid.UUID
	restricted     map[uuid.UUID]map[uuid.UUID]bool
	// A organização (associação ou seguradora) de cada veículo e rastreador
	// de associado: a equipe dela acompanha ao vivo.
	orgByVehicle map[uuid.UUID]uuid.UUID
	orgByDevice  map[uuid.UUID]uuid.UUID
	// O corretor do associado dono (só nos que têm).
	brokerByVehicle map[uuid.UUID]uuid.UUID
	brokerByDevice  map[uuid.UUID]uuid.UUID
}

func NewOwnerIndex(db *database.DB, log *slog.Logger) *OwnerIndex {
	return &OwnerIndex{
		db:              db,
		log:             log.With("component", "vehicle-owners"),
		byVehicle:       map[uuid.UUID]uuid.UUID{},
		byDevice:        map[uuid.UUID]uuid.UUID{},
		guestsByVehicle: map[uuid.UUID]map[uuid.UUID]bool{},
		guestsByDevice:  map[uuid.UUID]map[uuid.UUID]bool{},
		groupByVehicle:  map[uuid.UUID]uuid.UUID{},
		groupByDevice:   map[uuid.UUID]uuid.UUID{},
		restricted:      map[uuid.UUID]map[uuid.UUID]bool{},
		orgByVehicle:    map[uuid.UUID]uuid.UUID{},
		orgByDevice:     map[uuid.UUID]uuid.UUID{},
		brokerByVehicle: map[uuid.UUID]uuid.UUID{},
		brokerByDevice:  map[uuid.UUID]uuid.UUID{},
	}
}

// Refresh relê os donos do banco.
func (o *OwnerIndex) Refresh(ctx context.Context) error {
	rows, err := o.db.Query(ctx, `
		SELECT id, device_id, owner_id, group_id, (SELECT u.organization_id FROM users u WHERE u.id = vehicles.owner_id),
			(SELECT u.broker_id FROM users u WHERE u.id = vehicles.owner_id)
		FROM vehicles WHERE owner_id IS NOT NULL`)
	if err != nil {
		return database.MapError(err)
	}
	defer rows.Close()

	byVehicle := map[uuid.UUID]uuid.UUID{}
	byDevice := map[uuid.UUID]uuid.UUID{}
	groupByVehicle := map[uuid.UUID]uuid.UUID{}
	groupByDevice := map[uuid.UUID]uuid.UUID{}
	orgByVehicle := map[uuid.UUID]uuid.UUID{}
	orgByDevice := map[uuid.UUID]uuid.UUID{}
	brokerByVehicle := map[uuid.UUID]uuid.UUID{}
	brokerByDevice := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var vehicleID, ownerID uuid.UUID
		var deviceID, groupID, orgID, brokerID *uuid.UUID
		if err := rows.Scan(&vehicleID, &deviceID, &ownerID, &groupID, &orgID, &brokerID); err != nil {
			return err
		}
		if brokerID != nil {
			brokerByVehicle[vehicleID] = *brokerID
			if deviceID != nil {
				brokerByDevice[*deviceID] = *brokerID
			}
		}
		byVehicle[vehicleID] = ownerID
		if deviceID != nil {
			byDevice[*deviceID] = ownerID
		}
		if orgID != nil {
			orgByVehicle[vehicleID] = *orgID
			if deviceID != nil {
				orgByDevice[*deviceID] = *orgID
			}
		}
		if groupID != nil {
			groupByVehicle[vehicleID] = *groupID
			if deviceID != nil {
				groupByDevice[*deviceID] = *groupID
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	// Acesso de terceiro só vale enquanto quem deu for o dono do veículo.
	guestRows, err := o.db.Query(ctx, `
		SELECT s.vehicle_id, v.device_id, s.guest_id
		FROM vehicle_shares s
		JOIN vehicles v ON v.id = s.vehicle_id AND v.owner_id = s.owner_id
		JOIN users g ON g.id = s.guest_id AND g.active`)
	if err != nil {
		return database.MapError(err)
	}
	defer guestRows.Close()
	guestsByVehicle := map[uuid.UUID]map[uuid.UUID]bool{}
	guestsByDevice := map[uuid.UUID]map[uuid.UUID]bool{}
	add := func(m map[uuid.UUID]map[uuid.UUID]bool, key, guest uuid.UUID) {
		if m[key] == nil {
			m[key] = map[uuid.UUID]bool{}
		}
		m[key][guest] = true
	}
	for guestRows.Next() {
		var vehicleID, guestID uuid.UUID
		var deviceID *uuid.UUID
		if err := guestRows.Scan(&vehicleID, &deviceID, &guestID); err != nil {
			return err
		}
		add(guestsByVehicle, vehicleID, guestID)
		if deviceID != nil {
			add(guestsByDevice, *deviceID, guestID)
		}
	}
	if err := guestRows.Err(); err != nil {
		return err
	}
	guestRows.Close()

	memberRows, err := o.db.Query(ctx, `
		SELECT m.user_id, g.group_id FROM account_members m
		LEFT JOIN account_member_groups g ON g.user_id = m.user_id
		WHERE m.restricted`)
	if err != nil {
		return database.MapError(err)
	}
	defer memberRows.Close()
	restricted := map[uuid.UUID]map[uuid.UUID]bool{}
	for memberRows.Next() {
		var userID uuid.UUID
		var groupID *uuid.UUID
		if err := memberRows.Scan(&userID, &groupID); err != nil {
			return err
		}
		if restricted[userID] == nil {
			restricted[userID] = map[uuid.UUID]bool{}
		}
		if groupID != nil {
			restricted[userID][*groupID] = true
		}
	}
	if err := memberRows.Err(); err != nil {
		return err
	}

	o.mu.Lock()
	o.byVehicle, o.byDevice = byVehicle, byDevice
	o.guestsByVehicle, o.guestsByDevice = guestsByVehicle, guestsByDevice
	o.groupByVehicle, o.groupByDevice, o.restricted = groupByVehicle, groupByDevice, restricted
	o.orgByVehicle, o.orgByDevice = orgByVehicle, orgByDevice
	o.brokerByVehicle, o.brokerByDevice = brokerByVehicle, brokerByDevice
	o.mu.Unlock()
	return nil
}

// Run recarrega o índice periodicamente até o contexto acabar.
func (o *OwnerIndex) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := o.Refresh(ctx); err != nil && ctx.Err() == nil {
				o.log.Warn("falha ao recarregar os donos dos veículos", "err", err)
			}
		}
	}
}

// Owns diz se a mensagem sobre este veículo/rastreador pertence ao cliente.
// Sem nenhum dos dois identificadores, a resposta é não.
func (o *OwnerIndex) Owns(customerID uuid.UUID, vehicleID, deviceID *uuid.UUID) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if vehicleID != nil {
		owner, ok := o.byVehicle[*vehicleID]
		return ok && owner == customerID
	}
	if deviceID != nil {
		owner, ok := o.byDevice[*deviceID]
		return ok && owner == customerID
	}
	return false
}

// VisibleTo diz se a mensagem sobre este veículo/rastreador vai para a
// pessoa que enxerga a conta account: é da conta e, se a pessoa estiver
// restrita a grupos, de um veículo de um dos grupos dela.
func (o *OwnerIndex) VisibleTo(person, account uuid.UUID, vehicleID, deviceID *uuid.UUID) bool {
	if !o.Owns(account, vehicleID, deviceID) {
		return false
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	groups, limited := o.restricted[person]
	if !limited {
		return true
	}
	var group uuid.UUID
	var ok bool
	if vehicleID != nil {
		group, ok = o.groupByVehicle[*vehicleID]
	} else {
		group, ok = o.groupByDevice[*deviceID]
	}
	return ok && groups[group]
}

// SharedWith diz se a mensagem sobre este veículo/rastreador é de um veículo
// que o dono compartilhou com a pessoa. Quais mensagens ela recebe, quem
// decide é quem chama (só a posição e a situação).
func (o *OwnerIndex) SharedWith(userID uuid.UUID, vehicleID, deviceID *uuid.UUID) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if vehicleID != nil {
		return o.guestsByVehicle[*vehicleID][userID]
	}
	if deviceID != nil {
		return o.guestsByDevice[*deviceID][userID]
	}
	return false
}

// OfBroker diz se a mensagem sobre este veículo/rastreador é de um associado
// do corretor.
func (o *OwnerIndex) OfBroker(brokerID uuid.UUID, vehicleID, deviceID *uuid.UUID) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if vehicleID != nil {
		b, ok := o.brokerByVehicle[*vehicleID]
		return ok && b == brokerID
	}
	if deviceID != nil {
		b, ok := o.brokerByDevice[*deviceID]
		return ok && b == brokerID
	}
	return false
}

// InOrganization diz se a mensagem sobre este veículo/rastreador é de um
// associado da organização. Sem nenhum dos dois identificadores, não.
func (o *OwnerIndex) InOrganization(orgID uuid.UUID, vehicleID, deviceID *uuid.UUID) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if vehicleID != nil {
		org, ok := o.orgByVehicle[*vehicleID]
		return ok && org == orgID
	}
	if deviceID != nil {
		org, ok := o.orgByDevice[*deviceID]
		return ok && org == orgID
	}
	return false
}

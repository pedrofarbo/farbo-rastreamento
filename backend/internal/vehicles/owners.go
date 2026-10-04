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
// pessoas acompanham a posição dele ao vivo.
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
}

func NewOwnerIndex(db *database.DB, log *slog.Logger) *OwnerIndex {
	return &OwnerIndex{
		db:              db,
		log:             log.With("component", "vehicle-owners"),
		byVehicle:       map[uuid.UUID]uuid.UUID{},
		byDevice:        map[uuid.UUID]uuid.UUID{},
		guestsByVehicle: map[uuid.UUID]map[uuid.UUID]bool{},
		guestsByDevice:  map[uuid.UUID]map[uuid.UUID]bool{},
	}
}

// Refresh relê os donos do banco.
func (o *OwnerIndex) Refresh(ctx context.Context) error {
	rows, err := o.db.Query(ctx, `SELECT id, device_id, owner_id FROM vehicles WHERE owner_id IS NOT NULL`)
	if err != nil {
		return database.MapError(err)
	}
	defer rows.Close()

	byVehicle := map[uuid.UUID]uuid.UUID{}
	byDevice := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var vehicleID, ownerID uuid.UUID
		var deviceID *uuid.UUID
		if err := rows.Scan(&vehicleID, &deviceID, &ownerID); err != nil {
			return err
		}
		byVehicle[vehicleID] = ownerID
		if deviceID != nil {
			byDevice[*deviceID] = ownerID
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

	o.mu.Lock()
	o.byVehicle, o.byDevice = byVehicle, byDevice
	o.guestsByVehicle, o.guestsByDevice = guestsByVehicle, guestsByDevice
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

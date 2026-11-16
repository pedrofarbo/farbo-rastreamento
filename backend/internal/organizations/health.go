package organizations

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
)

// A saúde dos rastreadores dos associados: quantos estão mandando sinal, os
// que pararam (há mais de um dia, há mais de três), os que nunca mandaram e
// o corte de energia — e o corte seguido de silêncio, quando o rastreador
// pode ter sido desligado ou retirado do veículo. É o que a associação
// precisa saber antes do dia do roubo. O associado é avisado no app: pela
// equipe, na hora, ou sozinho, uma vez por dia (das 9h às 20h), no máximo a
// cada 3 dias por veículo e 3 vezes no mesmo silêncio.

const (
	// silentAfter: mesmo parado, o rastreador manda sinal (o batimento) a
	// cada poucos minutos; um dia sem nada é problema.
	silentAfter = 24 * time.Hour
	silentLong  = 72 * time.Hour
	// powerWindow: até quando o corte de energia aparece.
	powerWindow = 7 * 24 * time.Hour
	// removedAfterCut: na bateria interna, o rastreador ainda manda sinal
	// por algumas horas depois do corte; o silêncio que começou nesse prazo
	// e já dura removedSilence é o do rastreador desligado ou retirado.
	removedAfterCut = 12 * time.Hour
	removedSilence  = 2 * time.Hour
	// noticeEvery e noticesPerSilence: o aviso sozinho ao associado.
	noticeEvery       = 72 * time.Hour
	noticesPerSilence = 3
)

// Os problemas, do mais grave ao mais leve.
const (
	IssueRemoved    = "removed"     // o corte de energia e o silêncio desde então
	IssueSilentLong = "silent_long" // sem sinal há mais de 3 dias
	IssueSilent     = "silent"      // sem sinal há mais de 1 dia
	IssueNever      = "never"       // instalado, nunca mandou sinal
	IssuePowerCut   = "power_cut"   // o corte de energia na semana (voltou)
)

var issueOrder = []string{IssueRemoved, IssueSilentLong, IssueSilent, IssueNever, IssuePowerCut}

// HealthVehicle é um veículo com rastreador de associado e o problema dele.
type HealthVehicle struct {
	VehicleID      uuid.UUID  `json:"vehicleId"`
	VehicleName    string     `json:"vehicleName"`
	Plate          string     `json:"plate"`
	AssociateID    uuid.UUID  `json:"associateId"`
	AssociateName  string     `json:"associateName"`
	Phone          string     `json:"phone"`
	IMEI           string     `json:"imei"`
	Status         string     `json:"status"`
	LastSeenAt     *time.Time `json:"lastSeenAt"`
	LastPowerCutAt *time.Time `json:"lastPowerCutAt"`
	PowerCuts      int        `json:"powerCuts"`
	Issue          string     `json:"issue"`
	// NotifiedAt: o último aviso ao associado; PushEnabled: ele tem as
	// notificações do app ligadas (o aviso chega).
	NotifiedAt  *time.Time `json:"notifiedAt"`
	PushEnabled bool       `json:"pushEnabled"`

	autoSinceSeen int
}

// Health é a saúde dos rastreadores da organização.
type Health struct {
	Installed  int  `json:"installed"`
	Online     int  `json:"online"`
	Silent     int  `json:"silent"`
	SilentLong int  `json:"silentLong"`
	Never      int  `json:"never"`
	Removed    int  `json:"removed"`
	PowerCuts  int  `json:"powerCuts"`
	AutoNotice bool `json:"autoNotice"`
	// Vehicles: só os que têm problema, do mais grave.
	Vehicles []HealthVehicle `json:"vehicles"`
}

// issueOf é o problema do rastreador agora.
func issueOf(now time.Time, lastSeen, lastCut *time.Time) string {
	if lastSeen == nil {
		return IssueNever
	}
	silence := now.Sub(*lastSeen)
	if lastCut != nil && !lastSeen.Before(*lastCut) && lastSeen.Sub(*lastCut) <= removedAfterCut && silence >= removedSilence {
		return IssueRemoved
	}
	switch {
	case silence >= silentLong:
		return IssueSilentLong
	case silence >= silentAfter:
		return IssueSilent
	case lastCut != nil:
		return IssuePowerCut
	}
	return ""
}

// healthRows são os veículos com rastreador dos associados ativos (os do
// corretor, com broker).
func (s *Service) healthRows(ctx context.Context, orgID uuid.UUID, broker *uuid.UUID, now time.Time) ([]HealthVehicle, error) {
	rows, err := s.db.Query(ctx, `
		SELECT v.id, v.name, COALESCE(v.plate, ''), a.id, a.name, a.phone, d.imei, d.status, d.last_seen_at,
			pc.last, pc.count,
			(SELECT max(n.notified_at) FROM tracker_health_notices n WHERE n.vehicle_id = v.id),
			(SELECT count(*) FROM tracker_health_notices n WHERE n.vehicle_id = v.id AND n.automatic
				AND n.notified_at > COALESCE(d.last_seen_at, '-infinity')),
			EXISTS (SELECT 1 FROM push_subscriptions p WHERE p.user_id = a.id)
		FROM vehicles v
		JOIN users a ON a.id = v.owner_id AND a.organization_id = $1 AND a.active
		JOIN devices d ON d.id = v.device_id
		LEFT JOIN LATERAL (
			SELECT max(e.timestamp) AS last, count(*) AS count FROM vehicle_events e
			WHERE e.device_id = d.id AND e.type = 'POWER_LOSS' AND e.timestamp > $2
		) pc ON TRUE
		WHERE ($3::uuid IS NULL OR a.broker_id = $3)
		ORDER BY a.name, v.name`, orgID, now.Add(-powerWindow), broker)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []HealthVehicle
	for rows.Next() {
		var h HealthVehicle
		if err := rows.Scan(&h.VehicleID, &h.VehicleName, &h.Plate, &h.AssociateID, &h.AssociateName, &h.Phone, &h.IMEI,
			&h.Status, &h.LastSeenAt, &h.LastPowerCutAt, &h.PowerCuts, &h.NotifiedAt, &h.autoSinceSeen, &h.PushEnabled); err != nil {
			return nil, database.MapError(err)
		}
		h.Issue = issueOf(now, h.LastSeenAt, h.LastPowerCutAt)
		out = append(out, h)
	}
	return out, database.MapError(rows.Err())
}

// TrackerHealth é a saúde dos rastreadores da organização (o corretor vê os
// associados dele).
func (s *Service) TrackerHealth(ctx context.Context, orgID uuid.UUID, broker *uuid.UUID) (*Health, error) {
	now := s.billing.Now()
	list, err := s.healthRows(ctx, orgID, broker, now)
	if err != nil {
		return nil, err
	}
	out := &Health{Vehicles: []HealthVehicle{}}
	if err := s.db.QueryRow(ctx, `SELECT health_auto_notice FROM organizations WHERE id = $1`, orgID).Scan(&out.AutoNotice); err != nil {
		return nil, database.MapError(err)
	}
	for _, h := range list {
		out.Installed++
		if h.Status == "ONLINE" {
			out.Online++
		}
		switch h.Issue {
		case IssueRemoved:
			out.Removed++
		case IssueSilentLong:
			out.SilentLong++
		case IssueSilent:
			out.Silent++
		case IssueNever:
			out.Never++
		}
		if h.PowerCuts > 0 {
			out.PowerCuts++
		}
		if h.Issue != "" {
			out.Vehicles = append(out.Vehicles, h)
		}
	}
	slices.SortStableFunc(out.Vehicles, func(a, b HealthVehicle) int {
		return slices.Index(issueOrder, a.Issue) - slices.Index(issueOrder, b.Issue)
	})
	return out, nil
}

// SetHealthAutoNotice liga ou desliga o aviso sozinho ao associado.
func (s *Service) SetHealthAutoNotice(ctx context.Context, orgID uuid.UUID, on bool) error {
	tag, err := s.db.Exec(ctx, `UPDATE organizations SET health_auto_notice = $2, updated_at = NOW() WHERE id = $1`, orgID, on)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ErrHealthy: o rastreador do veículo está mandando sinal (nada a avisar).
var ErrHealthy = &InputError{"o rastreador deste veículo está mandando sinal: não há o que avisar"}

// healthNotice é o aviso ao associado sobre o rastreador do veículo.
func (s *Service) healthNotice(orgName string, h HealthVehicle) push.Notification {
	when := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.In(s.billing.Location()).Format("02/01 às 15h04")
	}
	n := push.Notification{
		Title: "Rastreador sem sinal: " + h.VehicleName, URL: "/app/mapa", Tag: "tracker-health:" + h.VehicleID.String(),
		Severity: mail.SeverityWarning, TTL: 24 * time.Hour,
	}
	switch h.Issue {
	case IssueRemoved:
		n.Body = fmt.Sprintf("O rastreador do %s ficou sem a energia do veículo em %s e parou de mandar sinal. Confira a bateria do veículo e fale com a %s.",
			h.VehicleName, when(h.LastPowerCutAt), orgName)
	case IssueNever:
		n.Body = fmt.Sprintf("O rastreador do %s ainda não mandou sinal. Ligue o veículo e deixe-o num lugar aberto por alguns minutos; se não resolver, fale com a %s.",
			h.VehicleName, orgName)
	default:
		n.Body = fmt.Sprintf("O rastreador do %s não manda sinal desde %s. Ligue o veículo e deixe-o num lugar aberto por alguns minutos; se continuar, fale com a %s.",
			h.VehicleName, when(h.LastSeenAt), orgName)
	}
	return n
}

// notifyHealth avisa o associado e guarda o aviso (quantos aparelhos receberam).
func (s *Service) notifyHealth(ctx context.Context, orgID uuid.UUID, orgName string, h HealthVehicle, automatic bool, by *uuid.UUID) (int, error) {
	delivered := 0
	if s.pusher != nil {
		n, err := s.pusher.Notify(ctx, h.AssociateID, s.healthNotice(orgName, h))
		if err != nil {
			s.log.Warn("aviso do rastreador sem sinal não chegou ao associado", "vehicle", h.VehicleID, "err", err)
		}
		delivered = n
	}
	kind := "silent"
	if h.Issue == IssueRemoved {
		kind = "removed"
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO tracker_health_notices (organization_id, vehicle_id, kind, automatic, sent_by, delivered)
		VALUES ($1, $2, $3, $4, $5, $6)`, orgID, h.VehicleID, kind, automatic, by, delivered)
	return delivered, database.MapError(err)
}

// NotifyTrackerVehicle: a equipe avisa o associado agora (o rastreador do
// veículo com problema). Devolve em quantos aparelhos chegou.
func (s *Service) NotifyTrackerVehicle(ctx context.Context, orgID, vehicleID uuid.UUID, broker *uuid.UUID, by uuid.UUID) (int, error) {
	o, err := s.Get(ctx, orgID)
	if err != nil {
		return 0, err
	}
	list, err := s.healthRows(ctx, orgID, broker, s.billing.Now())
	if err != nil {
		return 0, err
	}
	i := slices.IndexFunc(list, func(h HealthVehicle) bool { return h.VehicleID == vehicleID })
	if i < 0 {
		return 0, database.ErrNotFound
	}
	if list[i].Issue == "" || list[i].Issue == IssuePowerCut {
		return 0, ErrHealthy
	}
	return s.notifyHealth(ctx, orgID, o.Name, list[i], false, nullable(by))
}

// NotifyTrackerHealth avisa sozinho os associados dos rastreadores sem
// sinal, nas organizações com o aviso ligado (das 9h às 20h; no máximo a
// cada 3 dias por veículo e 3 vezes no mesmo silêncio).
func (s *Service) NotifyTrackerHealth(ctx context.Context) {
	now := s.billing.Now()
	if h := now.In(s.billing.Location()).Hour(); h < 9 || h >= 20 {
		return
	}
	rows, err := s.db.Query(ctx, `
		SELECT o.id, u.name FROM organizations o JOIN users u ON u.id = o.id WHERE u.active AND o.health_auto_notice`)
	if err != nil {
		s.log.Error("falha ao listar as organizações para o aviso dos rastreadores", "err", err)
		return
	}
	type org struct {
		id   uuid.UUID
		name string
	}
	var orgs []org
	for rows.Next() {
		var o org
		if err := rows.Scan(&o.id, &o.name); err != nil {
			rows.Close()
			s.log.Error("falha ao ler as organizações para o aviso dos rastreadores", "err", err)
			return
		}
		orgs = append(orgs, o)
	}
	rows.Close()
	sent := 0
	for _, o := range orgs {
		list, err := s.healthRows(ctx, o.id, nil, now)
		if err != nil {
			s.log.Error("falha ao ver a saúde dos rastreadores", "organization", o.id, "err", err)
			continue
		}
		for _, h := range list {
			if h.Issue != IssueRemoved && h.Issue != IssueSilent && h.Issue != IssueSilentLong && h.Issue != IssueNever {
				continue
			}
			if h.autoSinceSeen >= noticesPerSilence || (h.NotifiedAt != nil && now.Sub(*h.NotifiedAt) < noticeEvery) {
				continue
			}
			if _, err := s.notifyHealth(ctx, o.id, o.name, h, true, nil); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				s.log.Error("falha ao guardar o aviso do rastreador", "vehicle", h.VehicleID, "err", err)
				continue
			}
			sent++
		}
	}
	if sent > 0 {
		s.log.Info("associados avisados dos rastreadores sem sinal", "count", sent)
	}
}

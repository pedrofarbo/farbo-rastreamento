package appointments

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
)

/*
 * O andamento da instalação pelo link e a instalação parada. O rastreador só
 * fica ligado ao veículo (e o cliente só o vê no app) quando o instalador
 * toca em "Concluir" no link: até lá, a central e o representante veem em que
 * passo está — a chegada, o rastreador, o teste do sinal, a foto — e, parada
 * há StallAfter, a agenda destaca e um e-mail avisa a central e o
 * representante que agendou (um por situação; remarcar zera).
 */

// StallAfter é quando a instalação conta como parada: o horário marcado
// passou sem a chegada, ou o instalador chegou e não concluiu.
const StallAfter = 3 * time.Hour

// As situações da instalação parada.
const (
	StallLate    = "late"
	StallStalled = "stalled"
)

// stallOf é a situação de parada do agendamento agora (vazio: andando).
func stallOf(a *Appointment, now time.Time) string {
	switch {
	case a.Status == StatusScheduled && now.Sub(a.ScheduledFor) > StallAfter:
		return StallLate
	case a.Status == StatusArrived && a.ArrivedAt != nil && now.Sub(*a.ArrivedAt) > StallAfter:
		return StallStalled
	}
	return ""
}

// SaveCheck guarda o último teste do sinal pelo link: o rastreador testado
// (nulo: o IMEI não está cadastrado) e se passou.
func (s *Service) SaveCheck(ctx context.Context, id uuid.UUID, deviceID *uuid.UUID, ready bool) error {
	_, err := s.db.Exec(ctx, `
		UPDATE installation_appointments SET checked_at = NOW(), check_ready = $3, check_device_id = $2, updated_at = NOW()
		WHERE id = $1 AND status IN ('scheduled', 'arrived')`, id, deviceID, ready)
	return database.MapError(err)
}

// Stalled são os agendamentos em aberto parados há StallAfter, de qualquer
// dia (a agenda da semana não mostra o de dias atrás), com o link de cada um.
func (s *Service) Stalled(ctx context.Context) ([]*Appointment, error) {
	list, err := s.list(ctx, `a.status IN ('scheduled', 'arrived') AND COALESCE(a.arrived_at, a.scheduled_for) < $1`,
		s.now().Add(-StallAfter))
	for _, a := range list {
		a.URL, a.Pin = s.PublicURL(a.ID), a.accessPin
	}
	return list, err
}

// Progress é o que já foi feito e o que falta no link, em texto (o e-mail).
func Progress(a *Appointment) (done, missing []string) {
	step := func(ok bool, did, todo string) {
		if ok {
			done = append(done, did)
		} else {
			missing = append(missing, todo)
		}
	}
	arrived := "chegada"
	if a.ArrivedAt != nil {
		arrived += " às " + a.ArrivedAt.In(brt).Format("15:04")
	}
	step(a.ArrivedAt != nil, arrived, "registrar a chegada")
	if a.Kind == KindUninstall {
		step(slices.Contains(a.Photos, "tracker"), "foto do rastreador retirado", "foto do rastreador retirado")
		missing = append(missing, "concluir a retirada")
		return done, missing
	}
	tracker := "rastreador de IMEI final " + imeiTail(a.TrackerIMEI)
	if a.TrackerConnected {
		tracker += " conectado"
	}
	step(a.TrackerIMEI != "", tracker, "escolher (e ativar) o rastreador")
	switch {
	case a.CheckReady:
		done = append(done, "sinal testado")
	case a.CheckedAt != nil:
		missing = append(missing, "teste do sinal (o último não passou)")
	default:
		missing = append(missing, "teste do sinal")
	}
	step(slices.Contains(a.Photos, "tracker"), "foto do rastreador", "foto do rastreador")
	missing = append(missing, "concluir a instalação")
	return done, missing
}

// imeiTail são os 6 últimos números do IMEI (o e-mail não leva o inteiro).
func imeiTail(imei string) string {
	if len(imei) <= 6 {
		return imei
	}
	return imei[len(imei)-6:]
}

// StallNotifier manda o aviso da instalação parada (mail.InstallMailer).
type StallNotifier interface {
	InstallStalled(ctx context.Context, to string, n mail.StalledInstall) error
}

// SetStallNotifier liga o aviso da instalação parada: os e-mails da central
// e, no agendamento do representante, o dele.
func (s *Service) SetStallNotifier(n StallNotifier, central []string) {
	s.stallNotifier, s.stallTo = n, central
}

// NotifyStalled avisa, das 8h às 20h, cada instalação que parou e ainda não
// teve o aviso desta situação. Sem nenhum envio certo, tenta na próxima vez.
func (s *Service) NotifyStalled(ctx context.Context) {
	if s.stallNotifier == nil {
		return
	}
	if h := s.now().In(brt).Hour(); h < 8 || h >= 20 {
		return
	}
	list, err := s.Stalled(ctx)
	if err != nil {
		s.log.Error("falha ao procurar as instalações paradas", "err", err)
		return
	}
	for _, a := range list {
		if a.Stall == "" || a.Stall == a.stallNotice {
			continue
		}
		done, missing := Progress(a)
		n := mail.StalledInstall{
			Late: a.Stall == StallLate, What: what(a.Kind), VehicleName: a.VehicleName, Plate: a.Plate,
			CustomerName: a.CustomerName, InstallerName: a.InstallerName, ScheduledFor: a.ScheduledFor.In(brt),
			Done: done, Missing: missing,
		}
		if a.ArrivedAt != nil {
			at := a.ArrivedAt.In(brt)
			n.ArrivedAt = &at
		}
		sent, failed := 0, 0
		seen := map[string]bool{}
		send := func(to string, installer bool) {
			key := strings.ToLower(strings.TrimSpace(to))
			if key == "" || seen[key] {
				return
			}
			seen[key] = true
			m := n
			m.ForInstaller = installer
			if installer {
				m.ActionURL, m.ActionLabel = a.URL, "Abrir o link do instalador"
			} else {
				m.ActionURL, m.ActionLabel = strings.TrimRight(s.appURL, "/")+"/agenda", "Ver a agenda"
			}
			if err := s.stallNotifier.InstallStalled(ctx, to, m); err != nil {
				failed++
				s.log.Warn("aviso da instalação parada não saiu", "appointment", a.ID, "err", err)
				return
			}
			sent++
		}
		if a.AffiliateID != nil {
			var email string
			err := s.db.QueryRow(ctx, `SELECT email FROM affiliates WHERE id = $1`, *a.AffiliateID).Scan(&email)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				s.log.Warn("e-mail do representante da instalação parada", "appointment", a.ID, "err", err)
			}
			send(email, true)
		}
		for _, to := range s.stallTo {
			send(to, false)
		}
		if failed > 0 && sent == 0 {
			continue
		}
		if _, err := s.db.Exec(ctx, `UPDATE installation_appointments SET stall_notice = $2 WHERE id = $1`, a.ID, a.Stall); err != nil {
			s.log.Error("falha ao marcar o aviso da instalação parada", "appointment", a.ID, "err", err)
			continue
		}
		s.log.Info("instalação parada avisada", "appointment", a.ID, "stall", a.Stall, "emails", sent)
	}
}

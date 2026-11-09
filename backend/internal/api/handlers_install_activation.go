package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/appointments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/smssetup"
)

// A ativação pelo link do instalador (sem login): o rastreador livre que
// nunca conectou ganha o chip escolhido no estoque e a configuração por SMS,
// antes de ser instalado. No agendamento (/public/install/{token}), o
// instalador escolhe o rastreador e o chip; na instalação avulsa
// (/public/installer-links/{token}), o rastreador é o do link. A tela
// acompanha os SMS e a conexão até o rastreador conectar; depois, o teste do
// sinal e a conclusão de sempre.

// activationStep é um passo da configuração como o instalador vê: o nome e
// a situação (os textos dos comandos ficam só na central).
type activationStep struct {
	Label  string `json:"label"`
	Status string `json:"status"`
}

// activationView é a ativação do link: o rastreador, o chip, a situação da
// configuração por SMS (SENDING, WAITING, DONE, FAILED, TIMEOUT, CANCELED) e
// os passos.
type activationView struct {
	IMEI         string           `json:"imei"`
	ICCID        string           `json:"iccid"`
	Status       string           `json:"status"`
	Error        string           `json:"error"`
	Steps        []activationStep `json:"steps"`
	WaitingSince *time.Time       `json:"waitingSince"`
	ConnectedAt  *time.Time       `json:"connectedAt"`
	FinishedAt   *time.Time       `json:"finishedAt"`
}

// activationResponse: a ativação do link (nula: nenhuma ainda), quantas o
// link ainda pode fazer e se a ativação por SMS está disponível.
type activationResponse struct {
	Enabled    bool            `json:"enabled"`
	Left       int             `json:"left"`
	Activation *activationView `json:"activation"`
}

// activationError é a ativação recusada, com o status HTTP.
type activationError struct {
	status  int
	message string
}

func (e activationError) Error() string { return e.message }

// isActivated: o rastreador já conectou alguma vez.
func isActivated(d *devices.Device) bool {
	return d.LastSeenAt != nil || d.Status == devices.StatusOnline || d.Status == devices.StatusStale
}

// writeActivationError traduz os erros da ativação.
func writeActivationError(w http.ResponseWriter, err error, notFound string) {
	var refused activationError
	var sms smssetup.ValidationError
	var dev devices.ValidationError
	switch {
	case errors.As(err, &refused):
		writeError(w, refused.status, refused.message)
	case errors.As(err, &sms):
		writeError(w, http.StatusBadRequest, sms.Message)
	case errors.As(err, &dev):
		writeError(w, http.StatusBadRequest, dev.Message)
	default:
		writeAppointmentError(w, err, notFound)
	}
}

// activationOf monta a resposta da ativação do link.
func (s *Server) activationOf(r *http.Request, a appointments.Activation) (activationResponse, error) {
	out := activationResponse{Enabled: s.SMSSetup != nil && s.SMSSetup.Enabled(), Left: a.Left()}
	if a.SessionID == nil || s.SMSSetup == nil {
		return out, nil
	}
	session, err := s.SMSSetup.Session(r.Context(), *a.SessionID)
	if errors.Is(err, database.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	view := &activationView{Status: session.Status, Error: session.Error, Steps: make([]activationStep, len(session.Steps)),
		WaitingSince: session.WaitingSince, ConnectedAt: session.ConnectedAt, FinishedAt: session.FinishedAt}
	for i, st := range session.Steps {
		view.Steps[i] = activationStep{Label: st.Label, Status: st.Status}
	}
	if dev, err := s.Devices.Get(r.Context(), session.DeviceID); err == nil {
		view.IMEI, view.ICCID = dev.IMEI, dev.ICCID
	}
	out.Activation = view
	return out, nil
}

// startActivation põe o chip no rastreador e manda a configuração por SMS.
// current é a sessão da ativação anterior do link: ainda em andamento, a do
// mesmo rastreador e do mesmo chip continua (recusa começar outra); a de
// outro rastreador ou chip é interrompida. reserve conta a ativação (e
// reserva o rastreador) antes dos SMS; release a devolve se eles nem saíram;
// record guarda a sessão no link.
// ownChipOnly: o representante ativa com o chip que veio no rastreador do kit.
func (s *Server) startActivation(r *http.Request, dev *devices.Device, chipID uuid.UUID, act appointments.Activation,
	ownChipOnly bool, reserve, release func() error, record func(uuid.UUID) error, meta map[string]any) error {
	ctx := r.Context()
	if s.SMSSetup == nil || !s.SMSSetup.Enabled() {
		return activationError{http.StatusServiceUnavailable, "A ativação por SMS não está disponível agora: fale com a Farbo."}
	}
	// No teto, nem mexe na ativação que está andando.
	if act.Left() == 0 {
		return appointments.ErrTooManyActivations
	}
	current := act.SessionID
	if isActivated(dev) {
		return activationError{http.StatusConflict, "Este rastreador já está ativado: instale e teste o sinal."}
	}
	chip, err := s.Devices.Chip(ctx, chipID)
	if errors.Is(err, database.ErrNotFound) {
		return activationError{http.StatusBadRequest, "Este chip não está no estoque da central. Confira o ICCID."}
	}
	if err != nil {
		return err
	}
	if chip.Device != nil && chip.Device.ID != dev.ID {
		return activationError{http.StatusConflict, fmt.Sprintf("Este chip já está no rastreador %s. Escolha outro.", chip.Device.IMEI)}
	}
	if ownChipOnly && chip.Device == nil {
		return activationError{http.StatusBadRequest, "Use o chip que veio no rastreador do kit (se ele não tiver, fale com a Farbo)."}
	}
	if current != nil {
		previous, err := s.SMSSetup.Session(ctx, *current)
		if err != nil && !errors.Is(err, database.ErrNotFound) {
			return err
		}
		if previous != nil && (previous.Status == smssetup.StatusSending || previous.Status == smssetup.StatusWaiting) {
			if previous.DeviceID == dev.ID && dev.ICCID == chip.ICCID {
				return activationError{http.StatusConflict, "A ativação deste rastreador já está em andamento: espere ele conectar."}
			}
			if _, err := s.SMSSetup.Cancel(ctx, previous.ID); err != nil {
				return err
			}
		}
	}
	if err := reserve(); err != nil {
		return err
	}
	if dev.ICCID != chip.ICCID {
		if _, err := s.Devices.AttachChip(ctx, dev.ID, chip.ID); err != nil {
			return err
		}
	}
	meta["device"], meta["imei"], meta["iccid"], meta["origin"] = dev.ID, dev.IMEI, chip.ICCID, "installer-link"
	session, err := s.SMSSetup.Start(ctx, dev.ID, nil, smssetup.Options{}, nil)
	if err != nil {
		meta["result"], meta["error"] = "erro", err.Error()
		s.recordAudit(r, audit.ActionSMSSetup, nil, &dev.ID, meta)
		// Recusada antes do primeiro SMS (o cadastro do rastreador ou do chip
		// precisa de ajuste, a SMS Market recusou): a tentativa não conta.
		if releaseErr := release(); releaseErr != nil {
			s.Log.Warn("ativação pelo link recusada e não devolvida", "device", dev.ID, "err", releaseErr)
		}
		var refused smssetup.ValidationError
		if errors.As(err, &refused) {
			return activationError{http.StatusBadRequest, "A ativação não saiu: " + refused.Message +
				" Fale com a Farbo para ajustar o cadastro e tente de novo."}
		}
		return err
	}
	meta["session"], meta["steps"], meta["status"] = session.ID, len(session.Steps), session.Status
	s.recordAudit(r, audit.ActionSMSSetup, nil, &dev.ID, meta)
	return record(session.ID)
}

// installForActivation é o agendamento do link que pode ativar: de
// instalação, aberto e com o veículo ainda sem rastreador.
func (s *Server) installForActivation(w http.ResponseWriter, r *http.Request) (*appointments.Appointment, bool) {
	a, ok := s.installFor(w, r)
	if !ok {
		return nil, false
	}
	switch {
	case a.Kind != appointments.KindInstall:
		writeError(w, http.StatusConflict, "na desinstalação não há ativação")
		return nil, false
	case !a.Open():
		writeError(w, http.StatusConflict, "a instalação já foi concluída")
		return nil, false
	case a.VehicleIMEI != "":
		writeError(w, http.StatusConflict, fmt.Sprintf("Este veículo já tem o rastreador %s, configurado pela Farbo: instale e teste o sinal.",
			a.VehicleIMEI))
		return nil, false
	}
	return a, true
}

// handlePublicInstallStock: os rastreadores livres (ativados ou não) e os
// chips livres, para o instalador escolher.
func (s *Server) handlePublicInstallStock(w http.ResponseWriter, r *http.Request) {
	a, ok := s.installForActivation(w, r)
	if !ok {
		return
	}
	devs, chips, err := s.Appointments.InstallStock(r.Context(), a.ID)
	if !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devs, "chips": chips})
}

func (s *Server) handlePublicInstallActivation(w http.ResponseWriter, r *http.Request) {
	a, ok := s.installFor(w, r)
	if !ok {
		return
	}
	act, err := s.Appointments.ActivationOf(r.Context(), a.ID)
	if !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	out, err := s.activationOf(r, act)
	if !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePublicInstallActivate: o rastreador escolhido (livre e nunca
// conectado) ganha o chip escolhido e a configuração por SMS.
func (s *Server) handlePublicInstallActivate(w http.ResponseWriter, r *http.Request) {
	a, ok := s.installForActivation(w, r)
	if !ok {
		return
	}
	var req struct {
		DeviceID uuid.UUID `json:"deviceId"`
		ChipID   uuid.UUID `json:"chipId"`
	}
	if err := decodeJSON(w, r, &req); err != nil || req.DeviceID == uuid.Nil || req.ChipID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "escolha o rastreador e o chip")
		return
	}
	ctx := r.Context()
	dev, err := s.Devices.Get(ctx, req.DeviceID)
	if errors.Is(err, database.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "este rastreador não está cadastrado na central")
		return
	}
	if !writeAppointmentError(w, err, "rastreador não encontrado") {
		return
	}
	if err := s.Appointments.DeviceFree(ctx, a.ID, dev.ID); err != nil {
		writeActivationError(w, err, "rastreador não encontrado")
		return
	}
	act, err := s.Appointments.ActivationOf(ctx, a.ID)
	if !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	err = s.startActivation(r, dev, req.ChipID, act, a.AffiliateID != nil,
		func() error { return s.Appointments.ReserveActivation(ctx, a.ID, dev.ID) },
		func() error { return s.Appointments.ReleaseActivation(ctx, a.ID) },
		func(session uuid.UUID) error { return s.Appointments.RecordActivation(ctx, a.ID, session) },
		map[string]any{"appointmentId": a.ID, "installer": a.InstallerName})
	if err != nil {
		writeActivationError(w, err, "rastreador não encontrado")
		return
	}
	if act, err = s.Appointments.ActivationOf(ctx, a.ID); !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	out, err := s.activationOf(r, act)
	if !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// standaloneForActivation é a instalação avulsa do link que pode ativar:
// aberta e com o rastreador fora de veículo.
func (s *Server) standaloneForActivation(w http.ResponseWriter, r *http.Request) (*appointments.Standalone, bool) {
	l, ok := s.standaloneFor(w, r)
	if !ok {
		return nil, false
	}
	switch {
	case l.Status != appointments.StandaloneOpen:
		writeError(w, http.StatusConflict, "a instalação já foi encerrada")
		return nil, false
	case l.VehicleName != "":
		writeError(w, http.StatusConflict, "este rastreador já está num veículo: instale e teste o sinal")
		return nil, false
	}
	return l, true
}

// handlePublicStandaloneStock: os chips livres (o rastreador é o do link),
// com o que já está nele primeiro.
func (s *Server) handlePublicStandaloneStock(w http.ResponseWriter, r *http.Request) {
	l, ok := s.standaloneForActivation(w, r)
	if !ok {
		return
	}
	chips, err := s.Appointments.FreeChips(r.Context())
	if !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	if dev, err := s.Devices.Get(r.Context(), l.DeviceID); err == nil && dev.ICCID != "" {
		if own, err := s.Appointments.ChipByICCID(r.Context(), dev.ICCID); err == nil && own != nil {
			chips = append([]appointments.StockChip{*own}, chips...)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"chips": chips})
}

func (s *Server) handlePublicStandaloneActivation(w http.ResponseWriter, r *http.Request) {
	l, ok := s.standaloneFor(w, r)
	if !ok {
		return
	}
	act, err := s.Appointments.StandaloneActivationOf(r.Context(), l.ID)
	if !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	out, err := s.activationOf(r, act)
	if !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePublicStandaloneActivate: o rastreador do link (fora de veículo e
// nunca conectado) ganha o chip escolhido e a configuração por SMS.
func (s *Server) handlePublicStandaloneActivate(w http.ResponseWriter, r *http.Request) {
	l, ok := s.standaloneForActivation(w, r)
	if !ok {
		return
	}
	var req struct {
		ChipID uuid.UUID `json:"chipId"`
	}
	if err := decodeJSON(w, r, &req); err != nil || req.ChipID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "escolha o chip")
		return
	}
	ctx := r.Context()
	dev, err := s.Devices.Get(ctx, l.DeviceID)
	if !writeAppointmentError(w, err, "rastreador não encontrado") {
		return
	}
	act, err := s.Appointments.StandaloneActivationOf(ctx, l.ID)
	if !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	err = s.startActivation(r, dev, req.ChipID, act, false,
		func() error { return s.Appointments.ReserveStandaloneActivation(ctx, l.ID) },
		func() error { return s.Appointments.ReleaseStandaloneActivation(ctx, l.ID) },
		func(session uuid.UUID) error { return s.Appointments.RecordStandaloneActivation(ctx, l.ID, session) },
		map[string]any{"standaloneId": l.ID, "installer": l.InstallerName})
	if err != nil {
		writeActivationError(w, err, "rastreador não encontrado")
		return
	}
	if act, err = s.Appointments.StandaloneActivationOf(ctx, l.ID); !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	out, err := s.activationOf(r, act)
	if !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/appointments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
)

// A instalação avulsa: a central gera o link do rastreador escolhido
// (/installer-links) e encerra quando quiser; o instalador, pelo link
// (/public/installer-links/{token}), testa o sinal e conclui, sem login.

func (s *Server) handleStandaloneInstalls(w http.ResponseWriter, r *http.Request) {
	list, err := s.Appointments.Standalones(r.Context())
	if !writeAppointmentError(w, err, "links não encontrados") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateStandaloneInstall(w http.ResponseWriter, r *http.Request) {
	var in appointments.StandaloneInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	l, err := s.Appointments.CreateStandalone(r.Context(), personOf(r), in)
	if !writeAppointmentError(w, err, "rastreador não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionInstallationChanged, nil, &l.DeviceID, map[string]any{
		"standaloneId": l.ID, "change": "standalone link created", "imei": l.IMEI, "installer": l.InstallerName,
	})
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) handleCloseStandaloneInstall(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	l, err := s.Appointments.CloseStandalone(r.Context(), id, personOf(r))
	if !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionInstallationChanged, nil, &l.DeviceID, map[string]any{
		"standaloneId": l.ID, "change": "standalone link closed", "imei": l.IMEI,
	})
	writeJSON(w, http.StatusOK, l)
}

// standaloneView é o que o instalador vê: o rastreador (e o veículo, se já
// cadastrado), as observações da central e a situação.
type standaloneView struct {
	IMEI          string     `json:"imei"`
	DeviceModel   string     `json:"deviceModel"`
	VehicleName   string     `json:"vehicleName"`
	Plate         string     `json:"plate"`
	InstallerName string     `json:"installerName"`
	Notes         string     `json:"notes"`
	Status        string     `json:"status"`
	CompletedAt   *time.Time `json:"completedAt"`
}

func toStandaloneView(l *appointments.Standalone) standaloneView {
	return standaloneView{
		IMEI: l.IMEI, DeviceModel: l.DeviceModel, VehicleName: l.VehicleName, Plate: l.Plate,
		InstallerName: l.InstallerName, Notes: l.Notes, Status: l.Status, CompletedAt: l.CompletedAt,
	}
}

// standaloneFor é a instalação avulsa do link (o 410 já escrito se não vale).
func (s *Server) standaloneFor(w http.ResponseWriter, r *http.Request) (*appointments.Standalone, bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	l, err := s.Appointments.StandaloneByToken(r.Context(), chi.URLParam(r, "token"))
	if !writeAppointmentError(w, err, "link não encontrado") {
		return nil, false
	}
	return l, true
}

func (s *Server) handlePublicStandalone(w http.ResponseWriter, r *http.Request) {
	if l, ok := s.standaloneFor(w, r); ok {
		writeJSON(w, http.StatusOK, toStandaloneView(l))
	}
}

// standaloneSignal é o teste do sinal do rastreador do link, guardado para a
// central ver.
func (s *Server) standaloneSignal(r *http.Request, l *appointments.Standalone) (installCheck, error) {
	dev, err := s.Devices.Get(r.Context(), l.DeviceID)
	if err != nil {
		return installCheck{}, err
	}
	check := s.signalOf(r, dev)
	return check, s.Appointments.RecordStandaloneCheck(r.Context(), l.ID, check.Ready)
}

func (s *Server) handlePublicStandaloneCheck(w http.ResponseWriter, r *http.Request) {
	l, ok := s.standaloneFor(w, r)
	if !ok {
		return
	}
	check, err := s.standaloneSignal(r, l)
	if !writeAppointmentError(w, err, "rastreador não encontrado") {
		return
	}
	writeJSON(w, http.StatusOK, check)
}

// handlePublicStandaloneComplete conclui com o sinal testado de novo na hora;
// o link expira.
func (s *Server) handlePublicStandaloneComplete(w http.ResponseWriter, r *http.Request) {
	l, ok := s.standaloneFor(w, r)
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	check, err := s.standaloneSignal(r, l)
	if !writeAppointmentError(w, err, "rastreador não encontrado") {
		return
	}
	if !check.Ready {
		writeError(w, http.StatusConflict, check.Message)
		return
	}
	done, err := s.Appointments.CompleteStandalone(r.Context(), l.ID, req.Note)
	if !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionInstallationChanged, nil, &done.DeviceID, map[string]any{
		"standaloneId": done.ID, "change": "standalone installed", "imei": done.IMEI, "installer": done.InstallerName,
	})
	writeJSON(w, http.StatusOK, toStandaloneView(done))
}

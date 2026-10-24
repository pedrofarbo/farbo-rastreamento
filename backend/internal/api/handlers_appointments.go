package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/appointments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
)

// A agenda de instalação e de desinstalação: a central agenda e acompanha
// (/installations); o cliente e a associação veem as deles, e a associação
// pede a desinstalação; o instalador segue pelo link (/public/install/{token}),
// sem login.

func writeAppointmentError(w http.ResponseWriter, err error, notFound string) bool {
	var input *appointments.InputError
	switch {
	case err == nil:
		return true
	case errors.As(err, &input):
		writeError(w, http.StatusBadRequest, input.Message)
	case errors.Is(err, appointments.ErrLinkEnded):
		writeError(w, http.StatusGone, err.Error())
	default:
		handleStoreError(w, err, notFound)
	}
	return false
}

// weekOf lê ?de= e ?ate= (AAAA-MM-DD, no fuso da central); sem eles, de hoje
// até 7 dias.
func (s *Server) weekOf(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, bool) {
	loc := time.FixedZone("BRT", -3*60*60)
	if s.Reports != nil {
		loc = s.Reports.Location()
	}
	now := time.Now().In(loc)
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, 7)
	if raw := r.URL.Query().Get("de"); raw != "" {
		d, err := time.ParseInLocation(time.DateOnly, raw, loc)
		if err != nil {
			writeError(w, http.StatusBadRequest, "data inicial inválida")
			return time.Time{}, time.Time{}, false
		}
		from = d
	}
	if raw := r.URL.Query().Get("ate"); raw != "" {
		d, err := time.ParseInLocation(time.DateOnly, raw, loc)
		if err != nil {
			writeError(w, http.StatusBadRequest, "data final inválida")
			return time.Time{}, time.Time{}, false
		}
		to = d.AddDate(0, 0, 1)
	}
	if !to.After(from) || to.Sub(from) > 92*24*time.Hour {
		writeError(w, http.StatusBadRequest, "o período vai até 3 meses")
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

func (s *Server) handleInstallations(w http.ResponseWriter, r *http.Request) {
	from, to, ok := s.weekOf(w, r)
	if !ok {
		return
	}
	list, err := s.Appointments.Between(r.Context(), from, to)
	if !writeAppointmentError(w, err, "agenda não encontrada") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAwaitingInstall(w http.ResponseWriter, r *http.Request) {
	list, err := s.Appointments.AwaitingInstall(r.Context())
	if !writeAppointmentError(w, err, "veículos não encontrados") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAwaitingUninstall(w http.ResponseWriter, r *http.Request) {
	list, err := s.Appointments.AwaitingUninstall(r.Context())
	if !writeAppointmentError(w, err, "veículos não encontrados") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// orgVehicleFromURL: o veículo de associado da organização (404 se não for).
func (s *Server) orgVehicleFromURL(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	orgID, _ := orgOf(r)
	id, err := urlUUID(r, "vehicleId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, uuid.Nil, false
	}
	v, err := s.Vehicles.Get(r.Context(), id)
	if err != nil {
		handleStoreError(w, err, "veículo não encontrado")
		return uuid.Nil, uuid.Nil, false
	}
	if !seesVehicle(r, v) {
		writeError(w, http.StatusNotFound, "veículo não encontrado")
		return uuid.Nil, uuid.Nil, false
	}
	return orgID, id, true
}

// handleOrgRequestUninstall: a associação pede a retirada do rastreador.
func (s *Server) handleOrgRequestUninstall(w http.ResponseWriter, r *http.Request) {
	orgID, vehicleID, ok := s.orgVehicleFromURL(w, r)
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
	if !writeAppointmentError(w, s.Appointments.RequestUninstall(r.Context(), orgID, vehicleID, req.Note), "veículo não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionInstallationChanged, &vehicleID, nil, map[string]any{"change": "uninstall requested", "note": req.Note})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOrgCancelUninstall(w http.ResponseWriter, r *http.Request) {
	orgID, vehicleID, ok := s.orgVehicleFromURL(w, r)
	if !ok {
		return
	}
	if !writeAppointmentError(w, s.Appointments.CancelUninstallRequest(r.Context(), orgID, vehicleID), "veículo não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionInstallationChanged, &vehicleID, nil, map[string]any{"change": "uninstall request canceled"})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateInstallation(w http.ResponseWriter, r *http.Request) {
	var in appointments.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	a, err := s.Appointments.Create(r.Context(), personOf(r), in)
	if !writeAppointmentError(w, err, "veículo não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionInstallationChanged, &a.VehicleID, nil, map[string]any{
		"appointmentId": a.ID, "kind": a.Kind, "change": "scheduled", "scheduledFor": a.ScheduledFor, "installer": a.InstallerName,
	})
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleRescheduleInstallation(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var in appointments.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	a, err := s.Appointments.Reschedule(r.Context(), id, in)
	if !writeAppointmentError(w, err, "instalação não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionInstallationChanged, &a.VehicleID, nil, map[string]any{
		"appointmentId": a.ID, "change": "rescheduled", "scheduledFor": a.ScheduledFor,
	})
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleCloseInstallation(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	a, err := s.Appointments.Close(r.Context(), id, req.Status)
	if !writeAppointmentError(w, err, "instalação não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionInstallationChanged, &a.VehicleID, nil, map[string]any{"appointmentId": a.ID, "change": a.Status})
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleInstallationPhoto(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	s.writeInstallationPhoto(w, r, id)
}

func (s *Server) writeInstallationPhoto(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	slot := chi.URLParam(r, "slot")
	contentType, data, err := s.Appointments.Photo(r.Context(), id, slot)
	if !writeAppointmentError(w, err, "foto não encontrada") {
		return
	}
	writeAttachment(w, "instalacao-"+slot+".jpg", contentType, data)
}

func (s *Server) handleMyInstallations(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	list, err := s.Appointments.ForCustomer(r.Context(), customerID)
	if !writeAppointmentError(w, err, "instalações não encontradas") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleOrgInstallations(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	from, to, ok := s.weekOf(w, r)
	if !ok {
		return
	}
	list, err := s.Appointments.ForOrganization(r.Context(), orgID, from, to)
	if !writeAppointmentError(w, err, "instalações não encontradas") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// ---------------------------------------------------------------------------
// O link do instalador
// ---------------------------------------------------------------------------

// installView é o que o instalador vê: o veículo, onde e quando, o primeiro
// nome e o telefone do cliente (para combinar) — nada de financeiro. Na
// desinstalação, o IMEI é o do rastreador a retirar.
type installView struct {
	Kind           string     `json:"kind"`
	VehicleName    string     `json:"vehicleName"`
	Plate          string     `json:"plate"`
	VehicleDetails string     `json:"vehicleDetails"`
	CustomerName   string     `json:"customerName"`
	CustomerPhone  string     `json:"customerPhone"`
	InstallerName  string     `json:"installerName"`
	ScheduledFor   time.Time  `json:"scheduledFor"`
	Address        string     `json:"address"`
	Notes          string     `json:"notes"`
	Status         string     `json:"status"`
	IMEI           string     `json:"imei"`
	CompletedAt    *time.Time `json:"completedAt"`
	Photos         []string   `json:"photos"`
}

// installFor é o agendamento do link (o 410 já escrito se não vale).
func (s *Server) installFor(w http.ResponseWriter, r *http.Request) (*appointments.Appointment, bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	a, err := s.Appointments.ByToken(r.Context(), chi.URLParam(r, "token"))
	if !writeAppointmentError(w, err, "link não encontrado") {
		return nil, false
	}
	return a, true
}

func toInstallView(a *appointments.Appointment) installView {
	first, _, _ := strings.Cut(strings.TrimSpace(a.CustomerName), " ")
	return installView{
		Kind: a.Kind, VehicleName: a.VehicleName, Plate: a.Plate, VehicleDetails: a.VehicleDetails, CustomerName: first,
		CustomerPhone: a.CustomerPhone, InstallerName: a.InstallerName, ScheduledFor: a.ScheduledFor, Address: a.Address,
		Notes: a.Notes, Status: a.Status, IMEI: a.IMEI, CompletedAt: a.CompletedAt, Photos: a.Photos,
	}
}

func (s *Server) handlePublicInstall(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.installFor(w, r); ok {
		writeJSON(w, http.StatusOK, toInstallView(a))
	}
}

func (s *Server) handlePublicInstallArrive(w http.ResponseWriter, r *http.Request) {
	a, ok := s.installFor(w, r)
	if !ok {
		return
	}
	if !a.Open() {
		writeError(w, http.StatusConflict, "a instalação já foi concluída")
		return
	}
	if !writeAppointmentError(w, s.Appointments.Arrive(r.Context(), a.ID), "link não encontrado") {
		return
	}
	a, _ = s.Appointments.Get(r.Context(), a.ID)
	writeJSON(w, http.StatusOK, toInstallView(a))
}

// installCheck é o teste do rastreador pelo IMEI: cadastrado, livre,
// conectado e com posição recente.
type installCheck struct {
	Found        bool       `json:"found"`
	OtherVehicle bool       `json:"otherVehicle"`
	Online       bool       `json:"online"`
	LastPosition *time.Time `json:"lastPosition"`
	// Ready: dá para concluir (cadastrado, livre e mandando posição há pouco).
	Ready   bool   `json:"ready"`
	Message string `json:"message"`
}

// maxInstallPositionAge: a posição do teste precisa ser de agora.
const maxInstallPositionAge = 10 * time.Minute

func (s *Server) checkInstallDevice(r *http.Request, a *appointments.Appointment, imei string) (*devices.Device, installCheck) {
	imei = strings.Map(func(c rune) rune {
		if c >= '0' && c <= '9' {
			return c
		}
		return -1
	}, imei)
	var out installCheck
	dev, err := s.Devices.GetByIMEI(r.Context(), imei)
	if err != nil || dev == nil {
		out.Message = "Este IMEI não está cadastrado na central. Confira os números na etiqueta do rastreador."
		return nil, out
	}
	out.Found = true
	if other, err := s.Vehicles.GetByDeviceID(r.Context(), dev.ID); err == nil && other != nil && other.ID != a.VehicleID {
		out.OtherVehicle = true
		out.Message = "Este rastreador já está ligado a outro veículo. Confira o IMEI."
		return dev, out
	}
	if s.Conns != nil {
		_, out.Online = s.Conns.Get(dev.IMEI)
	}
	if p, err := s.Positions.Latest(r.Context(), dev.ID); err == nil && p != nil {
		at := p.ReceivedAt
		out.LastPosition = &at
	}
	recent := out.LastPosition != nil && time.Since(*out.LastPosition) <= maxInstallPositionAge
	out.Ready = out.Online || recent
	switch {
	case out.Ready:
		out.Message = "Rastreador conectado e mandando a posição. Pode concluir."
	default:
		out.Message = "O rastreador ainda não conectou. Confira a alimentação e o chip, espere 2 minutos e teste de novo."
	}
	return dev, out
}

func (s *Server) handlePublicInstallCheck(w http.ResponseWriter, r *http.Request) {
	a, ok := s.installFor(w, r)
	if !ok {
		return
	}
	if a.Kind != appointments.KindInstall {
		writeError(w, http.StatusConflict, "na desinstalação não há teste do sinal")
		return
	}
	var req struct {
		IMEI string `json:"imei"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	_, check := s.checkInstallDevice(r, a, req.IMEI)
	writeJSON(w, http.StatusOK, check)
}

func (s *Server) handlePublicInstallPhoto(w http.ResponseWriter, r *http.Request) {
	defer cleanupForm(r)
	a, ok := s.installFor(w, r)
	if !ok {
		return
	}
	if !a.Open() {
		writeError(w, http.StatusConflict, "a instalação já foi concluída")
		return
	}
	data, _, ok := readUpload(w, r, appointments.MaxPhotoBytes)
	if !ok {
		return
	}
	if !writeAppointmentError(w, s.Appointments.SavePhoto(r.Context(), a.ID, chi.URLParam(r, "slot"), data), "link não encontrado") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePublicInstallPhotoFile(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.installFor(w, r); ok {
		s.writeInstallationPhoto(w, r, a.ID)
	}
}

// handlePublicInstallComplete: na instalação, conclui com o rastreador
// conferido (o teste precisa passar) e liga o rastreador ao veículo; na
// desinstalação, tira o rastreador do veículo.
func (s *Server) handlePublicInstallComplete(w http.ResponseWriter, r *http.Request) {
	a, ok := s.installFor(w, r)
	if !ok {
		return
	}
	var req struct {
		IMEI string `json:"imei"`
		Note string `json:"note"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if a.Kind == appointments.KindUninstall {
		done, err := s.Appointments.CompleteUninstall(r.Context(), a.ID, req.Note)
		if !writeAppointmentError(w, err, "link não encontrado") {
			return
		}
		s.vehiclesChanged(r)
		s.recordAudit(r, audit.ActionInstallationChanged, &done.VehicleID, done.DeviceID, map[string]any{
			"appointmentId": done.ID, "change": "uninstalled", "imei": done.IMEI, "installer": done.InstallerName,
		})
		writeJSON(w, http.StatusOK, toInstallView(done))
		return
	}
	dev, check := s.checkInstallDevice(r, a, req.IMEI)
	if dev == nil || !check.Ready || check.OtherVehicle {
		writeError(w, http.StatusConflict, check.Message)
		return
	}
	done, err := s.Appointments.Complete(r.Context(), a.ID, dev.ID, req.Note)
	if !writeAppointmentError(w, err, "link não encontrado") {
		return
	}
	s.vehiclesChanged(r)
	s.recordAudit(r, audit.ActionInstallationChanged, &done.VehicleID, &dev.ID, map[string]any{
		"appointmentId": done.ID, "change": "installed", "imei": dev.IMEI, "installer": done.InstallerName,
	})
	writeJSON(w, http.StatusOK, toInstallView(done))
}

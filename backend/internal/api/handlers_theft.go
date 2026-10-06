package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/shares"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/theft"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// theftView é o modo roubo na tela do veículo.
type theftView struct {
	// Mode: o modo ligado; nulo, desligado.
	Mode *theft.Mode `json:"mode"`
	// PublicURL: o link da posição ao vivo, sem login (para a polícia).
	PublicURL string `json:"publicUrl"`
	// CanActivate: o dono, a equipe e quem pode bloquear o motor.
	CanActivate bool `json:"canActivate"`
	// CanEnd: só o dono (e a equipe) desliga.
	CanEnd bool `json:"canEnd"`
	// O que a tela explica: a posição parado e o prazo.
	ParkedSeconds int `json:"parkedSeconds"`
	DurationHours int `json:"durationHours"`
}

func (s *Server) theftView(vehicle *vehicles.Vehicle, share *shares.Share, m *theft.Mode) theftView {
	parked, duration := s.Theft.Settings()
	v := theftView{
		Mode:          m,
		CanActivate:   vehicle.DeviceID != nil && (share == nil || share.CanBlock),
		CanEnd:        share == nil,
		ParkedSeconds: parked,
		DurationHours: int(duration / time.Hour),
	}
	if m != nil {
		v.PublicURL = s.Theft.PublicURL(m.ID)
	}
	return v
}

func (s *Server) theftAvailable(w http.ResponseWriter) bool {
	if s.Theft == nil {
		writeError(w, http.StatusNotFound, "modo roubo indisponível")
		return false
	}
	return true
}

// handleTheftStatus: o modo roubo do veículo (dono, quem tem acesso e equipe).
func (s *Server) handleTheftStatus(w http.ResponseWriter, r *http.Request) {
	if !s.theftAvailable(w) {
		return
	}
	vehicle, _, share, ok := s.vehicleForViewer(w, r, false)
	if !ok {
		return
	}
	m, err := s.Theft.Active(r.Context(), vehicle.ID)
	if err != nil {
		handleStoreError(w, err, "veículo não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, s.theftView(vehicle, share, m))
}

// handleTheftActivate liga o modo roubo. Sem confirmação extra: na hora do
// roubo, cada segundo conta (e ligar não expõe nada a quem já tem acesso).
// Quem recebeu acesso só liga se o dono deixou bloquear o motor.
func (s *Server) handleTheftActivate(w http.ResponseWriter, r *http.Request) {
	if !s.theftAvailable(w) {
		return
	}
	vehicle, device, share, ok := s.vehicleForViewer(w, r, true)
	if !ok || !canBlock(w, share) {
		return
	}
	principal, _ := auth.FromContext(r.Context())
	actor := principal.UserID
	m, created, err := s.Theft.Activate(r.Context(), vehicle.ID, &actor)
	if err != nil {
		writeTheftError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		meta := map[string]any{"modeId": m.ID, "boost": m.BoostStatus}
		if share != nil {
			meta["shareId"], meta["ownerId"] = share.ID, share.OwnerID
		}
		s.recordAudit(r, audit.ActionTheftActivated, &vehicle.ID, &device.ID, meta)
	}
	writeJSON(w, status, s.theftView(vehicle, share, m))
}

type theftEndRequest struct {
	// Outcome: RECOVERED (recuperado) ou CANCELLED (desativado).
	Outcome string `json:"outcome"`
}

// handleTheftEnd desliga o modo roubo: só o dono (e a equipe), com a
// biometria ou a senha — quem está com o celular roubado não para o
// rastreamento.
func (s *Server) handleTheftEnd(w http.ResponseWriter, r *http.Request) {
	if !s.theftAvailable(w) {
		return
	}
	vehicle, _, ok := s.vehicleFromURL(w, r, false)
	if !ok {
		return
	}
	var req theftEndRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if req.Outcome != theft.OutcomeRecovered && req.Outcome != theft.OutcomeCancelled {
		writeError(w, http.StatusBadRequest, "diga se o veículo foi recuperado ou se é para desativar")
		return
	}
	// Antes da confirmação: sem modo ligado, não gasta o comprovante.
	if m, err := s.Theft.Active(r.Context(), vehicle.ID); err != nil || m == nil {
		if err != nil {
			handleStoreError(w, err, "veículo não encontrado")
		} else {
			writeError(w, http.StatusConflict, theft.ErrNotActive.Error())
		}
		return
	}
	if !s.requireStepUp(w, r, stepup.PurposeTheftEnd) {
		return
	}
	principal, _ := auth.FromContext(r.Context())
	actor := principal.UserID
	m, err := s.Theft.End(r.Context(), vehicle.ID, &actor, req.Outcome)
	if err != nil {
		writeTheftError(w, err)
		return
	}
	s.recordAudit(r, audit.ActionTheftEnded, &vehicle.ID, vehicle.DeviceID,
		map[string]any{"modeId": m.ID, "outcome": m.Outcome, "restore": m.RestoreStatus})
	writeJSON(w, http.StatusOK, s.theftView(vehicle, nil, nil))
}

func writeTheftError(w http.ResponseWriter, err error) {
	var rule theft.RuleError
	switch {
	case errors.As(err, &rule):
		writeError(w, http.StatusBadRequest, rule.Message)
	case errors.Is(err, theft.ErrNotActive):
		writeError(w, http.StatusConflict, err.Error())
	default:
		handleStoreError(w, err, "veículo não encontrado")
	}
}

// publicTheftView é o que o link público mostra: o veículo e onde ele está
// — nada do dono.
type publicTheftView struct {
	Vehicle struct {
		Name  string `json:"name"`
		Plate string `json:"plate"`
		Brand string `json:"brand"`
		Model string `json:"model"`
		Year  *int   `json:"year"`
		Color string `json:"color"`
	} `json:"vehicle"`
	ActivatedAt time.Time       `json:"activatedAt"`
	ExpiresAt   time.Time       `json:"expiresAt"`
	Position    *publicPosition `json:"position"`
	Address     string          `json:"address"`
	Ignition    *bool           `json:"ignition"`
	// Online: o rastreador está conectado agora.
	Online bool `json:"online"`
}

type publicPosition struct {
	Latitude     float64   `json:"latitude"`
	Longitude    float64   `json:"longitude"`
	SpeedKmh     float64   `json:"speedKmh"`
	Heading      *float64  `json:"heading"`
	GPSTimestamp time.Time `json:"gpsTimestamp"`
	ReceivedAt   time.Time `json:"receivedAt"`
}

// handlePublicTheft: a posição ao vivo do veículo roubado, pelo link (sem
// login), enquanto o modo roubo estiver ligado.
func (s *Server) handlePublicTheft(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	m, err := s.Theft.ByToken(r.Context(), chi.URLParam(r, "token"))
	if errors.Is(err, theft.ErrLinkEnded) {
		writeError(w, http.StatusGone, "este link de localização não está mais ativo: o modo roubo foi encerrado")
		return
	}
	if err != nil {
		handleStoreError(w, err, "link não encontrado")
		return
	}
	vehicle, err := s.Vehicles.Get(r.Context(), m.VehicleID)
	if err != nil {
		handleStoreError(w, err, "link não encontrado")
		return
	}
	var view publicTheftView
	view.Vehicle.Name, view.Vehicle.Plate, view.Vehicle.Brand = vehicle.Name, vehicle.Plate, vehicle.Brand
	view.Vehicle.Model, view.Vehicle.Year, view.Vehicle.Color = vehicle.Model, vehicle.Year, vehicle.Color
	view.ActivatedAt, view.ExpiresAt = m.ActivatedAt, m.ExpiresAt
	if vehicle.DeviceID != nil {
		if device, err := s.Devices.Get(r.Context(), *vehicle.DeviceID); err == nil && s.Conns != nil {
			_, view.Online = s.Conns.Get(device.IMEI)
		}
		if p, err := s.Positions.Latest(r.Context(), *vehicle.DeviceID); err == nil && p != nil {
			view.Position = &publicPosition{
				Latitude: p.Latitude, Longitude: p.Longitude, SpeedKmh: p.SpeedKmh, Heading: p.Heading,
				GPSTimestamp: p.GPSTimestamp, ReceivedAt: p.ReceivedAt,
			}
			view.Ignition = p.ACC
			if s.Geocoder != nil {
				if address, err := s.Geocoder.Reverse(r.Context(), p.Latitude, p.Longitude); err == nil {
					view.Address = address
				}
			}
		}
		if state := s.States.Get(*vehicle.DeviceID); state != nil && state.ACC != nil {
			view.Ignition = state.ACC
		}
	}
	writeJSON(w, http.StatusOK, view)
}

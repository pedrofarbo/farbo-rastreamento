package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/shares"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/theft"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// vehicleView é o que o painel consome: o veículo com o estado atual do seu
// rastreador já resolvido, para a lista não precisar de N requisições.
//
// O rastreador vai como devices.View, na representação do perfil de quem
// chama: nunca com senha, e só o admin vê a configuração do aparelho.
type vehicleView struct {
	*vehicles.Vehicle
	Device       *devices.View      `json:"device"`
	LastPosition *tracking.Position `json:"lastPosition"`
	State        *tracking.State    `json:"state"`
	Connected    bool               `json:"connected"`
	// HistoryDays é por quantos dias o histórico deste veículo é guardado
	// (a exceção dele, a do cliente ou o padrão da central).
	HistoryDays int `json:"historyDays"`
	// Shared: o veículo é de outro cliente, que deu acesso a quem chama
	// (posição ao vivo e, se liberado, o bloqueio). Nulo para o dono e para a
	// equipe.
	Shared *sharedAccess `json:"shared,omitempty"`
	// Theft: o modo roubo ligado (nulo: desligado).
	Theft *theft.Brief `json:"theft,omitempty"`
}

// customerOf devolve o id do cliente quando quem chama é um cliente final.
// Para a equipe da central devolve false, e nada é filtrado.
func customerOf(r *http.Request) (uuid.UUID, bool) {
	if principal, ok := auth.FromContext(r.Context()); ok && principal.IsCustomer() {
		return principal.UserID, true
	}
	return uuid.Nil, false
}

func (s *Server) handleListVehicles(w http.ResponseWriter, r *http.Request) {
	customerID, isCustomer := customerOf(r)

	var list []*vehicles.Vehicle
	var err error
	if isCustomer {
		list, err = s.Vehicles.ListByOwner(r.Context(), customerID)
	} else {
		list, err = s.Vehicles.List(r.Context())
	}
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	shared, err := s.sharedWith(r, customerID, isCustomer)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	for _, sv := range shared {
		list = append(list, sv.vehicle)
	}

	views, err := s.vehicleViews(r.Context(), list, deviceAudience(r))
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	for i := range views {
		if sv, ok := shared[views[i].ID]; ok {
			views[i].Shared = sharedAccessOf(sv.share)
			views[i].HistoryDays = 0
		}
	}
	writeJSON(w, http.StatusOK, views)
}

type sharedVehicle struct {
	vehicle *vehicles.Vehicle
	share   *shares.Share
}

// sharedWith: os veículos que outros clientes compartilharam com este (fora
// os de quem está suspenso).
func (s *Server) sharedWith(r *http.Request, customerID uuid.UUID, isCustomer bool) (map[uuid.UUID]sharedVehicle, error) {
	out := map[uuid.UUID]sharedVehicle{}
	if !isCustomer || s.Shares == nil {
		return out, nil
	}
	list, err := s.Shares.ForGuest(r.Context(), customerID)
	if err != nil {
		return nil, err
	}
	for _, share := range list {
		suspended, err := s.Billing.IsSuspended(r.Context(), share.OwnerID)
		if err != nil {
			return nil, err
		}
		if suspended {
			continue
		}
		vehicle, err := s.Vehicles.Get(r.Context(), share.VehicleID)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				continue
			}
			return nil, err
		}
		out[vehicle.ID] = sharedVehicle{vehicle: vehicle, share: share}
	}
	return out, nil
}

// vehicleViews junta a cada veículo o rastreador, a última posição e o
// estado. O rastreador sai na representação do perfil (audience).
//
// Só busca os rastreadores e as posições destes veículos: a lista de um
// cliente (pedida de novo a cada minuto por tela aberta) não pode custar o
// tamanho da frota nem o do histórico.
func (s *Server) vehicleViews(ctx context.Context, list []*vehicles.Vehicle, audience devices.Audience) ([]vehicleView, error) {
	deviceIDs := make([]uuid.UUID, 0, len(list))
	for _, vehicle := range list {
		if vehicle.DeviceID != nil {
			deviceIDs = append(deviceIDs, *vehicle.DeviceID)
		}
	}
	listed, err := s.Devices.ListByIDs(ctx, deviceIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]*devices.Device, len(listed))
	for _, d := range listed {
		byID[d.ID] = d
	}

	positions, err := s.Positions.LatestFor(ctx, deviceIDs)
	if err != nil {
		return nil, err
	}
	customerDays, err := s.Retention.CustomersDays(ctx)
	if err != nil {
		return nil, err
	}
	stolen := map[uuid.UUID]theft.Brief{}
	if s.Theft != nil {
		ids := make([]uuid.UUID, 0, len(list))
		for _, vehicle := range list {
			ids = append(ids, vehicle.ID)
		}
		if stolen, err = s.Theft.ActiveBriefs(ctx, ids); err != nil {
			return nil, err
		}
	}

	views := make([]vehicleView, 0, len(list))
	for _, vehicle := range list {
		view := vehicleView{Vehicle: vehicle}
		if b, ok := stolen[vehicle.ID]; ok {
			view.Theft = &b
		}
		var ownerDays *int
		if vehicle.OwnerID != nil {
			if d, ok := customerDays[*vehicle.OwnerID]; ok {
				ownerDays = &d
			}
		}
		view.HistoryDays = s.Retention.Effective(vehicle.HistoryRetentionDays, ownerDays)
		if vehicle.DeviceID != nil {
			device := byID[*vehicle.DeviceID]
			view.LastPosition = positions[*vehicle.DeviceID]
			view.State = s.States.Get(*vehicle.DeviceID)
			if device != nil {
				_, view.Connected = s.Conns.Get(device.IMEI)
				view.Device = device.View(audience)
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Server) handleGetVehicle(w http.ResponseWriter, r *http.Request) {
	vehicle, _, share, ok := s.vehicleForViewer(w, r, false)
	if !ok {
		return
	}
	view := vehicleView{Vehicle: vehicle, Shared: sharedAccessOf(share)}
	if s.Theft != nil {
		stolen, err := s.Theft.ActiveBriefs(r.Context(), []uuid.UUID{vehicle.ID})
		if err != nil {
			handleStoreError(w, err, "veículo não encontrado")
			return
		}
		if b, ok := stolen[vehicle.ID]; ok {
			view.Theft = &b
		}
	}
	if share == nil {
		days, err := s.historyDays(r.Context(), vehicle)
		if err != nil {
			handleStoreError(w, err, "veículo não encontrado")
			return
		}
		view.HistoryDays = days
	}
	if vehicle.DeviceID != nil {
		device, err := s.Devices.Get(r.Context(), *vehicle.DeviceID)
		if err == nil {
			_, view.Connected = s.Conns.Get(device.IMEI)
			view.Device = device.View(deviceAudience(r))
		}
		if position, err := s.Positions.Latest(r.Context(), *vehicle.DeviceID); err == nil {
			view.LastPosition = position
		}
		view.State = s.States.Get(*vehicle.DeviceID)
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleCreateVehicle(w http.ResponseWriter, r *http.Request) {
	var in vehicles.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	// Aqui entram só os veículos da própria central. Veículo de cliente segue
	// o fluxo único (veículo → rastreador → assinatura), pela ficha dele ou
	// pelo painel do cliente.
	if in.OwnerID != nil {
		writeError(w, http.StatusBadRequest,
			"veículo de cliente entra pelo fluxo Novo veículo, na ficha do cliente")
		return
	}
	vehicle, err := s.Vehicles.Create(r.Context(), in)
	if err != nil {
		var validation vehicles.ValidationError
		switch {
		case errors.As(err, &validation):
			writeError(w, http.StatusBadRequest, validation.Message)
		case errors.Is(err, database.ErrConflict):
			writeVehicleConflict(w, r)
		default:
			handleStoreError(w, err, "veículo não encontrado")
		}
		return
	}

	s.vehiclesChanged(r)
	s.recordAudit(r, audit.ActionVehicleCreated, &vehicle.ID, vehicle.DeviceID,
		map[string]any{"name": vehicle.Name, "plate": vehicle.Plate})
	writeJSON(w, http.StatusCreated, vehicle)
}

func (s *Server) handleUpdateVehicle(w http.ResponseWriter, r *http.Request) {
	current, _, ok := s.vehicleFromURL(w, r, false)
	if !ok {
		return
	}

	var in vehicles.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	// O cliente edita os dados do veículo, mas não troca o rastreador.
	if _, isCustomer := customerOf(r); isCustomer {
		in.DeviceID = current.DeviceID
	}

	vehicle, err := s.Vehicles.Update(r.Context(), current.ID, in)
	if err != nil {
		var validation vehicles.ValidationError
		switch {
		case errors.As(err, &validation):
			writeError(w, http.StatusBadRequest, validation.Message)
		case errors.Is(err, database.ErrConflict):
			writeVehicleConflict(w, r)
		default:
			handleStoreError(w, err, "veículo não encontrado")
		}
		return
	}

	s.vehiclesChanged(r)
	s.recordAudit(r, audit.ActionVehicleUpdated, &vehicle.ID, vehicle.DeviceID,
		map[string]any{"name": vehicle.Name})
	writeJSON(w, http.StatusOK, vehicle)
}

func (s *Server) handleDeleteVehicle(w http.ResponseWriter, r *http.Request) {
	vehicle, _, ok := s.vehicleFromURL(w, r, false)
	if !ok {
		return
	}
	// Com assinatura ativa, excluir o veículo deixaria a cobrança sem nada
	// para cobrir: primeiro encerra-se a assinatura.
	active, err := s.Billing.HasActiveForVehicle(r.Context(), vehicle.ID)
	if err != nil {
		handleStoreError(w, err, "veículo não encontrado")
		return
	}
	if active {
		writeError(w, http.StatusConflict, "este veículo tem assinatura ativa; encerre a assinatura antes de excluí-lo")
		return
	}

	if err := s.Vehicles.Delete(r.Context(), vehicle.ID); err != nil {
		handleStoreError(w, err, "veículo não encontrado")
		return
	}

	s.vehiclesChanged(r)
	s.recordAudit(r, audit.ActionVehicleDeleted, &vehicle.ID, nil, nil)
	writeJSON(w, http.StatusNoContent, nil)
}

// writeVehicleConflict explica a violação de unicidade: placa repetida ou,
// para a central, rastreador já vinculado a outro veículo.
func writeVehicleConflict(w http.ResponseWriter, r *http.Request) {
	if _, isCustomer := customerOf(r); isCustomer {
		writeError(w, http.StatusConflict,
			"essa placa já está cadastrada; se o veículo é seu, fale com a gente")
		return
	}
	writeError(w, http.StatusConflict,
		"já existe um veículo com essa placa, ou o rastreador escolhido já está vinculado a outro veículo")
}

// vehiclesChanged atualiza os caches que dependem do cadastro de veículos.
func (s *Server) vehiclesChanged(r *http.Request) {
	s.Ingestor.InvalidateVehicles()
	if s.Owners != nil {
		if err := s.Owners.Refresh(r.Context()); err != nil {
			s.Log.Warn("falha ao recarregar os donos dos veículos", "err", err)
		}
	}
}

// vehicleFromURL resolve o veículo da rota e, quando requireDevice, também o
// rastreador vinculado — que é o que os comandos precisam.
//
// É aqui que o cliente fica restrito aos próprios veículos: o de outra pessoa
// responde 404, como se não existisse, e todas as rotas de veículo (posição,
// histórico, eventos, comandos) passam por este ponto.
func (s *Server) vehicleFromURL(w http.ResponseWriter, r *http.Request, requireDevice bool) (*vehicles.Vehicle, *devices.Device, bool) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return nil, nil, false
	}

	vehicle, err := s.Vehicles.Get(r.Context(), id)
	if err != nil {
		handleStoreError(w, err, "veículo não encontrado")
		return nil, nil, false
	}
	if customerID, isCustomer := customerOf(r); isCustomer &&
		(vehicle.OwnerID == nil || *vehicle.OwnerID != customerID) {
		writeError(w, http.StatusNotFound, "veículo não encontrado")
		return nil, nil, false
	}

	if !requireDevice {
		return vehicle, nil, true
	}
	device, ok := s.vehicleDevice(w, r, vehicle)
	if !ok {
		return nil, nil, false
	}
	return vehicle, device, true
}

// vehicleDevice carrega o rastreador vinculado ao veículo.
func (s *Server) vehicleDevice(w http.ResponseWriter, r *http.Request, vehicle *vehicles.Vehicle) (*devices.Device, bool) {
	if vehicle.DeviceID == nil {
		writeError(w, http.StatusConflict, "este veículo não tem rastreador vinculado")
		return nil, false
	}
	device, err := s.Devices.Get(r.Context(), *vehicle.DeviceID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeError(w, http.StatusConflict, "o rastreador vinculado não existe mais")
			return nil, false
		}
		handleStoreError(w, err, "rastreador não encontrado")
		return nil, false
	}
	return device, true
}

func (s *Server) recordAudit(r *http.Request, action string, vehicleID, deviceID *uuid.UUID, metadata map[string]any) {
	var userID *uuid.UUID
	if principal, ok := auth.FromContext(r.Context()); ok {
		userID = &principal.UserID
	}
	s.Audit.Record(r.Context(), &audit.Entry{
		UserID: userID, Action: action, VehicleID: vehicleID, DeviceID: deviceID,
		Result: "OK", IPAddress: clientIP(r), Metadata: metadata,
	})
}

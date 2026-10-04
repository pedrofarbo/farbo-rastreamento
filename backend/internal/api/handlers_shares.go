package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/shares"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// Acessos de terceiros: o cliente dá a outra pessoa o acompanhamento de um
// veículo dele (a posição ao vivo) e, se quiser, o bloqueio de emergência.
// A pessoa entra com a própria conta. Só as rotas que usam vehicleForViewer
// a aceitam; todas as outras continuam só do dono (vehicleFromURL).

// codeShareSuspended: quem compartilhou está com a conta suspensa, e o
// acesso de terceiro fica suspenso junto (senão a suspensão seria contornada
// com uma segunda conta).
const codeShareSuspended = "SHARE_SUSPENDED"

// sharedAccess é o que a pessoa que recebeu o acesso vê sobre ele.
type sharedAccess struct {
	ShareID   uuid.UUID `json:"shareId"`
	OwnerName string    `json:"ownerName"`
	CanBlock  bool      `json:"canBlock"`
}

func sharedAccessOf(share *shares.Share) *sharedAccess {
	if share == nil {
		return nil
	}
	return &sharedAccess{ShareID: share.ID, OwnerName: share.OwnerName, CanBlock: share.CanBlock}
}

// vehicleForViewer é o vehicleFromURL das rotas que quem recebeu acesso
// também usa: o veículo, a posição ao vivo, o pedido de posição e o
// bloqueio. Para essa pessoa, share vem preenchido; para o dono e para a
// equipe, nil.
func (s *Server) vehicleForViewer(w http.ResponseWriter, r *http.Request, requireDevice bool) (*vehicles.Vehicle, *devices.Device, *shares.Share, bool) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return nil, nil, nil, false
	}
	vehicle, err := s.Vehicles.Get(r.Context(), id)
	if err != nil {
		handleStoreError(w, err, "veículo não encontrado")
		return nil, nil, nil, false
	}

	var share *shares.Share
	if customerID, isCustomer := customerOf(r); isCustomer && (vehicle.OwnerID == nil || *vehicle.OwnerID != customerID) {
		if s.Shares == nil {
			writeError(w, http.StatusNotFound, "veículo não encontrado")
			return nil, nil, nil, false
		}
		share, err = s.Shares.Access(r.Context(), customerID, vehicle.ID)
		if err != nil {
			handleStoreError(w, err, "veículo não encontrado")
			return nil, nil, nil, false
		}
		if !s.shareActive(w, r, share) {
			return nil, nil, nil, false
		}
	}

	if !requireDevice {
		return vehicle, nil, share, true
	}
	device, ok := s.vehicleDevice(w, r, vehicle)
	if !ok {
		return nil, nil, nil, false
	}
	return vehicle, device, share, true
}

// shareActive barra o acesso de terceiro enquanto quem compartilhou estiver
// suspenso.
func (s *Server) shareActive(w http.ResponseWriter, r *http.Request, share *shares.Share) bool {
	suspended, err := s.Billing.IsSuspended(r.Context(), share.OwnerID)
	if err != nil {
		s.Log.Error("falha ao verificar suspensão de quem compartilhou", "share", share.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "erro interno")
		return false
	}
	if suspended {
		writeCode(w, http.StatusForbidden,
			"o acompanhamento deste veículo está suspenso; fale com "+share.OwnerName, codeShareSuspended)
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Rotas do dono (e de quem recebeu, para deixar de acompanhar)
// ---------------------------------------------------------------------------

// handleMyShares: os acessos que o cliente deu, de todos os veículos dele.
func (s *Server) handleMyShares(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	list, err := s.Shares.ByOwner(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "acessos não encontrados")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type createShareRequest struct {
	VehicleID uuid.UUID `json:"vehicleId"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CanBlock  bool      `json:"canBlock"`
}

// handleCreateShare dá o acesso. Pede a confirmação extra: quem pega o
// celular do cliente por um instante não se cadastra para rastreá-lo.
func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	var req createShareRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	// Confere o veículo antes de gastar a confirmação.
	vehicle, err := s.Vehicles.Get(r.Context(), req.VehicleID)
	if err != nil || vehicle.OwnerID == nil || *vehicle.OwnerID != customerID {
		writeError(w, http.StatusNotFound, "veículo não encontrado")
		return
	}
	if !s.requireStepUp(w, r, stepup.PurposeVehicleShare) {
		return
	}

	share, created, err := s.Shares.Create(r.Context(), customerID, shares.Input{
		VehicleID: req.VehicleID, Name: req.Name, Email: req.Email, CanBlock: req.CanBlock,
	})
	if !writeShareError(w, err) {
		return
	}
	s.refreshOwners(r)
	s.recordAudit(r, audit.ActionVehicleShared, &share.VehicleID, nil, map[string]any{
		"shareId": share.ID, "guestId": share.GuestID, "guestEmail": share.GuestEmail,
		"canBlock": share.CanBlock, "newAccount": created,
	})
	writeJSON(w, http.StatusCreated, share)
}

type updateShareRequest struct {
	CanBlock bool `json:"canBlock"`
}

// handleUpdateShare liga ou desliga o bloqueio. Ligar pede a confirmação
// extra; desligar, não (tirar poder nunca é arriscado).
func (s *Server) handleUpdateShare(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var req updateShareRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if req.CanBlock && !s.requireStepUp(w, r, stepup.PurposeVehicleShare) {
		return
	}
	before, after, err := s.Shares.SetCanBlock(r.Context(), customerID, id, req.CanBlock)
	if !writeShareError(w, err) {
		return
	}
	s.recordAudit(r, audit.ActionVehicleShareUpdated, &after.VehicleID, nil, map[string]any{
		"shareId": id, "guestId": after.GuestID, "canBlock": after.CanBlock, "previous": before.CanBlock,
	})
	writeJSON(w, http.StatusOK, after)
}

// handleRemoveShare tira o acesso: o dono remove, ou quem recebeu deixa de
// acompanhar. Vale na hora.
func (s *Server) handleRemoveShare(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	share, err := s.Shares.Remove(r.Context(), customerID, id)
	if !writeShareError(w, err) {
		return
	}
	s.refreshOwners(r)
	s.recordAudit(r, audit.ActionVehicleShareRemoved, &share.VehicleID, nil, map[string]any{
		"shareId": id, "guestId": share.GuestID, "byGuest": share.GuestID == customerID,
	})
	writeJSON(w, http.StatusNoContent, nil)
}

// refreshOwners atualiza na hora quem recebe o tempo real de cada veículo.
func (s *Server) refreshOwners(r *http.Request) {
	if s.Owners == nil {
		return
	}
	if err := s.Owners.Refresh(r.Context()); err != nil {
		s.Log.Warn("falha ao atualizar os acessos do tempo real", "err", err)
	}
}

// writeShareError responde o erro dos acessos; devolve true se não havia.
func writeShareError(w http.ResponseWriter, err error) bool {
	var input *shares.InputError
	switch {
	case err == nil:
		return true
	case errors.As(err, &input):
		writeError(w, http.StatusBadRequest, input.Reason)
	case errors.Is(err, shares.ErrAlreadyShared):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, database.ErrNotFound):
		writeError(w, http.StatusNotFound, "acesso não encontrado")
	default:
		handleStoreError(w, err, "acesso não encontrado")
	}
	return false
}

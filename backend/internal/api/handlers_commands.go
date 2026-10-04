package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/commands"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/shares"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// handleEngineCut: o cliente confirma o corte com a biometria ou a senha
// (comprovante de uso único em X-Step-Up-Token). Quem recebeu do dono o
// acesso de emergência também bloqueia (com a própria confirmação), e o
// dono é avisado.
func (s *Server) handleEngineCut(w http.ResponseWriter, r *http.Request) {
	vehicle, device, share, ok := s.vehicleForViewer(w, r, true)
	if !ok || !canBlock(w, share) {
		return
	}
	if !s.requireStepUp(w, r, stepup.PurposeEngineCut) {
		return
	}
	if !s.dispatchCommand(w, r, vehicle, device, protocols.CommandEngineCut, nil) || share == nil {
		return
	}
	s.recordAudit(r, audit.ActionSharedEngineCut, &vehicle.ID, &device.ID,
		map[string]any{"shareId": share.ID, "ownerId": share.OwnerID})
	if s.Shares != nil {
		s.Shares.GuestBlocked(r.Context(), share, time.Now())
	}
}

// canBlock: com acesso de terceiro, só bloqueia quem o dono permitiu.
func canBlock(w http.ResponseWriter, share *shares.Share) bool {
	if share != nil && !share.CanBlock {
		writeError(w, http.StatusForbidden, "você acompanha este veículo, mas o bloqueio não foi liberado para você")
		return false
	}
	return true
}

// handleEngineCutCheck diz, sem enviar nada, se o corte passaria agora pela
// regra de segurança e, se não, por quê. Com a posição antiga demais, o
// painel e o app pedem uma posição nova ao rastreador antes de cortar.
func (s *Server) handleEngineCutCheck(w http.ResponseWriter, r *http.Request) {
	_, device, share, ok := s.vehicleForViewer(w, r, true)
	if !ok || !canBlock(w, share) {
		return
	}
	writeJSON(w, http.StatusOK, s.Commands.CheckEngineCut(r.Context(), device))
}

// handleEngineResume: desbloquear é do dono e da central — nunca de quem
// recebeu acesso. O cliente confirma com a biometria ou a senha: com o
// celular roubado e o app aberto, quem está com ele não desbloqueia.
func (s *Server) handleEngineResume(w http.ResponseWriter, r *http.Request) {
	vehicle, device, ok := s.vehicleFromURL(w, r, true)
	if !ok {
		return
	}
	if !s.requireStepUp(w, r, stepup.PurposeEngineResume) {
		return
	}
	s.dispatchCommand(w, r, vehicle, device, protocols.CommandEngineResume, nil)
}

// handleRequestPosition: pedir uma posição nova também vale para quem
// acompanha o veículo.
func (s *Server) handleRequestPosition(w http.ResponseWriter, r *http.Request) {
	vehicle, device, _, ok := s.vehicleForViewer(w, r, true)
	if !ok {
		return
	}
	s.dispatchCommand(w, r, vehicle, device, protocols.CommandRequestPosition, nil)
}

func (s *Server) handleRequestStatus(w http.ResponseWriter, r *http.Request) {
	vehicle, device, ok := s.vehicleFromURL(w, r, true)
	if !ok {
		return
	}
	s.dispatchCommand(w, r, vehicle, device, protocols.CommandRequestStatus, nil)
}

type genericCommandRequest struct {
	Command string            `json:"command"`
	Params  map[string]string `json:"params"`
	// Raw só vale para o comando CUSTOM.
	Raw string `json:"raw"`
}

// handleGenericCommand atende os comandos que não têm rota dedicada
// (intervalo, heartbeat, servidor, reboot e o texto livre).
func (s *Server) handleGenericCommand(w http.ResponseWriter, r *http.Request) {
	var req genericCommandRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	cmdType := protocols.CommandType(req.Command)
	if !cmdType.Valid() {
		writeError(w, http.StatusBadRequest, "comando desconhecido: "+req.Command)
		return
	}
	// Texto livre é sempre de administrador: é o único caminho em que bytes
	// escolhidos por uma pessoa chegam ao veículo sem tradução.
	if cmdType == protocols.CommandCustom {
		principal, _ := auth.FromContext(r.Context())
		if principal == nil || !principal.CanManage() {
			writeError(w, http.StatusForbidden, "somente administradores enviam comandos livres")
			return
		}
	}

	vehicle, device, ok := s.vehicleFromURL(w, r, true)
	if !ok {
		return
	}
	s.dispatchCommand(w, r, vehicle, device, cmdType, &req)
}

// dispatchCommand envia o comando ao rastreador do veículo e responde. Diz
// se o comando saiu (mesmo que a confirmação do aparelho ainda venha).
func (s *Server) dispatchCommand(w http.ResponseWriter, r *http.Request, vehicle *vehicles.Vehicle, device *devices.Device,
	cmdType protocols.CommandType, extra *genericCommandRequest) bool {
	var userID *uuid.UUID
	if principal, found := auth.FromContext(r.Context()); found {
		userID = &principal.UserID
	}

	req := commands.Request{
		Device:    device,
		VehicleID: &vehicle.ID,
		Type:      cmdType,
		UserID:    userID,
		IP:        clientIP(r),
	}
	if extra != nil {
		req.Params = extra.Params
		req.RawOverride = extra.Raw
	}

	cmd, err := s.Commands.Send(r.Context(), req)
	switch {
	case errors.Is(err, commands.ErrRejected):
		// 409: o pedido é válido, mas a condição de segurança não permite (§14).
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "comando recusado pela regra de segurança",
			"reason":  cmd.Error,
			"command": cmd,
		})
		return false
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}

	// 202: o comando saiu, mas a confirmação do aparelho ainda vai chegar —
	// pelo WebSocket (command.acknowledged / command.failed).
	writeJSON(w, http.StatusAccepted, cmd)
	return cmd.Status != commands.StatusFailed
}

func (s *Server) handleVehicleCommands(w http.ResponseWriter, r *http.Request) {
	_, device, ok := s.vehicleFromURL(w, r, true)
	if !ok {
		return
	}
	list, err := s.Commands.ListByDevice(r.Context(), device, queryInt(r, "limit", 50))
	if err != nil {
		handleStoreError(w, err, "comandos não encontrados")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDeviceCommands(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	device, err := s.Devices.Get(r.Context(), id)
	if err != nil {
		handleStoreError(w, err, "dispositivo não encontrado")
		return
	}
	list, err := s.Commands.ListByDevice(r.Context(), device, queryInt(r, "limit", 50))
	if err != nil {
		handleStoreError(w, err, "comandos não encontrados")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/commands"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
)

func (s *Server) handleEngineCut(w http.ResponseWriter, r *http.Request) {
	s.sendCommand(w, r, protocols.CommandEngineCut, nil)
}

// handleEngineCutCheck diz, sem enviar nada, se o corte passaria agora pela
// regra de segurança e, se não, por quê. Com a posição antiga demais, o
// painel e o app pedem uma posição nova ao rastreador antes de cortar.
func (s *Server) handleEngineCutCheck(w http.ResponseWriter, r *http.Request) {
	_, device, ok := s.vehicleFromURL(w, r, true)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.Commands.CheckEngineCut(r.Context(), device))
}

func (s *Server) handleEngineResume(w http.ResponseWriter, r *http.Request) {
	s.sendCommand(w, r, protocols.CommandEngineResume, nil)
}

func (s *Server) handleRequestPosition(w http.ResponseWriter, r *http.Request) {
	s.sendCommand(w, r, protocols.CommandRequestPosition, nil)
}

func (s *Server) handleRequestStatus(w http.ResponseWriter, r *http.Request) {
	s.sendCommand(w, r, protocols.CommandRequestStatus, nil)
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

	s.sendCommand(w, r, cmdType, &req)
}

func (s *Server) sendCommand(w http.ResponseWriter, r *http.Request, cmdType protocols.CommandType, extra *genericCommandRequest) {
	vehicle, device, ok := s.vehicleFromURL(w, r, true)
	if !ok {
		return
	}

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
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 202: o comando saiu, mas a confirmação do aparelho ainda vai chegar —
	// pelo WebSocket (command.acknowledged / command.failed).
	writeJSON(w, http.StatusAccepted, cmd)
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

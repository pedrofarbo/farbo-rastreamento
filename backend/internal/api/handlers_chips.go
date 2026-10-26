package api

import (
	"errors"
	"net/http"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
)

// O estoque de chips (administrador): a lista com o rastreador de cada um, o
// cadastro, a correção e a retirada dos livres.

func (s *Server) handleListChips(w http.ResponseWriter, r *http.Request) {
	list, err := s.Devices.Chips(r.Context())
	if err != nil {
		handleStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateChip(w http.ResponseWriter, r *http.Request) {
	var in devices.ChipInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	chip, err := s.Devices.CreateChip(r.Context(), in)
	if err != nil {
		writeChipError(w, err)
		return
	}
	s.recordAudit(r, audit.ActionChipCreated, nil, nil, map[string]any{"chipId": chip.ID, "iccid": chip.ICCID})
	writeJSON(w, http.StatusCreated, chip)
}

func (s *Server) handleUpdateChip(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	var in devices.ChipInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	chip, err := s.Devices.UpdateChip(r.Context(), id, in)
	if err != nil {
		writeChipError(w, err)
		return
	}
	// O rastreador com o chip mudou junto: o cache da ingestão relê.
	if chip.Device != nil {
		s.Ingestor.InvalidateDevice(chip.Device.IMEI)
	}
	s.recordAudit(r, audit.ActionChipUpdated, nil, nil, map[string]any{"chipId": chip.ID, "iccid": chip.ICCID,
		"apnPasswordChanged": in.APNPassword != "" || in.ClearAPNPassword})
	writeJSON(w, http.StatusOK, chip)
}

func (s *Server) handleDeleteChip(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	if err := s.Devices.DeleteChip(r.Context(), id); err != nil {
		writeChipError(w, err)
		return
	}
	s.recordAudit(r, audit.ActionChipDeleted, nil, nil, map[string]any{"chipId": id})
	w.WriteHeader(http.StatusNoContent)
}

func writeChipError(w http.ResponseWriter, err error) {
	var v devices.ValidationError
	if errors.As(err, &v) {
		writeError(w, http.StatusBadRequest, v.Message)
		return
	}
	handleStoreError(w, err, "chip não encontrado")
}

package api

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// handleVehicleDirectory: os veículos de todas as contas, paginados, com o
// dono, a associação e o rastreador (?plate=&organization=&customer=
// &document=&email=&page=&pageSize=). organization=none: só os de quem não é
// associado.
func (s *Server) handleVehicleDirectory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := vehicles.DirectoryFilter{
		Plate: q.Get("plate"), Customer: q.Get("customer"), Document: q.Get("document"), Email: q.Get("email"),
	}
	f.Page, _ = strconv.Atoi(q.Get("page"))
	f.PageSize, _ = strconv.Atoi(q.Get("pageSize"))
	switch org := q.Get("organization"); org {
	case "":
	case "none":
		f.NoOrganization = true
	default:
		id, err := uuid.Parse(org)
		if err != nil {
			writeError(w, http.StatusBadRequest, "associação inválida")
			return
		}
		f.Organization = &id
	}
	page, err := s.Vehicles.Directory(r.Context(), f)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

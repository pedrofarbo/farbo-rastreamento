package api

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/geofences"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// A equipe restrita a grupos: quem é da equipe de uma conta pode ver só os
// veículos de alguns grupos da frota (ver accounts). Toda rota de veículo
// passa por seesVehicle — o veículo de fora responde 404, como o de outra
// conta —, e o que é da conta inteira fica atrás de requireWholeAccount.

// codeRestrictedAccess acompanha o 403 de quem está restrito a grupos.
const codeRestrictedAccess = "RESTRICTED_ACCESS"

// seesVehicle diz se a requisição enxerga o veículo como da conta: a central
// vê todos; o cliente, os da conta; quem da equipe está restrito, só os dos
// grupos dele; a organização, os dos associados.
func seesVehicle(r *http.Request, v *vehicles.Vehicle) bool {
	principal, ok := auth.FromContext(r.Context())
	// Quem é de uma associação ou seguradora: só os veículos dos associados.
	if ok && principal.IsOrgMember() {
		org := principal.Organization()
		if org == uuid.Nil || v.OrganizationID == nil || *v.OrganizationID != org {
			return false
		}
		// O corretor: só os veículos dos associados dele.
		return !principal.IsOrgBroker() || (v.BrokerID != nil && *v.BrokerID == principal.UserID)
	}
	if !ok || !principal.IsCustomer() {
		return true
	}
	return v.OwnerID != nil && *v.OwnerID == principal.Account() && principal.SeesGroup(v.GroupID)
}

// restrictedPrincipal é quem da equipe está restrito a grupos (nil para os
// demais).
func restrictedPrincipal(r *http.Request) *auth.Principal {
	if principal, ok := auth.FromContext(r.Context()); ok && principal.IsCustomer() && !principal.SeesWholeAccount() {
		return principal
	}
	return nil
}

// requireWholeAccount barra quem da equipe está restrito a grupos no que é
// da conta inteira: faturas e cobranças, envios, os grupos e os campos da
// frota, os e-mails a mais dos alertas.
func (s *Server) requireWholeAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if restrictedPrincipal(r) != nil {
			writeCode(w, http.StatusForbidden, "seu acesso é só aos veículos dos seus grupos", codeRestrictedAccess)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// visibleVehicles são, para quem está restrito a grupos, os veículos da
// conta que ele vê; nil para os demais (veem todos).
func (s *Server) visibleVehicles(r *http.Request) (map[uuid.UUID]bool, error) {
	principal := restrictedPrincipal(r)
	if principal == nil {
		return nil, nil
	}
	list, err := s.Vehicles.ListByOwner(r.Context(), principal.Account())
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]bool{}
	for _, v := range list {
		if principal.SeesGroup(v.GroupID) {
			out[v.ID] = true
		}
	}
	return out, nil
}

// visibleFences são as cercas que vigiam algum veículo visível, cada uma só
// com os veículos visíveis.
func visibleFences(list []*geofences.Geofence, visible map[uuid.UUID]bool) []*geofences.Geofence {
	out := []*geofences.Geofence{}
	for _, fence := range list {
		mine := []uuid.UUID{}
		for _, id := range fence.VehicleIDs {
			if visible[id] {
				mine = append(mine, id)
			}
		}
		if len(mine) == 0 {
			continue
		}
		copied := *fence
		copied.VehicleIDs = mine
		out = append(out, &copied)
	}
	return out
}

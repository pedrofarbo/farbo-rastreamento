package api

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/organizations"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// As associações e seguradoras pela central (/api/organizations, admin): o
// cadastro, o preço e os termos do rastreador, a equipe, os associados e as
// faturas do mês.

func (s *Server) handleListOrganizations(w http.ResponseWriter, r *http.Request) {
	list, err := s.Organizations.List(r.Context())
	if err != nil {
		handleStoreError(w, err, "organizações não encontradas")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// createOrganizationRequest: o cadastro e, opcional, o primeiro Administrador
// (recebe o convite para criar a senha).
type createOrganizationRequest struct {
	organizations.Input
	Admin *organizations.MemberInput `json:"admin"`
}

func organizationInput(w http.ResponseWriter, in *organizations.Input) bool {
	doc, ok := taxIDOf(w, in.Document)
	in.Document = doc
	return ok
}

func (s *Server) handleCreateOrganization(w http.ResponseWriter, r *http.Request) {
	var req createOrganizationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !organizationInput(w, &req.Input) {
		return
	}
	if req.Admin != nil {
		req.Admin.Role = "admin"
	}
	org, err := s.Organizations.Create(r.Context(), req.Input, personOf(r))
	if !writeOrgError(w, r, err, "organização não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionOrgChanged, nil, nil,
		map[string]any{"organizationId": org.ID, "change": "created", "kind": org.Kind, "priceCents": org.PriceCents})
	if req.Admin != nil && req.Admin.Email != "" {
		if _, err := s.Organizations.AddMember(r.Context(), org.ID, *req.Admin, personOf(r)); err != nil {
			// A organização fica; o Administrador entra pela ficha dela.
			writeJSON(w, http.StatusCreated, map[string]any{"organization": org, "adminError": err.Error()})
			return
		}
		if org, err = s.Organizations.Get(r.Context(), org.ID); err != nil {
			handleStoreError(w, err, "organização não encontrada")
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"organization": org})
}

// orgFromURL é a organização da rota (central).
func (s *Server) orgFromURL(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := urlUUID(r, "orgId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, false
	}
	if _, err := s.Organizations.Get(r.Context(), id); err != nil {
		handleStoreError(w, err, "organização não encontrada")
		return uuid.Nil, false
	}
	return id, true
}

// organizationDetail é a ficha da organização na central.
type organizationDetail struct {
	*organizations.Organization
	// As listas (members e associates, na organização, são as contagens).
	Members    []*organizations.Member    `json:"memberList"`
	Associates []*organizations.Associate `json:"associateList"`
	Invoices   []*organizations.Invoice   `json:"invoices"`
	Estimate   *organizations.Estimate    `json:"estimate"`
}

func (s *Server) handleGetOrganization(w http.ResponseWriter, r *http.Request) {
	id, ok := s.orgFromURL(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	org, err := s.Organizations.Get(ctx, id)
	if err != nil {
		handleStoreError(w, err, "organização não encontrada")
		return
	}
	out := organizationDetail{Organization: org}
	if out.Members, err = s.Organizations.Members(ctx, id); err == nil {
		if out.Associates, err = s.Organizations.Associates(ctx, id, nil); err == nil {
			if out.Invoices, err = s.Organizations.Invoices(ctx, id); err == nil {
				out.Estimate, err = s.Organizations.NextEstimate(ctx, id)
			}
		}
	}
	if err != nil {
		handleStoreError(w, err, "organização não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpdateOrganization(w http.ResponseWriter, r *http.Request) {
	id, ok := s.orgFromURL(w, r)
	if !ok {
		return
	}
	var in organizations.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !organizationInput(w, &in) {
		return
	}
	org, err := s.Organizations.Update(r.Context(), id, in)
	if !writeOrgError(w, r, err, "organização não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionOrgChanged, nil, nil, map[string]any{
		"organizationId": id, "change": "updated", "active": org.Active, "priceCents": org.PriceCents,
		"equipmentMode": org.EquipmentMode, "installFeeCents": org.InstallFeeCents, "uninstallFeeCents": org.UninstallFeeCents,
	})
	writeJSON(w, http.StatusOK, org)
}

func (s *Server) handleAdminOrgAddMember(w http.ResponseWriter, r *http.Request) {
	id, ok := s.orgFromURL(w, r)
	if !ok {
		return
	}
	var in organizations.MemberInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	s.addOrgMember(w, r, id, in)
}

func (s *Server) handleAdminOrgSetMemberRole(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.orgFromURL(w, r); ok {
		s.setOrgMemberRole(w, r, id, true)
	}
}

func (s *Server) handleAdminOrgRemoveMember(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.orgFromURL(w, r); ok {
		s.removeOrgMember(w, r, id, true)
	}
}

func (s *Server) handleAdminOrgInviteMember(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.orgFromURL(w, r); ok {
		s.inviteOrgMember(w, r, id)
	}
}

func (s *Server) handleAdminOrgCreateAssociate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.orgFromURL(w, r)
	if !ok {
		return
	}
	in, ok := associateInput(w, r)
	if !ok {
		return
	}
	a, err := s.Organizations.CreateAssociate(r.Context(), id, in)
	if !writeOrgError(w, r, err, "organização não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionOrgAssociateChanged, nil, nil,
		map[string]any{"organizationId": id, "associateId": a.ID, "change": "created"})
	writeJSON(w, http.StatusCreated, a)
}

// handleAdminOrgImportAssociates: a central sobe a planilha dos associados
// dentro da organização (o mesmo da associação).
func (s *Server) handleAdminOrgImportAssociates(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.orgFromURL(w, r); ok {
		s.importAssociates(w, r, id)
	}
}

func (s *Server) handleAdminOrgRegisterVehicle(w http.ResponseWriter, r *http.Request) {
	id, ok := s.orgFromURL(w, r)
	if !ok {
		return
	}
	a, ok := s.associateFromURL(w, r, id)
	if !ok {
		return
	}
	var in vehicles.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	vehicle, err := s.Organizations.RegisterVehicle(r.Context(), id, a.ID, in)
	if !writeOrgError(w, r, err, "associado não encontrado") {
		return
	}
	s.vehiclesChanged(r)
	s.recordAudit(r, audit.ActionOrgVehicleRegistered, &vehicle.ID, nil,
		map[string]any{"organizationId": id, "associateId": a.ID, "name": vehicle.Name, "plate": vehicle.Plate})
	writeJSON(w, http.StatusCreated, vehicle)
}

// handleGenerateOrgInvoice lança já a fatura do próximo vencimento.
func (s *Server) handleGenerateOrgInvoice(w http.ResponseWriter, r *http.Request) {
	id, ok := s.orgFromURL(w, r)
	if !ok {
		return
	}
	inv, err := s.Organizations.GenerateNow(r.Context(), id)
	if !writeOrgError(w, r, err, "organização não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionOrgInvoiceGenerated, nil, nil, map[string]any{
		"organizationId": id, "invoiceId": inv.ID, "amountCents": inv.AmountCents, "dueDate": inv.DueDate.String(),
		"vehicles": inv.Vehicles,
	})
	writeJSON(w, http.StatusCreated, inv)
}

// customerOrganizationRequest: a organização que passa a pagar o
// rastreamento do cliente (nulo: nenhuma).
type customerOrganizationRequest struct {
	OrganizationID *uuid.UUID `json:"organizationId"`
}

// handleSetCustomerOrganization põe o cliente numa organização, ou o tira.
func (s *Server) handleSetCustomerOrganization(w http.ResponseWriter, r *http.Request) {
	customerID, ok := s.customerFromURL(w, r)
	if !ok {
		return
	}
	var req customerOrganizationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	var err error
	if req.OrganizationID == nil {
		err = s.Organizations.UnlinkCustomer(r.Context(), customerID)
	} else {
		err = s.Organizations.LinkCustomer(r.Context(), *req.OrganizationID, customerID)
	}
	if !writeOrgError(w, r, err, "cliente ou organização não encontrado") {
		return
	}
	s.vehiclesChanged(r)
	s.recordAudit(r, audit.ActionOrgAssociateChanged, nil, nil,
		map[string]any{"organizationId": req.OrganizationID, "associateId": customerID, "change": "linked"})
	org, err := s.Organizations.OfCustomer(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"organization": org})
}

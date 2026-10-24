package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/appointments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/organizations"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// O painel da associação ou seguradora (/api/org): os veículos dos
// associados (o mapa e a tela do veículo usam as rotas de /vehicles, com o
// filtro da organização), os associados, a equipe e as faturas.

// writeOrgError traduz os erros do cadastro da organização.
func writeOrgError(w http.ResponseWriter, r *http.Request, err error, notFound string) bool {
	var input *organizations.InputError
	var vehicleValidation vehicles.ValidationError
	switch {
	case err == nil:
		return true
	case errors.As(err, &input):
		writeError(w, http.StatusBadRequest, input.Message)
	case errors.As(err, &vehicleValidation):
		writeError(w, http.StatusBadRequest, vehicleValidation.Message)
	case errors.Is(err, database.ErrConflict):
		writeVehicleConflict(w, r)
	default:
		handleStoreError(w, err, notFound)
	}
	return false
}

// listOrgVehicles: os veículos dos associados, com o nome de cada um (o
// rastreador na representação do cliente).
func (s *Server) listOrgVehicles(w http.ResponseWriter, r *http.Request, orgID uuid.UUID) {
	list, err := s.Vehicles.ListByOrganization(r.Context(), orgID)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	list = brokerVehicles(r, list)
	views, err := s.orgVehicleViews(r.Context(), list)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	writeJSON(w, http.StatusOK, views)
}

// brokerVehicles: para o corretor, só os veículos dos associados dele.
func brokerVehicles(r *http.Request, list []*vehicles.Vehicle) []*vehicles.Vehicle {
	broker := brokerOf(r)
	if broker == nil {
		return list
	}
	out := list[:0:0]
	for _, v := range list {
		if v.BrokerID != nil && *v.BrokerID == *broker {
			out = append(out, v)
		}
	}
	return out
}

func (s *Server) orgVehicleViews(ctx context.Context, list []*vehicles.Vehicle) ([]vehicleView, error) {
	views, err := s.vehicleViews(ctx, list, devices.AudienceCustomer)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(list))
	for _, v := range list {
		if v.OwnerID != nil {
			ids = append(ids, *v.OwnerID)
		}
	}
	names, err := s.Auth.Names(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range views {
		if views[i].OwnerID != nil {
			views[i].OwnerName = names[*views[i].OwnerID]
		}
	}
	return views, nil
}

// orgOverview é o começo do painel: a organização, o papel de quem entrou e
// os números da frota.
type orgOverview struct {
	Organization *organizations.Organization `json:"organization"`
	Role         string                      `json:"role"`
	Online       int                         `json:"online"`
	// AwaitingInstall: veículos cadastrados ainda sem rastreador.
	AwaitingInstall int `json:"awaitingInstall"`
	// Estimate: a próxima fatura (só para o Administrador).
	Estimate *organizations.Estimate `json:"estimate,omitempty"`
}

func (s *Server) handleOrgOverview(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	principal, _ := auth.FromContext(r.Context())
	org, err := s.Organizations.Get(r.Context(), orgID)
	if err != nil {
		handleStoreError(w, err, "organização não encontrada")
		return
	}
	org.Notes = "" // as observações são da central
	out := orgOverview{Organization: org, Role: principal.Org.Role}
	list, err := s.Vehicles.ListByOrganization(r.Context(), orgID)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	// O corretor: os números dos associados dele.
	if broker := brokerOf(r); broker != nil {
		list = brokerVehicles(r, list)
		mine, err := s.Organizations.Associates(r.Context(), orgID, broker)
		if err != nil {
			handleStoreError(w, err, "associados não encontrados")
			return
		}
		org.Associates, org.Vehicles, org.Installed = 0, len(list), 0
		for _, a := range mine {
			if a.Active {
				org.Associates++
			}
		}
		for _, v := range list {
			if v.DeviceID != nil {
				org.Installed++
			}
		}
	}
	deviceIDs := []uuid.UUID{}
	for _, v := range list {
		if v.DeviceID == nil {
			out.AwaitingInstall++
		} else {
			deviceIDs = append(deviceIDs, *v.DeviceID)
		}
	}
	listed, err := s.Devices.ListByIDs(r.Context(), deviceIDs)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return
	}
	for _, d := range listed {
		if d.Status == devices.StatusOnline {
			out.Online++
		}
	}
	if principal.IsOrgAdmin() {
		if out.Estimate, err = s.Organizations.NextEstimate(r.Context(), orgID); err != nil {
			handleStoreError(w, err, "organização não encontrada")
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// Associados
// ---------------------------------------------------------------------------

func (s *Server) handleOrgAssociates(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	list, err := s.Organizations.Associates(r.Context(), orgID, brokerOf(r))
	if err != nil {
		handleStoreError(w, err, "associados não encontrados")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleOrgRecoveries: o painel de recuperação (?meses=, de 1 a 24; 12 sem ele).
func (s *Server) handleOrgRecoveries(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	months := 12
	if raw := r.URL.Query().Get("meses"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 24 {
			writeError(w, http.StatusBadRequest, "período inválido: de 1 a 24 meses")
			return
		}
		months = n
	}
	out, err := s.Organizations.Recoveries(r.Context(), orgID, time.Now().AddDate(0, -months, 0))
	if err != nil {
		handleStoreError(w, err, "organização não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleOrgAnnouncements: o histórico dos comunicados.
func (s *Server) handleOrgAnnouncements(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	list, err := s.Organizations.Announcements(r.Context(), orgID)
	if err != nil {
		handleStoreError(w, err, "organização não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"announcements": list, "maxTitle": organizations.MaxAnnouncementTitle,
		"maxBody": organizations.MaxAnnouncementBody, "maxPerDay": organizations.MaxAnnouncementsPerDay,
	})
}

// handleOrgAnnounce manda um comunicado aos associados.
func (s *Server) handleOrgAnnounce(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	var in organizations.AnnouncementInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	a, err := s.Organizations.Announce(r.Context(), orgID, personOf(r), in)
	if !writeOrgError(w, r, err, "organização não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionOrgAnnouncement, nil, nil, map[string]any{
		"organizationId": orgID, "announcementId": a.ID, "title": a.Title, "recipients": a.Recipients, "email": a.Email,
	})
	writeJSON(w, http.StatusCreated, a)
}

// associateFromURL é o associado da rota, da organização de quem chama.
func (s *Server) associateFromURL(w http.ResponseWriter, r *http.Request, orgID uuid.UUID) (*organizations.Associate, bool) {
	id, err := urlUUID(r, "associateId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return nil, false
	}
	a, err := s.Organizations.Associate(r.Context(), orgID, id)
	if err != nil {
		handleStoreError(w, err, "associado não encontrado")
		return nil, false
	}
	// O corretor: só os associados dele.
	if broker := brokerOf(r); broker != nil && (a.BrokerID == nil || *a.BrokerID != *broker) {
		writeError(w, http.StatusNotFound, "associado não encontrado")
		return nil, false
	}
	return a, true
}

// associateDetail: o associado e os veículos dele.
type associateDetail struct {
	*organizations.Associate
	VehicleList []vehicleView `json:"vehicleList"`
	// A instalação ou a desinstalação marcada (em aberto) e os pedidos de
	// desinstalação, pelo id do veículo.
	Appointments      []*appointments.Appointment                 `json:"appointments"`
	UninstallRequests map[uuid.UUID]appointments.UninstallRequest `json:"uninstallRequests"`
}

func (s *Server) associateDetail(ctx context.Context, a *organizations.Associate) (*associateDetail, error) {
	list, err := s.Vehicles.ListByOwner(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	views, err := s.orgVehicleViews(ctx, list)
	if err != nil {
		return nil, err
	}
	detail := &associateDetail{Associate: a, VehicleList: views, Appointments: []*appointments.Appointment{},
		UninstallRequests: map[uuid.UUID]appointments.UninstallRequest{}}
	if s.Appointments != nil {
		if detail.Appointments, err = s.Appointments.ForCustomer(ctx, a.ID); err != nil {
			return nil, err
		}
		if detail.UninstallRequests, err = s.Appointments.UninstallRequests(ctx, a.ID); err != nil {
			return nil, err
		}
	}
	return detail, nil
}

func (s *Server) handleOrgAssociate(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	a, ok := s.associateFromURL(w, r, orgID)
	if !ok {
		return
	}
	detail, err := s.associateDetail(r.Context(), a)
	if err != nil {
		handleStoreError(w, err, "associado não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// associateInput confere o documento do cadastro do associado.
func associateInput(w http.ResponseWriter, r *http.Request) (organizations.AssociateInput, bool) {
	var in organizations.AssociateInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return in, false
	}
	doc, ok := taxIDOf(w, in.Document)
	in.Document = doc
	return in, ok
}

func (s *Server) handleOrgCreateAssociate(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	in, ok := associateInput(w, r)
	if !ok {
		return
	}
	// O corretor cadastra para ele mesmo.
	if broker := brokerOf(r); broker != nil {
		me := broker.String()
		in.BrokerID = &me
	}
	a, err := s.Organizations.CreateAssociate(r.Context(), orgID, in)
	if !writeOrgError(w, r, err, "organização não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionOrgAssociateChanged, nil, nil,
		map[string]any{"organizationId": orgID, "associateId": a.ID, "change": "created"})
	writeJSON(w, http.StatusCreated, a)
}

type importAssociatesRequest struct {
	Rows   []organizations.ImportRow `json:"rows"`
	DryRun bool                      `json:"dryRun"`
}

// handleOrgImportAssociates: os associados (e os veículos) por planilha. Com
// dryRun, só confere; com problemas, 422 com a lista (nada entra).
func (s *Server) handleOrgImportAssociates(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	s.importAssociates(w, r, orgID)
}

// importAssociates: os associados (e os veículos) da planilha, pela
// associação ou pela central; dryRun só confere.
func (s *Server) importAssociates(w http.ResponseWriter, r *http.Request, orgID uuid.UUID) {
	var req importAssociatesRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	result, problems, err := s.Organizations.ImportAssociates(r.Context(), orgID, req.Rows, req.DryRun)
	if len(problems) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "a planilha tem problemas: nada foi cadastrado", "problems": problems,
		})
		return
	}
	if result != nil && !req.DryRun && (result.Associates > 0 || result.Vehicles > 0) {
		s.vehiclesChanged(r)
		s.recordAudit(r, audit.ActionOrgAssociateChanged, nil, nil, map[string]any{
			"organizationId": orgID, "change": "imported", "associates": result.Associates, "vehicles": result.Vehicles,
			"complete": err == nil,
		})
	}
	if !writeOrgError(w, r, err, "organização não encontrada") {
		return
	}
	status := http.StatusCreated
	if req.DryRun {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func (s *Server) handleOrgUpdateAssociate(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	current, ok := s.associateFromURL(w, r, orgID)
	if !ok {
		return
	}
	in, ok := associateInput(w, r)
	if !ok {
		return
	}
	// O corretor muda o cadastro, mas não desliga nem passa para outro.
	if brokerOf(r) != nil {
		in.Active, in.BrokerID = nil, nil
	}
	a, err := s.Organizations.UpdateAssociate(r.Context(), orgID, current.ID, in)
	if !writeOrgError(w, r, err, "associado não encontrado") {
		return
	}
	if a.Active != current.Active {
		s.vehiclesChanged(r)
	}
	s.recordAudit(r, audit.ActionOrgAssociateChanged, nil, nil,
		map[string]any{"organizationId": orgID, "associateId": a.ID, "change": "updated", "active": a.Active})
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleOrgInviteAssociate(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	current, ok := s.associateFromURL(w, r, orgID)
	if !ok {
		return
	}
	a, err := s.Organizations.ResendAssociateInvite(r.Context(), orgID, current.ID)
	if !writeOrgError(w, r, err, "associado não encontrado") {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "convite enviado para " + a.Email})
}

// handleOrgRegisterVehicle: o veículo do associado e o pedido de instalação.
func (s *Server) handleOrgRegisterVehicle(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	a, ok := s.associateFromURL(w, r, orgID)
	if !ok {
		return
	}
	var in vehicles.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	vehicle, err := s.Organizations.RegisterVehicle(r.Context(), orgID, a.ID, in)
	if !writeOrgError(w, r, err, "associado não encontrado") {
		return
	}
	s.vehiclesChanged(r)
	s.recordAudit(r, audit.ActionOrgVehicleRegistered, &vehicle.ID, nil,
		map[string]any{"organizationId": orgID, "associateId": a.ID, "name": vehicle.Name, "plate": vehicle.Plate})
	writeJSON(w, http.StatusCreated, vehicle)
}

// ---------------------------------------------------------------------------
// Equipe
// ---------------------------------------------------------------------------

func (s *Server) handleOrgTeam(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	s.writeOrgMembers(w, r, orgID)
}

func (s *Server) writeOrgMembers(w http.ResponseWriter, r *http.Request, orgID uuid.UUID) {
	list, err := s.Organizations.Members(r.Context(), orgID)
	if err != nil {
		handleStoreError(w, err, "equipe não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleOrgAddMember: quem pega o celular do Administrador não põe ninguém na
// equipe sem a biometria ou a senha.
func (s *Server) handleOrgAddMember(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	var in organizations.MemberInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !s.requireTeamStepUp(w, r, stepup.PurposeTeamMember) {
		return
	}
	s.addOrgMember(w, r, orgID, in)
}

func (s *Server) addOrgMember(w http.ResponseWriter, r *http.Request, orgID uuid.UUID, in organizations.MemberInput) {
	m, err := s.Organizations.AddMember(r.Context(), orgID, in, personOf(r))
	if !writeOrgError(w, r, err, "organização não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionOrgMemberChanged, nil, nil,
		map[string]any{"organizationId": orgID, "userId": m.UserID, "role": m.Role, "change": "added"})
	writeJSON(w, http.StatusCreated, m)
}

type orgRoleRequest struct {
	Role string `json:"role"`
}

// orgMemberFromURL: a pessoa da rota; o Administrador não muda o próprio
// papel nem se tira da equipe (outro Administrador, ou a central, faz).
func orgMemberFromURL(w http.ResponseWriter, r *http.Request, self bool) (uuid.UUID, bool) {
	id, err := urlUUID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, false
	}
	if !self && id == personOf(r) {
		writeError(w, http.StatusBadRequest, "você não pode mudar o próprio papel nem sair da equipe")
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) handleOrgSetMemberRole(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	s.setOrgMemberRole(w, r, orgID, false)
}

func (s *Server) setOrgMemberRole(w http.ResponseWriter, r *http.Request, orgID uuid.UUID, self bool) {
	userID, ok := orgMemberFromURL(w, r, self)
	if !ok {
		return
	}
	var req orgRoleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	m, err := s.Organizations.SetMemberRole(r.Context(), orgID, userID, req.Role)
	if !writeOrgError(w, r, err, "pessoa não encontrada na equipe") {
		return
	}
	s.recordAudit(r, audit.ActionOrgMemberChanged, nil, nil,
		map[string]any{"organizationId": orgID, "userId": userID, "role": m.Role, "change": "role"})
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleOrgRemoveMember(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	s.removeOrgMember(w, r, orgID, false)
}

func (s *Server) removeOrgMember(w http.ResponseWriter, r *http.Request, orgID uuid.UUID, self bool) {
	userID, ok := orgMemberFromURL(w, r, self)
	if !ok {
		return
	}
	m, err := s.Organizations.RemoveMember(r.Context(), orgID, userID)
	if !writeOrgError(w, r, err, "pessoa não encontrada na equipe") {
		return
	}
	s.recordAudit(r, audit.ActionOrgMemberChanged, nil, nil,
		map[string]any{"organizationId": orgID, "userId": userID, "change": "removed"})
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleOrgInviteMember(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	s.inviteOrgMember(w, r, orgID)
}

func (s *Server) inviteOrgMember(w http.ResponseWriter, r *http.Request, orgID uuid.UUID) {
	userID, err := urlUUID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	m, err := s.Organizations.ResendMemberInvite(r.Context(), orgID, userID)
	if !writeOrgError(w, r, err, "pessoa não encontrada na equipe") {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "convite enviado para " + m.Email})
}

// ---------------------------------------------------------------------------
// Faturas
// ---------------------------------------------------------------------------

// orgInvoicesView: as faturas e a simulação da próxima.
type orgInvoicesView struct {
	Invoices      []*organizations.Invoice `json:"invoices"`
	Estimate      *organizations.Estimate  `json:"estimate"`
	OnlinePayment bool                     `json:"onlinePayment"`
}

func (s *Server) handleOrgInvoices(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	s.writeOrgInvoices(w, r, orgID)
}

func (s *Server) writeOrgInvoices(w http.ResponseWriter, r *http.Request, orgID uuid.UUID) {
	list, err := s.Organizations.Invoices(r.Context(), orgID)
	if err != nil {
		handleStoreError(w, err, "faturas não encontradas")
		return
	}
	estimate, err := s.Organizations.NextEstimate(r.Context(), orgID)
	if err != nil {
		handleStoreError(w, err, "faturas não encontradas")
		return
	}
	writeJSON(w, http.StatusOK, orgInvoicesView{Invoices: list, Estimate: estimate, OnlinePayment: s.Payments.Enabled()})
}

// orgInvoice: a fatura da rota, da organização de quem chama.
func (s *Server) orgInvoice(w http.ResponseWriter, r *http.Request) (*billing.Invoice, bool) {
	orgID, _ := orgOf(r)
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return nil, false
	}
	inv, err := s.Billing.GetInvoice(r.Context(), id)
	if err != nil || inv.CustomerID != orgID {
		writeError(w, http.StatusNotFound, "fatura não encontrada")
		return nil, false
	}
	return inv, true
}

func (s *Server) handleOrgInvoicePix(w http.ResponseWriter, r *http.Request) {
	if inv, ok := s.orgInvoice(w, r); ok {
		s.createCharge(w, r, inv, payments.MethodPix)
	}
}

func (s *Server) handleOrgInvoiceBoleto(w http.ResponseWriter, r *http.Request) {
	if inv, ok := s.orgInvoice(w, r); ok {
		s.createCharge(w, r, inv, payments.MethodBoleto)
	}
}

// orgCharge: a cobrança da rota, de uma fatura da organização de quem chama.
func (s *Server) orgCharge(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	orgID, _ := orgOf(r)
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return uuid.Nil, false
	}
	charge, err := s.Payments.Charge(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "cobrança não encontrada")
		return uuid.Nil, false
	}
	inv, err := s.Billing.GetInvoice(r.Context(), charge.InvoiceID)
	if err != nil || inv.CustomerID != orgID {
		writeError(w, http.StatusNotFound, "cobrança não encontrada")
		return uuid.Nil, false
	}
	return id, true
}

// handleOrgCharge: o status do Pix ou do boleto de uma fatura da organização.
func (s *Server) handleOrgCharge(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.orgCharge(w, r); ok {
		s.refreshCharge(w, r, id)
	}
}

// handleOrgChargeSimulate: o pagamento de mentira (só com a chave de testes).
func (s *Server) handleOrgChargeSimulate(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.orgCharge(w, r); ok {
		s.simulateCharge(w, r, id)
	}
}

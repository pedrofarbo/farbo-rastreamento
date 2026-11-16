package api

import (
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/claims"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/inspections"
)

// As ocorrências e as vistorias: o associado abre a ocorrência e tira as
// fotos da vistoria pelo app (/me); a equipe da associação ou seguradora
// acompanha, muda a situação e revisa (/org).

// codeNotAssociate: a rota é só de quem é associado de uma organização.
const codeNotAssociate = "NOT_ASSOCIATE"

// associateOf é o associado logado e a organização dele (com o 403 escrito
// quando não é associado).
func (s *Server) associateOf(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	customerID, ok := customerOf(r)
	if !ok {
		writeError(w, http.StatusForbidden, "seu perfil não permite esta operação")
		return uuid.Nil, uuid.Nil, false
	}
	user, err := s.Auth.GetUser(r.Context(), customerID)
	if err != nil {
		handleStoreError(w, err, "cliente não encontrado")
		return uuid.Nil, uuid.Nil, false
	}
	if user.OrganizationID == nil {
		writeCode(w, http.StatusForbidden, "as ocorrências e as vistorias são de quem é associado de uma associação ou seguradora", codeNotAssociate)
		return uuid.Nil, uuid.Nil, false
	}
	return customerID, *user.OrganizationID, true
}

// writeAttachment manda o arquivo: as fotos aparecem na tela (inline), o
// resto vai como download; sempre com o tipo conferido no envio.
func writeAttachment(w http.ResponseWriter, name, contentType string, data []byte) {
	disposition := "attachment"
	if contentType == "image/jpeg" || contentType == "image/png" {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// readUpload lê o arquivo do formulário (campo file) até max bytes.
func readUpload(w http.ResponseWriter, r *http.Request, max int64) ([]byte, string, bool) {
	limit := max + 64<<10
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseMultipartForm(limit); err != nil {
		writeError(w, http.StatusBadRequest, "envie um arquivo de até "+strconv.FormatInt(max>>20, 10)+" MB")
		return nil, "", false
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "envie o arquivo")
		return nil, "", false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "não foi possível ler o arquivo")
		return nil, "", false
	}
	return data, header.Filename, true
}

func cleanupForm(r *http.Request) {
	if r.MultipartForm != nil {
		_ = r.MultipartForm.RemoveAll()
	}
}

// writeClaimsError traduz os erros das ocorrências e das vistorias.
func writeClaimsError(w http.ResponseWriter, err error, notFound string) bool {
	var claimErr *claims.InputError
	var inspectionErr *inspections.InputError
	switch {
	case err == nil:
		return true
	case errors.As(err, &claimErr):
		writeError(w, http.StatusBadRequest, claimErr.Message)
	case errors.As(err, &inspectionErr):
		writeError(w, http.StatusBadRequest, inspectionErr.Message)
	case errors.Is(err, database.ErrNotFound):
		writeError(w, http.StatusNotFound, notFound)
	default:
		handleStoreError(w, err, notFound)
	}
	return false
}

// ---------------------------------------------------------------------------
// Ocorrências
// ---------------------------------------------------------------------------

// claimFor é a ocorrência da rota, se quem chama pode vê-la: o associado dela
// ou a equipe da organização dela.
func (s *Server) claimFor(w http.ResponseWriter, r *http.Request) (*claims.Claim, bool) {
	id, err := urlUUID(r, "claimId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return nil, false
	}
	c, err := s.Claims.Get(r.Context(), id)
	if !writeClaimsError(w, err, "ocorrência não encontrada") {
		return nil, false
	}
	if orgID, isOrg := orgOf(r); isOrg {
		if c.OrganizationID != orgID {
			writeError(w, http.StatusNotFound, "ocorrência não encontrada")
			return nil, false
		}
		return c, true
	}
	if customerID, ok := customerOf(r); !ok || c.AssociateID != customerID {
		writeError(w, http.StatusNotFound, "ocorrência não encontrada")
		return nil, false
	}
	return c, true
}

func (s *Server) handleMyClaims(w http.ResponseWriter, r *http.Request) {
	associate, _, ok := s.associateOf(w, r)
	if !ok {
		return
	}
	list, err := s.Claims.List(r.Context(), claims.Filter{AssociateID: &associate})
	if !writeClaimsError(w, err, "ocorrências não encontradas") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleOpenClaim(w http.ResponseWriter, r *http.Request) {
	associate, orgID, ok := s.associateOf(w, r)
	if !ok {
		return
	}
	var in claims.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	c, err := s.Claims.Open(r.Context(), orgID, associate, in)
	if !writeClaimsError(w, err, "ocorrência não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionClaimChanged, c.VehicleID, nil, map[string]any{
		"claimId": c.ID, "organizationId": orgID, "change": "opened", "kind": c.Kind,
	})
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleGetClaim(w http.ResponseWriter, r *http.Request) {
	if c, ok := s.claimFor(w, r); ok {
		writeJSON(w, http.StatusOK, c)
	}
}

// handleClaimTelemetry: a análise do rastreador em volta do horário
// declarado (onde estava, a velocidade, o corte de energia, o modo roubo) e
// o que confere ou não.
func (s *Server) handleClaimTelemetry(w http.ResponseWriter, r *http.Request) {
	c, ok := s.claimFor(w, r)
	if !ok {
		return
	}
	out, err := s.Claims.Telemetry(r.Context(), c)
	if !writeClaimsError(w, err, "ocorrência não encontrada") {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAddClaimFile: uma foto, o B.O. ou um documento (campo kind).
func (s *Server) handleAddClaimFile(w http.ResponseWriter, r *http.Request) {
	defer cleanupForm(r)
	c, ok := s.claimFor(w, r)
	if !ok {
		return
	}
	data, name, ok := readUpload(w, r, claims.MaxFileBytes)
	if !ok {
		return
	}
	f, err := s.Claims.AddFile(r.Context(), c.ID, personOf(r), r.FormValue("kind"), name, data)
	if !writeClaimsError(w, err, "ocorrência não encontrada") {
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) handleClaimFile(w http.ResponseWriter, r *http.Request) {
	c, ok := s.claimFor(w, r)
	if !ok {
		return
	}
	fileID, err := uuid.Parse(chi.URLParam(r, "fileId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	f, data, err := s.Claims.FileData(r.Context(), c.ID, fileID)
	if !writeClaimsError(w, err, "arquivo não encontrado") {
		return
	}
	name := f.Filename
	if name == "" {
		name = "ocorrencia-" + f.ID.String()[:8]
	}
	writeAttachment(w, name, f.ContentType, data)
}

type claimNoteRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

// handleClaimNote: uma nota no histórico (o associado ou a equipe).
func (s *Server) handleClaimNote(w http.ResponseWriter, r *http.Request) {
	c, ok := s.claimFor(w, r)
	if !ok {
		return
	}
	var req claimNoteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	kind := "associate"
	if _, isOrg := orgOf(r); isOrg {
		kind = "organization"
	}
	if !writeClaimsError(w, s.Claims.AddNote(r.Context(), c.ID, personOf(r), kind, req.Note), "ocorrência não encontrada") {
		return
	}
	updated, err := s.Claims.Get(r.Context(), c.ID)
	if !writeClaimsError(w, err, "ocorrência não encontrada") {
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleOrgClaims(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	list, err := s.Claims.List(r.Context(), claims.Filter{OrganizationID: &orgID, Status: r.URL.Query().Get("situacao")})
	if !writeClaimsError(w, err, "ocorrências não encontradas") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleOrgClaimStatus: a equipe muda a situação (com uma nota opcional).
func (s *Server) handleOrgClaimStatus(w http.ResponseWriter, r *http.Request) {
	c, ok := s.claimFor(w, r)
	if !ok {
		return
	}
	var req claimNoteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	updated, err := s.Claims.SetStatus(r.Context(), c.ID, personOf(r), req.Status, req.Note)
	if !writeClaimsError(w, err, "ocorrência não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionClaimChanged, c.VehicleID, nil, map[string]any{
		"claimId": c.ID, "organizationId": c.OrganizationID, "change": "status", "from": c.Status, "to": updated.Status,
	})
	writeJSON(w, http.StatusOK, updated)
}

// ---------------------------------------------------------------------------
// Vistorias
// ---------------------------------------------------------------------------

func (s *Server) inspectionFor(w http.ResponseWriter, r *http.Request) (*inspections.Inspection, bool) {
	id, err := urlUUID(r, "inspectionId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return nil, false
	}
	in, err := s.Inspections.Get(r.Context(), id)
	if !writeClaimsError(w, err, "vistoria não encontrada") {
		return nil, false
	}
	if orgID, isOrg := orgOf(r); isOrg {
		if in.OrganizationID != orgID {
			writeError(w, http.StatusNotFound, "vistoria não encontrada")
			return nil, false
		}
		return in, true
	}
	if customerID, ok := customerOf(r); !ok || in.AssociateID == nil || *in.AssociateID != customerID {
		writeError(w, http.StatusNotFound, "vistoria não encontrada")
		return nil, false
	}
	return in, true
}

func (s *Server) handleMyInspections(w http.ResponseWriter, r *http.Request) {
	associate, _, ok := s.associateOf(w, r)
	if !ok {
		return
	}
	list, err := s.Inspections.List(r.Context(), inspections.Filter{AssociateID: &associate})
	if !writeClaimsError(w, err, "vistorias não encontradas") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetInspection(w http.ResponseWriter, r *http.Request) {
	if in, ok := s.inspectionFor(w, r); ok {
		writeJSON(w, http.StatusOK, in)
	}
}

// inspectionPhotoMeta: a posição do celular (se a pessoa deixou), a precisão
// dela e de onde a foto veio (camera: a câmera ao vivo da vistoria).
func inspectionPhotoMeta(r *http.Request) inspections.PhotoMeta {
	value := func(name string) *float64 {
		if v, err := strconv.ParseFloat(r.FormValue(name), 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
			return &v
		}
		return nil
	}
	return inspections.PhotoMeta{Latitude: value("latitude"), Longitude: value("longitude"), AccuracyM: value("accuracy"),
		Source: r.FormValue("source")}
}

// saveInspectionPhoto: a foto de um ângulo da vistoria (pelo app ou pelo link).
func (s *Server) saveInspectionPhoto(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	data, _, ok := readUpload(w, r, inspections.MaxPhotoBytes)
	if !ok {
		return
	}
	p, err := s.Inspections.SavePhoto(r.Context(), id, chi.URLParam(r, "slot"), data, inspectionPhotoMeta(r))
	if !writeClaimsError(w, err, "vistoria não encontrada") {
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleSaveInspectionPhoto(w http.ResponseWriter, r *http.Request) {
	defer cleanupForm(r)
	if in, ok := s.inspectionFor(w, r); ok {
		s.saveInspectionPhoto(w, r, in.ID)
	}
}

// writeInspectionPhoto manda a foto de um ângulo.
func (s *Server) writeInspectionPhoto(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	slot := chi.URLParam(r, "slot")
	contentType, data, err := s.Inspections.PhotoData(r.Context(), id, slot)
	if !writeClaimsError(w, err, "foto não encontrada") {
		return
	}
	ext := ".jpg"
	if contentType == "image/png" {
		ext = ".png"
	}
	writeAttachment(w, "vistoria-"+slot+ext, contentType, data)
}

func (s *Server) handleInspectionPhoto(w http.ResponseWriter, r *http.Request) {
	if in, ok := s.inspectionFor(w, r); ok {
		s.writeInspectionPhoto(w, r, in.ID)
	}
}

// decodeSubmit: as avarias e o aceite da declaração.
func decodeSubmit(w http.ResponseWriter, r *http.Request) (inspections.SubmitInput, bool) {
	var req inspections.SubmitInput
	if r.ContentLength == 0 {
		return req, true
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return req, false
	}
	return req, true
}

func (s *Server) handleSubmitInspection(w http.ResponseWriter, r *http.Request) {
	in, ok := s.inspectionFor(w, r)
	if !ok {
		return
	}
	req, ok := decodeSubmit(w, r)
	if !ok {
		return
	}
	updated, err := s.Inspections.Submit(r.Context(), in.ID, req)
	if !writeClaimsError(w, err, "vistoria não encontrada") {
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleOrgInspections(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	list, err := s.Inspections.List(r.Context(), inspections.Filter{OrganizationID: &orgID, Status: r.URL.Query().Get("situacao")})
	if !writeClaimsError(w, err, "vistorias não encontradas") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleOrgRequestInspection: a vistoria de um veículo de associado
// (vehicleId) ou, de quem ainda vai entrar, pelo nome, o celular e a placa.
// A resposta traz o link para mandar.
func (s *Server) handleOrgRequestInspection(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	var req inspections.RequestInput
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if req.VehicleID != nil && *req.VehicleID == uuid.Nil {
		req.VehicleID = nil
	}
	in, err := s.Inspections.Request(r.Context(), orgID, personOf(r), req)
	if !writeClaimsError(w, err, "veículo não encontrado") {
		return
	}
	s.recordAudit(r, audit.ActionInspectionChanged, in.VehicleID, nil, map[string]any{
		"inspectionId": in.ID, "organizationId": orgID, "change": "requested", "kind": in.Kind, "mode": in.Mode,
	})
	writeJSON(w, http.StatusCreated, in)
}

// handleOrgCancelInspection: o link para de abrir (a vistoria que espera as fotos).
func (s *Server) handleOrgCancelInspection(w http.ResponseWriter, r *http.Request) {
	in, ok := s.inspectionFor(w, r)
	if !ok {
		return
	}
	updated, err := s.Inspections.Cancel(r.Context(), in.ID)
	if !writeClaimsError(w, err, "vistoria não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionInspectionChanged, in.VehicleID, nil, map[string]any{
		"inspectionId": in.ID, "organizationId": in.OrganizationID, "change": "canceled",
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleOrgRenewInspection: mais 7 dias para o link.
func (s *Server) handleOrgRenewInspection(w http.ResponseWriter, r *http.Request) {
	in, ok := s.inspectionFor(w, r)
	if !ok {
		return
	}
	updated, err := s.Inspections.Renew(r.Context(), in.ID)
	if !writeClaimsError(w, err, "vistoria não encontrada") {
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// ---------------------------------------------------------------------------
// O link da vistoria (sem login)
// ---------------------------------------------------------------------------

// publicInspectionView é o que o link mostra: sem o celular, o sobrenome nem
// a posição das fotos.
type publicInspectionView struct {
	OrganizationName string                  `json:"organizationName"`
	Kind             string                  `json:"kind"`
	Mode             string                  `json:"mode"`
	VehicleType      string                  `json:"vehicleType"`
	VehicleName      string                  `json:"vehicleName"`
	Plate            string                  `json:"plate"`
	FirstName        string                  `json:"firstName"`
	Status           string                  `json:"status"`
	Expired          bool                    `json:"expired"`
	ExpiresAt        *time.Time              `json:"expiresAt"`
	RequestNote      string                  `json:"requestNote"`
	SubmittedAt      *time.Time              `json:"submittedAt"`
	Slots            []inspections.Slot      `json:"slots"`
	Photos           []publicInspectionPhoto `json:"photos"`
}

type publicInspectionPhoto struct {
	Slot    string    `json:"slot"`
	TakenAt time.Time `json:"takenAt"`
}

func (s *Server) toPublicInspection(in *inspections.Inspection) publicInspectionView {
	first, _, _ := strings.Cut(strings.TrimSpace(in.AssociateName), " ")
	photos := make([]publicInspectionPhoto, len(in.Photos))
	for i, p := range in.Photos {
		photos[i] = publicInspectionPhoto{Slot: p.Slot, TakenAt: p.TakenAt}
	}
	return publicInspectionView{
		OrganizationName: in.OrganizationName, Kind: in.Kind, Mode: in.Mode, VehicleType: in.VehicleType,
		VehicleName: in.VehicleName, Plate: in.Plate, FirstName: first, Status: in.Status, Expired: in.Expired(s.Inspections.Now()),
		ExpiresAt: in.ExpiresAt, RequestNote: in.RequestNote, SubmittedAt: in.SubmittedAt, Slots: in.Slots, Photos: photos,
	}
}

// publicInspectionFor é a vistoria mais nova da série do link.
func (s *Server) publicInspectionFor(w http.ResponseWriter, r *http.Request) (*inspections.Inspection, bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	in, err := s.Inspections.ByToken(r.Context(), chi.URLParam(r, "token"))
	if !writeClaimsError(w, err, "link não encontrado") {
		return nil, false
	}
	return in, true
}

func (s *Server) handlePublicInspection(w http.ResponseWriter, r *http.Request) {
	if in, ok := s.publicInspectionFor(w, r); ok {
		writeJSON(w, http.StatusOK, s.toPublicInspection(in))
	}
}

func (s *Server) handlePublicInspectionPhoto(w http.ResponseWriter, r *http.Request) {
	defer cleanupForm(r)
	if in, ok := s.publicInspectionFor(w, r); ok {
		s.saveInspectionPhoto(w, r, in.ID)
	}
}

func (s *Server) handlePublicInspectionPhotoFile(w http.ResponseWriter, r *http.Request) {
	if in, ok := s.publicInspectionFor(w, r); ok {
		s.writeInspectionPhoto(w, r, in.ID)
	}
}

func (s *Server) handlePublicInspectionSubmit(w http.ResponseWriter, r *http.Request) {
	in, ok := s.publicInspectionFor(w, r)
	if !ok {
		return
	}
	req, ok := decodeSubmit(w, r)
	if !ok {
		return
	}
	updated, err := s.Inspections.Submit(r.Context(), in.ID, req)
	if !writeClaimsError(w, err, "link não encontrado") {
		return
	}
	writeJSON(w, http.StatusOK, s.toPublicInspection(updated))
}

type reviewInspectionRequest struct {
	Approve bool   `json:"approve"`
	Note    string `json:"note"`
}

func (s *Server) handleOrgReviewInspection(w http.ResponseWriter, r *http.Request) {
	in, ok := s.inspectionFor(w, r)
	if !ok {
		return
	}
	var req reviewInspectionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	updated, err := s.Inspections.Review(r.Context(), in.ID, personOf(r), req.Approve, req.Note)
	if !writeClaimsError(w, err, "vistoria não encontrada") {
		return
	}
	s.recordAudit(r, audit.ActionInspectionChanged, in.VehicleID, nil, map[string]any{
		"inspectionId": in.ID, "organizationId": in.OrganizationID, "change": updated.Status,
	})
	writeJSON(w, http.StatusOK, updated)
}

package api

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
)

// Gestão da empresa (só o admin): contas a pagar e receitas avulsas, anexos,
// recorrências, cadastros, caixa, resultado e estoque. Ver o pacote finance.

// financeError: validação vira 400 com a mensagem; o resto, como sempre.
func financeError(w http.ResponseWriter, err error, notFound string) {
	var v finance.ValidationError
	if errors.As(err, &v) {
		writeError(w, http.StatusBadRequest, v.Message)
		return
	}
	handleStoreError(w, err, notFound)
}

// actor é quem está fazendo (para created_by).
func actor(r *http.Request) *uuid.UUID {
	if principal, ok := auth.FromContext(r.Context()); ok {
		return &principal.UserID
	}
	return nil
}

func (s *Server) auditFinance(r *http.Request, op string, meta map[string]any) {
	meta["op"] = op
	s.recordAudit(r, audit.ActionFinanceChanged, nil, nil, meta)
}

// decodeOr400 lê o corpo; devolve false (e responde) se não der.
func decodeOr400(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := decodeJSON(w, r, dst); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return false
	}
	return true
}

func idOr400(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return id, false
	}
	return id, true
}

func (s *Server) handleFinanceAlerts(w http.ResponseWriter, r *http.Request) {
	out, err := s.Finance.Alerts(r.Context())
	if err != nil {
		financeError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleFinanceOverview(w http.ResponseWriter, r *http.Request) {
	out, err := s.Finance.Overview(r.Context())
	if err != nil {
		financeError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleFinanceCashFlow(w http.ResponseWriter, r *http.Request) {
	out, err := s.Finance.CashFlow(r.Context(), queryInt(r, "months", 12))
	if err != nil {
		financeError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleFinanceDRE(w http.ResponseWriter, r *http.Request) {
	out, err := s.Finance.DRE(r.Context(), queryInt(r, "months", 12))
	if err != nil {
		financeError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleFinanceSettings(w http.ResponseWriter, r *http.Request) {
	var in finance.Settings
	if !decodeOr400(w, r, &in) {
		return
	}
	out, err := s.Finance.SaveSettings(r.Context(), in)
	if err != nil {
		financeError(w, err, "")
		return
	}
	s.auditFinance(r, "settings", map[string]any{
		"openingBalanceCents": out.OpeningBalanceCents, "openingDate": out.OpeningDate.String()})
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// Lançamentos
// ---------------------------------------------------------------------------

func (s *Server) handleListEntries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := finance.EntryFilter{Kind: q.Get("kind"), Status: q.Get("status"), Search: q.Get("q")}
	for name, dst := range map[string]**billing.Date{"from": &f.From, "to": &f.To} {
		if raw := q.Get(name); raw != "" {
			parsed, err := time.Parse(time.DateOnly, raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, name+" inválido: use AAAA-MM-DD")
				return
			}
			*dst = &billing.Date{Time: parsed}
		}
	}
	for name, dst := range map[string]**uuid.UUID{"category": &f.CategoryID, "supplier": &f.SupplierID} {
		if raw := q.Get(name); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, name+" inválido")
				return
			}
			*dst = &id
		}
	}
	list, err := s.Finance.Entries(r.Context(), f)
	if err != nil {
		financeError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateEntries(w http.ResponseWriter, r *http.Request) {
	var in finance.EntryInput
	if !decodeOr400(w, r, &in) {
		return
	}
	created, err := s.Finance.CreateEntries(r.Context(), in, actor(r))
	if err != nil {
		financeError(w, err, "")
		return
	}
	ids := make([]uuid.UUID, len(created))
	for i, e := range created {
		ids[i] = e.ID
	}
	s.auditFinance(r, "entry.create", map[string]any{
		"kind": in.Kind, "entries": ids, "amountCents": in.AmountCents, "description": in.Description})
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	var in finance.EntryUpdate
	if !decodeOr400(w, r, &in) {
		return
	}
	updated, err := s.Finance.UpdateEntry(r.Context(), id, in)
	if err != nil {
		financeError(w, err, "lançamento não encontrado")
		return
	}
	s.auditFinance(r, "entry.update", map[string]any{"entry": id, "amountCents": updated.AmountCents})
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handlePayEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	var in finance.PayInput
	if !decodeOr400(w, r, &in) {
		return
	}
	paid, err := s.Finance.Pay(r.Context(), id, in)
	if err != nil {
		financeError(w, err, "lançamento não encontrado")
		return
	}
	s.auditFinance(r, "entry.pay", map[string]any{"entry": id, "paidCents": paid.PaidCents, "paidOn": paid.PaidOn})
	writeJSON(w, http.StatusOK, paid)
}

func (s *Server) handleReopenEntry(w http.ResponseWriter, r *http.Request) {
	s.entryStatus(w, r, "entry.reopen", s.Finance.Reopen)
}

func (s *Server) handleCancelEntry(w http.ResponseWriter, r *http.Request) {
	s.entryStatus(w, r, "entry.cancel", s.Finance.Cancel)
}

// entryStatus reabre ou cancela.
func (s *Server) entryStatus(w http.ResponseWriter, r *http.Request, op string,
	change func(context.Context, uuid.UUID) (*finance.Entry, error)) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	entry, err := change(r.Context(), id)
	if err != nil {
		financeError(w, err, "lançamento não encontrado")
		return
	}
	s.auditFinance(r, op, map[string]any{"entry": id, "status": entry.Status})
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	if err := s.Finance.DeleteEntry(r.Context(), id); err != nil {
		financeError(w, err, "lançamento não encontrado")
		return
	}
	s.auditFinance(r, "entry.delete", map[string]any{"entry": id})
	writeJSON(w, http.StatusNoContent, nil)
}

// ---------------------------------------------------------------------------
// Anexos
// ---------------------------------------------------------------------------

func (s *Server) handleAddAttachment(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	// O arquivo e um pouco de folga para o resto do formulário.
	limit := int64(finance.MaxAttachmentBytes + 64<<10)
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseMultipartForm(limit); err != nil {
		writeError(w, http.StatusBadRequest, "envie um arquivo de até 5 MB")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "envie o arquivo no campo file")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, finance.MaxAttachmentBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "não foi possível ler o arquivo")
		return
	}
	if _, err := s.Finance.Entry(r.Context(), id); err != nil {
		financeError(w, err, "lançamento não encontrado")
		return
	}
	attachment, err := s.Finance.AddAttachment(r.Context(), id, header.Filename, data, actor(r))
	if err != nil {
		financeError(w, err, "lançamento não encontrado")
		return
	}
	s.auditFinance(r, "attachment.add", map[string]any{"entry": id, "attachment": attachment.ID, "filename": attachment.Filename})
	writeJSON(w, http.StatusCreated, attachment)
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	attachment, data, err := s.Finance.AttachmentFile(r.Context(), id)
	if err != nil {
		financeError(w, err, "anexo não encontrado")
		return
	}
	// Sempre como download e com o tipo conferido no envio: o navegador não
	// interpreta o arquivo como página.
	w.Header().Set("Content-Type", attachment.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": attachment.Filename}))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	if err := s.Finance.DeleteAttachment(r.Context(), id); err != nil {
		financeError(w, err, "anexo não encontrado")
		return
	}
	s.auditFinance(r, "attachment.delete", map[string]any{"attachment": id})
	writeJSON(w, http.StatusNoContent, nil)
}

// ---------------------------------------------------------------------------
// Recorrências
// ---------------------------------------------------------------------------

func (s *Server) handleListRecurrences(w http.ResponseWriter, r *http.Request) {
	list, err := s.Finance.Recurrences(r.Context())
	if err != nil {
		financeError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateRecurrence(w http.ResponseWriter, r *http.Request) {
	var in finance.RecurrenceInput
	if !decodeOr400(w, r, &in) {
		return
	}
	created, err := s.Finance.CreateRecurrence(r.Context(), in)
	if err != nil {
		financeError(w, err, "")
		return
	}
	s.auditFinance(r, "recurrence.create", map[string]any{
		"recurrence": created.ID, "description": created.Description, "amountCents": created.AmountCents})
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateRecurrence(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	var in finance.RecurrenceUpdate
	if !decodeOr400(w, r, &in) {
		return
	}
	updated, err := s.Finance.UpdateRecurrence(r.Context(), id, in)
	if err != nil {
		financeError(w, err, "conta recorrente não encontrada")
		return
	}
	s.auditFinance(r, "recurrence.update", map[string]any{"recurrence": id, "amountCents": updated.AmountCents})
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleEndRecurrence(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	ended, err := s.Finance.EndRecurrence(r.Context(), id)
	if err != nil {
		financeError(w, err, "conta recorrente não encontrada")
		return
	}
	s.auditFinance(r, "recurrence.end", map[string]any{"recurrence": id})
	writeJSON(w, http.StatusOK, ended)
}

// ---------------------------------------------------------------------------
// Cadastros
// ---------------------------------------------------------------------------

// optionalID: o {id} da rota no PATCH, nil no POST.
func optionalID(w http.ResponseWriter, r *http.Request) (*uuid.UUID, bool) {
	if r.Method == http.MethodPost {
		return nil, true
	}
	id, ok := idOr400(w, r)
	return &id, ok
}

func (s *Server) handleListFinanceCategories(w http.ResponseWriter, r *http.Request) {
	list, err := s.Finance.Categories(r.Context())
	if err != nil {
		financeError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSaveFinanceCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := optionalID(w, r)
	if !ok {
		return
	}
	var in finance.CategoryInput
	if !decodeOr400(w, r, &in) {
		return
	}
	saved, err := s.Finance.SaveCategory(r.Context(), id, in)
	if err != nil {
		financeError(w, err, "categoria não encontrada")
		return
	}
	s.auditFinance(r, "category.save", map[string]any{"category": saved.ID, "name": saved.Name, "group": saved.Group})
	writeJSON(w, map[bool]int{true: http.StatusCreated, false: http.StatusOK}[id == nil], saved)
}

func (s *Server) handleListSuppliers(w http.ResponseWriter, r *http.Request) {
	list, err := s.Finance.Suppliers(r.Context())
	if err != nil {
		financeError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSaveSupplier(w http.ResponseWriter, r *http.Request) {
	id, ok := optionalID(w, r)
	if !ok {
		return
	}
	var in finance.SupplierInput
	if !decodeOr400(w, r, &in) {
		return
	}
	saved, err := s.Finance.SaveSupplier(r.Context(), id, in)
	if err != nil {
		financeError(w, err, "fornecedor não encontrado")
		return
	}
	s.auditFinance(r, "supplier.save", map[string]any{"supplier": saved.ID, "name": saved.Name})
	writeJSON(w, map[bool]int{true: http.StatusCreated, false: http.StatusOK}[id == nil], saved)
}

// ---------------------------------------------------------------------------
// Estoque
// ---------------------------------------------------------------------------

func (s *Server) handleListStockItems(w http.ResponseWriter, r *http.Request) {
	items, err := s.Finance.StockItems(r.Context())
	if err != nil {
		financeError(w, err, "")
		return
	}
	trackers, err := s.Finance.InstalledTrackers(r.Context())
	if err != nil {
		financeError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "trackers": trackers})
}

func (s *Server) handleSaveStockItem(w http.ResponseWriter, r *http.Request) {
	id, ok := optionalID(w, r)
	if !ok {
		return
	}
	var in finance.StockItemInput
	if !decodeOr400(w, r, &in) {
		return
	}
	saved, err := s.Finance.SaveStockItem(r.Context(), id, in)
	if err != nil {
		financeError(w, err, "item não encontrado")
		return
	}
	s.auditFinance(r, "stock.item.save", map[string]any{"item": saved.ID, "name": saved.Name})
	writeJSON(w, map[bool]int{true: http.StatusCreated, false: http.StatusOK}[id == nil], saved)
}

func (s *Server) handleListStockMovements(w http.ResponseWriter, r *http.Request) {
	var itemID *uuid.UUID
	if raw := r.URL.Query().Get("item"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "item inválido")
			return
		}
		itemID = &id
	}
	list, err := s.Finance.StockMovements(r.Context(), itemID, queryInt(r, "limit", 100))
	if err != nil {
		financeError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleMoveStock(w http.ResponseWriter, r *http.Request) {
	var in finance.MovementInput
	if !decodeOr400(w, r, &in) {
		return
	}
	movement, entries, err := s.Finance.MoveStock(r.Context(), in, actor(r))
	if err != nil {
		financeError(w, err, "item não encontrado")
		return
	}
	s.recordAudit(r, audit.ActionStockMoved, nil, nil, map[string]any{
		"movement": movement.ID, "item": movement.ItemID, "type": movement.Type, "quantity": movement.Quantity,
		"unitCostCents": movement.UnitCostCents, "payables": len(entries)})
	writeJSON(w, http.StatusCreated, map[string]any{"movement": movement, "payables": entries})
}

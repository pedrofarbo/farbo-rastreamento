package api

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/closing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/reports"
)

// O fechamento do mês e a nota fiscal: a central vê o que cada pagador (o
// cliente ou a associação/seguradora) teve faturado no mês, registra a nota
// emitida (número, data, valor e o PDF ou XML) e exporta a planilha para a
// contabilidade; o cliente e a organização baixam as notas deles.

func writeClosingError(w http.ResponseWriter, err error, notFound string) bool {
	var input *closing.InputError
	switch {
	case err == nil:
		return true
	case errors.As(err, &input):
		writeError(w, http.StatusBadRequest, input.Message)
	default:
		handleStoreError(w, err, notFound)
	}
	return false
}

// closingMonth é o mês da consulta (?mes=AAAA-MM); sem ele, o mês passado
// (o que se fecha no começo do mês).
func (s *Server) closingMonth(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	raw := r.URL.Query().Get("mes")
	if raw == "" {
		today := s.Billing.Today()
		return time.Date(today.Year(), today.Month()-1, 1, 0, 0, 0, 0, time.UTC), true
	}
	month, err := closing.ParseMonth(raw)
	return month, writeClosingError(w, err, "")
}

func (s *Server) handleClosing(w http.ResponseWriter, r *http.Request) {
	month, ok := s.closingMonth(w, r)
	if !ok {
		return
	}
	out, err := s.Closing.Close(r.Context(), month)
	if !writeClosingError(w, err, "fechamento não encontrado") {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// reais: 12345 → "123,45" (a planilha em português).
func reais(cents int) string {
	return strings.Replace(fmt.Sprintf("%.2f", float64(cents)/100), ".", ",", 1)
}

// handleClosingCSV: o fechamento do mês em planilha, para a contabilidade.
func (s *Server) handleClosingCSV(w http.ResponseWriter, r *http.Request) {
	month, ok := s.closingMonth(w, r)
	if !ok {
		return
	}
	out, err := s.Closing.Close(r.Context(), month)
	if !writeClosingError(w, err, "fechamento não encontrado") {
		return
	}
	rows := [][]string{{"Pagador", "Tipo", "CPF/CNPJ", "E-mail", "Linhas de fatura", "Faturado (R$)", "Pago (R$)",
		"Nota", "Emitida em", "Valor da nota (R$)"}}
	for _, l := range out.Lines {
		kind := "Cliente"
		if l.Kind == "organization" {
			kind = "Associação/seguradora"
		}
		row := []string{l.Name, kind, l.Document, l.Email, strconv.Itoa(l.Invoices), reais(l.AmountCents), reais(l.PaidCents), "", "", ""}
		if l.Note != nil {
			row[7], row[8], row[9] = l.Note.Number, l.Note.IssuedOn.Format("02/01/2006"), reais(l.Note.AmountCents)
		}
		rows = append(rows, row)
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="farbo-fechamento-`+out.Month+`.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	_ = reports.WriteCSV(w, rows)
}

// handleSaveFiscalNote registra a nota emitida (formulário com o arquivo):
// customerId, month (AAAA-MM), number, issuedOn (AAAA-MM-DD), amountCents e
// file (opcional; sem ele, fica o que já tinha).
func (s *Server) handleSaveFiscalNote(w http.ResponseWriter, r *http.Request) {
	limit := int64(closing.MaxFileBytes + 64<<10)
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
	customerID, err := uuid.Parse(r.FormValue("customerId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "cliente inválido")
		return
	}
	month, err := closing.ParseMonth(r.FormValue("month"))
	if !writeClosingError(w, err, "") {
		return
	}
	issued, err := time.Parse(time.DateOnly, r.FormValue("issuedOn"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "data de emissão inválida (use AAAA-MM-DD)")
		return
	}
	amount, err := strconv.Atoi(r.FormValue("amountCents"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "valor da nota inválido")
		return
	}
	in := closing.NoteInput{
		CustomerID: customerID, Month: month, Number: r.FormValue("number"),
		IssuedOn: billing.Date{Time: issued}, AmountCents: amount,
	}
	if file, header, err := r.FormFile("file"); err == nil {
		defer file.Close()
		if in.Data, err = io.ReadAll(io.LimitReader(file, closing.MaxFileBytes+1)); err != nil {
			writeError(w, http.StatusBadRequest, "não foi possível ler o arquivo")
			return
		}
		in.Filename = header.Filename
	} else if !errors.Is(err, http.ErrMissingFile) {
		writeError(w, http.StatusBadRequest, "não foi possível ler o arquivo")
		return
	}
	note, err := s.Closing.SaveNote(r.Context(), in, actor(r))
	if !writeClosingError(w, err, "cliente não encontrado") {
		return
	}
	s.recordBillingAudit(r, audit.ActionFiscalNoteSaved, map[string]any{
		"customerId": customerID, "month": note.Month, "number": note.Number, "amountCents": note.AmountCents, "file": note.HasFile,
	})
	writeJSON(w, http.StatusOK, note)
}

func (s *Server) handleDeleteFiscalNote(w http.ResponseWriter, r *http.Request) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	note, err := s.Closing.DeleteNote(r.Context(), id)
	if !writeClosingError(w, err, "nota não encontrada") {
		return
	}
	s.recordBillingAudit(r, audit.ActionFiscalNoteSaved, map[string]any{
		"customerId": note.CustomerID, "month": note.Month, "number": note.Number, "deleted": true,
	})
	w.WriteHeader(http.StatusNoContent)
}

// writeNoteFile manda o arquivo da nota para baixar (de owner, se não nulo).
func (s *Server) writeNoteFile(w http.ResponseWriter, r *http.Request, owner *uuid.UUID) {
	id, err := urlUUID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	f, err := s.Closing.NoteFile(r.Context(), id, owner)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeError(w, http.StatusNotFound, "nota não encontrada")
			return
		}
		handleStoreError(w, err, "nota não encontrada")
		return
	}
	name := f.Note.Filename
	if name == "" {
		name = "nota-" + f.Note.Number
	}
	// Sempre como download e com o tipo conferido no envio.
	w.Header().Set("Content-Type", f.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Content-Length", strconv.Itoa(len(f.Data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(f.Data)
}

func (s *Server) handleFiscalNoteFile(w http.ResponseWriter, r *http.Request) {
	s.writeNoteFile(w, r, nil)
}

// handleMyFiscalNotes: as notas da conta do cliente.
func (s *Server) handleMyFiscalNotes(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	list, err := s.Closing.Notes(r.Context(), customerID)
	if !writeClosingError(w, err, "notas não encontradas") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleMyFiscalNoteFile(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	s.writeNoteFile(w, r, &customerID)
}

// handleOrgFiscalNotes: as notas da associação ou seguradora.
func (s *Server) handleOrgFiscalNotes(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	list, err := s.Closing.Notes(r.Context(), orgID)
	if !writeClosingError(w, err, "notas não encontradas") {
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleOrgFiscalNoteFile(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgOf(r)
	s.writeNoteFile(w, r, &orgID)
}

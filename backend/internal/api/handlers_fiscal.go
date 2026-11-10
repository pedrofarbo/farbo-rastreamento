package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fiscal"
)

// As notas fiscais de serviço (NFS-e) das faturas pagas, pela Notaas: a
// central emite de novo a recusada e baixa o PDF e o XML; o cliente baixa os
// das dele; o webhook da Notaas avisa quando a nota muda.

func writeFiscalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fiscal.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, fiscal.ErrNotRetryable):
		writeError(w, http.StatusConflict, "só a nota recusada pode ser emitida de novo")
	default:
		handleStoreError(w, err, "nota fiscal não encontrada")
	}
}

func (s *Server) handleRetryFiscalDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := idOr400(w, r)
	if !ok {
		return
	}
	d, err := s.Fiscal.Retry(r.Context(), id)
	if err != nil {
		writeFiscalError(w, err)
		return
	}
	s.Log.Info("nota fiscal mandada emitir de novo", "document", id, "by", personOf(r))
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleFiscalDocumentPDF(w http.ResponseWriter, r *http.Request) {
	if id, ok := idOr400(w, r); ok {
		s.writeFiscalFile(w, r, id, nil, "pdf")
	}
}

func (s *Server) handleFiscalDocumentXML(w http.ResponseWriter, r *http.Request) {
	if id, ok := idOr400(w, r); ok {
		s.writeFiscalFile(w, r, id, nil, "xml")
	}
}

// handleMyFiscalDocuments: as notas do cliente, pela fatura.
func (s *Server) handleMyFiscalDocuments(w http.ResponseWriter, r *http.Request) {
	customerID, _ := customerOf(r)
	list, err := s.Fiscal.ForCustomer(r.Context(), customerID)
	if err != nil {
		writeFiscalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleMyFiscalDocumentPDF(w http.ResponseWriter, r *http.Request) {
	if id, ok := idOr400(w, r); ok {
		customerID, _ := customerOf(r)
		s.writeFiscalFile(w, r, id, &customerID, "pdf")
	}
}

func (s *Server) handleMyFiscalDocumentXML(w http.ResponseWriter, r *http.Request) {
	if id, ok := idOr400(w, r); ok {
		customerID, _ := customerOf(r)
		s.writeFiscalFile(w, r, id, &customerID, "xml")
	}
}

// writeFiscalFile entrega o arquivo da nota — o DANFSE (pdf) ou o XML —
// para baixar pela tela com o login.
func (s *Server) writeFiscalFile(w http.ResponseWriter, r *http.Request, id uuid.UUID, owner *uuid.UUID, kind string) {
	get, label, contentType := s.Fiscal.PDF, "PDF", "application/pdf"
	if kind == "xml" {
		get, label, contentType = s.Fiscal.XML, "XML", "application/xml"
	}
	file, number, err := get(r.Context(), id, owner)
	if err != nil {
		var api *fiscal.APIError
		if errors.As(err, &api) {
			s.Log.Warn(label+" da nota fiscal indisponível na Notaas", "document", id, "err", err)
			writeError(w, http.StatusBadGateway, "o "+label+" da nota não está disponível agora: tente de novo em instantes")
			return
		}
		writeFiscalError(w, err)
		return
	}
	name := "nota-fiscal." + kind
	if number != "" {
		name = "nfse-" + number + "." + kind
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(file.Body)
}

// handleNotaasWebhook: a Notaas avisa que a nota mudou (assinado com o
// segredo do endpoint); a rotina consulta a nota na API, nunca confia no
// corpo.
func (s *Server) handleNotaasWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	if !fiscal.VerifySignature(s.Config.Fiscal.NotaasWebhookSecret, body, r.Header.Get("X-Notaas-Signature")) {
		s.Log.Warn("webhook da Notaas com assinatura inválida ou ausente", "ip", clientIP(r))
		writeError(w, http.StatusUnauthorized, "webhook não autorizado")
		return
	}
	event, err := fiscal.ParseWebhook(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "evento inválido")
		return
	}
	if event.Data.InvoiceID != "" {
		if err := s.Fiscal.Touch(r.Context(), event.Data.InvoiceID); err != nil {
			s.Log.Error("falha ao processar o webhook da Notaas", "event", event.Event, "err", err)
			writeError(w, http.StatusInternalServerError, "falha ao processar o evento")
			return
		}
	}
	s.Log.Info("webhook da Notaas recebido", "event", event.Event, "notaas", event.Data.InvoiceID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

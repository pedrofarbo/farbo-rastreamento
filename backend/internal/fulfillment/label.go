package fulfillment

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// ErrLabelNotPDF: o Melhor Envios entregou a etiqueta como página para
// imprimir, não como PDF (o painel abre a impressão).
var ErrLabelNotPDF = errors.New("o Melhor Envios entregou a etiqueta como página para imprimir")

// Label busca a etiqueta comprada para baixar: pede ao Melhor Envios um link
// de impressão novo (e guarda no acompanhamento, para o "Imprimir") e baixa
// o arquivo. Devolve o PDF; se vier a página de impressão, ErrLabelNotPDF com
// o link.
func (s *Service) Label(ctx context.Context, id uuid.UUID) (*Fulfillment, []byte, string, error) {
	if err := s.requireCarrier(); err != nil {
		return nil, nil, "", err
	}
	f, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, nil, "", err
	}
	if f.ShippingOrderID == nil {
		return f, nil, "", RuleError{"este pedido não tem etiqueta comprada"}
	}
	link, err := s.carrier.Print(ctx, *f.ShippingOrderID)
	switch {
	case err != nil || link == "":
		if f.LabelURL == "" {
			if err == nil {
				err = RuleError{"o Melhor Envios não devolveu o link da etiqueta"}
			}
			return f, nil, "", err
		}
		s.log.Warn("link novo da etiqueta indisponível; usando o guardado", "fulfillment", id, "err", err)
		link = f.LabelURL
	case link != f.LabelURL:
		if err := s.repo.updateShipping(ctx, s.db, id, ShippingUpdate{LabelURL: &link}); err != nil {
			s.log.Warn("falha ao guardar o link novo da etiqueta", "fulfillment", id, "err", err)
		}
		f.LabelURL = link
	}
	body, contentType, err := s.carrier.FetchLabel(ctx, link)
	if err != nil {
		return f, nil, link, err
	}
	if strings.Contains(strings.ToLower(contentType), "application/pdf") || bytes.HasPrefix(body, []byte("%PDF")) {
		return f, body, link, nil
	}
	s.log.Info("a etiqueta do Melhor Envios não veio em PDF", "fulfillment", id, "content_type", contentType)
	return f, nil, link, ErrLabelNotPDF
}

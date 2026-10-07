// Package topups guarda as recargas da carteira do Melhor Envios geradas
// pelo painel. A recarga por Pix pode ser paga pela AbacatePay: ela vira uma
// conta a pagar (Frete e envio) e sai pelo mesmo Pix dos fornecedores, com a
// confirmação extra, a conferência de quando a AbacatePay não responde e a
// tarifa nas contas.
package topups

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/melhorenvio"
)

// categoryNames: onde a recarga entra nas contas (a primeira ativa).
var categoryNames = []string{"Frete e envio", "Outras despesas"}

type Service struct {
	db      *database.DB
	carrier *melhorenvio.Client
	finance *finance.Service
	company config.Company
	appURL  string
	log     *slog.Logger
}

func NewService(db *database.DB, carrier *melhorenvio.Client, fin *finance.Service, company config.Company,
	appURL string, log *slog.Logger) *Service {
	return &Service{db: db, carrier: carrier, finance: fin, company: company, appURL: appURL,
		log: log.With("component", "topups")}
}

// PayWithAbacate diz se a recarga por Pix pode sair pela AbacatePay.
func (s *Service) PayWithAbacate() bool { return s.finance != nil && s.finance.PixEnabled() }

// TopUp é uma recarga gerada.
type TopUp struct {
	ID         uuid.UUID `json:"id"`
	ProviderID string    `json:"providerId"`
	Protocol   string    `json:"protocol"`
	// Status é o do Melhor Envios quando a cobrança foi gerada.
	Status     string     `json:"status"`
	Method     string     `json:"method"`
	ValueCents int        `json:"valueCents"`
	Link       string     `json:"link"`
	Digitable  string     `json:"digitable"`
	PixCode    string     `json:"pixCode"`
	EntryID    *uuid.UUID `json:"entryId"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// Create gera a cobrança no Melhor Envios (o boleto em nome da empresa) e a
// guarda. Depois de paga, o Melhor Envios volta para Pedidos.
func (s *Service) Create(ctx context.Context, method string, valueCents int, by *uuid.UUID) (*TopUp, error) {
	charge, err := s.carrier.AddBalance(ctx, melhorenvio.TopUpRequest{
		Method: method, ValueCents: valueCents, RedirectURL: s.appURL + "/pedidos?saldo=pago",
		CompanyName: s.company.LegalName, CNPJ: s.company.CNPJ,
	})
	if err != nil {
		return nil, err
	}
	if method == melhorenvio.TopUpPix && charge.PixCode == "" {
		// O formato da resposta não é documentado: os campos que vieram
		// (sem os valores) ajudam a achar o copia-e-cola.
		s.log.Warn("recarga do Melhor Envios sem o Pix copia-e-cola na resposta",
			"protocol", charge.Protocol, "link", charge.Link != "", "campos", charge.Shape)
	}
	top := &TopUp{
		ProviderID: charge.ID, Protocol: charge.Protocol, Status: charge.Status, Method: method, ValueCents: valueCents,
		Link: charge.Link, Digitable: charge.Digitable, PixCode: charge.PixCode,
	}
	err = s.db.QueryRow(ctx, `
		INSERT INTO shipping_topups (provider_id, protocol, method, value_cents, link, digitable, pix_code, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`,
		top.ProviderID, top.Protocol, method, valueCents, top.Link, top.Digitable, top.PixCode, by,
	).Scan(&top.ID, &top.CreatedAt)
	if err != nil {
		// A cobrança existe no Melhor Envios; só não ficou guardada aqui.
		s.log.Error("recarga gerada e não guardada", "protocol", top.Protocol, "err", err)
		return nil, database.MapError(err)
	}
	return top, nil
}

// Entry devolve a conta a pagar da recarga por Pix, para pagar pela
// AbacatePay; cria na primeira vez (uma por recarga: dois cliques não abrem
// duas contas). code é o copia-e-cola colado no painel, quando a resposta do
// Melhor Envios não trouxe.
func (s *Service) Entry(ctx context.Context, id uuid.UUID, code string, by *uuid.UUID) (uuid.UUID, error) {
	if !s.PayWithAbacate() {
		return uuid.Nil, finance.ErrPixDisabled
	}
	// Colado de uma página, pode vir quebrado em linhas; os espaços de dentro
	// (no nome do recebedor) fazem parte do código.
	code = strings.TrimSpace(strings.NewReplacer("\r", "", "\n", "", "\t", "").Replace(code))
	var entryID uuid.UUID
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var method, protocol, stored, entryStatus string
		var valueCents int
		var current *uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT t.method, t.protocol, t.value_cents, t.pix_code, t.entry_id, COALESCE(e.status, '')
			FROM shipping_topups t LEFT JOIN finance_entries e ON e.id = t.entry_id
			WHERE t.id = $1 FOR UPDATE OF t`, id,
		).Scan(&method, &protocol, &valueCents, &stored, &current, &entryStatus)
		if err != nil {
			return database.MapError(err)
		}
		if method != melhorenvio.TopUpPix {
			return finance.ValidationError{Message: "Só a recarga por Pix sai pela AbacatePay."}
		}
		if stored == "" {
			if code == "" {
				return finance.ValidationError{Message: "Cole o Pix copia-e-cola da recarga (fica na página do Pix do Melhor Envios)."}
			}
			stored = code
		}
		parsed, ok := finance.ParseBRCode(stored)
		if !ok {
			return finance.ValidationError{Message: "Pix copia-e-cola inválido: copie o código inteiro na página do Pix do Melhor Envios."}
		}
		if parsed.AmountCents > 0 && parsed.AmountCents != int64(valueCents) {
			return finance.ValidationError{Message: fmt.Sprintf("O Pix copia-e-cola é de %s e a recarga é de %s: confira se é o Pix desta recarga.",
				brl(parsed.AmountCents), brl(int64(valueCents)))}
		}
		switch {
		case current != nil && entryStatus == finance.StatusPaid:
			return finance.ValidationError{Message: "Esta recarga já está paga."}
		case current != nil && entryStatus == finance.StatusOpen:
			entryID = *current
			return nil
		}
		categoryID, err := category(ctx, tx)
		if err != nil {
			return err
		}
		description := "Recarga da carteira do Melhor Envios"
		if protocol != "" {
			description += " (" + protocol + ")"
		}
		entryID, err = s.finance.CreateTx(ctx, tx, finance.EntryInput{
			Kind: finance.KindPayable, Description: description, CategoryID: categoryID,
			AmountCents: int64(valueCents), DueDate: s.finance.Today(), PaymentCode: stored,
			Notes: "Saldo para as etiquetas de envio, gerado em Pedidos.",
		}, by)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE shipping_topups SET entry_id = $2, pix_code = $3 WHERE id = $1`, id, entryID, stored)
		return err
	})
	if err != nil {
		var v finance.ValidationError
		if errors.As(err, &v) {
			return uuid.Nil, v
		}
		return uuid.Nil, database.MapError(err)
	}
	return entryID, nil
}

// category é a categoria da recarga nas contas.
func category(ctx context.Context, q database.Querier) (uuid.UUID, error) {
	for _, name := range categoryNames {
		var id uuid.UUID
		err := q.QueryRow(ctx, `
			SELECT id FROM finance_categories WHERE kind = 'EXPENSE' AND active AND lower(name) = lower($1)`, name).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, err
		}
	}
	return uuid.Nil, finance.ValidationError{Message: "Crie a categoria de despesa \"Frete e envio\" em Empresa para lançar a recarga."}
}

// brl: 12345 → "R$ 123,45".
func brl(cents int64) string {
	reais := fmt.Sprintf("%d", cents/100)
	for i := len(reais) - 3; i > 0; i -= 3 {
		reais = reais[:i] + "." + reais[i:]
	}
	return fmt.Sprintf("R$ %s,%02d", reais, cents%100)
}

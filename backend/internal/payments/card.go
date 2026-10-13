package payments

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
)

// O cartão de crédito: pagar faturas no cartão (com o token que o formulário
// do navegador gera, ou com o cartão salvo da conta), guardar o cartão
// (cifrado) com a autorização do débito automático e conferir as cobranças
// que ficaram sem resposta. O débito automático está em debit.go.
//
// Cada cobrança no cartão é gravada antes de ir à AbacatePay (CREATING, com o
// nosso id em metadata.chargeId): erro de rede não é recusa — a cobrança pode
// ter passado —, e é por esse id que ReconcileCards a acha lá depois. Assim
// nem a tela nem o débito automático cobram duas vezes.

var (
	// ErrCardDisabled: sem a chave pública, o cartão está desligado.
	ErrCardDisabled = errors.New("o pagamento com cartão não está disponível")
	// ErrCardPayerIncomplete: o cartão exige os quatro dados do pagador.
	ErrCardPayerIncomplete = errors.New("para pagar com cartão, o cadastro precisa do nome, e-mail, CPF ou CNPJ válido e celular")
	// ErrNoSavedCard: a conta não tem cartão salvo.
	ErrNoSavedCard = errors.New("a conta não tem cartão salvo")
	// ErrCardTokenMissing: nem o cartão do formulário nem o salvo.
	ErrCardTokenMissing = errors.New("informe o cartão")
	// ErrCardUnknown: a AbacatePay não respondeu e a cobrança pode ter
	// passado; a conferência decide em alguns minutos.
	ErrCardUnknown = errors.New("não deu para confirmar o pagamento com o banco agora; não tente de novo: em alguns minutos a fatura mostra se foi paga")
)

const (
	// cardCheckAfter: a conferência procura a cobrança sem resposta depois
	// disso; cardGiveUp: sem achar até aqui, ela não chegou à AbacatePay.
	cardCheckAfter = time.Minute
	cardGiveUp     = 30 * time.Minute
)

// CardInput é o pagamento no cartão: o token do formulário (ctok_..., vale
// 60 s) ou o cartão salvo da conta.
type CardInput struct {
	Token    string
	UseSaved bool
	// Save guarda o cartão para as próximas; AutoDebit liga o débito
	// automático (a autorização de By).
	Save      bool
	AutoDebit bool
	By        *uuid.UUID
	// Metadata a mais para a AbacatePay (o débito automático manda o dele).
	Metadata map[string]string
}

// SavedCard é o cartão salvo da conta, como a tela mostra (o token não sai
// daqui).
type SavedCard struct {
	Brand     string     `json:"brand"`
	Last4     string     `json:"last4"`
	AutoDebit bool       `json:"autoDebit"`
	ConsentAt *time.Time `json:"consentAt"`
	// A última recusa do débito automático.
	FailedAt  *time.Time `json:"failedAt"`
	LastError string     `json:"lastError"`
	CreatedAt time.Time  `json:"createdAt"`
}

// SetCardSecret liga a cifra do cartão salvo, com a chave derivada do
// segredo do servidor (como os tokens das integrações). Trocar o segredo
// invalida os cartões salvos: os clientes cadastram de novo.
func (s *Service) SetCardSecret(secret string) error {
	key := sha256.Sum256([]byte("farbo:card-tokens:" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	s.cardKey = gcm
	return nil
}

// CardEnabled diz se o cartão de crédito está ligado (a chave pública
// configurada e a cifra do cartão salvo pronta).
func (s *Service) CardEnabled() bool { return s.Enabled() && s.cfg.CardEnabled() && s.cardKey != nil }

// CardPublicKey é a chave que o navegador usa para montar o formulário.
func (s *Service) CardPublicKey() string {
	if !s.CardEnabled() {
		return ""
	}
	return s.cfg.PublicKey
}

// CardForInvoices cobra no cartão as faturas em aberto do cliente (uma ou
// várias, pelo total). Volta a cobrança sempre que ela foi gravada — também
// com o erro: recusada (APIError, a cobrança FAILED) ou sem resposta
// (ErrCardUnknown, a cobrança CREATING até a conferência).
func (s *Service) CardForInvoices(ctx context.Context, customerID uuid.UUID, invoiceIDs []uuid.UUID, in CardInput, payer *Payer) (*Charge, error) {
	if !s.CardEnabled() {
		return nil, ErrCardDisabled
	}
	invoices, total, err := s.openInvoices(ctx, customerID, invoiceIDs)
	if err != nil {
		return nil, err
	}
	customer, ok := cardCustomerFor(payer)
	if !ok {
		return nil, ErrCardPayerIncomplete
	}
	req := abacatepay.CardRequest{AmountCents: total, Customer: customer}
	switch {
	case in.UseSaved:
		token, err := s.savedCardToken(ctx, customerID)
		if err != nil {
			return nil, err
		}
		req.CardToken = token
	case strings.TrimSpace(in.Token) != "":
		req.Token = strings.TrimSpace(in.Token)
	default:
		return nil, ErrCardTokenMissing
	}

	ids := make([]uuid.UUID, len(invoices))
	items := make([]ChargeInvoice, len(invoices))
	for i, inv := range invoices {
		ids[i] = inv.ID
		items[i] = ChargeInvoice{InvoiceID: inv.ID, AmountCents: inv.AmountCents}
	}
	id := uuid.New()
	req.Metadata = map[string]string{"chargeId": id.String(), "invoiceId": ids[0].String(), "customerId": customerID.String()}
	if len(ids) > 1 {
		req.Metadata["invoices"] = fmt.Sprint(len(ids))
	}
	for k, v := range in.Metadata {
		req.Metadata[k] = v
	}
	charge := &Charge{
		ID: id, InvoiceID: ids[0], Provider: providerAbacatePay, Method: MethodCard,
		ProviderChargeID: "creating:" + id.String(), AmountCents: total, Status: StatusCreating,
		DevMode: abacatepay.IsDevKey(s.cfg.AbacatePayAPIKey),
	}
	if len(ids) > 1 {
		err = s.repo.InsertGroup(ctx, charge, items)
	} else {
		err = s.repo.Insert(ctx, charge)
	}
	if err != nil {
		return nil, err
	}

	card, err := s.gateway.CreateCard(ctx, req)
	var apiErr *abacatepay.APIError
	switch {
	case errors.As(err, &apiErr):
		// Recusada: não houve cobrança.
		if serr := s.repo.SetStatus(ctx, id, abacatepay.StatusFailed, false); serr != nil {
			return nil, serr
		}
		s.log.Info("cartão recusado", "charge", id, "customer", customerID, "err", apiErr.Message)
		charge.Status = abacatepay.StatusFailed
		return charge, err
	case err != nil:
		s.log.Warn("cartão sem resposta da AbacatePay; a conferência decide", "charge", id, "customer", customerID, "err", err)
		return s.withInvoiceStatus(ctx, charge), ErrCardUnknown
	}
	if err := s.recordCard(ctx, charge, card); err != nil {
		return nil, err
	}
	if in.Save && card.CardToken != "" && card.Status != abacatepay.StatusFailed {
		// O pagamento passou; se só guardar o cartão falhar, a fatura não volta.
		if err := s.saveCard(ctx, customerID, card, in.AutoDebit, in.By); err != nil {
			s.log.Error("cartão pago, mas não guardado", "customer", customerID, "err", err)
		}
	}
	s.log.Info("cobrança no cartão", "charge", id, "customer", customerID, "invoices", len(ids), "amount_cents", total,
		"status", card.Status, "saved_card", in.UseSaved, "dev_mode", card.DevMode)
	return s.Charge(ctx, id)
}

// recordCard grava a cobrança que o provedor criou e aplica o status dela.
func (s *Service) recordCard(ctx context.Context, charge *Charge, card *abacatepay.Card) error {
	amount := card.Amount
	if amount <= 0 {
		amount = charge.AmountCents
	}
	if err := s.repo.SetProviderCharge(ctx, charge.ID, card.ID, amount, card.Brand, card.LastFour, card.DevMode,
		card.PlatformFee, card.ExpiresAt); err != nil {
		return err
	}
	charge.ProviderChargeID, charge.AmountCents, charge.Status = card.ID, amount, abacatepay.StatusPending
	charge.CardBrand, charge.CardLast4, charge.DevMode = card.Brand, card.LastFour, card.DevMode
	return s.apply(ctx, charge, card.Status)
}

// ReconcileCards procura na AbacatePay as cobranças de cartão que ficaram sem
// resposta: achou, grava e aplica; passado cardGiveUp sem achar, ela não
// chegou lá (FAILED).
func (s *Service) ReconcileCards(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	list, err := s.repo.Creating(ctx, cardCheckAfter, 50)
	if err != nil {
		s.log.Warn("falha ao listar cobranças de cartão sem resposta", "err", err)
		return
	}
	for _, charge := range list {
		card, err := s.gateway.FindCard(ctx, charge.CreatedAt.Add(-time.Hour), "chargeId", charge.ID.String())
		switch {
		case err != nil:
			s.log.Warn("falha ao conferir cobrança de cartão", "charge", charge.ID, "err", err)
		case card != nil:
			if err := s.recordCard(ctx, charge, card); err != nil {
				s.log.Error("falha ao gravar cobrança de cartão conferida", "charge", charge.ID, "err", err)
			} else {
				s.log.Info("cobrança de cartão conferida", "charge", charge.ID, "provider_charge", card.ID, "status", card.Status)
			}
		case time.Since(charge.CreatedAt) > cardGiveUp:
			if err := s.repo.SetStatus(ctx, charge.ID, abacatepay.StatusFailed, false); err != nil {
				s.log.Error("falha ao encerrar cobrança de cartão sem resposta", "charge", charge.ID, "err", err)
			} else {
				s.log.Info("cobrança de cartão não chegou à AbacatePay", "charge", charge.ID)
			}
		}
	}
}

// openInvoices são as faturas pedidas (sem repetir), todas do cliente e em
// aberto, da primeira a vencer à última, com o total.
func (s *Service) openInvoices(ctx context.Context, customerID uuid.UUID, invoiceIDs []uuid.UUID) ([]*billing.Invoice, int, error) {
	seen := map[uuid.UUID]bool{}
	unique := make([]uuid.UUID, 0, len(invoiceIDs))
	for _, id := range invoiceIDs {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	switch {
	case len(unique) == 0:
		return nil, 0, ErrNoInvoices
	case len(unique) > MaxGroupInvoices:
		return nil, 0, fmt.Errorf("%w: no máximo %d de uma vez", ErrNoInvoices, MaxGroupInvoices)
	}
	invoices, err := s.invoices.GetInvoices(ctx, unique)
	if err != nil {
		return nil, 0, err
	}
	if len(invoices) != len(unique) {
		return nil, 0, database.ErrNotFound
	}
	total := 0
	for _, inv := range invoices {
		if inv.CustomerID != customerID {
			return nil, 0, database.ErrNotFound
		}
		if inv.Status != billing.InvoiceOpen || inv.AmountCents <= 0 {
			return nil, 0, ErrInvoiceNotOpen
		}
		total += inv.AmountCents
	}
	sortByDue(invoices)
	return invoices, total, nil
}

// ---------------------------------------------------------------------------
// O cartão salvo
// ---------------------------------------------------------------------------

// SavedCard é o cartão salvo da conta (nil: nenhum).
func (s *Service) SavedCard(ctx context.Context, customerID uuid.UUID) (*SavedCard, error) {
	var c SavedCard
	err := s.repo.db.QueryRow(ctx, `
		SELECT brand, last4, auto_debit, consent_at, failed_at, last_error, created_at
		FROM customer_cards WHERE customer_id = $1`, customerID).
		Scan(&c.Brand, &c.Last4, &c.AutoDebit, &c.ConsentAt, &c.FailedAt, &c.LastError, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return &c, nil
}

// SetAutoDebit liga (com a autorização de quem pediu) ou desliga o débito
// automático no cartão salvo.
func (s *Service) SetAutoDebit(ctx context.Context, customerID uuid.UUID, on bool, by uuid.UUID) error {
	tag, err := s.repo.db.Exec(ctx, `
		UPDATE customer_cards SET auto_debit = $2,
			consent_at = CASE WHEN $2 THEN NOW() ELSE consent_at END,
			consent_by = CASE WHEN $2 THEN $3 ELSE consent_by END,
			failed_at = CASE WHEN $2 THEN NULL ELSE failed_at END,
			last_error = CASE WHEN $2 THEN '' ELSE last_error END,
			updated_at = NOW()
		WHERE customer_id = $1`, customerID, on, by)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoSavedCard
	}
	return nil
}

// RemoveCard apaga o cartão salvo (e com ele o débito automático).
func (s *Service) RemoveCard(ctx context.Context, customerID uuid.UUID) error {
	tag, err := s.repo.db.Exec(ctx, `DELETE FROM customer_cards WHERE customer_id = $1`, customerID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoSavedCard
	}
	return nil
}

// saveCard guarda (ou troca) o cartão da conta, cifrado.
func (s *Service) saveCard(ctx context.Context, customerID uuid.UUID, card *abacatepay.Card, autoDebit bool, by *uuid.UUID) error {
	sealed, err := s.sealCard(card.CardToken)
	if err != nil {
		return err
	}
	_, err = s.repo.db.Exec(ctx, `
		INSERT INTO customer_cards (customer_id, card_token, brand, last4, auto_debit, consent_at, consent_by)
		VALUES ($1, $2, $3, $4, $5, CASE WHEN $5 THEN NOW() END, CASE WHEN $5 THEN $6::uuid END)
		ON CONFLICT (customer_id) DO UPDATE SET card_token = EXCLUDED.card_token, brand = EXCLUDED.brand,
			last4 = EXCLUDED.last4, auto_debit = EXCLUDED.auto_debit, consent_at = EXCLUDED.consent_at,
			consent_by = EXCLUDED.consent_by, failed_at = NULL, last_error = '', updated_at = NOW()`,
		customerID, sealed, card.Brand, card.LastFour, autoDebit, by)
	return database.MapError(err)
}

func (s *Service) savedCardToken(ctx context.Context, customerID uuid.UUID) (string, error) {
	var sealed string
	err := s.repo.db.QueryRow(ctx, `SELECT card_token FROM customer_cards WHERE customer_id = $1`, customerID).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoSavedCard
	}
	if err != nil {
		return "", database.MapError(err)
	}
	return s.openCard(sealed)
}

func (s *Service) sealCard(plain string) (string, error) {
	nonce := make([]byte, s.cardKey.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return "v1:" + base64.StdEncoding.EncodeToString(s.cardKey.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (s *Service) openCard(sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, "v1:"))
	if err != nil || len(raw) < s.cardKey.NonceSize() {
		return "", errors.New("cartão salvo ilegível")
	}
	plain, err := s.cardKey.Open(nil, raw[:s.cardKey.NonceSize()], raw[s.cardKey.NonceSize():], nil)
	if err != nil {
		// Segredo do servidor trocado: o cliente cadastra o cartão de novo.
		return "", ErrNoSavedCard
	}
	return string(plain), nil
}

// CardPayer são os dados do pagador que o formulário do cartão pede (os
// mesmos da cobrança), ou nil se falta algum dos quatro.
func CardPayer(p *Payer) *abacatepay.Customer {
	if c, ok := cardCustomerFor(p); ok {
		return &c
	}
	return nil
}

// cardCustomerFor é o pagador do cartão: os quatro dados, válidos.
func cardCustomerFor(p *Payer) (abacatepay.Customer, bool) {
	if c := customerFor(p); c != nil {
		return *c, true
	}
	return abacatepay.Customer{}, false
}

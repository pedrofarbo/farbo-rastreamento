package finance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
)

// Pagar fornecedores por Pix pela AbacatePay. O Pix sai do saldo da conta
// da AbacatePay para a chave do fornecedor ou para o Pix copia-e-cola da
// conta. O registro do envio nasce antes da chamada (SENDING) e uma conta
// tem no máximo um envio vivo: dois cliques não pagam duas vezes. Saiu
// (COMPLETE), a conta é baixada e a tarifa entra nas contas; a AbacatePay
// recusou (FAILED), a conta continua em aberto. Sem resposta (a rede caiu),
// o envio fica UNKNOWN até alguém conferir no painel da AbacatePay — nunca
// é reenviado sozinho. Um envio pode falhar depois de enviado (o dinheiro
// volta ao saldo): o acompanhamento reabre a conta.

// PixSender é a AbacatePay (ou o falso dos testes).
type PixSender interface {
	SendPix(ctx context.Context, req abacatepay.TransferRequest) (*abacatepay.Transfer, error)
	GetTransfer(ctx context.Context, id string) (*abacatepay.Transfer, error)
	Balance(ctx context.Context) (*abacatepay.Balance, error)
	// PayoutsSince conta os saques do mês (entram na conta da tarifa).
	PayoutsSince(ctx context.Context, since time.Time) (int, error)
}

// Status de um envio.
const (
	PixSending  = "SENDING"
	PixComplete = "COMPLETE"
	PixFailed   = "FAILED"
	PixUnknown  = "UNKNOWN"
)

const (
	// pixLostAfter: um envio SENDING sem resposta há mais que isso é de um
	// servidor que parou no meio — vira UNKNOWN.
	pixLostAfter = 5 * time.Minute
	// pixWatchFor: por quanto tempo um envio feito ainda é conferido (pode
	// falhar depois); pixCheckEvery é o intervalo entre as conferências.
	pixWatchFor   = 72 * time.Hour
	pixCheckEvery = 15 * time.Minute
	// feeCategory é onde entra a tarifa de cada envio.
	feeCategory = "Taxas de pagamento"
)

// SetPixSender liga o pagamento por Pix (devMode: a chave é de testes).
func (s *Service) SetPixSender(sender PixSender, devMode bool) {
	s.pix, s.pixDev = sender, devMode
}

// PixEnabled diz se dá para pagar por Pix.
func (s *Service) PixEnabled() bool { return s.pix != nil }

// PixTransfer é um envio (o último de cada conta vem junto com ela).
type PixTransfer struct {
	ID         uuid.UUID `json:"id"`
	EntryID    uuid.UUID `json:"entryId"`
	ProviderID string    `json:"providerId"`
	Status     string    `json:"status"`
	// AmountCents é o que o fornecedor deve receber; SentCents, o que foi
	// pedido à AbacatePay (com a tarifa, que ela desconta); DeliveredCents, o
	// que chegou (SentCents menos a tarifa cobrada).
	AmountCents    int64      `json:"amountCents"`
	SentCents      int64      `json:"sentCents"`
	DeliveredCents int64      `json:"deliveredCents"`
	FeeCents       int64      `json:"feeCents"`
	Key            string     `json:"key"`
	KeyType        string     `json:"keyType"`
	ReceiptURL     string     `json:"receiptUrl"`
	DevMode        bool       `json:"devMode"`
	Error          string     `json:"error"`
	CreatedAt      time.Time  `json:"createdAt"`
	CompletedAt    *time.Time `json:"completedAt"`
}

const pixColumns = `id, entry_id, COALESCE(provider_id, ''), status, amount_cents, sent_cents, fee_cents, pix_key, key_type,
	receipt_url, dev_mode, error, created_at, completed_at`

func scanPix(row database.Scanner) (*PixTransfer, error) {
	var p PixTransfer
	err := row.Scan(&p.ID, &p.EntryID, &p.ProviderID, &p.Status, &p.AmountCents, &p.SentCents, &p.FeeCents, &p.Key, &p.KeyType,
		&p.ReceiptURL, &p.DevMode, &p.Error, &p.CreatedAt, &p.CompletedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	if p.Status == PixComplete {
		p.DeliveredCents = p.SentCents - p.FeeCents
	}
	return &p, nil
}

// transferFee é a tarifa do próximo envio: conta os Pix enviados pelo sistema
// no mês e os saques (o dono também saca pelo painel da AbacatePay; os dois
// entram nos 20 da tarifa menor). Sem a lista de saques, conta só os Pix.
func (s *Service) transferFee(ctx context.Context) int64 {
	today := s.Today()
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, s.loc)
	var sent int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM finance_pix_transfers WHERE status <> 'FAILED' AND created_at >= $1`,
		monthStart).Scan(&sent); err != nil {
		s.log.Warn("falha ao contar os Pix do mês (tarifa)", "err", err)
	}
	payouts, err := s.pix.PayoutsSince(ctx, monthStart)
	if err != nil {
		s.log.Warn("falha ao contar os saques do mês na AbacatePay (tarifa)", "err", providerMessage(err))
	}
	return int64(abacatepay.TransferFee(sent + payouts))
}

// PixInfo é o que a tela mostra antes de pagar: se está ligado, o modo e o
// saldo disponível.
type PixInfo struct {
	Enabled bool `json:"enabled"`
	DevMode bool `json:"devMode"`
	// AvailableCents: o saldo que pode sair (nulo se a consulta falhou).
	AvailableCents *int64 `json:"availableCents"`
	BalanceError   string `json:"balanceError"`
}

func (s *Service) PixInfo(ctx context.Context) *PixInfo {
	info := &PixInfo{Enabled: s.pix != nil, DevMode: s.pixDev}
	if s.pix == nil {
		return info
	}
	balance, err := s.pix.Balance(ctx)
	if err != nil {
		info.BalanceError = providerMessage(err)
		return info
	}
	available := int64(balance.Available)
	info.AvailableCents = &available
	return info
}

// PixPlan é para onde e quanto vai sair, antes de confirmar.
type PixPlan struct {
	EntryID      uuid.UUID `json:"entryId"`
	Description  string    `json:"description"`
	SupplierName string    `json:"supplierName"`
	AmountCents  int64     `json:"amountCents"`
	// Source: code (o Pix copia-e-cola da conta) ou key (a chave do fornecedor).
	Source  string `json:"source"`
	Key     string `json:"key"`
	KeyType string `json:"keyType"`
	// Recipient: o recebedor como está no copia-e-cola.
	Recipient string `json:"recipient"`
	// FeeCents é a tarifa da AbacatePay, paga pela empresa: o envio leva
	// SendCents (a conta + a tarifa) para o fornecedor receber AmountCents.
	FeeCents  int64 `json:"feeCents"`
	SendCents int64 `json:"sendCents"`
}

// planFor decide o destino: o copia-e-cola da conta, se houver; senão a
// chave do fornecedor.
func planFor(e *Entry, supplierKey, supplierKeyType string) (*PixPlan, error) {
	if e.Kind != KindPayable {
		return nil, invalid("Só contas a pagar saem por Pix.")
	}
	if e.Status != StatusOpen {
		return nil, ErrNotOpen
	}
	if e.AmountCents < abacatepay.MinTransferCents {
		return nil, invalid("O Pix pela AbacatePay é de pelo menos R$ 1,00.")
	}
	plan := &PixPlan{EntryID: e.ID, Description: e.Description, SupplierName: e.SupplierName, AmountCents: e.AmountCents}
	if code, ok := ParseBRCode(e.PaymentCode); ok {
		if code.AmountCents > 0 && code.AmountCents != e.AmountCents {
			return nil, invalid("O Pix copia-e-cola é de %s e a conta é de %s: acerte o valor da conta antes.",
				money(code.AmountCents), money(e.AmountCents))
		}
		plan.Source, plan.Key, plan.KeyType, plan.Recipient = "code", strings.TrimSpace(e.PaymentCode), abacatepay.KeyBRCode, code.Name
		return plan, nil
	}
	if strings.HasPrefix(strings.TrimSpace(e.PaymentCode), "000201") {
		return nil, invalid("O Pix copia-e-cola da conta está incompleto (cole de novo o código inteiro).")
	}
	if e.SupplierID == nil {
		return nil, invalid("A conta não tem fornecedor nem Pix copia-e-cola: diga para quem vai o Pix.")
	}
	if strings.TrimSpace(supplierKey) == "" {
		return nil, invalid("%s não tem chave Pix: cadastre em Empresa → Cadastros.", e.SupplierName)
	}
	keyType := supplierKeyType
	if keyType == "" {
		keyType = DetectKeyType(supplierKey)
	}
	if keyType == "" {
		return nil, invalid("Diga o tipo da chave Pix de %s em Empresa → Cadastros (11 dígitos podem ser CPF ou celular).", e.SupplierName)
	}
	key, err := PixKeyFor(supplierKey, keyType)
	if err != nil {
		return nil, err
	}
	plan.Source, plan.Key, plan.KeyType = "key", key, keyType
	return plan, nil
}

// money: 12345 → "R$ 123,45".
func money(cents int64) string {
	reais := fmt.Sprintf("%d", cents/100)
	for i := len(reais) - 3; i > 0; i -= 3 {
		reais = reais[:i] + "." + reais[i:]
	}
	return fmt.Sprintf("R$ %s,%02d", reais, cents%100)
}

// supplierKey é a chave Pix do fornecedor da conta.
func supplierKey(ctx context.Context, q database.Querier, supplierID *uuid.UUID) (key, keyType string, err error) {
	if supplierID == nil {
		return "", "", nil
	}
	err = q.QueryRow(ctx, `SELECT pix_key, pix_key_type FROM suppliers WHERE id = $1`, *supplierID).Scan(&key, &keyType)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	return key, keyType, err
}

// PlanPix mostra o envio antes de confirmar.
func (s *Service) PlanPix(ctx context.Context, entryID uuid.UUID) (*PixPlan, error) {
	if s.pix == nil {
		return nil, ErrPixDisabled
	}
	e, err := s.Entry(ctx, entryID)
	if err != nil {
		return nil, err
	}
	key, keyType, err := supplierKey(ctx, s.db, e.SupplierID)
	if err != nil {
		return nil, database.MapError(err)
	}
	plan, err := planFor(e, key, keyType)
	if err != nil {
		return nil, err
	}
	plan.FeeCents = s.transferFee(ctx)
	plan.SendCents = plan.AmountCents + plan.FeeCents
	return plan, nil
}

// ErrPixDisabled: sem a AbacatePay configurada.
var ErrPixDisabled = ValidationError{Message: "O Pix pela AbacatePay não está configurado (ABACATEPAY_API_KEY)."}

// SendPix paga a conta por Pix. Recusa da AbacatePay volta como erro com a
// mensagem dela (o envio fica registrado como FAILED); sem resposta, o envio
// fica UNKNOWN e o erro pede para conferir antes de tentar de novo.
func (s *Service) SendPix(ctx context.Context, entryID uuid.UUID, by *uuid.UUID) (*PixTransfer, error) {
	if s.pix == nil {
		return nil, ErrPixDisabled
	}
	// A tarifa sai do valor enviado: o envio leva a conta + a tarifa, para o
	// fornecedor receber o valor da conta (a tarifa é da empresa).
	fee := s.transferFee(ctx)
	var transfer *PixTransfer
	var plan *PixPlan
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		e, err := s.scanEntry(tx.QueryRow(ctx, `SELECT `+entryColumns+entryFrom+` WHERE e.id = $1 FOR UPDATE OF e`, entryID))
		if err != nil {
			return err
		}
		key, keyType, err := supplierKey(ctx, tx, e.SupplierID)
		if err != nil {
			return err
		}
		if plan, err = planFor(e, key, keyType); err != nil {
			return err
		}
		transfer, err = scanPix(tx.QueryRow(ctx, `
			INSERT INTO finance_pix_transfers (entry_id, external_id, amount_cents, sent_cents, pix_key, key_type, dev_mode, created_by)
			VALUES ($1, 'farbo-pix-' || gen_random_uuid(), $2, $3, $4, $5, $6, $7)
			RETURNING `+pixColumns, entryID, plan.AmountCents, plan.AmountCents+fee, plan.Key, plan.KeyType, s.pixDev, by))
		if errors.Is(err, database.ErrConflict) {
			return invalid("Esta conta já tem um Pix enviado ou em andamento.")
		}
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}

	var externalID string
	if err := s.db.QueryRow(ctx, `SELECT external_id FROM finance_pix_transfers WHERE id = $1`, transfer.ID).Scan(&externalID); err != nil {
		return nil, database.MapError(err)
	}
	// A chamada não pode ser cancelada no meio por quem pediu (fechar a aba):
	// o resultado precisa ser gravado.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	sent, sendErr := s.pix.SendPix(callCtx, abacatepay.TransferRequest{
		AmountCents: int(plan.AmountCents + fee), ExternalID: externalID, Description: plan.Description,
		Key: plan.Key, KeyType: plan.KeyType,
	})
	switch {
	case sendErr != nil && abacatepay.IsDefinitive(sendErr):
		message := providerMessage(sendErr)
		if err := s.failPix(callCtx, transfer.ID, message); err != nil {
			return nil, err
		}
		s.log.Warn("Pix ao fornecedor recusado pela AbacatePay", "entry", entryID, "transfer", transfer.ID, "err", sendErr)
		return nil, invalid("A AbacatePay recusou o Pix: %s", message)
	case sendErr != nil:
		if _, err := s.db.Exec(callCtx, `
			UPDATE finance_pix_transfers SET status = 'UNKNOWN', error = $2, updated_at = NOW() WHERE id = $1`,
			transfer.ID, "Sem resposta da AbacatePay: "+sendErr.Error()); err != nil {
			return nil, database.MapError(err)
		}
		s.log.Error("Pix ao fornecedor sem resposta da AbacatePay", "entry", entryID, "transfer", transfer.ID, "err", sendErr)
		return nil, invalid("A AbacatePay não respondeu. Confira no painel dela se o Pix saiu e marque aqui (Pix a conferir) antes de tentar de novo.")
	}
	if err := s.applyTransfer(callCtx, transfer.ID, sent); err != nil {
		return nil, err
	}
	out, err := s.pixTransfer(callCtx, transfer.ID)
	if err != nil {
		return nil, err
	}
	if out.Status == PixFailed {
		return nil, invalid("A AbacatePay não completou o Pix: %s", out.Error)
	}
	s.log.Info("Pix ao fornecedor enviado", "entry", entryID, "transfer", out.ID, "provider", out.ProviderID,
		"status", out.Status, "enviado", out.SentCents, "tarifa", out.FeeCents, "dev_mode", out.DevMode)
	return out, nil
}

func (s *Service) pixTransfer(ctx context.Context, id uuid.UUID) (*PixTransfer, error) {
	return scanPix(s.db.QueryRow(ctx, `SELECT `+pixColumns+` FROM finance_pix_transfers WHERE id = $1`, id))
}

// providerMessage é o que a AbacatePay disse (sem o "AbacatePay respondeu 400:").
func providerMessage(err error) string {
	var apiErr *abacatepay.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Message
	}
	return err.Error()
}

// applyTransfer grava o que a AbacatePay diz do envio: COMPLETE baixa a
// conta (uma vez só), FAILED desfaz; outro status fica para a próxima
// conferência.
func (s *Service) applyTransfer(ctx context.Context, id uuid.UUID, t *abacatepay.Transfer) error {
	switch t.Status {
	case abacatepay.TransferComplete:
		return s.completePix(ctx, id, t)
	case abacatepay.TransferFailed:
		if _, err := s.db.Exec(ctx, `UPDATE finance_pix_transfers SET provider_id = COALESCE(provider_id, $2) WHERE id = $1`,
			id, t.ID); err != nil {
			return database.MapError(err)
		}
		return s.failPix(ctx, id, "O envio falhou na AbacatePay; o valor voltou ao saldo.")
	}
	_, err := s.db.Exec(ctx, `
		UPDATE finance_pix_transfers SET provider_id = COALESCE(provider_id, $2), status = 'SENDING', checked_at = NOW(),
			updated_at = NOW()
		WHERE id = $1 AND status IN ('SENDING', 'UNKNOWN')`, id, t.ID)
	return database.MapError(err)
}

// completePix baixa a conta pelo envio e lança a tarifa. Idempotente.
func (s *Service) completePix(ctx context.Context, id uuid.UUID, t *abacatepay.Transfer) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var entryID uuid.UUID
		var status string
		var amount, sentCents int64
		if err := tx.QueryRow(ctx, `SELECT entry_id, status, amount_cents, sent_cents FROM finance_pix_transfers WHERE id = $1 FOR UPDATE`, id).
			Scan(&entryID, &status, &amount, &sentCents); err != nil {
			return err
		}
		if status == PixComplete || status == PixFailed {
			_, err := tx.Exec(ctx, `UPDATE finance_pix_transfers SET checked_at = NOW() WHERE id = $1`, id)
			return err
		}
		// O que chegou ao fornecedor: o enviado menos a tarifa cobrada. A
		// tarifa estimada errada (o 21º envio do mês, contado de outro jeito)
		// deixa diferença: fica registrada e aparece na conta.
		delivered := sentCents - int64(t.PlatformFee)
		if delivered != amount {
			s.log.Warn("Pix ao fornecedor com valor entregue diferente da conta", "transfer", id,
				"conta", amount, "enviado", sentCents, "tarifa", t.PlatformFee, "entregue", delivered)
		}
		today := s.Today().Time
		if _, err := tx.Exec(ctx, `
			UPDATE finance_pix_transfers SET status = 'COMPLETE', provider_id = NULLIF($2, ''), fee_cents = $3,
				receipt_url = $4, dev_mode = $5, error = '', completed_at = NOW(), checked_at = NOW(), updated_at = NOW()
			WHERE id = $1`, id, t.ID, t.PlatformFee, t.ReceiptURL, t.DevMode); err != nil {
			return err
		}
		// A conta: baixada pelo Pix (a baixa à mão fica travada enquanto há
		// um envio vivo).
		if _, err := tx.Exec(ctx, `
			UPDATE finance_entries SET status = 'PAID', paid_on = $2, paid_cents = $3, payment_method = 'PIX', updated_at = NOW()
			WHERE id = $1 AND status = 'OPEN'`, entryID, today, max(delivered, 1)); err != nil {
			return err
		}
		var supplierName, description string
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(sp.name, ''), e.description FROM finance_entries e LEFT JOIN suppliers sp ON sp.id = e.supplier_id
			WHERE e.id = $1`, entryID).Scan(&supplierName, &description); err != nil {
			return err
		}
		if t.PlatformFee <= 0 {
			return nil
		}
		// A tarifa da AbacatePay, como uma despesa já paga.
		category, ok, err := feeCategoryID(ctx, tx)
		if err != nil {
			return err
		}
		if !ok {
			s.log.Warn("tarifa do Pix sem categoria; não lançada", "transfer", id)
			return nil
		}
		who := supplierName
		if who == "" {
			who = description
		}
		var fee uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO finance_entries (kind, description, category_id, amount_cents, due_date, status, paid_on, paid_cents,
				payment_method, notes)
			VALUES ('PAYABLE', $1, $2, $3, $4, 'PAID', $4, $3, 'PIX', $5) RETURNING id`,
			truncateText("Tarifa do Pix (AbacatePay): "+who, maxText), category, int64(t.PlatformFee), today,
			"Envio "+t.ID).Scan(&fee); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE finance_pix_transfers SET fee_entry_id = $2 WHERE id = $1`, id, fee)
		return err
	})
	return mapErr(err)
}

// feeCategoryID é a categoria das tarifas (Taxas de pagamento; sem ela,
// Tarifas bancárias e juros). ok falso: nenhuma das duas existe.
func feeCategoryID(ctx context.Context, q database.Querier) (uuid.UUID, bool, error) {
	var category uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT id FROM finance_categories WHERE kind = 'EXPENSE' AND lower(name) IN (lower($1), lower('Tarifas bancárias e juros'))
		ORDER BY lower(name) = lower($1) DESC, active DESC LIMIT 1`, feeCategory).Scan(&category)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return category, err == nil, err
}

// failPix marca o envio como falho. Se ele já tinha baixado a conta (falhou
// depois de enviado), a conta volta a ficar em aberto e a tarifa sai.
func (s *Service) failPix(ctx context.Context, id uuid.UUID, message string) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var entryID uuid.UUID
		var status string
		var amount, sentCents, feeCents int64
		var fee *uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT entry_id, status, amount_cents, sent_cents, fee_cents, fee_entry_id FROM finance_pix_transfers WHERE id = $1 FOR UPDATE`, id).
			Scan(&entryID, &status, &amount, &sentCents, &feeCents, &fee); err != nil {
			return err
		}
		if status == PixFailed {
			return nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE finance_pix_transfers SET status = 'FAILED', error = $2, fee_entry_id = NULL, checked_at = NOW(), updated_at = NOW()
			WHERE id = $1`, id, truncateText(message, 500)); err != nil {
			return err
		}
		if status != PixComplete {
			return nil
		}
		if fee != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM finance_entries WHERE id = $1`, *fee); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			UPDATE finance_entries SET status = 'OPEN', paid_on = NULL, paid_cents = NULL, payment_method = '', updated_at = NOW()
			WHERE id = $1 AND status = 'PAID' AND payment_method = 'PIX' AND paid_cents IN ($2, $3)`,
			entryID, amount, max(sentCents-feeCents, 1))
		return err
	})
	if err != nil {
		return mapErr(err)
	}
	return nil
}

// ResolvePix é a decisão de quem conferiu no painel da AbacatePay um envio
// sem resposta: saiu (com o id do envio lá, se tiver) ou não saiu.
func (s *Service) ResolvePix(ctx context.Context, id uuid.UUID, sent bool, providerID string) (*PixTransfer, error) {
	t, err := s.pixTransfer(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.Status != PixUnknown {
		return nil, invalid("Só um Pix sem confirmação (a conferir) é resolvido à mão.")
	}
	if !sent {
		if err := s.failPix(ctx, id, "Conferido no painel da AbacatePay: o Pix não saiu."); err != nil {
			return nil, err
		}
		return s.pixTransfer(ctx, id)
	}
	providerID = strings.TrimSpace(providerID)
	// Sem o id, vale a tarifa que foi somada ao envio.
	result := &abacatepay.Transfer{ID: providerID, Status: abacatepay.TransferComplete, DevMode: t.DevMode,
		PlatformFee: int(t.SentCents - t.AmountCents)}
	if providerID != "" && s.pix != nil {
		found, err := s.pix.GetTransfer(ctx, providerID)
		if err != nil {
			return nil, invalid("A AbacatePay não achou o envio %s: %s", providerID, providerMessage(err))
		}
		if found.Amount != int(t.SentCents) {
			return nil, invalid("O envio %s é de %s, e o Pix desta conta foi de %s.", providerID, money(int64(found.Amount)), money(t.SentCents))
		}
		if found.Status != abacatepay.TransferComplete {
			return nil, invalid("O envio %s está %s na AbacatePay.", providerID, found.Status)
		}
		result = found
	}
	if err := s.completePix(ctx, id, result); err != nil {
		return nil, err
	}
	return s.pixTransfer(ctx, id)
}

// WatchPix confere os envios: os sem resposta de um servidor que parou
// viram UNKNOWN; os em andamento e os feitos nos últimos dias são
// consultados de novo (um envio pode falhar depois).
func (s *Service) WatchPix(ctx context.Context) {
	if s.pix == nil {
		return
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE finance_pix_transfers SET status = 'UNKNOWN', updated_at = NOW(),
			error = 'O servidor parou durante o envio: confira no painel da AbacatePay se o Pix saiu.'
		WHERE status = 'SENDING' AND provider_id IS NULL AND created_at < $1`, s.now().Add(-pixLostAfter)); err != nil {
		s.log.Error("falha ao conferir os Pix sem resposta", "err", err)
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, provider_id FROM finance_pix_transfers
		WHERE provider_id IS NOT NULL AND (status = 'SENDING' OR (status = 'COMPLETE' AND completed_at > $1))
			AND (checked_at IS NULL OR checked_at < $2)
		ORDER BY checked_at NULLS FIRST LIMIT 50`, s.now().Add(-pixWatchFor), s.now().Add(-pixCheckEvery))
	if err != nil {
		s.log.Error("falha ao listar os Pix a conferir", "err", err)
		return
	}
	type watch struct {
		id       uuid.UUID
		provider string
	}
	var list []watch
	for rows.Next() {
		var w watch
		if err := rows.Scan(&w.id, &w.provider); err == nil {
			list = append(list, w)
		}
	}
	rows.Close()
	for _, w := range list {
		s.recheckPix(ctx, w.id, w.provider)
	}
}

// recheckPix consulta um envio na AbacatePay e grava o que ela diz.
func (s *Service) recheckPix(ctx context.Context, id uuid.UUID, providerID string) {
	t, err := s.pix.GetTransfer(ctx, providerID)
	if err != nil {
		s.log.Warn("falha ao consultar o Pix na AbacatePay", "transfer", id, "provider", providerID, "err", err)
		_, _ = s.db.Exec(ctx, `UPDATE finance_pix_transfers SET checked_at = NOW() WHERE id = $1`, id)
		return
	}
	if err := s.applyTransfer(ctx, id, t); err != nil {
		s.log.Error("falha ao gravar o Pix conferido", "transfer", id, "err", err)
		return
	}
	if t.Status == abacatepay.TransferFailed {
		s.log.Warn("Pix ao fornecedor falhou depois de enviado; conta reaberta", "transfer", id, "provider", providerID)
	}
}

// HandleTransferWebhook confere os envios citados num webhook da AbacatePay
// (transfer.completed, transfer.failed). O webhook só diz quais consultar;
// a fonte da verdade é a API.
func (s *Service) HandleTransferWebhook(ctx context.Context, providerIDs []string) error {
	if s.pix == nil || len(providerIDs) == 0 {
		return nil
	}
	rows, err := s.db.Query(ctx, `SELECT id, provider_id FROM finance_pix_transfers WHERE provider_id = ANY($1)`, providerIDs)
	if err != nil {
		return database.MapError(err)
	}
	type found struct {
		id       uuid.UUID
		provider string
	}
	var list []found
	for rows.Next() {
		var f found
		if err := rows.Scan(&f.id, &f.provider); err != nil {
			rows.Close()
			return err
		}
		list = append(list, f)
	}
	rows.Close()
	for _, f := range list {
		s.recheckPix(ctx, f.id, f.provider)
	}
	return nil
}

// PixTransfers lista os envios de uma conta, o mais recente primeiro.
func (s *Service) PixTransfers(ctx context.Context, entryID uuid.UUID) ([]*PixTransfer, error) {
	rows, err := s.db.Query(ctx, `SELECT `+pixColumns+` FROM finance_pix_transfers WHERE entry_id = $1 ORDER BY created_at DESC`, entryID)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*PixTransfer{}
	for rows.Next() {
		p, err := scanPix(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// noLivePix recusa mexer à mão numa conta com Pix enviado ou em andamento.
func noLivePix(ctx context.Context, q database.Querier, entryID uuid.UUID) error {
	var status string
	err := q.QueryRow(ctx, `
		SELECT status FROM finance_pix_transfers WHERE entry_id = $1 AND status <> 'FAILED' LIMIT 1`, entryID).Scan(&status)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return database.MapError(err)
	case status == PixComplete:
		return invalid("Esta conta foi paga por Pix pela AbacatePay: o dinheiro já saiu.")
	}
	return invalid("Esta conta tem um Pix pela AbacatePay sem confirmação: resolva o Pix antes.")
}

func truncateText(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

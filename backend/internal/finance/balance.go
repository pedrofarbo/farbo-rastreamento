package finance

import (
	"context"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/pagarme"
)

// BalanceReader lê o saldo da conta na AbacatePay.
type BalanceReader interface {
	Balance(ctx context.Context) (*abacatepay.Balance, error)
}

// SetBalanceReader liga o saldo da AbacatePay na tela da empresa (devMode: a
// chave é de testes, e o saldo também).
func (s *Service) SetBalanceReader(r BalanceReader, devMode bool) {
	s.balance, s.balanceDev = r, devMode
}

// GatewayBalance é o saldo na AbacatePay agora, como ela informa: o que já
// pode sair, o que ainda vai ser liberado (boleto e cartão compensando) e o
// bloqueado (em disputa). É o dinheiro de verdade, não o saldo do caixa (que
// é o que a central lança).
type GatewayBalance struct {
	Enabled bool `json:"enabled"`
	DevMode bool `json:"devMode"`
	// Os valores são nulos se a consulta falhou (Error diz por quê).
	AvailableCents *int64    `json:"availableCents"`
	PendingCents   *int64    `json:"pendingCents"`
	BlockedCents   *int64    `json:"blockedCents"`
	CheckedAt      time.Time `json:"checkedAt"`
	Error          string    `json:"error"`
}

// GatewayBalance consulta o saldo na hora, sem guardar: a tela pede de
// tempos em tempos.
func (s *Service) GatewayBalance(ctx context.Context) *GatewayBalance {
	out := &GatewayBalance{Enabled: s.balance != nil, DevMode: s.balanceDev, CheckedAt: s.now()}
	if s.balance == nil {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b, err := s.balance.Balance(ctx)
	if err != nil {
		s.log.Warn("falha ao consultar o saldo na AbacatePay", "err", providerMessage(err))
		out.Error = providerMessage(err)
		return out
	}
	available, pending, blocked := int64(b.Available), int64(b.Pending), int64(b.Blocked)
	out.AvailableCents, out.PendingCents, out.BlockedCents = &available, &pending, &blocked
	return out
}

// PagarmeBalanceReader lê o saldo do recebedor na Pagar.me.
type PagarmeBalanceReader interface {
	Balance(ctx context.Context, recipientID string) (*pagarme.RecipientBalance, error)
}

// SetPagarmeBalance liga o saldo da Pagar.me na tela da empresa: o do
// recebedor da conta da Farbo (testMode: a chave é de testes).
func (s *Service) SetPagarmeBalance(r PagarmeBalanceReader, recipientID string, testMode bool) {
	s.pagarme, s.pagarmeRecipient, s.pagarmeDev = r, recipientID, testMode
}

// PagarmeBalance é o saldo na Pagar.me agora, no mesmo formato do da
// AbacatePay: o disponível e o que ainda vai ser liberado (os recebíveis a
// vencer). Sem a Pagar.me configurada (a chave e o recebedor), desligado.
func (s *Service) PagarmeBalance(ctx context.Context) *GatewayBalance {
	out := &GatewayBalance{Enabled: s.pagarme != nil, DevMode: s.pagarmeDev, CheckedAt: s.now()}
	if s.pagarme == nil {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b, err := s.pagarme.Balance(ctx, s.pagarmeRecipient)
	if err != nil {
		s.log.Warn("falha ao consultar o saldo na Pagar.me", "err", providerMessage(err))
		out.Error = providerMessage(err)
		return out
	}
	out.AvailableCents, out.PendingCents = &b.AvailableAmount, &b.WaitingFundsAmount
	return out
}

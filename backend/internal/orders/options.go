package orders

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// LoanOffer é o comodato que o próprio cliente escolhe no pedido, quando a
// central não definiu um plano para ele: a mensalidade (nula: a do plano) e
// as taxas. Desligado, o pedido só tem o rastreador comprado.
type LoanOffer struct {
	Enabled           bool `json:"enabled"`
	MonthlyCents      *int `json:"monthlyCents"`
	InstallFeeCents   int  `json:"installFeeCents"`
	UninstallFeeCents int  `json:"uninstallFeeCents"`
}

// InstallService é a instalação pelos instaladores da Farbo no rastreador
// comprado, pelo preço daqui (desligada, o cliente instala por conta
// própria). No comodato, a instalação é sempre da Farbo, com a taxa dele.
type InstallService struct {
	Enabled    bool `json:"enabled"`
	PriceCents int  `json:"priceCents"`
}

// Options são as opções do pedido do cliente.
type Options struct {
	Loan      LoanOffer      `json:"loan"`
	Install   InstallService `json:"install"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// Options são as opções em vigor.
func (s *Service) Options(ctx context.Context) (*Options, error) {
	var o Options
	err := s.db.QueryRow(ctx, `
		SELECT loan_enabled, loan_monthly_cents, loan_install_fee_cents, loan_uninstall_fee_cents,
			install_enabled, install_cents, updated_at
		FROM order_settings`).
		Scan(&o.Loan.Enabled, &o.Loan.MonthlyCents, &o.Loan.InstallFeeCents, &o.Loan.UninstallFeeCents,
			&o.Install.Enabled, &o.Install.PriceCents, &o.UpdatedAt)
	return &o, database.MapError(err)
}

// Loan é o comodato oferecido no pedido.
func (s *Service) Loan(ctx context.Context) (*LoanOffer, error) {
	o, err := s.Options(ctx)
	if err != nil {
		return nil, err
	}
	return &o.Loan, nil
}

// SetOptions muda as opções (valem para os pedidos novos).
func (s *Service) SetOptions(ctx context.Context, in Options, by *uuid.UUID) (*Options, error) {
	const max = 100_000_00
	l := in.Loan
	switch {
	case l.MonthlyCents != nil && (*l.MonthlyCents < 0 || *l.MonthlyCents > max):
		return nil, ValidationError{"a mensalidade do comodato vai de R$ 0 a R$ 100.000"}
	case l.InstallFeeCents < 0 || l.InstallFeeCents > max || l.UninstallFeeCents < 0 || l.UninstallFeeCents > max:
		return nil, ValidationError{"as taxas de instalação e de desinstalação vão de R$ 0 a R$ 100.000"}
	case in.Install.PriceCents < 0 || in.Install.PriceCents > max:
		return nil, ValidationError{"a instalação vai de R$ 0 a R$ 100.000"}
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE order_settings SET loan_enabled = $1, loan_monthly_cents = $2, loan_install_fee_cents = $3,
			loan_uninstall_fee_cents = $4, install_enabled = $5, install_cents = $6, updated_by = $7, updated_at = NOW()`,
		l.Enabled, l.MonthlyCents, l.InstallFeeCents, l.UninstallFeeCents, in.Install.Enabled, in.Install.PriceCents, by); err != nil {
		return nil, database.MapError(err)
	}
	return s.Options(ctx)
}

// PaysUpfront diz se o pedido do cliente é pago no ato: a pessoa física
// (CPF, ou ainda sem documento) paga a 1ª mensalidade, o frete e o
// rastreador (ou a instalação, no comodato) na contratação; a empresa (CNPJ)
// tem tudo na próxima fatura do mês da conta, como as associações.
func (s *Service) PaysUpfront(ctx context.Context, customerID uuid.UUID) (bool, error) {
	var document string
	if err := s.db.QueryRow(ctx, `SELECT document FROM users WHERE id = $1`, customerID).Scan(&document); err != nil {
		return false, database.MapError(err)
	}
	return !isCompany(document), nil
}

// isCompany: o documento é um CNPJ (14 números).
func isCompany(document string) bool {
	digits := 0
	for _, r := range document {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits == 14
}

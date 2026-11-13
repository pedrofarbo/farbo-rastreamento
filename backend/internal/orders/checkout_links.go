package orders

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// Os links de pagamento: a central gera um link com uma oferta fechada — o
// rastreador incluso ou em comodato, o mensal, o semestral ou o anual, o
// valor de cada cobrança (com tudo dentro), quem instala, com ou sem o relé
// do bloqueio, as taxas (a instalação na 1ª cobrança ou paga direto ao
// instalador) e, se quiser, a entrega em mãos pela Farbo (sem frete) — e
// manda a quem vai contratar. Quem abre
// /contratar?link=<token> vê só essa oferta, informa o veículo e os dados e
// paga. Vale com a contratação pelo site fechada e sem o link de um
// representante; o cupom não vai junto. O próximo veículo do cliente sai
// nas condições normais (customerPlan não copia a assinatura do link).

// ErrCheckoutLinkUsed: o link já foi usado e não pode ser excluído (desligue).
var ErrCheckoutLinkUsed = errors.New("o link já foi usado: desligue em vez de excluir")

// CheckoutLink é um link de pagamento, com quantas contratações já o usaram.
type CheckoutLink struct {
	ID    uuid.UUID `json:"id"`
	Token string    `json:"token"`
	// Label: para quem ou para quê (só a central vê).
	Label         string `json:"label"`
	EquipmentMode string `json:"equipmentMode"`
	// PeriodMonths: 1, o mensal; 6, o semestral; 12, o anual.
	PeriodMonths int `json:"periodMonths"`
	// PriceCents: o valor de cada cobrança (o mês, o semestre ou o ano).
	PriceCents int `json:"priceCents"`
	// FarboInstall: a Farbo instala (no comodato, sempre); sem isso, o
	// cliente instala por conta própria.
	FarboInstall    bool `json:"farboInstall"`
	InstallCents    int  `json:"installCents"`
	UninstallCents  int  `json:"uninstallCents"`
	InstallBlocking bool `json:"installBlocking"`
	// InstallDirect: a instalação é paga direto ao instalador, no dia — o
	// valor aparece para o cliente, mas não entra na 1ª cobrança.
	InstallDirect bool `json:"installDirect"`
	// HandDelivery: a Farbo entrega o rastreador em mãos e combina o dia com
	// o cliente, sem frete.
	HandDelivery bool `json:"handDelivery"`
	// ValidUntil: até que dia (inclusive) dá para usar; nulo, sem validade.
	ValidUntil *billing.Date `json:"validUntil"`
	// MaxUses: quantas contratações podem usar; nulo, sem limite.
	MaxUses   *int      `json:"maxUses"`
	Active    bool      `json:"active"`
	Uses      int       `json:"uses"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CheckoutLinkInput é o que a central informa (o código sai sozinho).
type CheckoutLinkInput struct {
	Label           string        `json:"label"`
	EquipmentMode   string        `json:"equipmentMode"`
	PeriodMonths    int           `json:"periodMonths"`
	PriceCents      int           `json:"priceCents"`
	FarboInstall    bool          `json:"farboInstall"`
	InstallCents    int           `json:"installCents"`
	UninstallCents  int           `json:"uninstallCents"`
	InstallBlocking bool          `json:"installBlocking"`
	InstallDirect   bool          `json:"installDirect"`
	HandDelivery    bool          `json:"handDelivery"`
	ValidUntil      *billing.Date `json:"validUntil"`
	MaxUses         *int          `json:"maxUses"`
	Active          bool          `json:"active"`
}

// CheckoutLinkOffer é o que o site mostra do link: a oferta, sem o rótulo
// (que é da central).
type CheckoutLinkOffer struct {
	Token           string        `json:"token"`
	EquipmentMode   string        `json:"equipmentMode"`
	PeriodMonths    int           `json:"periodMonths"`
	PriceCents      int           `json:"priceCents"`
	FarboInstall    bool          `json:"farboInstall"`
	InstallCents    int           `json:"installCents"`
	UninstallCents  int           `json:"uninstallCents"`
	InstallBlocking bool          `json:"installBlocking"`
	InstallDirect   bool          `json:"installDirect"`
	HandDelivery    bool          `json:"handDelivery"`
	ValidUntil      *billing.Date `json:"validUntil"`
}

// Offer é o link como o site mostra.
func (l *CheckoutLink) Offer() CheckoutLinkOffer {
	return CheckoutLinkOffer{Token: l.Token, EquipmentMode: l.EquipmentMode, PeriodMonths: l.PeriodMonths,
		PriceCents: l.PriceCents, FarboInstall: l.FarboInstall, InstallCents: l.InstallCents,
		UninstallCents: l.UninstallCents, InstallBlocking: l.InstallBlocking, InstallDirect: l.InstallDirect,
		HandDelivery: l.HandDelivery, ValidUntil: l.ValidUntil}
}

// check confere a oferta e acerta o que não se aplica: no comodato, quem
// instala é a Farbo; no incluso, não há desinstalação, e por conta própria
// não há taxa nem bloqueio.
func (in *CheckoutLinkInput) check() error {
	in.Label = strings.TrimSpace(in.Label)
	if in.PeriodMonths == 0 {
		in.PeriodMonths = billing.PeriodMonthly
	}
	switch in.EquipmentMode {
	case billing.EquipmentLoan:
		in.FarboInstall = true
	case billing.EquipmentSale:
		in.UninstallCents = 0
		if !in.FarboInstall {
			in.InstallCents, in.InstallBlocking, in.InstallDirect = 0, false, false
		}
	}
	inRange := func(cents int) bool { return cents >= 0 && cents <= vehicles.MaxPlanCents }
	switch {
	case in.Label == "" || len([]rune(in.Label)) > 100:
		return ValidationError{"dê um nome ao link (até 100 caracteres): para quem ou para quê"}
	case in.EquipmentMode != billing.EquipmentSale && in.EquipmentMode != billing.EquipmentLoan:
		return ValidationError{"escolha o rastreador do link: incluso ou em comodato"}
	case in.PeriodMonths != billing.PeriodMonthly && in.PeriodMonths != billing.PeriodSemiannual && in.PeriodMonths != billing.PeriodAnnual:
		return ValidationError{"a recorrência do link é mensal, semestral ou anual"}
	case in.PriceCents < 100 || in.PriceCents > vehicles.MaxPlanCents:
		return ValidationError{"o valor do link vai de R$ 1,00 a R$ 100.000,00"}
	case !inRange(in.InstallCents) || !inRange(in.UninstallCents):
		return ValidationError{"as taxas de instalação e de desinstalação vão de R$ 0 a R$ 100.000"}
	case in.MaxUses != nil && (*in.MaxUses < 1 || *in.MaxUses > 100_000):
		return ValidationError{"o limite de usos vai de 1 a 100.000 (vazio: sem limite)"}
	}
	return nil
}

// tokenAlphabet: letras e números sem os que se confundem (l, o, 0 e 1). São
// 32, então cada byte sorteado vira uma letra sem viés.
const tokenAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// newLinkToken sorteia o código do link: 14 letras, uns 70 bits.
func newLinkToken() (string, error) {
	b := make([]byte, 14)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = tokenAlphabet[b[i]&31]
	}
	return string(b), nil
}

// NormalizeLinkToken deixa o código como fica guardado: sem espaços, em
// minúsculas.
func NormalizeLinkToken(token string) string {
	return strings.ToLower(strings.TrimSpace(token))
}

const checkoutLinkColumns = `l.id, l.token, l.label, l.equipment_mode, l.period_months, l.price_cents, l.farbo_install,
	l.install_cents, l.uninstall_cents, l.install_blocking, l.install_direct, l.hand_delivery, l.valid_until, l.max_uses, l.active,
	(SELECT count(*) FROM checkout_link_uses u WHERE u.link_id = l.id), l.created_at, l.updated_at`

func scanCheckoutLink(row database.Scanner) (*CheckoutLink, error) {
	var l CheckoutLink
	var until *time.Time
	if err := row.Scan(&l.ID, &l.Token, &l.Label, &l.EquipmentMode, &l.PeriodMonths, &l.PriceCents, &l.FarboInstall,
		&l.InstallCents, &l.UninstallCents, &l.InstallBlocking, &l.InstallDirect, &l.HandDelivery, &until, &l.MaxUses, &l.Active, &l.Uses,
		&l.CreatedAt, &l.UpdatedAt); err != nil {
		return nil, database.MapError(err)
	}
	if until != nil {
		l.ValidUntil = &billing.Date{Time: *until}
	}
	return &l, nil
}

// CheckoutLinks são todos os links, os ligados primeiro e os mais novos antes.
func (s *Service) CheckoutLinks(ctx context.Context) ([]CheckoutLink, error) {
	rows, err := s.db.Query(ctx, `SELECT `+checkoutLinkColumns+` FROM checkout_links l ORDER BY l.active DESC, l.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CheckoutLink{}
	for rows.Next() {
		l, err := scanCheckoutLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// CreateCheckoutLink cria o link, com um código novo.
func (s *Service) CreateCheckoutLink(ctx context.Context, in CheckoutLinkInput, by *uuid.UUID) (*CheckoutLink, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	// Um código repetido (raríssimo) sorteia outro.
	for attempt := 0; ; attempt++ {
		token, err := newLinkToken()
		if err != nil {
			return nil, err
		}
		var id uuid.UUID
		err = s.db.QueryRow(ctx, `
			INSERT INTO checkout_links (token, label, equipment_mode, period_months, price_cents, farbo_install,
				install_cents, uninstall_cents, install_blocking, install_direct, hand_delivery, valid_until, max_uses, active,
				created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING id`,
			token, in.Label, in.EquipmentMode, in.PeriodMonths, in.PriceCents, in.FarboInstall, in.InstallCents,
			in.UninstallCents, in.InstallBlocking, in.InstallDirect, in.HandDelivery, dateOrNil(in.ValidUntil), in.MaxUses,
			in.Active, by).Scan(&id)
		if errors.Is(database.MapError(err), database.ErrConflict) && attempt < 3 {
			continue
		}
		if err != nil {
			return nil, database.MapError(err)
		}
		return s.checkoutLinkByID(ctx, s.db, id)
	}
}

// UpdateCheckoutLink muda o link (o código fica). Vale para as contratações
// daqui em diante: quem já contratou segue nas condições que contratou.
func (s *Service) UpdateCheckoutLink(ctx context.Context, id uuid.UUID, in CheckoutLinkInput) (*CheckoutLink, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE checkout_links SET label = $2, equipment_mode = $3, period_months = $4, price_cents = $5,
			farbo_install = $6, install_cents = $7, uninstall_cents = $8, install_blocking = $9, install_direct = $10,
			hand_delivery = $11, valid_until = $12, max_uses = $13, active = $14, updated_at = NOW()
		WHERE id = $1`, id, in.Label, in.EquipmentMode, in.PeriodMonths, in.PriceCents, in.FarboInstall, in.InstallCents,
		in.UninstallCents, in.InstallBlocking, in.InstallDirect, in.HandDelivery, dateOrNil(in.ValidUntil), in.MaxUses, in.Active)
	if err != nil {
		return nil, database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, database.ErrNotFound
	}
	return s.checkoutLinkByID(ctx, s.db, id)
}

// DeleteCheckoutLink exclui o link que ninguém usou (ErrCheckoutLinkUsed se
// já usaram).
func (s *Service) DeleteCheckoutLink(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM checkout_links l WHERE l.id = $1
		AND NOT EXISTS (SELECT 1 FROM checkout_link_uses u WHERE u.link_id = l.id)`, id)
	if err != nil {
		if errors.Is(database.MapError(err), database.ErrForeignKey) {
			return ErrCheckoutLinkUsed
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.checkoutLinkByID(ctx, s.db, id); err != nil {
			return err
		}
		return ErrCheckoutLinkUsed
	}
	return nil
}

func (s *Service) checkoutLinkByID(ctx context.Context, q database.Querier, id uuid.UUID) (*CheckoutLink, error) {
	return scanCheckoutLink(q.QueryRow(ctx, `SELECT `+checkoutLinkColumns+` FROM checkout_links l WHERE l.id = $1`, id))
}

// usable diz por que o link não vale hoje (vazio: vale).
func (l *CheckoutLink) usable(today billing.Date) string {
	switch {
	case !l.Active:
		return "este link de pagamento não está mais valendo"
	case l.ValidUntil != nil && l.ValidUntil.Before(today):
		return "este link de pagamento venceu em " + l.ValidUntil.Format("02/01/2006")
	case l.MaxUses != nil && *l.MaxUses == 1 && l.Uses >= 1:
		return "este link de pagamento já foi usado"
	case l.MaxUses != nil && l.Uses >= *l.MaxUses:
		return "este link de pagamento já foi usado o máximo de vezes"
	}
	return ""
}

// CheckoutLinkFor é o link do código, se ele vale hoje; senão,
// ValidationError com o motivo.
func (s *Service) CheckoutLinkFor(ctx context.Context, token string) (*CheckoutLink, error) {
	return s.checkoutLink(ctx, s.db, token, false)
}

func (s *Service) checkoutLink(ctx context.Context, q database.Querier, token string, lock bool) (*CheckoutLink, error) {
	token = NormalizeLinkToken(token)
	if token == "" {
		return nil, ValidationError{"link de pagamento não encontrado: confira o endereço"}
	}
	if lock {
		// A trava antes, num comando à parte: a contagem dos usos, no seguinte,
		// já vê o uso de quem tinha a trava antes.
		if _, err := q.Exec(ctx, `SELECT 1 FROM checkout_links WHERE token = $1 FOR UPDATE`, token); err != nil {
			return nil, err
		}
	}
	l, err := scanCheckoutLink(q.QueryRow(ctx, `SELECT `+checkoutLinkColumns+` FROM checkout_links l WHERE l.token = $1`, token))
	if errors.Is(err, database.ErrNotFound) {
		return nil, ValidationError{"link de pagamento não encontrado: confira o endereço"}
	}
	if err != nil {
		return nil, err
	}
	if reason := l.usable(s.billing.Today()); reason != "" {
		return nil, ValidationError{reason}
	}
	return l, nil
}

// linkOrder monta o pedido pelo link: a oferta dele, sem escolha. O
// rastreador incluso fica com o cliente, com a parte dele já dentro do valor
// (encerrado antes, o que falta vence de uma vez, como no site); no
// comodato, a instalação é da Farbo, com as taxas do link.
func (s *Service) linkOrder(opts *Options, l *CheckoutLink, vehicle vehicles.Input) Order {
	plan := billing.SubscriptionInput{PlanName: s.catalog.PlanName, PriceCents: l.PriceCents, DueDay: s.catalog.DefaultDueDay}
	if l.PeriodMonths > 1 {
		plan.PlanName, plan.PeriodMonths = PeriodPlanName(l.PeriodMonths), l.PeriodMonths
	}
	order := Order{Vehicle: vehicle, Plan: plan, RequireAddress: true, Link: l.Token}
	// A instalação paga direto ao instalador: fora da 1ª cobrança.
	install := l.InstallCents
	if l.InstallDirect {
		install = 0
	}
	if l.EquipmentMode == billing.EquipmentLoan {
		order.Terms = Terms{EquipmentMode: billing.EquipmentLoan, InstallFeeCents: install, UninstallFeeCents: l.UninstallCents}
	} else {
		order.Terms = Terms{EquipmentMode: billing.EquipmentSale}
		if l.PeriodMonths <= 1 {
			order.Tracker = Tracker{Cents: opts.Tracker.Cents, Months: opts.Tracker.Months}
		}
		order.FarboInstall, order.InstallCents = l.FarboInstall, install
	}
	order.InstallBlocking = l.InstallBlocking && (l.EquipmentMode == billing.EquipmentLoan || l.FarboInstall)
	// A entrega em mãos pela Farbo: sem frete, combinada com o cliente.
	if l.HandDelivery {
		order.Shipping = fulfillment.InPerson()
	}
	return order
}

// useCheckoutLink confere o link de novo (travado, na transação do pedido:
// duas contratações ao mesmo tempo não passam do limite).
func (s *Service) useCheckoutLink(ctx context.Context, tx pgx.Tx, token string) (*CheckoutLink, error) {
	return s.checkoutLink(ctx, tx, token, true)
}

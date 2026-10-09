// Package contract é o contrato de prestação de serviços que o cliente aceita
// no primeiro acesso (e a cada versão nova): o texto, com a versão e o hash,
// e o registro do aceite eletrônico — nome, CPF/CNPJ, data, IP e navegador.
// O CPF é obrigatório: é com ele que saem a NF-e do rastreador e a NFS-e das
// mensalidades.
package contract

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
)

// A versão em vigor. Ao mudar o texto (contrato.md), mude a versão e a data:
// todo cliente aceita de novo no próximo acesso.
const (
	Version       = "4"
	EffectiveDate = "9 de outubro de 2026"
	// Changes resume o que mudou desde a versão anterior (o cliente que
	// aceitou a anterior lê ao aceitar de novo, e recebe por e-mail).
	Changes = "No rastreador parcelado (cláusula 5), o pedido cobra só o frete: as parcelas vêm todas somadas às mensalidades, uma por mês, a 1ª junto com a 1ª mensalidade — nunca duas no mesmo mês. A assinatura fica ativa até a mensalidade com a última parcela. Para quem pagou o rastreador à vista, nada muda."
	// MinMonths é a permanência mínima de cada assinatura.
	MinMonths = 3
)

//go:embed contrato.md
var source string

// Block é um parágrafo ou uma lista do contrato.
type Block struct {
	Kind  string   `json:"kind"` // "p" ou "ul"
	Text  string   `json:"text,omitempty"`
	Items []string `json:"items,omitempty"`
}

// Section é uma cláusula.
type Section struct {
	Title  string  `json:"title"`
	Blocks []Block `json:"blocks"`
}

// Document é o contrato em vigor, já com os dados da empresa.
type Document struct {
	Version       string    `json:"version"`
	EffectiveDate string    `json:"effectiveDate"`
	Title         string    `json:"title"`
	Intro         []Block   `json:"intro"`
	Sections      []Section `json:"sections"`
	// SHA256 é o hash do texto: o aceite guarda qual texto foi aceito.
	SHA256 string `json:"sha256"`
	// Changes resume o que mudou desde a versão anterior.
	Changes string `json:"changes"`
	// Text é o texto completo (markdown simples).
	Text string `json:"-"`
}

// Params são os dados que o texto cita.
type Params struct {
	Company          config.Company
	SuspendAfterDays int
	HistoryOptions   []int
	// MaxInstallments: em até quantas vezes o rastreador pode ser parcelado
	// (1: só à vista, e o contrato não fala em parcelamento).
	MaxInstallments int
}

// numberWords escreve por extenso as vezes do parcelamento.
var numberWords = map[int]string{
	2: "duas", 3: "três", 4: "quatro", 5: "cinco", 6: "seis", 7: "sete", 8: "oito", 9: "nove", 10: "dez",
	11: "onze", 12: "doze",
}

// Render monta o contrato em vigor.
func Render(p Params) (*Document, error) {
	tpl, err := template.New("contrato").Option("missingkey=error").Parse(source)
	if err != nil {
		return nil, fmt.Errorf("contrato: %w", err)
	}
	name := strings.TrimSpace(p.Company.Name)
	if name == "" {
		name = "Farbo Rastreadores"
	}
	legal := strings.TrimSpace(p.Company.LegalName)
	if legal == "" {
		legal = name
	}
	options := make([]string, 0, len(p.HistoryOptions))
	for _, d := range p.HistoryOptions {
		options = append(options, fmt.Sprint(d))
	}
	historyOptions := strings.Join(options, ", ")
	if i := strings.LastIndex(historyOptions, ", "); i >= 0 {
		historyOptions = historyOptions[:i] + " ou " + historyOptions[i+2:]
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, map[string]any{
		"Version": Version, "EffectiveDate": EffectiveDate, "MinMonths": MinMonths,
		"Name": name, "LegalName": legal, "CNPJ": strings.TrimSpace(p.Company.CNPJ),
		"Address": strings.TrimSpace(p.Company.Address), "Email": strings.TrimSpace(p.Company.Email),
		"SuspendAfterDays": p.SuspendAfterDays, "HistoryOptions": historyOptions,
		"MaxInstallments": p.MaxInstallments, "MaxInstallmentsWords": numberWords[p.MaxInstallments],
	}); err != nil {
		return nil, fmt.Errorf("contrato: %w", err)
	}
	text := out.String()
	sum := sha256.Sum256([]byte(text))
	doc := parse(text)
	doc.Version, doc.EffectiveDate, doc.SHA256, doc.Text = Version, EffectiveDate, hex.EncodeToString(sum[:]), text
	doc.Changes = Changes
	return doc, nil
}

// parse lê o markdown simples do contrato: "# " é o título, "## " abre uma
// cláusula, blocos separados por linha em branco são parágrafos ou, com
// "- ", listas.
func parse(text string) *Document {
	doc := &Document{Intro: []Block{}, Sections: []Section{}}
	add := func(b Block) {
		if len(doc.Sections) == 0 {
			doc.Intro = append(doc.Intro, b)
			return
		}
		last := &doc.Sections[len(doc.Sections)-1]
		last.Blocks = append(last.Blocks, b)
	}
	for _, chunk := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		chunk = strings.TrimSpace(chunk)
		switch {
		case chunk == "":
		case strings.HasPrefix(chunk, "## "):
			doc.Sections = append(doc.Sections, Section{Title: strings.TrimSpace(chunk[3:]), Blocks: []Block{}})
		case strings.HasPrefix(chunk, "# "):
			doc.Title = strings.TrimSpace(chunk[2:])
		case strings.HasPrefix(chunk, "- "):
			b := Block{Kind: "ul"}
			for _, line := range strings.Split(chunk, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "- ") {
					b.Items = append(b.Items, strings.TrimSpace(line[2:]))
				} else if len(b.Items) > 0 && line != "" {
					b.Items[len(b.Items)-1] += " " + line
				}
			}
			add(b)
		default:
			add(Block{Kind: "p", Text: strings.Join(strings.Fields(chunk), " ")})
		}
	}
	return doc
}

// RuleError é um aceite recusado; a mensagem vai para a tela.
type RuleError struct{ Message string }

func (e RuleError) Error() string { return e.Message }

// Acceptance é um aceite registrado.
type Acceptance struct {
	Version    string    `json:"version"`
	SHA256     string    `json:"sha256"`
	Name       string    `json:"name"`
	Document   string    `json:"document"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"userAgent"`
	AcceptedAt time.Time `json:"acceptedAt"`
}

// Status é o contrato do ponto de vista do cliente.
type Status struct {
	Contract *Document `json:"contract"`
	// Required: o cliente precisa aceitar esta versão para usar a plataforma.
	Required bool `json:"required"`
	// Accepted: o aceite desta versão (nulo se ainda não aceitou).
	Accepted *Acceptance `json:"accepted"`
	// Previous: o aceite de uma versão anterior, quando ainda não aceitou
	// esta (a tela explica o que mudou).
	Previous *Acceptance `json:"previous"`
	Name     string      `json:"name"`
	// TaxID é o CPF/CNPJ do cadastro (vazio se ainda não informado).
	TaxID string `json:"taxId"`
}

// Mailer manda a cópia do contrato aceito (mail.ContractMailer).
type Mailer interface {
	ContractAccepted(ctx context.Context, to, name string, c mail.ContractCopy) error
	ContractUpdated(ctx context.Context, to, name string, u mail.ContractUpdate) error
}

type Service struct {
	db       *database.DB
	doc      *Document
	mailer   Mailer
	accepted sync.Map // usuário → já aceitou a versão em vigor
	runAsync func(func())
	log      *slog.Logger
}

func NewService(db *database.DB, doc *Document, mailer Mailer, log *slog.Logger) *Service {
	return &Service{db: db, doc: doc, mailer: mailer, runAsync: func(f func()) { go f() }, log: log.With("component", "contract")}
}

// SetSync faz a cópia por e-mail sair na hora (testes).
func (s *Service) SetSync() { s.runAsync = func(f func()) { f() } }

// Current é o contrato em vigor.
func (s *Service) Current() *Document { return s.doc }

const acceptanceColumns = `version, content_sha256, name, document, ip, user_agent, accepted_at`

func scanAcceptance(row pgx.Row) (*Acceptance, error) {
	var a Acceptance
	err := row.Scan(&a.Version, &a.SHA256, &a.Name, &a.Document, &a.IP, &a.UserAgent, &a.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &a, database.MapError(err)
}

// Accepted é o aceite da versão em vigor (nil se ainda não aceitou).
func (s *Service) Accepted(ctx context.Context, userID uuid.UUID) (*Acceptance, error) {
	return scanAcceptance(s.db.QueryRow(ctx, `SELECT `+acceptanceColumns+` FROM contract_acceptances
		WHERE user_id = $1 AND version = $2`, userID, s.doc.Version))
}

// Latest é o último aceite, de qualquer versão (a ficha do cliente).
func (s *Service) Latest(ctx context.Context, userID uuid.UUID) (*Acceptance, error) {
	return scanAcceptance(s.db.QueryRow(ctx, `SELECT `+acceptanceColumns+` FROM contract_acceptances
		WHERE user_id = $1 ORDER BY accepted_at DESC, id DESC LIMIT 1`, userID))
}

// HasAccepted diz se o cliente aceitou a versão em vigor (guardado em
// memória depois do primeiro sim: não custa uma consulta por requisição).
func (s *Service) HasAccepted(ctx context.Context, userID uuid.UUID) (bool, error) {
	if _, ok := s.accepted.Load(userID); ok {
		return true, nil
	}
	a, err := s.Accepted(ctx, userID)
	if err != nil || a == nil {
		return false, err
	}
	s.accepted.Store(userID, true)
	return true, nil
}

// Required diz se o cliente precisa aceitar antes de usar a plataforma. Quem
// só acompanha o veículo de outra pessoa (acesso compartilhado, sem veículo,
// assinatura nem fatura dele) não é parte do contrato e fica dispensado — até
// pedir um rastreador.
func (s *Service) Required(ctx context.Context, userID uuid.UUID) (bool, error) {
	ok, err := s.HasAccepted(ctx, userID)
	if err != nil || ok {
		return false, err
	}
	var guestOnly bool
	err = s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM vehicle_shares WHERE guest_id = $1)
			AND NOT EXISTS (SELECT 1 FROM vehicles WHERE owner_id = $1)
			AND NOT EXISTS (SELECT 1 FROM subscriptions WHERE customer_id = $1)
			AND NOT EXISTS (SELECT 1 FROM invoices WHERE customer_id = $1)`, userID).Scan(&guestOnly)
	if err != nil {
		return false, database.MapError(err)
	}
	return !guestOnly, nil
}

// Status é o contrato, se o cliente precisa aceitar e o que já aceitou.
func (s *Service) Status(ctx context.Context, userID uuid.UUID) (*Status, error) {
	st := &Status{Contract: s.doc}
	if err := s.db.QueryRow(ctx, `SELECT name, document FROM users WHERE id = $1`, userID).Scan(&st.Name, &st.TaxID); err != nil {
		return nil, database.MapError(err)
	}
	var err error
	if st.Accepted, err = s.Accepted(ctx, userID); err != nil {
		return nil, err
	}
	if st.Accepted == nil {
		if st.Required, err = s.Required(ctx, userID); err != nil {
			return nil, err
		}
		if st.Previous, err = s.Latest(ctx, userID); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// NotifyChanges avisa por e-mail, uma vez, quem aceitou uma versão anterior
// do contrato e ainda não aceitou a atual (cláusula 14: as mudanças são
// avisadas por e-mail e a plataforma pede o novo aceite).
func (s *Service) NotifyChanges(ctx context.Context) {
	if s.mailer == nil {
		return
	}
	rows, err := s.db.Query(ctx, `
		SELECT u.id, u.email, u.name FROM users u
		WHERE u.role = 'customer' AND u.active
			AND EXISTS (SELECT 1 FROM contract_acceptances a WHERE a.user_id = u.id AND a.version <> $1)
			AND NOT EXISTS (SELECT 1 FROM contract_acceptances a WHERE a.user_id = u.id AND a.version = $1)
			AND NOT EXISTS (SELECT 1 FROM contract_change_notices n WHERE n.user_id = u.id AND n.version = $1)
		ORDER BY u.created_at LIMIT 200`, s.doc.Version)
	if err != nil {
		s.log.Error("falha ao listar quem precisa do aviso do contrato novo", "err", err)
		return
	}
	type who struct {
		id          uuid.UUID
		email, name string
	}
	var list []who
	for rows.Next() {
		var w who
		if rows.Scan(&w.id, &w.email, &w.name) == nil {
			list = append(list, w)
		}
	}
	rows.Close()
	for _, w := range list {
		if err := s.mailer.ContractUpdated(ctx, w.email, w.name, mail.ContractUpdate{
			Title: s.doc.Title, Version: s.doc.Version, EffectiveDate: s.doc.EffectiveDate, Changes: s.doc.Changes,
		}); err != nil {
			s.log.Warn("aviso do contrato novo não enviado (tenta de novo)", "email", w.email, "err", err)
			continue
		}
		if _, err := s.db.Exec(ctx, `INSERT INTO contract_change_notices (user_id, version) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, w.id, s.doc.Version); err != nil {
			s.log.Error("aviso do contrato novo enviado e não marcado", "email", w.email, "err", err)
		}
	}
	if len(list) > 0 {
		s.log.Info("avisos do contrato novo enviados", "versao", s.doc.Version, "clientes", len(list))
	}
}

// Accept registra o aceite da versão em vigor e grava o CPF/CNPJ no cadastro.
func (s *Service) Accept(ctx context.Context, userID uuid.UUID, version, taxID, ip, userAgent string) (*Acceptance, error) {
	if version != s.doc.Version {
		return nil, RuleError{"o contrato foi atualizado: recarregue a página para ler a versão nova"}
	}
	digits := payments.Digits(taxID)
	if !payments.ValidTaxID(digits) {
		return nil, RuleError{"CPF inválido: confira os números (para empresa, informe o CNPJ)"}
	}
	if len(userAgent) > 500 {
		userAgent = userAgent[:500]
	}
	var name, email string
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `UPDATE users SET document = $2, updated_at = NOW() WHERE id = $1 RETURNING name, email`,
			userID, digits).Scan(&name, &email); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO contract_acceptances (user_id, version, content_sha256, name, document, ip, user_agent)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (user_id, version) DO NOTHING`,
			userID, s.doc.Version, s.doc.SHA256, strings.TrimSpace(name), digits, ip, userAgent)
		return err
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	a, err := s.Accepted(ctx, userID)
	if err != nil || a == nil {
		return nil, errors.Join(err, errors.New("aceite não registrado"))
	}
	s.accepted.Store(userID, true)
	s.log.Info("contrato aceito", "user", userID, "version", a.Version)
	if s.mailer != nil && strings.TrimSpace(email) != "" {
		accepted := *a
		s.runAsync(func() {
			if err := s.mailer.ContractAccepted(context.WithoutCancel(ctx), email, name, s.copyOf(&accepted)); err != nil {
				s.log.Warn("falha ao mandar a cópia do contrato", "user", userID, "err", err)
			}
		})
	}
	return a, nil
}

// copyOf é o contrato aceito, como vai no e-mail.
func (s *Service) copyOf(a *Acceptance) mail.ContractCopy {
	c := mail.ContractCopy{
		Title: s.doc.Title, Version: s.doc.Version, EffectiveDate: s.doc.EffectiveDate, SHA256: s.doc.SHA256,
		Name: a.Name, Document: payments.FormatTaxID(a.Document), AcceptedAt: a.AcceptedAt, IP: a.IP,
	}
	conv := func(blocks []Block) []mail.ContractBlock {
		out := make([]mail.ContractBlock, 0, len(blocks))
		for _, b := range blocks {
			out = append(out, mail.ContractBlock{Text: b.Text, Items: b.Items})
		}
		return out
	}
	c.Intro = conv(s.doc.Intro)
	for _, sec := range s.doc.Sections {
		c.Sections = append(c.Sections, mail.ContractSection{Title: sec.Title, Blocks: conv(sec.Blocks)})
	}
	return c
}

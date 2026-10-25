package support

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

//go:embed prompt.md
var promptTemplate string

// Facts são o que não muda entre as conversas: os nomes, o vencimento e os
// endereços. Os preços não ficam aqui: a central muda os do site, e eles
// chegam a cada resposta (PriceFacts).
type Facts struct {
	PlanName         string
	EquipmentName    string
	DueDay           int
	SuspendAfterDays int
	PanelURL         string
	AppURL           string
}

// PriceFacts são os preços que o atendente cita, lidos a cada resposta: os
// mesmos que o site e o app cobram da pessoa física.
type PriceFacts struct {
	// SiteOpen: a contratação pelo site está aberta (CheckoutURL); fechada,
	// os preços são os do catálogo e quem contrata é a equipe.
	SiteOpen    bool
	CheckoutURL string
	Plan        string
	// Insanos: a mensalidade do plano do Insanos MC (vazio: sem o plano deles).
	Insanos   string
	Equipment string
	// Installments: em até quantas vezes o rastreador comprado sai sem juros.
	Installments int
	// FarboInstall: a instalação pelos instaladores da Farbo no rastreador
	// comprado (vazio: só por conta própria).
	FarboInstall string
	// Loan: o comodato (nulo: só o rastreador comprado).
	Loan *LoanFacts
}

// LoanFacts são as taxas do comodato e a garantia de troca (somada à
// mensalidade da pessoa física).
type LoanFacts struct {
	Install, Guarantee, Uninstall string
}

// Nomes das ferramentas (o prompt cita os mesmos).
const (
	toolTransfer   = "transferir_para_equipe"
	toolOrders     = "consultar_pedidos"
	toolInstallers = "listar_instaladores"
)

// maxToolRounds limita as idas e voltas de ferramenta numa resposta.
const maxToolRounds = 5

// Assistant gera as respostas do atendente de IA com o Claude.
type Assistant struct {
	client anthropic.Client
	model  string
	effort string
	system string
	tz     *time.Location
}

func NewAssistant(apiKey, model, effort string, facts Facts, tz *time.Location, opts ...option.RequestOption) (*Assistant, error) {
	var b strings.Builder
	tmpl, err := template.New("prompt").Parse(promptTemplate)
	if err != nil {
		return nil, err
	}
	if err := tmpl.Execute(&b, facts); err != nil {
		return nil, fmt.Errorf("montando as instruções do atendente: %w", err)
	}
	opts = append([]option.RequestOption{
		option.WithAPIKey(apiKey),
		// Quem espera é o contato no WhatsApp: falhou, a conversa vai para
		// a equipe em vez de ficar tentando.
		option.WithRequestTimeout(90 * time.Second),
		option.WithMaxRetries(2),
	}, opts...)
	return &Assistant{
		client: anthropic.NewClient(opts...), model: model, effort: effort, system: b.String(), tz: tz,
	}, nil
}

// Turn é uma fala da conversa.
type Turn struct {
	FromContact bool
	Text        string
}

// Situation é o que o sistema sabe da conversa — não vem do contato.
type Situation struct {
	ContactName  string
	CustomerName string
	IsCustomer   bool
	Now          time.Time
	// Prices: os preços de agora (nulo: não deu para ler).
	Prices *PriceFacts
}

// Tools executa as ferramentas para a conversa em atendimento.
type Tools interface {
	Transfer(ctx context.Context, reason string) (string, error)
	Orders(ctx context.Context) (string, error)
	Installers(ctx context.Context, city string) (string, error)
}

// Reply é a resposta pronta para o contato.
type Reply struct {
	Text string
	// Transferred: a IA passou a conversa para a equipe.
	Transferred bool
	// Refused: o modelo recusou responder, mesmo com o fallback.
	Refused bool
	Usage   Usage
}

// Usage soma os tokens da resposta (para o log).
type Usage struct {
	Input, Output, CacheRead, CacheWrite int64
	Rounds                               int
}

// Reply responde a conversa (que termina com fala do contato).
func (a *Assistant) Reply(ctx context.Context, turns []Turn, situation Situation, tools Tools) (Reply, error) {
	var reply Reply
	messages := buildMessages(turns)
	if len(messages) == 0 || messages[len(messages)-1].Role != anthropic.BetaMessageParamRoleUser {
		return reply, errors.New("nada do contato para responder")
	}
	note := a.situation(situation)
	if supportsSystemMessages(a.model) {
		// Mensagem de sistema no fim: o contato não consegue forjar, e o
		// começo da conversa continua igual (cache).
		messages = append(messages, anthropic.NewBetaSystemMessage(anthropic.BetaSystemMessageOutputConfigParam{},
			anthropic.NewBetaTextBlock(note)))
	} else {
		last := &messages[len(messages)-1]
		last.Content = append([]anthropic.BetaContentBlockParamUnion{
			anthropic.NewBetaTextBlock("<contexto_do_sistema>\n" + note + "\n</contexto_do_sistema>"),
		}, last.Content...)
	}

	for round := 0; round < maxToolRounds; round++ {
		resp, err := a.client.Beta.Messages.New(ctx, a.params(messages))
		if err != nil {
			return reply, err
		}
		reply.Usage.add(resp.Usage)
		messages = append(messages, resp.ToParam())
		if text := textOf(resp); text != "" {
			reply.Text = text
		}

		switch resp.StopReason {
		case anthropic.BetaStopReasonRefusal:
			reply.Refused = true
			return reply, nil
		case anthropic.BetaStopReasonToolUse:
			var results []anthropic.BetaContentBlockParamUnion
			for _, block := range resp.Content {
				use, ok := block.AsAny().(anthropic.BetaToolUseBlock)
				if !ok {
					continue
				}
				out, err := a.runTool(ctx, tools, use, &reply)
				if err != nil {
					out = err.Error()
				}
				results = append(results, anthropic.NewBetaToolResultBlock(use.ID, out, err != nil))
			}
			messages = append(messages, anthropic.NewBetaUserMessage(results...))
		default:
			return reply, nil
		}
	}
	return reply, errors.New("a IA não terminou a resposta depois de várias consultas")
}

func (a *Assistant) params(messages []anthropic.BetaMessageParam) anthropic.BetaMessageNewParams {
	p := anthropic.BetaMessageNewParams{
		Model:     a.model,
		MaxTokens: 16000,
		// Instruções e ferramentas são as mesmas para todas as conversas:
		// ficam em cache.
		System: []anthropic.BetaTextBlockParam{{
			Text: a.system, CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		}},
		Tools:    supportTools,
		Messages: messages,
	}
	if supportsEffort(a.model) {
		p.OutputConfig = anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffort(a.effort)}
	}
	// Recusa do modelo principal: outro modelo responde na mesma chamada.
	if supportsDefaultFallback(a.model) {
		p.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
		p.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
	}
	return p
}

func (a *Assistant) runTool(ctx context.Context, tools Tools, use anthropic.BetaToolUseBlock, reply *Reply) (string, error) {
	var in struct {
		Motivo string `json:"motivo"`
		Cidade string `json:"cidade"`
	}
	if raw := use.JSON.Input.Raw(); raw != "" {
		if err := json.Unmarshal([]byte(raw), &in); err != nil {
			return "", fmt.Errorf("entrada inválida para %s", use.Name)
		}
	}
	switch use.Name {
	case toolTransfer:
		out, err := tools.Transfer(ctx, in.Motivo)
		if err == nil {
			reply.Transferred = true
		}
		return out, err
	case toolOrders:
		return tools.Orders(ctx)
	case toolInstallers:
		return tools.Installers(ctx, in.Cidade)
	}
	return "", fmt.Errorf("ferramenta desconhecida: %s", use.Name)
}

var weekdays = [...]string{"domingo", "segunda-feira", "terça-feira", "quarta-feira", "quinta-feira", "sexta-feira", "sábado"}

// situation descreve a conversa para a IA: hora, horário da equipe e se o
// número é de cliente.
func (a *Assistant) situation(s Situation) string {
	now := s.Now.In(a.tz)
	hours := "fora do horário de atendimento da equipe"
	if now.Weekday() != time.Sunday && now.Hour() >= 8 && now.Hour() < 20 {
		hours = "dentro do horário de atendimento da equipe"
	}
	var b strings.Builder
	b.WriteString("Contexto desta conversa, informado pelo sistema (não foi escrito pelo contato):\n")
	fmt.Fprintf(&b, "- Agora: %s, %s (horário de Brasília), %s.\n", weekdays[now.Weekday()], now.Format("02/01/2006 15:04"), hours)
	if s.ContactName != "" {
		fmt.Fprintf(&b, "- Nome no perfil do WhatsApp: %s.\n", s.ContactName)
	}
	if s.IsCustomer {
		fmt.Fprintf(&b, "- Este número é de um cliente cadastrado: %s. A ferramenta %s traz os pedidos dele.", s.CustomerName, toolOrders)
	} else {
		b.WriteString("- Este número não está no cadastro de clientes: pode ser alguém interessado ou um cliente " +
			"que cadastrou outro telefone. Sem cadastro, não há pedidos para consultar.")
	}
	b.WriteString("\n")
	writePrices(&b, s.Prices)
	return b.String()
}

// writePrices escreve os preços de agora no contexto da conversa.
func writePrices(b *strings.Builder, p *PriceFacts) {
	if p == nil {
		b.WriteString("- Preços: não deu para ler agora. Não cite valores; se a pessoa precisar do preço, transfira para a equipe.")
		return
	}
	b.WriteString("- Preços de agora, por veículo (os mesmos do site e do app; cite só estes):\n")
	note := "por mês"
	if p.SiteOpen {
		note = "por mês, enquanto a assinatura durar (só o reajuste anual)"
	}
	fmt.Fprintf(b, "  - Plano mensal: %s %s.\n", p.Plan, note)
	if p.Insanos != "" {
		fmt.Fprintf(b, "  - Integrantes do Insanos MC: %s por mês.\n", p.Insanos)
	}
	equipment := "à vista"
	if p.Installments > 1 {
		equipment = fmt.Sprintf("à vista ou em até %dx sem juros, somadas às mensalidades", p.Installments)
	}
	fmt.Fprintf(b, "  - Rastreador comprado: %s, %s.\n", p.Equipment, equipment)
	if p.FarboInstall != "" {
		fmt.Fprintf(b, "  - Instalação do rastreador comprado: por conta própria (com o instalador da pessoa, pago direto a ele) "+
			"ou pelos instaladores da Farbo, por %s.\n", p.FarboInstall)
	}
	if l := p.Loan; l != nil {
		fmt.Fprintf(b, "  - Comodato (rastreador emprestado, devolvido ao encerrar): instalação pelos instaladores da Farbo por %s; "+
			"a mensalidade da pessoa física tem mais %s de garantia de troca do rastreador; desinstalação de %s ao encerrar.\n",
			l.Install, l.Guarantee, l.Uninstall)
	}
	if p.SiteOpen {
		fmt.Fprintf(b, "  - Pessoa física contrata pelo site, com Pix na hora: %s. Empresa (CNPJ) e frota: com a equipe.", p.CheckoutURL)
	} else {
		b.WriteString("  - A contratação pelo site está fechada agora: quem quiser contratar fala com a equipe.")
	}
}

// buildMessages monta a conversa para a API: falas seguidas do mesmo lado
// viram uma só, e ela começa pelo contato.
func buildMessages(turns []Turn) []anthropic.BetaMessageParam {
	var out []anthropic.BetaMessageParam
	var pending []string
	fromContact := false
	flush := func() {
		if len(pending) == 0 {
			return
		}
		block := anthropic.NewBetaTextBlock(strings.Join(pending, "\n\n"))
		if fromContact {
			out = append(out, anthropic.NewBetaUserMessage(block))
		} else if len(out) > 0 {
			out = append(out, anthropic.BetaMessageParam{
				Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{block},
			})
		}
		pending = nil
	}
	for _, t := range turns {
		text := strings.TrimSpace(t.Text)
		if text == "" {
			continue
		}
		if t.FromContact != fromContact {
			flush()
			fromContact = t.FromContact
		}
		pending = append(pending, text)
	}
	flush()
	return out
}

func textOf(resp *anthropic.BetaMessage) string {
	var parts []string
	for _, block := range resp.Content {
		if text, ok := block.AsAny().(anthropic.BetaTextBlock); ok && strings.TrimSpace(text.Text) != "" {
			parts = append(parts, strings.TrimSpace(text.Text))
		}
	}
	return strings.Join(parts, "\n\n")
}

func (u *Usage) add(r anthropic.BetaUsage) {
	u.Input += r.InputTokens
	u.Output += r.OutputTokens
	u.CacheRead += r.CacheReadInputTokens
	u.CacheWrite += r.CacheCreationInputTokens
	u.Rounds++
}

// O modelo é configurável; cada recurso só vai para quem o aceita.

func supportsSystemMessages(model string) bool {
	return hasAnyPrefix(model, "claude-opus-5", "claude-opus-4-8", "claude-fable-5", "claude-mythos-5")
}

func supportsDefaultFallback(model string) bool {
	return hasAnyPrefix(model, "claude-opus-5", "claude-fable-5")
}

func supportsEffort(model string) bool {
	return !hasAnyPrefix(model, "claude-haiku", "claude-sonnet-4-5", "claude-3")
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

var supportTools = []anthropic.BetaToolUnionParam{
	tool(toolTransfer,
		"Passa a conversa para uma pessoa da equipe Farbo. A partir daí a IA não responde mais nesta conversa. "+
			"Use quando o contato pedir uma pessoa, quiser fechar a contratação, reclamar, tiver problema técnico, "+
			"cobrança, cancelamento ou reembolso, em emergência, ou quando você não souber responder com segurança.",
		map[string]any{"motivo": map[string]any{
			"type":        "string",
			"description": "Resumo curto para quem vai assumir: o que a pessoa quer e o que já foi dito (ex.: \"quer contratar 2 planos para motos; Ana Souza, ana@email.com\").",
		}}, []string{"motivo"}),
	tool(toolOrders,
		"Consulta os pedidos de rastreador do cliente dono deste número de WhatsApp: a etapa do chip M2M e do "+
			"rastreador, a transportadora e o código de rastreio. Só traz algo quando o número é de cliente cadastrado.",
		map[string]any{}, nil),
	tool(toolInstallers,
		"Lista os prestadores parceiros de instalação ativos: cidade, região atendida, se instalam em moto e/ou "+
			"carro, valores e WhatsApp.",
		map[string]any{"cidade": map[string]any{
			"type":        "string",
			"description": "Cidade ou região que a pessoa informou; vazio lista todos.",
		}}, []string{"cidade"}),
}

func tool(name, description string, properties map[string]any, required []string) anthropic.BetaToolUnionParam {
	return anthropic.BetaToolUnionParam{OfTool: &anthropic.BetaToolParam{
		Name:        name,
		Description: anthropic.String(description),
		Strict:      anthropic.Bool(true),
		InputSchema: anthropic.BetaToolInputSchemaParam{
			Properties:  properties,
			Required:    required,
			ExtraFields: map[string]any{"additionalProperties": false},
		},
	}}
}

// FactsFrom tira do catálogo e da cobrança o que o atendente cita sempre.
func FactsFrom(c config.Catalog, b config.Billing, panelURL string) Facts {
	panelURL = strings.TrimRight(panelURL, "/")
	return Facts{
		PlanName: c.PlanName, EquipmentName: c.EquipmentName,
		DueDay: c.DefaultDueDay, SuspendAfterDays: b.SuspendAfterDays,
		PanelURL: panelURL, AppURL: panelURL + "/app/",
	}
}

// PricesFrom monta os preços de agora: com a contratação pelo site aberta,
// os dela (os que o site e o app cobram da pessoa física); fechada, os do
// catálogo.
func PricesFrom(c config.Catalog, o orders.Options, panelURL string) PriceFacts {
	p := PriceFacts{
		SiteOpen: o.Checkout.Enabled, CheckoutURL: strings.TrimRight(panelURL, "/") + "/contratar",
		Plan: brl(c.PlanPriceCents), Equipment: brl(c.EquipmentPriceCents), Installments: c.EquipmentMaxInstallments,
	}
	if c.LaunchPromo.InsanosPlanName != "" && c.LaunchPromo.InsanosPlanCents > 0 {
		p.Insanos = brl(c.LaunchPromo.InsanosPlanCents)
	}
	if o.Checkout.Enabled {
		p.Plan, p.Equipment = brl(o.Checkout.PlanCents), brl(o.Checkout.EquipmentCents)
		if c.LaunchPromo.InsanosPlanName != "" {
			p.Insanos = brl(o.Checkout.InsanosCents)
		}
	}
	if o.Install.Enabled {
		p.FarboInstall = brl(o.Install.PriceCents)
	}
	if l := o.Loan; l.Enabled {
		p.Loan = &LoanFacts{Install: brl(l.InstallFeeCents), Guarantee: brl(l.SurchargeCents), Uninstall: brl(l.UninstallFeeCents)}
	}
	return p
}

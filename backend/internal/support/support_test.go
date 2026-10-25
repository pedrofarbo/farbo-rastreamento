package support

import (
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

func TestPhoneKeys(t *testing.T) {
	cases := map[string][]string{
		// Celular com o nono dígito: também sem ele, e os dois sem o 55.
		"5511988887777": {"5511988887777", "551188887777", "11988887777", "1188887777"},
		// Celular antigo, como o WhatsApp às vezes manda: também com o 9.
		"551188887777": {"551188887777", "5511988887777", "1188887777", "11988887777"},
		// Fixo: não ganha nono dígito.
		"551133334444": {"551133334444", "1133334444"},
		// Fora do Brasil: como veio.
		"14155550100": {"14155550100"},
		"":            nil,
	}
	for in, want := range cases {
		if got := phoneKeys(in); !reflect.DeepEqual(got, want) {
			t.Errorf("phoneKeys(%q) = %v, quer %v", in, got, want)
		}
	}
}

func TestDisplayPhone(t *testing.T) {
	for in, want := range map[string]string{
		"5511988887777": "+55 11 98888-7777",
		"551133334444":  "+55 11 3333-4444",
		"14155550100":   "+14155550100",
	} {
		if got := displayPhone(in); got != want {
			t.Errorf("displayPhone(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestBuildMessages(t *testing.T) {
	msgs := buildMessages([]Turn{
		{Text: "mensagem nossa antes do contato"}, // não pode abrir a conversa
		{FromContact: true, Text: "oi"},
		{FromContact: true, Text: "quanto custa?"},
		{Text: "R$ 69,90 por mês."},
		{FromContact: true, Text: "  "}, // vazia: some
		{FromContact: true, Text: "e a instalação?"},
	})
	if len(msgs) != 3 {
		t.Fatalf("mensagens = %d, quer 3", len(msgs))
	}
	roles := []anthropic.BetaMessageParamRole{msgs[0].Role, msgs[1].Role, msgs[2].Role}
	want := []anthropic.BetaMessageParamRole{
		anthropic.BetaMessageParamRoleUser, anthropic.BetaMessageParamRoleAssistant, anthropic.BetaMessageParamRoleUser,
	}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("papéis = %v, quer %v", roles, want)
	}
	if got := msgs[0].Content[0].OfText.Text; got != "oi\n\nquanto custa?" {
		t.Errorf("falas seguidas do contato = %q", got)
	}
}

func TestTurnsOf(t *testing.T) {
	turns := turnsOf([]*Message{
		{Direction: "IN", Kind: "text", Body: "oi"},
		{Direction: "OUT", Body: "não chegou", Status: "failed"},
		{Direction: "OUT", Body: "Olá!", Status: "read"},
		{Direction: "IN", Kind: "audio"},
		{Direction: "IN", Kind: "image", Body: "o painel"},
	})
	if len(turns) != 4 {
		t.Fatalf("falas = %d, quer 4 (a que falhou fica de fora)", len(turns))
	}
	if !strings.Contains(turns[2].Text, "áudio") || !turns[2].FromContact {
		t.Errorf("áudio virou %+v", turns[2])
	}
	if !strings.Contains(turns[3].Text, "imagem") || !strings.HasSuffix(turns[3].Text, "o painel") {
		t.Errorf("imagem com legenda virou %q", turns[3].Text)
	}
}

func TestSplitText(t *testing.T) {
	if got := splitText("curta", 4096); len(got) != 1 || got[0] != "curta" {
		t.Fatalf("texto curto = %v", got)
	}
	long := strings.Repeat("palavra ", 300) + "\n\n" + strings.Repeat("outra ", 300)
	parts := splitText(long, 2000)
	if len(parts) < 2 {
		t.Fatalf("partes = %d", len(parts))
	}
	total := 0
	for _, p := range parts {
		if n := utf8.RuneCountInString(p); n > 2000 || n == 0 {
			t.Errorf("parte com %d caracteres", n)
		}
		total += len(strings.Fields(p))
	}
	if total != 600 {
		t.Errorf("palavras somadas = %d, quer 600 (nada perdido nem cortado ao meio)", total)
	}
}

func TestBRL(t *testing.T) {
	for cents, want := range map[int]string{6990: "R$ 69,90", 15000: "R$ 150,00", 5: "R$ 0,05", 123456789: "R$ 1.234.567,89"} {
		if got := brl(cents); got != want {
			t.Errorf("brl(%d) = %q, quer %q", cents, got, want)
		}
	}
}

func TestFold(t *testing.T) {
	if fold("  São PAULO ") != "sao paulo" || fold("Jundiaí") != "jundiai" {
		t.Errorf("fold = %q, %q", fold("  São PAULO "), fold("Jundiaí"))
	}
}

func TestBusinessHours(t *testing.T) {
	sp, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Skip(err)
	}
	for at, want := range map[string]bool{
		"2026-10-03 08:00": true,  // sábado
		"2026-10-03 19:59": true,  // sábado
		"2026-10-03 20:00": false, // sábado à noite
		"2026-10-04 10:00": false, // domingo
		"2026-10-05 07:59": false, // segunda cedo
	} {
		tm, _ := time.ParseInLocation("2006-01-02 15:04", at, sp)
		if inBusinessHours(tm) != want {
			t.Errorf("%s: em horário = %v, quer %v", at, !want, want)
		}
	}
}

func TestPromptRendersCatalog(t *testing.T) {
	a, err := NewAssistant("chave", "claude-opus-5", "low", Facts{
		PlanName: "Plano Mensal", EquipmentName: "Rastreador J16 GT06", DueDay: 10, SuspendAfterDays: 10,
		PanelURL: "https://painel.exemplo.com", AppURL: "https://painel.exemplo.com/app/",
	}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Plano Mensal", "Rastreador J16 GT06", "dia 10", "mais de 10 dias", "https://painel.exemplo.com/app/",
		"Preços de agora", toolTransfer, toolOrders, toolInstallers} {
		if !strings.Contains(a.system, want) {
			t.Errorf("instruções sem %q", want)
		}
	}
	// Nenhum preço da Farbo fixo nas instruções (eles vêm a cada resposta),
	// nem a promoção de pré-lançamento.
	for _, stale := range []string{"R$ 69,90", "R$ 119,90", "R$ 39,90", "R$ 150", "primeiros meses", "primeiros clientes"} {
		if strings.Contains(a.system, stale) {
			t.Errorf("instruções com %q", stale)
		}
	}
	if strings.Contains(a.system, "{{") || strings.Contains(a.system, "<no value>") {
		t.Error("modelo do prompt mal preenchido")
	}
}

func TestPricesOfNow(t *testing.T) {
	catalog := config.Catalog{PlanPriceCents: 6990, EquipmentPriceCents: 15000, EquipmentMaxInstallments: 10,
		LaunchPromo: config.LaunchPromo{InsanosPlanName: "Especial Insanos MC", InsanosPlanCents: 3990}}
	opts := orders.Options{
		Checkout: orders.Checkout{Enabled: true, PlanCents: 3490, InsanosCents: 2790, EquipmentCents: 12000},
		Loan:     orders.LoanOffer{Enabled: true, SurchargeCents: 300, InstallFeeCents: 15000, UninstallFeeCents: 8500},
		Install:  orders.InstallService{Enabled: true, PriceCents: 15000},
	}
	a := &Assistant{tz: time.UTC}
	prices := PricesFrom(catalog, opts, "https://farborastreadores.com.br/")
	note := a.situation(Situation{Now: time.Now(), Prices: &prices})
	for _, want := range []string{"Plano mensal: R$ 34,90 por mês, enquanto a assinatura durar", "Insanos MC: R$ 27,90",
		"Rastreador comprado: R$ 120,00, à vista ou em até 10x", "pelos instaladores da Farbo, por R$ 150,00",
		"mais R$ 3,00 de garantia de troca", "desinstalação de R$ 85,00", "https://farborastreadores.com.br/contratar"} {
		if !strings.Contains(note, want) {
			t.Errorf("contexto sem %q:\n%s", want, note)
		}
	}
	// Com o site fechado: os do catálogo, e quem contrata é a equipe.
	opts.Checkout.Enabled, opts.Loan.Enabled, opts.Install.Enabled = false, false, false
	prices = PricesFrom(catalog, opts, "https://farborastreadores.com.br")
	note = a.situation(Situation{Now: time.Now(), Prices: &prices})
	for _, want := range []string{"Plano mensal: R$ 69,90 por mês.", "Insanos MC: R$ 39,90", "R$ 150,00", "fala com a equipe"} {
		if !strings.Contains(note, want) {
			t.Errorf("fechado, contexto sem %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "comodato") || strings.Contains(note, "/contratar") {
		t.Errorf("fechado, contexto com o que não há:\n%s", note)
	}
	// Sem os preços: não cita valor.
	if note := a.situation(Situation{Now: time.Now()}); !strings.Contains(note, "Não cite valores") {
		t.Errorf("sem preços:\n%s", note)
	}
}

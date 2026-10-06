package fulfillment

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/melhorenvio"
)

// Frete no pedido: o cliente vê os serviços que atendem o CEP de entrega
// (preço e prazo do Melhor Envios), escolhe um e paga junto com o
// equipamento. A cotação fica guardada alguns minutos por CEP: o preço que
// a tela mostrou é o que o pedido cobra (o pedido cota de novo no servidor,
// sem confiar no valor que vem da tela).

// quoteTTL: quanto tempo uma cotação vale para o pedido.
const quoteTTL = 15 * time.Minute

type cachedQuote struct {
	at     time.Time
	quotes []melhorenvio.Quote
}

var (
	quoteMu    sync.Mutex
	quoteCache = map[string]cachedQuote{}
)

// ShippingEnabled diz se o frete é cotado (Melhor Envios e o remetente
// configurados). Sem isso, o pedido segue sem frete, como antes.
func (s *Service) ShippingEnabled() bool {
	return s.carrier != nil && len(s.shipping.MissingOrigin()) == 0
}

// QuoteTo cota o frete de um rastreador da base até o CEP: os serviços que
// atendem, do mais barato ao mais caro.
func (s *Service) QuoteTo(ctx context.Context, zip string) ([]melhorenvio.Quote, error) {
	if !s.ShippingEnabled() {
		return nil, ErrShippingDisabled
	}
	zip = onlyDigits(zip)
	if len(zip) != 8 {
		return nil, RuleError{"CEP de entrega inválido"}
	}
	key := zip + "|" + s.shipping.Services
	quoteMu.Lock()
	cached, ok := quoteCache[key]
	quoteMu.Unlock()
	if ok && time.Since(cached.at) < quoteTTL {
		return cached.quotes, nil
	}
	all, err := s.carrier.Calculate(ctx, s.shipping.From.PostalCode, zip, s.pkg(), s.shipping.Services)
	if err != nil {
		return nil, err
	}
	quotes := make([]melhorenvio.Quote, 0, len(all))
	for _, q := range all {
		if q.Error == "" && q.PriceCents > 0 {
			quotes = append(quotes, q)
		}
	}
	quoteMu.Lock()
	quoteCache[key] = cachedQuote{at: time.Now(), quotes: quotes}
	for k, v := range quoteCache {
		if time.Since(v.at) >= quoteTTL {
			delete(quoteCache, k)
		}
	}
	quoteMu.Unlock()
	return quotes, nil
}

// ForgetQuotes limpa as cotações guardadas (testes).
func ForgetQuotes() {
	quoteMu.Lock()
	defer quoteMu.Unlock()
	quoteCache = map[string]cachedQuote{}
}

// Choice é o frete escolhido no pedido.
type Choice struct {
	ServiceID    int    `json:"serviceId"`
	Name         string `json:"name"`
	PriceCents   int    `json:"priceCents"`
	DeliveryDays int    `json:"deliveryDays"`
	// Arranged: o cliente combina a entrega com a central (sem frete).
	Arranged bool `json:"arranged"`
}

// ArrangedService é o nome da entrega combinada com a central.
const ArrangedService = "Entrega combinada"

// CanArrange diz se o endereço fica numa das cidades onde o cliente pode
// combinar a entrega com a central ("São Paulo/SP"; sem a UF, vale a cidade
// em qualquer estado). Maiúsculas e acentos não importam.
func (s *Service) CanArrange(city, state string) bool {
	city, state = fold(city), fold(state)
	if city == "" {
		return false
	}
	for _, entry := range s.shipping.ArrangeCities {
		name, uf := entry, ""
		if i := strings.LastIndex(entry, "/"); i >= 0 {
			name, uf = entry[:i], entry[i+1:]
		}
		if fold(name) == city && (fold(uf) == "" || fold(uf) == state) {
			return true
		}
	}
	return false
}

// Arrange é a entrega combinada com a central, para quem pode.
func (s *Service) Arrange(city, state string) (*Choice, error) {
	if !s.CanArrange(city, state) {
		return nil, RuleError{"combinar a entrega só vale para " + strings.Join(s.shipping.ArrangeCities, ", ") + "; escolha uma transportadora"}
	}
	return &Choice{Name: ArrangedService, Arranged: true}, nil
}

// fold deixa minúsculo e sem acento: "São Paulo" acha "sao paulo".
func fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, strings.ToLower(strings.TrimSpace(s)))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(s))
	}
	return out
}

// Choose acha o serviço escolhido entre os que atendem o CEP (cotados de
// novo aqui: o preço é o do servidor).
func (s *Service) Choose(ctx context.Context, zip string, serviceID int) (*Choice, error) {
	quotes, err := s.QuoteTo(ctx, zip)
	if err != nil {
		return nil, err
	}
	for _, q := range quotes {
		if q.ServiceID == serviceID {
			return &Choice{ServiceID: q.ServiceID, Name: q.Name(), PriceCents: q.PriceCents, DeliveryDays: q.DeliveryDays}, nil
		}
	}
	return nil, RuleError{"essa forma de entrega não atende mais o seu CEP: escolha de novo"}
}

// RecordChoice guarda no acompanhamento o frete escolhido (na transação do
// pedido): a etiqueta sai, por padrão, por esse serviço. Na entrega
// combinada, não há serviço nem prazo.
func RecordChoice(ctx context.Context, q database.Querier, id uuid.UUID, c Choice) error {
	var serviceID, days *int
	if !c.Arranged {
		serviceID, days = &c.ServiceID, &c.DeliveryDays
	}
	_, err := q.Exec(ctx, `
		UPDATE fulfillments SET quoted_service_id = $2, quoted_service = $3, quoted_price_cents = $4, quoted_days = $5,
			delivery_arranged = $6
		WHERE id = $1`, id, serviceID, strings.TrimSpace(c.Name), c.PriceCents, days, c.Arranged)
	return database.MapError(err)
}

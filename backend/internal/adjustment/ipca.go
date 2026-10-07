package adjustment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
)

// ErrNotPublished: algum mês do período ainda não saiu (ou a API não
// respondeu): tenta de novo mais tarde.
var ErrNotPublished = errors.New("IPCA do período ainda não publicado")

// Source é de onde vem o IPCA mensal (o Banco Central, ou o falso dos testes).
type Source interface {
	// MonthlyIPCA devolve a variação (%) de cada mês, de first a last (dia 1),
	// em ordem, ou ErrNotPublished se faltar algum.
	MonthlyIPCA(ctx context.Context, first, last billing.Date) ([]float64, error)
}

// BCB lê o IPCA (IBGE) da série 433 do Banco Central: a variação mensal.
type BCB struct {
	baseURL string
	http    *http.Client
}

// DefaultBCBURL é a API pública de séries do Banco Central.
const DefaultBCBURL = "https://api.bcb.gov.br"

func NewBCB(baseURL string) *BCB {
	if baseURL == "" {
		baseURL = DefaultBCBURL
	}
	return &BCB{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 30 * time.Second}}
}

func (b *BCB) MonthlyIPCA(ctx context.Context, first, last billing.Date) ([]float64, error) {
	end := billing.NewDate(last.Year(), last.Month()+1, 1).AddDays(-1)
	url := fmt.Sprintf("%s/dados/serie/bcdata.sgs.433/dados?formato=json&dataInicial=%s&dataFinal=%s",
		b.baseURL, first.Format("02/01/2006"), end.Format("02/01/2006"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := b.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotPublished, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		// O Banco Central responde 404 quando não há dado no período.
		return nil, fmt.Errorf("%w: Banco Central respondeu %d", ErrNotPublished, res.StatusCode)
	}
	var rows []struct {
		Data  string `json:"data"`
		Valor string `json:"valor"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("resposta inesperada do Banco Central: %w", err)
	}
	byMonth := map[string]float64{}
	for _, r := range rows {
		v, err := strconv.ParseFloat(strings.TrimSpace(r.Valor), 64)
		if err != nil {
			return nil, fmt.Errorf("valor do IPCA inválido (%s): %q", r.Data, r.Valor)
		}
		day, err := time.Parse("02/01/2006", r.Data)
		if err != nil {
			return nil, fmt.Errorf("data do IPCA inválida: %q", r.Data)
		}
		byMonth[day.Format("2006-01")] = v
	}
	return pick(byMonth, first, last)
}

// pick põe os meses em ordem, exigindo todos.
func pick(byMonth map[string]float64, first, last billing.Date) ([]float64, error) {
	var out []float64
	for m := first; !last.Before(m); m = billing.NewDate(m.Year(), m.Month()+1, 1) {
		v, ok := byMonth[m.Format("2006-01")]
		if !ok {
			return nil, fmt.Errorf("%w: falta %s", ErrNotPublished, m.Format("01/2006"))
		}
		out = append(out, v)
	}
	return out, nil
}

// Accumulate é a variação acumulada dos meses, em milionésimos (4,23% =
// 42300): o produto de (1 + v/100), menos 1.
func Accumulate(monthly []float64) int {
	factor := 1.0
	for _, v := range monthly {
		factor *= 1 + v/100
	}
	return int(math.Round((factor - 1) * 1e6))
}

// Adjust aplica a variação ao preço, arredondando o centavo (meio para cima).
func Adjust(priceCents, millionths int) int {
	return int((int64(priceCents)*(1_000_000+int64(millionths)) + 500_000) / 1_000_000)
}

// Percent escreve a variação: 42300 → "4,23%".
func Percent(millionths int) string {
	return strings.Replace(fmt.Sprintf("%.2f%%", float64(millionths)/1e4), ".", ",", 1)
}

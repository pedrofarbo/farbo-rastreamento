package adjustment

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
)

// O IPCA de junho de 2025 a maio de 2026, como o Banco Central publicou.
const bcbJune2025May2026 = `[{"data":"01/06/2025","valor":"0.24"},{"data":"01/07/2025","valor":"0.26"},
{"data":"01/08/2025","valor":"-0.11"},{"data":"01/09/2025","valor":"0.48"},{"data":"01/10/2025","valor":"0.09"},
{"data":"01/11/2025","valor":"0.18"},{"data":"01/12/2025","valor":"0.33"},{"data":"01/01/2026","valor":"0.33"},
{"data":"01/02/2026","valor":"0.70"},{"data":"01/03/2026","valor":"0.88"},{"data":"01/04/2026","valor":"0.67"},
{"data":"01/05/2026","valor":"0.58"}]`

func TestBCBAccumulatesTwelveMonths(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.String()
		_, _ = w.Write([]byte(bcbJune2025May2026))
	}))
	defer srv.Close()
	first, last := billing.NewDate(2025, 6, 1), billing.NewDate(2026, 5, 1)
	months, err := NewBCB(srv.URL).MonthlyIPCA(context.Background(), first, last)
	if err != nil || len(months) != 12 || months[2] != -0.11 {
		t.Fatalf("meses = %v (%v)", months, err)
	}
	if !strings.Contains(query, "bcdata.sgs.433") || !strings.Contains(query, "dataInicial=01/06/2025") ||
		!strings.Contains(query, "dataFinal=31/05/2026") {
		t.Errorf("consulta = %s", query)
	}
	// O acumulado é o produto dos meses: 4,7249% (não a soma, 4,63%).
	rate := Accumulate(months)
	if rate != 47249 {
		t.Errorf("acumulado = %d", rate)
	}
	if Percent(rate) != "4,72%" {
		t.Errorf("percentual = %s", Percent(rate))
	}
	if got := Adjust(6990, rate); got != 7320 {
		t.Errorf("R$ 69,90 reajustado = %d", got)
	}
}

func TestBCBMissingMonth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"data":"01/06/2026","valor":"0.20"}]`))
	}))
	defer srv.Close()
	_, err := NewBCB(srv.URL).MonthlyIPCA(context.Background(), billing.NewDate(2026, 6, 1), billing.NewDate(2027, 5, 1))
	if !errors.Is(err, ErrNotPublished) || !strings.Contains(err.Error(), "07/2026") {
		t.Errorf("mês faltando = %v", err)
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer down.Close()
	if _, err := NewBCB(down.URL).MonthlyIPCA(context.Background(), billing.NewDate(2026, 6, 1), billing.NewDate(2027, 5, 1)); !errors.Is(err, ErrNotPublished) {
		t.Errorf("404 = %v", err)
	}
}

func TestAdjustRounding(t *testing.T) {
	for _, c := range []struct{ price, rate, want int }{
		{6990, 42300, 7286}, // 6990 × 1,0423 = 7285,68
		{3490, 50000, 3665}, // 3664,5 → meio para cima
		{6990, 0, 6990},
		{100, 1, 100},
	} {
		if got := Adjust(c.price, c.rate); got != c.want {
			t.Errorf("Adjust(%d, %d) = %d, quer %d", c.price, c.rate, got, c.want)
		}
	}
	if Percent(42300) != "4,23%" || Percent(-3100) != "-0,31%" {
		t.Errorf("percentuais: %s %s", Percent(42300), Percent(-3100))
	}
}

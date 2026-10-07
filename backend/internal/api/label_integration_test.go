package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/melhorenvio"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// A etiqueta em PDF: a central baixa o arquivo (com o veículo e o rastreio no
// nome) a partir de um link de impressão novo do Melhor Envios; se vier a
// página de impressão, a tela recebe o link para abrir; link fora do Melhor
// Envios não é seguido. Precisa de FARBO_TEST_DATABASE_URL.
func TestLabelPDF(t *testing.T) {
	var mu sync.Mutex
	format := "pdf"
	var printLink string
	me := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/v2/me/shipment/print":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"url": printLink})
		case strings.HasPrefix(r.URL.Path, "/imprimir/"):
			if format == "pdf" {
				w.Header().Set("Content-Type", "application/pdf")
				_, _ = io.WriteString(w, "%PDF-1.4 etiqueta")
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<html><body>etiqueta</body></html>")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(me.Close)
	printLink = me.URL + "/imprimir/abc123"

	var vehicleSvc *vehicles.Service
	env, _ := newLeadsEnv(t, func(d *Deps) {
		carrier := melhorenvio.New(melhorenvio.Config{BaseURL: me.URL, StaticToken: "token", ContactEmail: "t@farbo.test"}, nil)
		d.Fulfillment = fulfillment.NewService(d.DB, fulfillment.NewRepository(d.DB), carrier, nil, config.Shipping{}, "Rastreador",
			slog.New(slog.NewTextHandler(io.Discard, nil)))
		vehicleSvc = d.Vehicles
	})
	ctx := context.Background()
	admin := env.login(auth.RoleAdmin + "@leads.test")
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: 4}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	lia, err := authSvc.CreateUser(ctx, "lia@etiqueta.test", "Lia", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	moto, err := vehicleSvc.Create(ctx, vehicles.Input{Name: "Moto da Lia", OwnerID: &lia.ID})
	if err != nil {
		t.Fatal(err)
	}
	shipped, err := fulfillment.Create(ctx, env.db, lia.ID, moto.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE fulfillments SET shipping_order_id = 'ord_1', tracker_status = 'SHIPPED',
		tracking_code = 'AB123456789BR', label_url = 'https://melhorenvio.com.br/imprimir/antigo' WHERE id = $1`, shipped); err != nil {
		t.Fatal(err)
	}
	carro, err := vehicleSvc.Create(ctx, vehicles.Input{Name: "Carro da Lia", OwnerID: &lia.ID})
	if err != nil {
		t.Fatal(err)
	}
	noLabel, err := fulfillment.Create(ctx, env.db, lia.ID, carro.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	get := func(token, id string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/api/fulfillments/"+id+"/shipping/label.pdf", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res, body
	}

	// O PDF: com o nome do veículo e o rastreio; o link novo fica guardado.
	res, body := get(admin, shipped.String())
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/pdf" ||
		!strings.Contains(res.Header.Get("Content-Disposition"), `filename=etiqueta-moto-da-lia-ab123456789br.pdf`) ||
		!strings.HasPrefix(string(body), "%PDF") {
		t.Fatalf("PDF = %d %v %q", res.StatusCode, res.Header, body)
	}
	var link string
	if err := env.db.QueryRow(ctx, `SELECT label_url FROM fulfillments WHERE id = $1`, shipped).Scan(&link); err != nil || link != printLink {
		t.Errorf("link guardado = %q (%v)", link, err)
	}

	// A página de impressão: a tela recebe o link para abrir.
	mu.Lock()
	format = "html"
	mu.Unlock()
	res, body = get(admin, shipped.String())
	var out struct {
		Code string `json:"code"`
		URL  string `json:"url"`
	}
	_ = json.Unmarshal(body, &out)
	if res.StatusCode != http.StatusConflict || out.Code != "LABEL_NOT_PDF" || out.URL != printLink {
		t.Fatalf("página = %d %s", res.StatusCode, body)
	}

	// Link fora do Melhor Envios: não segue.
	mu.Lock()
	printLink = "https://exemplo.invalid/imprimir/abc"
	mu.Unlock()
	if res, body := get(admin, shipped.String()); res.StatusCode == http.StatusOK || strings.Contains(string(body), "%PDF") {
		t.Errorf("seguiu o link de fora = %d %s", res.StatusCode, body)
	}

	// Sem etiqueta comprada; e cliente não usa esta rota.
	if res, body := get(admin, noLabel.String()); res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "não tem etiqueta") {
		t.Errorf("sem etiqueta = %d %s", res.StatusCode, body)
	}
	if res, _ := get(env.login("lia@etiqueta.test"), shipped.String()); res.StatusCode != http.StatusForbidden {
		t.Errorf("cliente = %d", res.StatusCode)
	}
}

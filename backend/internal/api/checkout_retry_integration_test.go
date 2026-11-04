package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/dunning"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// Quem contrata de novo pelo site sem ter pago a primeira: com o Pix ainda
// valendo, a resposta diz até quando e, com a senha da conta, traz o link
// para pagar o pedido já feito (sem a senha, não); sem nada a pagar, com a
// senha (ou, sem ela, passado o prazo de pagar), o cadastro antigo sai na
// hora e a contratação nova segue. A conta de quem pagou continua sendo
// "e-mail já tem conta". Precisa de FARBO_TEST_DATABASE_URL.
func TestCheckoutAgainAfterUnpaid(t *testing.T) {
	env, _ := newLeadsEnv(t, func(d *Deps) {
		d.Config.Catalog.SetupDueDays = 0
		d.Orders = orders.NewService(d.DB, d.Billing, d.Vehicles, d.Config.Catalog, d.Log)
		doc, err := contract.Render(contract.Params{Company: config.Company{Name: "Farbo Rastreadores"}, HistoryOptions: []int{7, 14, 30}})
		if err != nil {
			t.Fatal(err)
		}
		c := contract.NewService(d.DB, doc, &recordingContractMailer{}, d.Log)
		c.SetSync()
		d.Contract = c
		d.Dunning = dunning.NewService(d.DB, d.Billing, &recordingInvoiceMailer{}, dunning.Config{
			SuspendAfterDays: 10, Location: time.UTC, FromHour: 9, ToHour: 20,
			AppURL: "https://painel.farbo.test", LinkSecret: d.Config.Auth.JWTSecret,
		}, d.Log)
	})
	ctx := context.Background()
	var o struct {
		ContractVersion string `json:"contractVersion"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/checkout", nil, http.StatusOK), &o)
	address := map[string]any{"zipCode": "01305-000", "street": "Rua Augusta", "number": "500", "district": "Consolação",
		"city": "São Paulo", "state": "SP"}
	checkout := func(password string) (int, []byte) {
		t.Helper()
		return env.do("", http.MethodPost, "/api/public/checkout", map[string]any{
			"name": "Duane Victoria", "email": "duane@site.test", "phone": "(11) 95555-0000", "document": "529.982.247-25",
			"password": password, "vehicle": map[string]any{"kind": "CAR", "brand": "Ford", "model": "Fiesta", "plate": ""},
			"address": address, "contractVersion": o.ContractVersion, "accept": true,
		})
	}
	account := func() uuid.UUID {
		t.Helper()
		var id uuid.UUID
		_ = env.db.QueryRow(ctx, `SELECT id FROM users WHERE email = 'duane@site.test'`).Scan(&id)
		return id
	}
	if status, out := checkout("senha-forte-1"); status != http.StatusCreated {
		t.Fatalf("primeira contratação = %d %s", status, out)
	}
	first := account()
	// O Pix da contratação, valendo por 24 h.
	if _, err := env.db.Exec(ctx, `
		INSERT INTO payment_charges (invoice_id, provider, provider_charge_id, amount_cents, status, method, expires_at)
		SELECT id, 'abacatepay', $2, amount_cents, 'PENDING', 'PIX', $3 FROM invoices WHERE customer_id = $1 LIMIT 1`,
		first, uuid.NewString(), time.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	type unpaid struct {
		Code         string    `json:"code"`
		Error        string    `json:"error"`
		PayURL       string    `json:"payUrl"`
		PayableUntil time.Time `json:"payableUntil"`
	}
	again := func(password string) unpaid {
		t.Helper()
		status, out := checkout(password)
		var u unpaid
		_ = json.Unmarshal(out, &u)
		if status != http.StatusConflict || u.Code != "CHECKOUT_UNPAID" {
			t.Fatalf("de novo com o Pix valendo = %d %s", status, out)
		}
		return u
	}
	// Com a senha dela: o link do pedido já feito e até quando vale.
	u := again("senha-forte-1")
	if !strings.HasPrefix(u.PayURL, "https://painel.farbo.test/pagar/") || !strings.HasSuffix(u.PayURL, "?todas=1") ||
		u.PayableUntil.Before(time.Now().Add(23*time.Hour)) || !strings.Contains(u.Error, "pague o pedido que já fez") {
		t.Errorf("com a senha = %+v", u)
	}
	// Sem a senha: só o aviso.
	if u := again("outra-senha-9"); u.PayURL != "" || !strings.Contains(u.Error, "entre no app") {
		t.Errorf("sem a senha = %+v", u)
	}
	if account() != first {
		t.Fatal("a conta com o Pix valendo saiu")
	}

	// O Pix venceu (e passou a folga). Sem a senha, a contratação de hoje
	// ainda fica (o prazo de pagar dela não acabou); com a senha, ela está
	// recomeçando: o cadastro antigo sai e a contratação nova segue.
	if _, err := env.db.Exec(ctx, `UPDATE payment_charges SET expires_at = NOW() - INTERVAL '2 hours'
		WHERE invoice_id IN (SELECT id FROM invoices WHERE customer_id = $1)`, first); err != nil {
		t.Fatal(err)
	}
	if u := again("outra-senha-9"); u.PayURL != "" || account() != first {
		t.Fatalf("sem a senha, com o Pix vencido = %+v", u)
	}
	if status, out := checkout("senha-forte-1"); status != http.StatusCreated {
		t.Fatalf("de novo com a senha e o Pix vencido = %d %s", status, out)
	}
	second := account()
	if second == uuid.Nil || second == first {
		t.Fatalf("conta nova = %s (antiga %s)", second, first)
	}
	var notes string
	_ = env.db.QueryRow(ctx, `SELECT string_agg(notes, ' ') FROM leads WHERE lower(email) = 'duane@site.test'`).Scan(&notes)
	if !strings.Contains(notes, "não pagou: o cadastro foi apagado") {
		t.Errorf("anotação do pré-cliente = %q", notes)
	}

	// Passado o prazo de pagar sem nada gerado, nem precisa da senha.
	if _, err := env.db.Exec(ctx, `UPDATE users SET checkout_at = NOW() - INTERVAL '26 hours' WHERE id = $1`, second); err != nil {
		t.Fatal(err)
	}
	if status, out := checkout("senha-nova-22"); status != http.StatusCreated {
		t.Fatalf("de novo depois do prazo = %d %s", status, out)
	}
	third := account()
	if third == second {
		t.Fatal("a contratação vencida não saiu")
	}

	// Pagou: o e-mail é de uma conta de verdade.
	if _, err := env.db.Exec(ctx, `UPDATE invoices SET status = 'PAID', paid_at = NOW() WHERE customer_id = $1`, third); err != nil {
		t.Fatal(err)
	}
	if status, out := checkout("senha-nova-22"); status != http.StatusConflict || !containsCode(out, "EMAIL_TAKEN") {
		t.Errorf("depois de pagar = %d %s", status, out)
	}
}

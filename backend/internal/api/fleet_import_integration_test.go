package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// A frota por planilha, pela central: a conferência aponta cada linha com
// problema (nome, placa repetida ou já cadastrada, rastreador em outro
// veículo ou fora do cadastro) sem gravar nada; a planilha certa cria os
// veículos, as assinaturas no mesmo vencimento, os grupos que faltam, o
// envio dos que ainda não têm rastreador e uma fatura só do equipamento.
// Precisa de FARBO_TEST_DATABASE_URL.
func TestFleetImport(t *testing.T) {
	env, _ := newLeadsEnv(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	authSvc := auth.NewService(auth.NewRepository(env.db), config.Auth{BcryptCost: bcrypt.MinCost}, nil, log)
	frota, err := authSvc.CreateUser(ctx, "frota@planilha.test", "Frota Ltda", auth.RoleCustomer, userPassword)
	if err != nil {
		t.Fatal(err)
	}
	customer, admin := env.login("frota@planilha.test"), env.login(auth.RoleAdmin+"@leads.test")
	env.must(customer, http.MethodPost, "/api/me/fleet/groups", map[string]any{"name": "Zona Sul"}, http.StatusCreated)

	var free, linked uuid.UUID
	if err := env.db.QueryRow(ctx, `INSERT INTO devices (imei) VALUES ('869247061900001') RETURNING id`).Scan(&free); err != nil {
		t.Fatal(err)
	}
	if err := env.db.QueryRow(ctx, `INSERT INTO devices (imei) VALUES ('869247061900002') RETURNING id`).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `INSERT INTO vehicles (name, plate, device_id) VALUES ('Da central', 'IMP0A00', $1)`, linked); err != nil {
		t.Fatal(err)
	}

	path := "/api/customers/" + frota.ID.String() + "/trackers/import"
	plan := map[string]any{"planName": "Plano Frota", "priceCents": 4990, "dueDay": 10}
	body := func(rows []map[string]any, dryRun bool) map[string]any {
		return map[string]any{"rows": rows, "equipmentCents": 29900, "plan": plan, "dryRun": dryRun}
	}
	row := func(name, plate, group, imei string) map[string]any {
		return map[string]any{"vehicle": map[string]any{"name": name, "plate": plate}, "group": group, "imei": imei}
	}
	owned := func() int {
		t.Helper()
		var n int
		if err := env.db.QueryRow(ctx, `SELECT count(*) FROM vehicles WHERE owner_id = $1`, frota.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Cada linha com problema aparece, e nada é gravado.
	var refused struct {
		Code     string                 `json:"code"`
		Problems []orders.ImportProblem `json:"problems"`
	}
	_ = json.Unmarshal(env.must(admin, http.MethodPost, path, body([]map[string]any{
		row("Van 01", "IMP1A11", "zona sul", "869247061900001"),
		row("", "IMP2B22", "", ""),
		row("Van 03", "imp1a11", "", ""),
		row("Van 04", "IMP0A00", "", ""),
		row("Van 05", "", "", "869247061900002"),
		row("Van 06", "", "", "999"),
	}, false), http.StatusUnprocessableEntity), &refused)
	lines := []int{}
	for _, p := range refused.Problems {
		lines = append(lines, p.Line)
	}
	if refused.Code != "IMPORT_PROBLEMS" || len(lines) != 5 || lines[0] != 2 || lines[4] != 6 ||
		!strings.Contains(refused.Problems[1].Message, "repete (linha 1)") ||
		!strings.Contains(refused.Problems[2].Message, "já está cadastrada") ||
		!strings.Contains(refused.Problems[3].Message, "outro veículo") ||
		!strings.Contains(refused.Problems[4].Message, "não está cadastrado") {
		t.Fatalf("problemas = %+v", refused)
	}
	if n := owned(); n != 0 {
		t.Fatalf("a planilha recusada gravou %d veículos", n)
	}

	good := []map[string]any{
		row("Van 01", "IMP1A11", "zona sul", "869247061900001"),
		{"vehicle": map[string]any{"name": "Van 02", "plate": "IMP2B22", "custom1": "PMSP-7"}, "group": "Norte"},
		row("Van 03", "IMP3C33", "norte", ""),
	}
	// A conferência mostra o que vai acontecer, sem gravar.
	var preview orders.ImportResult
	_ = json.Unmarshal(env.must(admin, http.MethodPost, path, body(good, true), http.StatusOK), &preview)
	if !preview.DryRun || preview.Vehicles != 3 || preview.Installed != 1 || len(preview.NewGroups) != 1 ||
		preview.NewGroups[0] != "Norte" || preview.EquipmentCents != 3*29900 || owned() != 0 {
		t.Fatalf("conferência = %+v", preview)
	}

	// O cliente não importa; a central, sim.
	env.must(customer, http.MethodPost, path, body(good, false), http.StatusForbidden)
	var done orders.ImportResult
	_ = json.Unmarshal(env.must(admin, http.MethodPost, path, body(good, false), http.StatusCreated), &done)
	if done.DryRun || done.Vehicles != 3 || done.SetupInvoice == nil || done.SetupInvoice.AmountCents != 3*29900 ||
		!strings.Contains(done.SetupInvoice.Description, "× 3") {
		t.Fatalf("importação = %+v", done)
	}

	type created struct {
		name, group, custom1 string
		device               *uuid.UUID
		dueDay               int
		shipping             bool
	}
	rows, err := env.db.Query(ctx, `
		SELECT v.name, COALESCE(g.name, ''), v.custom1, v.device_id, s.due_day,
			EXISTS (SELECT 1 FROM fulfillments f WHERE f.vehicle_id = v.id)
		FROM vehicles v JOIN subscriptions s ON s.vehicle_id = v.id LEFT JOIN vehicle_groups g ON g.id = v.group_id
		WHERE v.owner_id = $1 ORDER BY v.name`, frota.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []created
	for rows.Next() {
		var c created
		if err := rows.Scan(&c.name, &c.group, &c.custom1, &c.device, &c.dueDay, &c.shipping); err != nil {
			t.Fatal(err)
		}
		got = append(got, c)
	}
	rows.Close()
	if len(got) != 3 ||
		got[0].group != "Zona Sul" || got[0].device == nil || *got[0].device != free || got[0].shipping ||
		got[1].group != "Norte" || got[1].custom1 != "PMSP-7" || got[1].device != nil || !got[1].shipping ||
		got[2].group != "Norte" || got[2].dueDay != got[0].dueDay {
		t.Errorf("veículos = %+v", got)
	}
	var setupInvoices, audits int
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM invoices WHERE customer_id = $1 AND subscription_id IS NULL`, frota.ID).Scan(&setupInvoices)
	_ = env.db.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'FLEET_IMPORTED'`).Scan(&audits)
	if setupInvoices != 1 || audits != 1 {
		t.Errorf("faturas do equipamento = %d, auditoria = %d", setupInvoices, audits)
	}

	// A mesma planilha de novo: as placas já existem.
	_ = json.Unmarshal(env.must(admin, http.MethodPost, path, body(good, true), http.StatusUnprocessableEntity), &refused)
	if len(refused.Problems) != 4 { // 3 placas e o rastreador, agora vinculado
		t.Errorf("reimportação = %+v", refused.Problems)
	}
}

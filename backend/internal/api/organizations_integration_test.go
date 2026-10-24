package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
)

// recordingOrgNotifier guarda o convite de quem entra na equipe de uma
// organização.
type recordingOrgNotifier struct {
	mu        sync.Mutex
	invited   map[string]mail.OrgNotice
	tokens    map[string]string
	announced []string
	claims    []string
}

func (n *recordingOrgNotifier) MemberInvited(_ context.Context, to string, notice mail.OrgNotice, token string, _ time.Duration) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.invited == nil {
		n.invited, n.tokens = map[string]mail.OrgNotice{}, map[string]string{}
	}
	n.invited[to], n.tokens[to] = notice, token
	return nil
}

func (n *recordingOrgNotifier) Announcement(_ context.Context, to string, a mail.OrgAnnouncement) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.announced = append(n.announced, to+": "+a.Title)
	return nil
}

func (n *recordingOrgNotifier) ClaimOpened(_ context.Context, to, _ string, c mail.ClaimNotice) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.claims = append(n.claims, to+": "+c.KindLabel+" — "+c.AssociateName)
	return nil
}

func (n *recordingOrgNotifier) token(email string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.tokens[email]
}

// tokenFor abre uma sessão sem passar pelo login HTTP (o limite de logins
// por IP é apertado).
func (e *credEnv) tokenFor(email string) string {
	e.t.Helper()
	user, err := e.auth.FindByEmail(context.Background(), email)
	if err != nil {
		e.t.Fatalf("usuário %s: %v", email, err)
	}
	tokens, err := e.auth.LoginVerified(context.Background(), user.ID, "teste")
	if err != nil {
		e.t.Fatalf("sessão de %s: %v", email, err)
	}
	return tokens.AccessToken
}

// A associação: a central cadastra com o preço, o comodato e as taxas; a
// equipe dela entra só no painel dela, vê e bloqueia (com a confirmação) só
// os veículos dos associados, cadastra associados e veículos; o associado
// usa o app sem faturas nem pedido; a fatura do mês sai uma só, com o
// extrato. Precisa de FARBO_TEST_DATABASE_URL.
func TestOrganizationPanel(t *testing.T) {
	notifier := &recordingOrgNotifier{}
	env := newCredEnvWith(t, credEnvOptions{stepUp: true, orgs: notifier})
	ctx := context.Background()
	if _, err := env.auth.CreateUser(ctx, "admin@org.test", "Central", auth.RoleAdmin, userPassword); err != nil {
		t.Fatal(err)
	}
	central := env.tokenFor("admin@org.test")

	// A central cadastra a associação com o primeiro Administrador.
	var created struct {
		Organization struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		} `json:"organization"`
		AdminError string `json:"adminError"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations", map[string]any{
		"kind": "association", "name": "Associação Motoclube", "document": "11.222.333/0001-81",
		"email": "contato@motoclube.test", "phone": "11999990000", "priceCents": 5000, "dueDay": 10,
		"equipmentMode": "loan", "installFeeCents": 8000, "uninstallFeeCents": 3000,
		"admin": map[string]string{"name": "Ana Presidente", "email": "contato@motoclube.test"},
	}, http.StatusCreated), &created)
	orgID := created.Organization.ID
	if orgID == uuid.Nil || created.AdminError != "" {
		t.Fatalf("cadastro = %+v", created)
	}
	// O e-mail de cobrança pode ser o do login do Administrador.
	invite := notifier.token("contato@motoclube.test")
	if invite == "" || notifier.invited["contato@motoclube.test"].OrgName != "Associação Motoclube" {
		t.Fatalf("convite = %+v", notifier.invited)
	}
	env.must("", http.MethodPost, "/api/auth/reset-password",
		map[string]string{"token": invite, "password": userPassword}, http.StatusNoContent)
	ana := env.tokenFor("contato@motoclube.test")

	var me struct {
		Role         string `json:"role"`
		Organization struct {
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"organization"`
	}
	_ = json.Unmarshal(env.must(ana, http.MethodGet, "/api/auth/me", nil, http.StatusOK), &me)
	if me.Role != auth.RoleOrgMember || me.Organization.Name != "Associação Motoclube" || me.Organization.Role != "admin" {
		t.Fatalf("me = %+v", me)
	}

	// Fora do painel dela, nada da central nem do cliente.
	for _, path := range []string{"/api/customers", "/api/devices", "/api/me/account", "/api/geofences", "/api/events",
		"/api/organizations", "/api/users", "/api/catalog"} {
		if status, out := env.do(ana, http.MethodGet, path, nil); status != http.StatusForbidden {
			t.Errorf("GET %s pela associação = %d %s", path, status, out)
		}
	}

	// Associados: um novo (convite do app); o e-mail de quem já é cliente, não.
	if _, err := env.auth.Register(ctx, auth.NewUser{Email: "outro@cliente.test", Name: "Outro", Role: auth.RoleCustomer, Password: userPassword}); err != nil {
		t.Fatal(err)
	}
	var bia struct {
		ID uuid.UUID `json:"id"`
	}
	_ = json.Unmarshal(env.must(ana, http.MethodPost, "/api/org/associates", map[string]any{
		"name": "Bia Associada", "email": "bia@associada.test", "phone": "11988887777", "document": "529.982.247-25",
	}, http.StatusCreated), &bia)
	env.must(ana, http.MethodPost, "/api/org/associates", map[string]any{"name": "Outro", "email": "outro@cliente.test"}, http.StatusBadRequest)

	// O veículo da Bia, com o pedido de instalação na fila da central.
	var moto struct {
		ID uuid.UUID `json:"id"`
	}
	_ = json.Unmarshal(env.must(ana, http.MethodPost, "/api/org/associates/"+bia.ID.String()+"/vehicles",
		map[string]any{"name": "CB 500", "plate": "ORG1A23", "kind": "MOTORCYCLE"}, http.StatusCreated), &moto)
	var queued int
	if err := env.db.QueryRow(ctx, `SELECT count(*) FROM fulfillments WHERE vehicle_id = $1 AND customer_id = $2`, moto.ID, bia.ID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("pedido de instalação = %d %v", queued, err)
	}

	// A instalação: o rastreador no veículo da Bia; e o carro de outro cliente.
	dev, err := env.devices.Create(ctx, devices.Input{IMEIConfirmed: true, IMEI: "869247061230001", Protocol: "gt06"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE vehicles SET device_id = $2 WHERE id = $1`, moto.ID, dev.ID); err != nil {
		t.Fatal(err)
	}
	outro, _ := env.auth.FindByEmail(ctx, "outro@cliente.test")
	carro, err := env.vehicles.Create(ctx, vehicles.Input{Name: "Carro de fora", OwnerID: &outro.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.owners.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	// O mapa da associação: só o veículo do associado, com o nome dele.
	var list []struct {
		ID        uuid.UUID `json:"id"`
		OwnerName string    `json:"ownerName"`
	}
	_ = json.Unmarshal(env.must(ana, http.MethodGet, "/api/vehicles", nil, http.StatusOK), &list)
	if len(list) != 1 || list[0].ID != moto.ID || list[0].OwnerName != "Bia Associada" {
		t.Fatalf("veículos da associação = %+v", list)
	}
	env.must(ana, http.MethodGet, "/api/vehicles/"+carro.ID.String(), nil, http.StatusNotFound)
	env.must(ana, http.MethodGet, "/api/vehicles/"+carro.ID.String()+"/positions", nil, http.StatusNotFound)
	env.must(ana, http.MethodGet, "/api/vehicles/"+moto.ID.String(), nil, http.StatusOK)

	// Ao vivo: a associação recebe o que é do veículo do associado; o outro
	// cliente, não.
	anaWS := env.connectWS(ana)
	outroWS := env.connectWS(env.tokenFor("outro@cliente.test"))

	// Bloquear pede a confirmação de quem é da associação.
	env.sender.setConnected("869247061230001", true)
	cut := "/api/vehicles/" + moto.ID.String() + "/commands/engine-cut"
	if status, out := env.do(ana, http.MethodPost, cut, nil); status != http.StatusForbidden || !strings.Contains(string(out), codeStepUpRequired) {
		t.Fatalf("bloqueio sem confirmação = %d %s", status, out)
	}
	var grant struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(env.must(ana, http.MethodPost, "/api/step-up/password",
		map[string]string{"purpose": "engine_cut", "password": userPassword}, http.StatusOK), &grant)
	if status, out := env.doWith(ana, http.MethodPost, cut, nil, map[string]string{stepUpHeader: grant.Token}); status != http.StatusAccepted {
		t.Fatalf("bloqueio pela associação = %d %s", status, out)
	}
	env.must(ana, http.MethodPost, "/api/vehicles/"+carro.ID.String()+"/commands/request-position", nil, http.StatusNotFound)
	if events := anaWS.waitCommandEvents(t, 1); *events[0].VehicleID != moto.ID {
		t.Fatalf("ao vivo na associação = %+v", events)
	}
	if events := outroWS.commandEvents(); len(events) != 0 {
		t.Fatalf("o outro cliente recebeu %+v", events)
	}

	// O Visualizador da associação só acompanha.
	_ = json.Unmarshal(env.must(ana, http.MethodPost, "/api/step-up/password",
		map[string]string{"purpose": "team_member", "password": userPassword}, http.StatusOK), &grant)
	status, out := env.doWith(ana, http.MethodPost, "/api/org/team",
		map[string]string{"name": "Vi Visualizador", "email": "vi@motoclube.test", "role": "viewer"}, map[string]string{stepUpHeader: grant.Token})
	if status != http.StatusCreated {
		t.Fatalf("pôr na equipe = %d %s", status, out)
	}
	env.must("", http.MethodPost, "/api/auth/reset-password",
		map[string]string{"token": notifier.token("vi@motoclube.test"), "password": userPassword}, http.StatusNoContent)
	vi := env.tokenFor("vi@motoclube.test")
	if status, out := env.do(vi, http.MethodPost, cut, nil); status != http.StatusForbidden || !strings.Contains(string(out), codeOrgRole) {
		t.Fatalf("bloqueio pelo Visualizador = %d %s", status, out)
	}
	env.must(vi, http.MethodPost, "/api/org/associates", map[string]any{"name": "X", "email": "x@x.test"}, http.StatusForbidden)
	env.must(vi, http.MethodGet, "/api/org/invoices", nil, http.StatusForbidden)
	env.must(vi, http.MethodGet, "/api/org/associates", nil, http.StatusOK)

	// A associada: sem contrato, faturas nem pedido; vê o bloqueio pelo nome
	// da associação, sem o aparelho de quem bloqueou.
	if _, err := env.auth.SetPassword(ctx, bia.ID, userPassword); err != nil {
		t.Fatal(err)
	}
	biaToken := env.tokenFor("bia@associada.test")
	var biaMe struct {
		PaidByOrganization bool            `json:"paidByOrganization"`
		Organization       json.RawMessage `json:"organization"`
	}
	_ = json.Unmarshal(env.must(biaToken, http.MethodGet, "/api/auth/me", nil, http.StatusOK), &biaMe)
	if !biaMe.PaidByOrganization || len(biaMe.Organization) != 0 {
		t.Fatalf("me da associada = %+v", biaMe)
	}
	env.must(biaToken, http.MethodPost, "/api/me/trackers", map[string]any{"vehicle": map[string]string{"name": "Novo"}}, http.StatusForbidden)
	type historyItem struct {
		Who, WhoKind, UserAgent string
	}
	var history []historyItem
	_ = json.Unmarshal(env.must(biaToken, http.MethodGet, "/api/vehicles/"+moto.ID.String()+"/engine-history", nil, http.StatusOK), &history)
	if len(history) != 1 || history[0].Who != "Associação Motoclube" || history[0].WhoKind != "organization" || history[0].UserAgent != "" {
		t.Fatalf("rastreio da associada = %+v", history)
	}
	_ = json.Unmarshal(env.must(ana, http.MethodGet, "/api/vehicles/"+moto.ID.String()+"/engine-history", nil, http.StatusOK), &history)
	if history[0].Who != "Você" {
		t.Fatalf("rastreio da associação = %+v", history)
	}
	_ = json.Unmarshal(env.must(central, http.MethodGet, "/api/vehicles/"+moto.ID.String()+"/engine-history", nil, http.StatusOK), &history)
	if history[0].Who != "Ana Presidente (Associação Motoclube)" {
		t.Fatalf("rastreio na central = %+v", history)
	}

	// O painel de recuperação: o modo roubo dos veículos dos associados — o
	// em andamento, os recuperados (com a taxa e o tempo do meio) e os que
	// desligaram no prazo; o que é de fora do período não entra.
	if _, err := env.db.Exec(ctx, `
		INSERT INTO theft_modes (vehicle_id, activated_at, expires_at, ended_at, outcome) VALUES
			($1, NOW() - INTERVAL '40 days', NOW() - INTERVAL '37 days', NOW() - INTERVAL '40 days' + INTERVAL '90 minutes', 'RECOVERED'),
			($1, NOW() - INTERVAL '20 days', NOW() - INTERVAL '17 days', NOW() - INTERVAL '20 days' + INTERVAL '150 minutes', 'RECOVERED'),
			($1, NOW() - INTERVAL '10 days', NOW() - INTERVAL '7 days', NOW() - INTERVAL '7 days', 'EXPIRED'),
			($1, NOW() - INTERVAL '5 days', NOW() - INTERVAL '2 days', NOW() - INTERVAL '5 days' + INTERVAL '5 minutes', 'CANCELLED'),
			($1, NOW() - INTERVAL '800 days', NOW() - INTERVAL '797 days', NOW() - INTERVAL '799 days', 'RECOVERED'),
			($1, NOW() - INTERVAL '1 hour', NOW() + INTERVAL '71 hours', NULL, NULL)`, moto.ID); err != nil {
		t.Fatal(err)
	}
	var recoveries struct {
		Total, Active, Recovered, Cancelled, Expired int
		RecoveryRate                                 *float64 `json:"recoveryRate"`
		MedianRecoveryMinutes                        *int     `json:"medianRecoveryMinutes"`
		Cases                                        []struct {
			Outcome       string `json:"outcome"`
			AssociateName string `json:"associateName"`
		} `json:"cases"`
	}
	_ = json.Unmarshal(env.must(vi, http.MethodGet, "/api/org/recoveries", nil, http.StatusOK), &recoveries)
	if recoveries.Total != 5 || recoveries.Active != 1 || recoveries.Recovered != 2 || recoveries.Expired != 1 ||
		recoveries.Cancelled != 1 || *recoveries.RecoveryRate != 66.7 || *recoveries.MedianRecoveryMinutes != 120 ||
		recoveries.Cases[0].Outcome != "" || recoveries.Cases[0].AssociateName != "Bia Associada" {
		t.Fatalf("recuperação = %+v", recoveries)
	}
	env.must(vi, http.MethodGet, "/api/org/recoveries?meses=30", nil, http.StatusBadRequest)
	env.must(biaToken, http.MethodGet, "/api/org/recoveries", nil, http.StatusForbidden)
	if _, err := env.db.Exec(ctx, `DELETE FROM theft_modes WHERE vehicle_id = $1`, moto.ID); err != nil {
		t.Fatal(err)
	}

	// Os comunicados: vão para a área de notificações do app de cada
	// associado ativo e, se pedido, por e-mail; o Visualizador não manda;
	// são até 5 por dia.
	pusher := &recordingPusher{}
	env.orgs.SetPusher(pusher)
	env.must(vi, http.MethodPost, "/api/org/announcements", map[string]any{"title": "Oi", "body": "Oi"}, http.StatusForbidden)
	env.must(ana, http.MethodPost, "/api/org/announcements", map[string]any{"title": " ", "body": "Oi"}, http.StatusBadRequest)
	var sent struct {
		Recipients int  `json:"recipients"`
		Email      bool `json:"email"`
	}
	_ = json.Unmarshal(env.must(ana, http.MethodPost, "/api/org/announcements", map[string]any{
		"title": "Assembleia geral", "body": "Dia 30, às 19h, na sede.\nContamos com você.", "email": true,
	}, http.StatusCreated), &sent)
	if sent.Recipients != 1 || !sent.Email {
		t.Fatalf("comunicado = %+v", sent)
	}
	if got := pusher.take(); len(got[bia.ID]) != 1 || got[bia.ID][0].Title != "Assembleia geral" || got[bia.ID][0].URL != "/app/alertas" {
		t.Fatalf("app = %+v", got)
	}
	if len(notifier.announced) != 1 || notifier.announced[0] != "bia@associada.test: Assembleia geral" {
		t.Fatalf("e-mails = %v", notifier.announced)
	}
	var sentList struct {
		Announcements []struct {
			Delivered, Emailed int
			FinishedAt         *time.Time `json:"finishedAt"`
		} `json:"announcements"`
	}
	_ = json.Unmarshal(env.must(vi, http.MethodGet, "/api/org/announcements", nil, http.StatusOK), &sentList)
	if len(sentList.Announcements) != 1 || sentList.Announcements[0].Delivered != 1 || sentList.Announcements[0].Emailed != 1 ||
		sentList.Announcements[0].FinishedAt == nil {
		t.Fatalf("comunicados enviados = %+v", sentList)
	}
	for range 4 {
		env.must(ana, http.MethodPost, "/api/org/announcements", map[string]any{"title": "Lembrete", "body": "Revisão"}, http.StatusCreated)
	}
	env.must(ana, http.MethodPost, "/api/org/announcements", map[string]any{"title": "Mais um", "body": "Revisão"}, http.StatusBadRequest)
	env.must(biaToken, http.MethodGet, "/api/org/announcements", nil, http.StatusForbidden)

	// A fatura do mês: a mensalidade, o proporcional dos dias até o
	// vencimento e a instalação, numa fatura só.
	type invoice struct {
		ID          uuid.UUID `json:"id"`
		AmountCents int       `json:"amountCents"`
		DueDate     string    `json:"dueDate"`
		Vehicles    int       `json:"vehicles"`
		Items       []struct {
			Kind        string `json:"kind"`
			AmountCents int    `json:"amountCents"`
			Days        int    `json:"days"`
			OwnerName   string `json:"ownerName"`
		} `json:"items"`
	}
	var first invoice
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations/"+orgID.String()+"/invoices/generate", nil, http.StatusCreated), &first)
	kinds := map[string]int{}
	for _, it := range first.Items {
		kinds[it.Kind] = it.AmountCents
	}
	due, _ := time.Parse(time.DateOnly, first.DueDate)
	days := int(due.Sub(time.Now().UTC().Truncate(24*time.Hour)).Hours()/24 + 0.5)
	if first.Vehicles != 1 || kinds["monthly"] != 5000 || kinds["install"] != 8000 || due.Day() != 10 ||
		first.AmountCents != 5000+8000+kinds["prorata"] || (days > 0 && kinds["prorata"] != (5000*days+15)/30) {
		t.Fatalf("1ª fatura = %+v (dias %d)", first, days)
	}
	// A seguinte: só a mensalidade.
	var second invoice
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations/"+orgID.String()+"/invoices/generate", nil, http.StatusCreated), &second)
	if second.AmountCents != 5000 || len(second.Items) != 1 {
		t.Fatalf("2ª fatura = %+v", second)
	}
	// O rastreador tirado: só a desinstalação.
	if _, err := env.db.Exec(ctx, `UPDATE vehicles SET device_id = NULL WHERE id = $1`, moto.ID); err != nil {
		t.Fatal(err)
	}
	var third invoice
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations/"+orgID.String()+"/invoices/generate", nil, http.StatusCreated), &third)
	if third.AmountCents != 3000 || len(third.Items) != 1 || third.Items[0].Kind != "uninstall" {
		t.Fatalf("3ª fatura = %+v", third)
	}
	env.must(central, http.MethodPost, "/api/organizations/"+orgID.String()+"/invoices/generate", nil, http.StatusBadRequest)

	// O Administrador da associação vê as faturas com o extrato.
	var invoices struct {
		Invoices []invoice `json:"invoices"`
	}
	_ = json.Unmarshal(env.must(ana, http.MethodGet, "/api/org/invoices", nil, http.StatusOK), &invoices)
	if len(invoices.Invoices) != 3 {
		t.Fatalf("faturas da associação = %+v", invoices)
	}

	// A organização precisa de um Administrador; a central põe e tira gente.
	anaUser, _ := env.auth.FindByEmail(ctx, "contato@motoclube.test")
	env.must(central, http.MethodPatch, "/api/organizations/"+orgID.String()+"/members/"+anaUser.ID.String(),
		map[string]string{"role": "viewer"}, http.StatusBadRequest)
	env.must(ana, http.MethodDelete, "/api/org/team/"+anaUser.ID.String(), nil, http.StatusBadRequest)

	// Um cliente com assinatura paga não vira associado sem encerrá-la.
	if _, err := env.db.Exec(ctx, `INSERT INTO subscriptions (customer_id, plan_name, price_cents, due_day, next_due_date)
		VALUES ($1, 'Mensal', 2990, 10, CURRENT_DATE)`, outro.ID); err != nil {
		t.Fatal(err)
	}
	env.must(central, http.MethodPut, "/api/customers/"+outro.ID.String()+"/organization",
		map[string]any{"organizationId": orgID}, http.StatusBadRequest)

	// Desativada, a equipe dela perde o painel na hora.
	env.must(central, http.MethodPatch, "/api/organizations/"+orgID.String(), map[string]any{
		"kind": "association", "name": "Associação Motoclube", "document": "11222333000181",
		"email": "contato@motoclube.test", "priceCents": 5000, "dueDay": 10, "active": false,
	}, http.StatusOK)
	env.must(ana, http.MethodGet, "/api/org/", nil, http.StatusForbidden)
	env.must(ana, http.MethodGet, "/api/vehicles", nil, http.StatusForbidden)
}

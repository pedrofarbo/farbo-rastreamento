package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// photoBytes é um JPEG diferente para cada semente (a foto repetida é a do
// mesmo arquivo).
func photoBytes(seed string) []byte {
	return append(append([]byte{}, jpegBytes...), []byte(seed)...)
}

type inspectionCheckView struct {
	Code  string `json:"code"`
	Level string `json:"level"`
	Text  string `json:"text"`
}

type inspectionView struct {
	ID          uuid.UUID  `json:"id"`
	AssociateID *uuid.UUID `json:"associateId"`
	VehicleID   *uuid.UUID `json:"vehicleId"`
	Plate       string     `json:"plate"`
	Kind        string     `json:"kind"`
	Mode        string     `json:"mode"`
	Status      string     `json:"status"`
	PriceCents  int        `json:"priceCents"`
	Billed      bool       `json:"billed"`
	DamagesNote string     `json:"damagesNote"`
	AcceptedAt  *time.Time `json:"acceptedAt"`
	RedoOf      *uuid.UUID `json:"redoOf"`
	Link        string     `json:"link"`
	Slots       []struct {
		ID       string `json:"id"`
		Optional bool   `json:"optional"`
	} `json:"slots"`
	Photos []struct {
		Slot   string `json:"slot"`
		Source string `json:"source"`
	} `json:"photos"`
	Checks []inspectionCheckView `json:"checks"`
}

func checkCodes(checks []inspectionCheckView) map[string]inspectionCheckView {
	out := map[string]inspectionCheckView{}
	for _, c := range checks {
		out[c.Code] = c
	}
	return out
}

// TestInspectionLinks: a autovistoria por link de ponta a ponta. A
// associação pede a vistoria de adesão de quem ainda vai entrar (só o nome,
// o celular e a placa) e a com técnico do veículo de um associado; o link
// abre sem login, com o primeiro nome; as fotos sobem com a posição e a
// origem; o envio pede a declaração e faz as conferências (a foto sem
// localização, a escolhida do aparelho, a foto repetida de outra vistoria, a
// posição do rastreador); a devolvida reabre no mesmo link e não é cobrada
// de novo; o link cancelado e o vencido não abrem; a fatura da associação
// cobra cada vistoria enviada uma vez. Precisa de FARBO_TEST_DATABASE_URL.
func TestInspectionLinks(t *testing.T) {
	notifier := &recordingOrgNotifier{}
	env := newCredEnvWith(t, credEnvOptions{orgs: notifier, occurrences: notifier})
	pusher := &recordingPusher{}
	env.inspects.SetPusher(pusher)
	ctx := context.Background()
	if _, err := env.auth.CreateUser(ctx, "admin@vistoria.test", "Central", auth.RoleAdmin, userPassword); err != nil {
		t.Fatal(err)
	}
	central := env.tokenFor("admin@vistoria.test")
	var created struct {
		Organization struct {
			ID                  uuid.UUID `json:"id"`
			InspectionCents     int       `json:"inspectionCents"`
			InspectionTechCents int       `json:"inspectionTechCents"`
		} `json:"organization"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations", map[string]any{
		"kind": "association", "name": "Associação Rota Segura", "document": "11.222.333/0001-81",
		"email": "contato@rota.test", "priceCents": 3500, "dueDay": 10, "inspectionCents": 1500, "inspectionTechCents": 8000,
		"admin": map[string]string{"name": "Ivo Analista", "email": "ivo@rota.test"},
	}, http.StatusCreated), &created)
	org := created.Organization
	if org.InspectionCents != 1500 || org.InspectionTechCents != 8000 {
		t.Fatalf("preços da vistoria = %+v", org)
	}
	env.must("", http.MethodPost, "/api/auth/reset-password",
		map[string]string{"token": notifier.token("ivo@rota.test"), "password": userPassword}, http.StatusNoContent)
	ivo := env.tokenFor("ivo@rota.test")

	request := func(body map[string]any, want int) inspectionView {
		t.Helper()
		var out inspectionView
		_ = json.Unmarshal(env.must(ivo, http.MethodPost, "/api/org/inspections", body, want), &out)
		return out
	}
	token := func(in inspectionView) string {
		t.Helper()
		_, tok, ok := strings.Cut(in.Link, "/vistoria/")
		if !ok || !strings.HasPrefix(in.Link, "https://painel.farbo.test/vistoria/") {
			t.Fatalf("link = %q", in.Link)
		}
		return tok
	}
	upload := func(tok, slot string, data []byte, fields map[string]string, want int) {
		t.Helper()
		if status, out := env.upload("", http.MethodPut, "/api/public/inspections/"+tok+"/photos/"+slot, data, fields); status != want {
			t.Fatalf("foto %s: %d %s", slot, status, out)
		}
	}
	here := map[string]string{"latitude": "-18.9186", "longitude": "-48.2772", "accuracy": "12", "source": "camera"}
	orgGet := func(id uuid.UUID) inspectionView {
		t.Helper()
		var out inspectionView
		_ = json.Unmarshal(env.must(ivo, http.MethodGet, "/api/org/inspections/"+id.String(), nil, http.StatusOK), &out)
		return out
	}

	// A adesão de quem ainda vai entrar: a placa conferida e normalizada, uma em aberto por placa.
	request(map[string]any{"kind": "entry", "personName": "Carla", "plate": "AB-12"}, http.StatusBadRequest)
	request(map[string]any{"kind": "entry", "plate": "ABC1D23"}, http.StatusBadRequest)
	request(map[string]any{"kind": "bobagem", "personName": "Carla", "plate": "ABC1D23"}, http.StatusBadRequest)
	carla := request(map[string]any{"kind": "entry", "mode": "self", "personName": "  Carla   Nova Silva ", "personPhone": "34988881234",
		"plate": "abc-1d23", "vehicleType": "car", "vehicleDesc": "Gol prata 2015"}, http.StatusCreated)
	if carla.Plate != "ABC1D23" || carla.AssociateID != nil || carla.VehicleID != nil || carla.PriceCents != 1500 ||
		carla.Kind != "entry" || len(carla.Slots) != 15 || carla.Status != "pending" {
		t.Fatalf("pedido da Carla = %+v", carla)
	}
	request(map[string]any{"kind": "entry", "personName": "Carla", "plate": "ABC1D23"}, http.StatusBadRequest)
	carlaTok := token(carla)

	// O link: sem login, só o primeiro nome (sem o celular nem o sobrenome).
	env.must("", http.MethodGet, "/api/public/inspections/abc", nil, http.StatusNotFound)
	raw := env.must("", http.MethodGet, "/api/public/inspections/"+carlaTok, nil, http.StatusOK)
	var view struct {
		OrganizationName string `json:"organizationName"`
		FirstName        string `json:"firstName"`
		Plate            string `json:"plate"`
		VehicleName      string `json:"vehicleName"`
		Status           string `json:"status"`
		Expired          bool   `json:"expired"`
		RequestNote      string `json:"requestNote"`
		Slots            []struct {
			ID string `json:"id"`
		} `json:"slots"`
		Photos []struct {
			Slot string `json:"slot"`
		} `json:"photos"`
	}
	_ = json.Unmarshal(raw, &view)
	if view.FirstName != "Carla" || view.OrganizationName != "Associação Rota Segura" || view.Plate != "ABC1D23" ||
		view.VehicleName != "Gol prata 2015" || len(view.Slots) != 15 || view.Expired {
		t.Fatalf("link = %s", raw)
	}
	if body := string(raw); strings.Contains(body, "8888") || strings.Contains(body, "Silva") {
		t.Fatalf("o link mostra dados demais: %s", body)
	}

	// As fotos: o ângulo do roteiro; o documento sem localização e escolhido do aparelho.
	upload(carlaTok, "teto", photoBytes("x"), here, http.StatusBadRequest)
	upload(carlaTok, "front", []byte("não é foto"), here, http.StatusBadRequest)
	for _, slot := range carla.Slots {
		if slot.ID == "document" {
			upload(carlaTok, slot.ID, photoBytes("carla-"+slot.ID), map[string]string{"source": "file"}, http.StatusOK)
			continue
		}
		upload(carlaTok, slot.ID, photoBytes("carla-"+slot.ID), here, http.StatusOK)
	}
	submit := func(tok string, body any, want int) []byte {
		t.Helper()
		return env.must("", http.MethodPost, "/api/public/inspections/"+tok+"/submit", body, want)
	}
	submit(carlaTok, map[string]any{"damagesNote": "Risco na porta"}, http.StatusBadRequest)
	submit(carlaTok, map[string]any{"damagesNote": "Risco na porta do motorista", "accept": true}, http.StatusOK)
	upload(carlaTok, "front", photoBytes("depois"), here, http.StatusBadRequest)
	submit(carlaTok, map[string]any{"accept": true}, http.StatusBadRequest)

	carla = orgGet(carla.ID)
	codes := checkCodes(carla.Checks)
	if carla.Status != "submitted" || carla.DamagesNote != "Risco na porta do motorista" || carla.AcceptedAt == nil || carla.Link != "" {
		t.Fatalf("enviada = %+v", carla)
	}
	for code, level := range map[string]string{"no_location": "warn", "same_place": "ok", "same_time": "ok", "file_source": "warn"} {
		if codes[code].Level != level {
			t.Errorf("conferência %s = %+v (todas: %+v)", code, codes[code], carla.Checks)
		}
	}
	if _, ok := codes["duplicate"]; ok {
		t.Errorf("a primeira vistoria não tem foto repetida: %+v", carla.Checks)
	}

	// Outra placa com a mesma foto da frente: a foto repetida.
	dino := request(map[string]any{"kind": "periodic", "personName": "Dino", "plate": "DEF4G56", "vehicleType": "motorcycle"}, http.StatusCreated)
	dinoTok := token(dino)
	for _, slot := range dino.Slots {
		data := photoBytes("dino-" + slot.ID)
		if slot.ID == "front" {
			data = photoBytes("carla-front")
		}
		upload(dinoTok, slot.ID, data, here, http.StatusOK)
	}
	submit(dinoTok, map[string]any{"accept": true}, http.StatusOK)
	if c := checkCodes(orgGet(dino.ID).Checks)["duplicate"]; c.Level != "warn" || !strings.Contains(c.Text, "frente") ||
		!strings.Contains(c.Text, "ABC1D23") {
		t.Errorf("foto repetida = %+v", c)
	}

	// A vistoria com técnico do veículo do associado, que tem rastreador.
	var bia struct {
		ID uuid.UUID `json:"id"`
	}
	_ = json.Unmarshal(env.must(ivo, http.MethodPost, "/api/org/associates", map[string]any{"name": "Bia Associada", "email": "bia@rota.test"},
		http.StatusCreated), &bia)
	var car struct {
		ID uuid.UUID `json:"id"`
	}
	_ = json.Unmarshal(env.must(ivo, http.MethodPost, "/api/org/associates/"+bia.ID.String()+"/vehicles",
		map[string]any{"name": "Onix da Bia", "plate": "ROT1A23"}, http.StatusCreated), &car)
	var device uuid.UUID
	if err := env.db.QueryRow(ctx, `INSERT INTO devices (imei) VALUES ('869247061239001') RETURNING id`).Scan(&device); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `UPDATE vehicles SET device_id = $1 WHERE id = $2`, device, car.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, `INSERT INTO positions (device_id, gps_timestamp, latitude, longitude, speed_kmh)
		VALUES ($1, NOW() - interval '2 minutes', -18.91875, -48.27735, 0)`, device); err != nil {
		t.Fatal(err)
	}
	tech := request(map[string]any{"vehicleId": car.ID, "mode": "technician"}, http.StatusCreated)
	if tech.PriceCents != 8000 || tech.Mode != "technician" || tech.Kind != "periodic" || tech.AssociateID == nil || *tech.AssociateID != bia.ID ||
		tech.Plate != "ROT1A23" {
		t.Fatalf("com técnico = %+v", tech)
	}
	if got := pusher.take(); len(got[bia.ID]) != 0 {
		t.Errorf("a vistoria com técnico não avisa o associado: %+v", got)
	}
	if link := env.inspects.TechnicianLink(ctx, car.ID); link != tech.Link {
		t.Errorf("link do técnico na instalação = %q, quero %q", link, tech.Link)
	}
	techTok := token(tech)
	for _, slot := range tech.Slots {
		upload(techTok, slot.ID, photoBytes("tech-"+slot.ID), here, http.StatusOK)
	}
	submit(techTok, map[string]any{"accept": true}, http.StatusOK)
	if c := checkCodes(orgGet(tech.ID).Checks)["tracker_match"]; c.Level != "ok" {
		t.Errorf("conferência do rastreador = %+v", orgGet(tech.ID).Checks)
	}
	if link := env.inspects.TechnicianLink(ctx, car.ID); link != "" {
		t.Errorf("link do técnico depois do envio = %q", link)
	}

	// Devolvida: a refação abre no mesmo link, com o motivo e as fotos boas;
	// a frente refeita longe do rastreador.
	env.must(ivo, http.MethodPost, "/api/org/inspections/"+tech.ID.String()+"/review",
		map[string]any{"approve": false, "note": "A frente saiu sem a placa."}, http.StatusOK)
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/inspections/"+techTok, nil, http.StatusOK), &view)
	if view.Status != "pending" || view.RequestNote != "A frente saiu sem a placa." || len(view.Photos) != 6 {
		t.Fatalf("refação no link = %+v", view)
	}
	upload(techTok, "front", photoBytes("tech-front-2"), map[string]string{"latitude": "-18.96", "longitude": "-48.27", "source": "camera"},
		http.StatusOK)
	submit(techTok, map[string]any{"accept": true}, http.StatusOK)
	var all []inspectionView
	_ = json.Unmarshal(env.must(ivo, http.MethodGet, "/api/org/inspections", nil, http.StatusOK), &all)
	var redo inspectionView
	for _, in := range all {
		if in.RedoOf != nil && *in.RedoOf == tech.ID {
			redo = in
		}
	}
	codes = checkCodes(redo.Checks)
	if redo.Status != "submitted" || redo.PriceCents != 0 || codes["kept"].Level != "info" || codes["tracker_far"].Level != "warn" {
		t.Fatalf("refação = %+v", redo)
	}
	if _, ok := codes["spread_place"]; ok {
		t.Errorf("as fotos aproveitadas não contam no lugar: %+v", redo.Checks)
	}
	env.must(ivo, http.MethodPost, "/api/org/inspections/"+redo.ID.String()+"/review", map[string]any{"approve": true}, http.StatusOK)

	// Cancelada e vencida: o link não aceita mais fotos; renovar volta a abrir.
	eva := request(map[string]any{"kind": "claim", "personName": "Eva", "plate": "GHI7J89"}, http.StatusCreated)
	evaTok := token(eva)
	var renewed inspectionView
	_ = json.Unmarshal(env.must(ivo, http.MethodPost, "/api/org/inspections/"+eva.ID.String()+"/renew", nil, http.StatusOK), &renewed)
	env.inspects.SetClock(func() time.Time { return time.Now().Add(8 * 24 * time.Hour) })
	upload(evaTok, "front", photoBytes("eva"), here, http.StatusBadRequest)
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/inspections/"+evaTok, nil, http.StatusOK), &view)
	if !view.Expired {
		t.Errorf("o link vencido não aparece vencido: %+v", view)
	}
	env.must(ivo, http.MethodPost, "/api/org/inspections/"+eva.ID.String()+"/renew", nil, http.StatusOK)
	upload(evaTok, "front", photoBytes("eva"), here, http.StatusOK)
	env.inspects.SetClock(time.Now)
	env.must(ivo, http.MethodPost, "/api/org/inspections/"+eva.ID.String()+"/cancel", nil, http.StatusOK)
	upload(evaTok, "rear", photoBytes("eva-rear"), here, http.StatusBadRequest)
	env.must(ivo, http.MethodPost, "/api/org/inspections/"+eva.ID.String()+"/cancel", nil, http.StatusBadRequest)

	// A fatura da associação: cada vistoria enviada uma vez (a refação e a cancelada, não).
	var inv struct {
		Description string `json:"description"`
		AmountCents int    `json:"amountCents"`
		Items       []struct {
			Kind        string `json:"kind"`
			Plate       string `json:"plate"`
			Detail      string `json:"detail"`
			AmountCents int    `json:"amountCents"`
			Date        string `json:"date"`
		} `json:"items"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations/"+org.ID.String()+"/invoices/generate", nil,
		http.StatusCreated), &inv)
	total := 0
	var details []string
	for _, it := range inv.Items {
		if it.Kind == "inspection" {
			total += it.AmountCents
			details = append(details, it.Plate+" "+it.Detail)
			if it.Date == "" {
				t.Errorf("vistoria sem o dia: %+v", it)
			}
		}
	}
	if total != 1500+1500+8000 || len(details) != 3 || !strings.Contains(inv.Description, "3 vistorias") ||
		details[0] != "ABC1D23 Autovistoria · Adesão" || details[2] != "ROT1A23 Vistoria com técnico · Revistoria" {
		t.Fatalf("fatura = %+v (vistorias: %v)", inv, details)
	}
	for _, id := range []uuid.UUID{carla.ID, dino.ID, tech.ID} {
		if !orgGet(id).Billed {
			t.Errorf("vistoria %s não ficou marcada como cobrada", id)
		}
	}
	var estimate struct {
		Estimate struct {
			Items []struct {
				Kind string `json:"kind"`
			} `json:"items"`
		} `json:"estimate"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodGet, "/api/organizations/"+org.ID.String(), nil, http.StatusOK), &estimate)
	for _, it := range estimate.Estimate.Items {
		if it.Kind == "inspection" {
			t.Fatalf("a próxima fatura cobra a vistoria de novo: %+v", estimate.Estimate.Items)
		}
	}
}

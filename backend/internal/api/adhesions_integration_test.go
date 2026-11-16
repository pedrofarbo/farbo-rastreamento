package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// TestAdhesions: a adesão digital de ponta a ponta. O link da associação só
// abre ligado; a pessoa manda os dados (conferidos), faz a vistoria de
// adesão pelo link dela, aceita o termo do associado com a preferência da
// instalação e acompanha; a associação aprova: o associado, o veículo e o
// endereço são cadastrados, a vistoria passa para o veículo, o aceite do
// termo vale para a conta nova e o veículo entra na fila de instalação com
// a preferência. Outra adesão é recusada, com o motivo. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestAdhesions(t *testing.T) {
	notifier := &recordingOrgNotifier{}
	env := newCredEnvWith(t, credEnvOptions{orgs: notifier, occurrences: notifier, adhesions: true})
	ctx := context.Background()
	if _, err := env.auth.CreateUser(ctx, "admin@adesao.test", "Central", auth.RoleAdmin, userPassword); err != nil {
		t.Fatal(err)
	}
	central := env.tokenFor("admin@adesao.test")
	var created struct {
		Organization struct {
			ID uuid.UUID `json:"id"`
		} `json:"organization"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations", map[string]any{
		"kind": "association", "name": "Associação Porta Aberta", "document": "11.222.333/0001-81",
		"email": "contato@porta.test", "priceCents": 3500, "dueDay": 10, "inspectionCents": 1500,
		"admin": map[string]string{"name": "Ivo Analista", "email": "ivo@porta.test"},
	}, http.StatusCreated), &created)
	env.must("", http.MethodPost, "/api/auth/reset-password",
		map[string]string{"token": notifier.token("ivo@porta.test"), "password": userPassword}, http.StatusNoContent)
	ivo := env.tokenFor("ivo@porta.test")

	var settings struct {
		Enabled bool   `json:"enabled"`
		URL     string `json:"url"`
	}
	_ = json.Unmarshal(env.must(ivo, http.MethodGet, "/api/org/adhesion-settings", nil, http.StatusOK), &settings)
	_, orgToken, ok := strings.Cut(settings.URL, "/adesao/")
	if settings.Enabled || !ok || !strings.HasPrefix(settings.URL, "https://painel.farbo.test/adesao/") {
		t.Fatalf("link de adesão = %+v", settings)
	}
	var org struct {
		OrganizationName string `json:"organizationName"`
		Enabled          bool   `json:"enabled"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/adhesion/"+orgToken, nil, http.StatusOK), &org)
	if org.OrganizationName != "Associação Porta Aberta" || org.Enabled {
		t.Fatalf("organização no link = %+v", org)
	}
	env.must("", http.MethodGet, "/api/public/adhesion/xyz", nil, http.StatusNotFound)

	body := func(over map[string]any) map[string]any {
		b := map[string]any{
			"name": "Carla Nova Silva", "document": "529.982.247-25", "email": "carla@nova.test", "phone": "(34) 98888-1234",
			"address": map[string]string{"zipCode": "38400-000", "street": "Rua das Acácias", "number": "120", "district": "Centro",
				"city": "Uberlândia", "state": "mg"},
			"vehicleType": "car", "plate": "abc-1d23", "brand": "Volkswagen", "model": "Gol", "year": 2015, "color": "Prata",
		}
		for k, v := range over {
			b[k] = v
		}
		return b
	}
	env.must("", http.MethodPost, "/api/public/adhesion/"+orgToken, body(nil), http.StatusBadRequest) // desligado
	env.must(ivo, http.MethodPut, "/api/org/adhesion-settings", map[string]any{"enabled": true}, http.StatusOK)
	env.must("", http.MethodPost, "/api/public/adhesion/"+orgToken, body(map[string]any{"document": "111.111.111-11"}), http.StatusBadRequest)
	env.must("", http.MethodPost, "/api/public/adhesion/"+orgToken, body(map[string]any{"plate": "AB12"}), http.StatusBadRequest)
	env.must("", http.MethodPost, "/api/public/adhesion/"+orgToken, body(map[string]any{"email": "ivo@porta.test"}), http.StatusBadRequest)
	env.must("", http.MethodPost, "/api/public/adhesion/"+orgToken,
		body(map[string]any{"address": map[string]string{"zipCode": "123"}}), http.StatusBadRequest)

	type follow struct {
		Status            string `json:"status"`
		FirstName         string `json:"firstName"`
		Plate             string `json:"plate"`
		VehicleName       string `json:"vehicleName"`
		InspectionStatus  string `json:"inspectionStatus"`
		InspectionLink    string `json:"inspectionLink"`
		InstallPreference string `json:"installPreference"`
		ReviewNote        string `json:"reviewNote"`
		FollowToken       string `json:"followToken"`
	}
	var started follow
	raw := env.must("", http.MethodPost, "/api/public/adhesion/"+orgToken, body(nil), http.StatusCreated)
	_ = json.Unmarshal(raw, &started)
	if started.Status != "pending" || started.FirstName != "Carla" || started.Plate != "ABC1D23" || started.VehicleName != "Gol" ||
		started.InspectionStatus != "pending" || started.FollowToken == "" || !strings.Contains(started.InspectionLink, "/vistoria/") {
		t.Fatalf("adesão nova = %s", raw)
	}
	for _, private := range []string{"52998224725", "carla@nova.test", "8888", "Acácias", "Silva"} {
		if strings.Contains(string(raw), private) {
			t.Errorf("o acompanhamento mostra %q: %s", private, raw)
		}
	}
	env.must("", http.MethodPost, "/api/public/adhesion/"+orgToken, body(map[string]any{"email": "outra@nova.test"}), http.StatusBadRequest)
	followPath := "/api/public/adhesions/" + started.FollowToken

	// O termo só depois da vistoria.
	term := env.contract.AssociateTerm()
	env.must("", http.MethodPost, followPath+"/accept", map[string]any{"version": term.Version}, http.StatusBadRequest)
	_, inspectionToken, _ := strings.Cut(started.InspectionLink, "/vistoria/")
	var view struct {
		Slots []struct {
			ID       string `json:"id"`
			Optional bool   `json:"optional"`
		} `json:"slots"`
	}
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/inspections/"+inspectionToken, nil, http.StatusOK), &view)
	if len(view.Slots) != 15 {
		t.Fatalf("a vistoria da adesão é a de adesão: %+v", view)
	}
	for _, slot := range view.Slots {
		if status, out := env.upload("", http.MethodPut, "/api/public/inspections/"+inspectionToken+"/photos/"+slot.ID,
			photoBytes("adesao-"+slot.ID), map[string]string{"latitude": "-18.91", "longitude": "-48.27", "source": "camera"}); status != http.StatusOK {
			t.Fatalf("foto %s: %d %s", slot.ID, status, out)
		}
	}
	env.must("", http.MethodPost, "/api/public/inspections/"+inspectionToken+"/submit", map[string]any{"accept": true}, http.StatusOK)

	env.must("", http.MethodPost, followPath+"/accept", map[string]any{"version": "A0"}, http.StatusBadRequest)
	var accepted follow
	_ = json.Unmarshal(env.must("", http.MethodPost, followPath+"/accept",
		map[string]any{"version": term.Version, "preference": "Sábado de manhã"}, http.StatusOK), &accepted)
	if accepted.Status != "submitted" || accepted.InstallPreference != "Sábado de manhã" || accepted.InspectionStatus != "submitted" {
		t.Fatalf("depois do termo = %+v", accepted)
	}
	env.must("", http.MethodPost, followPath+"/accept", map[string]any{"version": term.Version}, http.StatusBadRequest)

	// A associação: a lista e a aprovação (a vistoria em revisão é aprovada junto).
	var list []struct {
		ID         uuid.UUID `json:"id"`
		Status     string    `json:"status"`
		Document   string    `json:"document"`
		Inspection struct {
			ID     uuid.UUID `json:"id"`
			Status string    `json:"status"`
		} `json:"inspection"`
	}
	_ = json.Unmarshal(env.must(ivo, http.MethodGet, "/api/org/adhesions", nil, http.StatusOK), &list)
	if len(list) != 1 || list[0].Status != "submitted" || list[0].Document != "52998224725" || list[0].Inspection.Status != "submitted" {
		t.Fatalf("adesões no painel = %+v", list)
	}
	var approved struct {
		Status      string     `json:"status"`
		AssociateID *uuid.UUID `json:"associateId"`
		VehicleID   *uuid.UUID `json:"vehicleId"`
		Inspection  struct {
			Status    string     `json:"status"`
			VehicleID *uuid.UUID `json:"vehicleId"`
		} `json:"inspection"`
	}
	_ = json.Unmarshal(env.must(ivo, http.MethodPost, "/api/org/adhesions/"+list[0].ID.String()+"/approve", nil, http.StatusOK), &approved)
	if approved.Status != "approved" || approved.AssociateID == nil || approved.VehicleID == nil || approved.Inspection.Status != "approved" ||
		approved.Inspection.VehicleID == nil || *approved.Inspection.VehicleID != *approved.VehicleID {
		t.Fatalf("aprovada = %+v", approved)
	}
	env.must(ivo, http.MethodPost, "/api/org/adhesions/"+list[0].ID.String()+"/approve", nil, http.StatusBadRequest)

	var name, email, document, plate, kind, zip string
	if err := env.db.QueryRow(ctx, `
		SELECT u.name, u.email, u.document, v.plate, v.kind, ad.zip_code
		FROM users u JOIN vehicles v ON v.owner_id = u.id JOIN customer_addresses ad ON ad.customer_id = u.id
		WHERE u.id = $1 AND u.organization_id = $2`, *approved.AssociateID, created.Organization.ID).
		Scan(&name, &email, &document, &plate, &kind, &zip); err != nil {
		t.Fatal(err)
	}
	if name != "Carla Nova Silva" || email != "carla@nova.test" || document != "52998224725" || plate != "ABC1D23" || kind != "CAR" ||
		zip != "38400000" {
		t.Errorf("cadastro = %s %s %s %s %s %s", name, email, document, plate, kind, zip)
	}
	if ok, err := env.contract.HasAccepted(ctx, *approved.AssociateID); err != nil || !ok {
		t.Errorf("o aceite do termo não passou para a conta: %v %v", ok, err)
	}
	var pref string
	if err := env.db.QueryRow(ctx, `SELECT install_preference FROM adhesions WHERE vehicle_id = $1`, *approved.VehicleID).Scan(&pref); err != nil ||
		pref != "Sábado de manhã" {
		t.Errorf("preferência da instalação (a fila da central lê daqui) = %q %v", pref, err)
	}
	var done follow
	_ = json.Unmarshal(env.must("", http.MethodGet, followPath, nil, http.StatusOK), &done)
	if done.Status != "approved" {
		t.Errorf("acompanhamento depois da aprovação = %+v", done)
	}

	// Outra adesão, recusada (a vistoria em aberto é cancelada).
	var other follow
	_ = json.Unmarshal(env.must("", http.MethodPost, "/api/public/adhesion/"+orgToken,
		body(map[string]any{"email": "dino@prado.test", "plate": "DEF4G56", "vehicleType": "motorcycle", "model": "CG 160"}),
		http.StatusCreated), &other)
	_ = json.Unmarshal(env.must(ivo, http.MethodGet, "/api/org/adhesions?situacao=pending", nil, http.StatusOK), &list)
	if len(list) != 1 {
		t.Fatalf("pendentes = %+v", list)
	}
	env.must(ivo, http.MethodPost, "/api/org/adhesions/"+list[0].ID.String()+"/reject", map[string]any{}, http.StatusBadRequest)
	env.must(ivo, http.MethodPost, "/api/org/adhesions/"+list[0].ID.String()+"/reject", map[string]any{"note": "Moto fora da área de atendimento."},
		http.StatusOK)
	_ = json.Unmarshal(env.must("", http.MethodGet, "/api/public/adhesions/"+other.FollowToken, nil, http.StatusOK), &other)
	if other.Status != "rejected" || other.ReviewNote != "Moto fora da área de atendimento." || other.InspectionStatus != "canceled" {
		t.Errorf("recusada = %+v", other)
	}
}

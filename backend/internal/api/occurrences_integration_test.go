package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
)

// jpegBytes é o começo de um JPEG (o que a conferência do tipo olha).
var jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{1}, 2000)...)

// upload manda um arquivo (multipart, campo file) com os campos extras.
func (e *credEnv) upload(token, method, path string, data []byte, fields map[string]string) (int, []byte) {
	e.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = form.WriteField(k, v)
	}
	part, _ := form.CreateFormFile("file", "foto.jpg")
	_, _ = part.Write(data)
	_ = form.Close()
	req, _ := http.NewRequest(method, e.srv.URL+path, &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, out
}

// As ocorrências e a vistoria de ponta a ponta: o associado abre a
// ocorrência com a foto (a equipe recebe o e-mail), a equipe muda a
// situação (o associado é avisado) e ninguém de fora vê; a equipe pede a
// vistoria, o associado manda as fotos de todos os ângulos, a equipe
// devolve (com o motivo) e aprova a refeita. Precisa de
// FARBO_TEST_DATABASE_URL.
func TestClaimsAndInspections(t *testing.T) {
	notifier := &recordingOrgNotifier{}
	env := newCredEnvWith(t, credEnvOptions{orgs: notifier, occurrences: notifier})
	pusher := &recordingPusher{}
	env.claims.SetPusher(pusher)
	env.inspects.SetPusher(pusher)
	ctx := context.Background()
	if _, err := env.auth.CreateUser(ctx, "admin@ocorr.test", "Central", auth.RoleAdmin, userPassword); err != nil {
		t.Fatal(err)
	}
	central := env.tokenFor("admin@ocorr.test")
	var created struct {
		Organization struct {
			ID uuid.UUID `json:"id"`
		} `json:"organization"`
	}
	_ = json.Unmarshal(env.must(central, http.MethodPost, "/api/organizations", map[string]any{
		"kind": "insurer", "name": "Proteja Seguros", "document": "11.222.333/0001-81",
		"email": "contato@proteja.test", "priceCents": 4000, "dueDay": 10,
		"admin": map[string]string{"name": "Ivo Analista", "email": "ivo@proteja.test"},
	}, http.StatusCreated), &created)
	env.must("", http.MethodPost, "/api/auth/reset-password",
		map[string]string{"token": notifier.token("ivo@proteja.test"), "password": userPassword}, http.StatusNoContent)
	ivo := env.tokenFor("ivo@proteja.test")
	var bia struct {
		ID uuid.UUID `json:"id"`
	}
	_ = json.Unmarshal(env.must(ivo, http.MethodPost, "/api/org/associates", map[string]any{"name": "Bia Segurada", "email": "bia@segurada.test"},
		http.StatusCreated), &bia)
	var car struct {
		ID uuid.UUID `json:"id"`
	}
	_ = json.Unmarshal(env.must(ivo, http.MethodPost, "/api/org/associates/"+bia.ID.String()+"/vehicles",
		map[string]any{"name": "Onix da Bia", "plate": "SEG1A23"}, http.StatusCreated), &car)
	if _, err := env.auth.SetPassword(ctx, bia.ID, userPassword); err != nil {
		t.Fatal(err)
	}
	biaToken := env.tokenFor("bia@segurada.test")
	if _, err := env.auth.CreateUser(ctx, "zeca@cliente.test", "Zeca", auth.RoleCustomer, userPassword); err != nil {
		t.Fatal(err)
	}
	zeca := env.tokenFor("zeca@cliente.test")

	// Quem não é associado não tem ocorrências.
	env.must(zeca, http.MethodGet, "/api/me/claims", nil, http.StatusForbidden)

	// A associada abre a ocorrência.
	env.must(biaToken, http.MethodPost, "/api/me/claims", map[string]any{"kind": "collision", "occurredAt": time.Now().Add(-time.Hour),
		"description": ""}, http.StatusBadRequest)
	var claim struct {
		ID      uuid.UUID `json:"id"`
		Status  string    `json:"status"`
		Photos  int       `json:"photos"`
		Updates []struct {
			Status, AuthorKind, Note string
		} `json:"updates"`
		Files []struct {
			ID   uuid.UUID `json:"id"`
			Kind string    `json:"kind"`
		} `json:"files"`
	}
	_ = json.Unmarshal(env.must(biaToken, http.MethodPost, "/api/me/claims", map[string]any{
		"vehicleId": car.ID, "kind": "collision", "occurredAt": time.Now().Add(-2 * time.Hour),
		"place": "Av. Paulista, 1000", "description": "Bateram na traseira no semáforo.", "policeReport": "BO 123/2026",
	}, http.StatusCreated), &claim)
	if claim.Status != "open" || len(claim.Updates) != 1 {
		t.Fatalf("ocorrência = %+v", claim)
	}
	if len(notifier.claims) != 1 || notifier.claims[0] != "ivo@proteja.test: Colisão — Bia Segurada" {
		t.Fatalf("aviso à equipe = %v", notifier.claims)
	}
	path := "/api/me/claims/" + claim.ID.String()
	if status, out := env.upload(biaToken, http.MethodPost, path+"/files", jpegBytes, map[string]string{"kind": "photo"}); status != http.StatusCreated {
		t.Fatalf("foto: %d %s", status, out)
	}
	if status, _ := env.upload(biaToken, http.MethodPost, path+"/files", []byte("não é imagem"), map[string]string{"kind": "photo"}); status != http.StatusBadRequest {
		t.Fatalf("arquivo que não é foto: %d", status)
	}
	env.must(zeca, http.MethodGet, path, nil, http.StatusNotFound)

	// A equipe vê, baixa a foto e muda a situação; a associada é avisada.
	orgPath := "/api/org/claims/" + claim.ID.String()
	_ = json.Unmarshal(env.must(ivo, http.MethodGet, orgPath, nil, http.StatusOK), &claim)
	if claim.Photos != 1 || len(claim.Files) != 1 {
		t.Fatalf("na equipe = %+v", claim)
	}
	if body := env.must(ivo, http.MethodGet, orgPath+"/files/"+claim.Files[0].ID.String(), nil, http.StatusOK); !bytes.Equal(body, jpegBytes) {
		t.Fatalf("a foto baixada não é a enviada (%d bytes)", len(body))
	}
	_ = json.Unmarshal(env.must(ivo, http.MethodPost, orgPath+"/status",
		map[string]string{"status": "analyzing", "note": "Mande a foto da outra placa, por favor."}, http.StatusOK), &claim)
	if claim.Status != "analyzing" || len(claim.Updates) != 2 || claim.Updates[1].AuthorKind != "organization" {
		t.Fatalf("em análise = %+v", claim)
	}
	if got := pusher.take(); len(got[bia.ID]) != 1 || got[bia.ID][0].Title != "Ocorrência: em análise" {
		t.Fatalf("aviso à associada = %+v", got)
	}
	_ = json.Unmarshal(env.must(biaToken, http.MethodPost, path+"/notes", map[string]string{"note": "Enviei."}, http.StatusOK), &claim)
	if len(claim.Updates) != 3 || claim.Updates[2].AuthorKind != "associate" {
		t.Fatalf("nota da associada = %+v", claim)
	}
	env.must(biaToken, http.MethodPost, path+"/status", map[string]string{"status": "approved"}, http.StatusNotFound)
	_ = json.Unmarshal(env.must(ivo, http.MethodPost, orgPath+"/status", map[string]string{"status": "closed"}, http.StatusOK), &claim)
	pusher.take()
	if status, _ := env.upload(biaToken, http.MethodPost, path+"/files", jpegBytes, map[string]string{"kind": "photo"}); status != http.StatusBadRequest {
		t.Fatalf("anexar na encerrada: %d", status)
	}
	var list []struct{ ID uuid.UUID }
	_ = json.Unmarshal(env.must(ivo, http.MethodGet, "/api/org/claims?situacao=closed", nil, http.StatusOK), &list)
	if len(list) != 1 {
		t.Fatalf("lista = %+v", list)
	}

	// A vistoria: pedida pela equipe, uma em aberto por veículo.
	var inspection struct {
		ID     uuid.UUID `json:"id"`
		Status string    `json:"status"`
		Photos []struct {
			Slot string `json:"slot"`
		} `json:"photos"`
		ReviewNote string `json:"reviewNote"`
	}
	_ = json.Unmarshal(env.must(ivo, http.MethodPost, "/api/org/inspections", map[string]any{"vehicleId": car.ID, "note": "Vistoria de entrada"},
		http.StatusCreated), &inspection)
	env.must(ivo, http.MethodPost, "/api/org/inspections", map[string]any{"vehicleId": car.ID}, http.StatusBadRequest)
	if got := pusher.take(); len(got[bia.ID]) != 1 {
		t.Fatalf("aviso da vistoria = %+v", got)
	}
	ipath := "/api/me/inspections/" + inspection.ID.String()
	env.must(biaToken, http.MethodPost, ipath+"/submit", nil, http.StatusBadRequest)
	for _, slot := range []string{"front", "rear", "left", "right", "dashboard", "chassis"} {
		if status, out := env.upload(biaToken, http.MethodPut, ipath+"/photos/"+slot, jpegBytes,
			map[string]string{"latitude": "-23.5", "longitude": "-46.6"}); status != http.StatusOK {
			t.Fatalf("foto %s: %d %s", slot, status, out)
		}
	}
	if status, _ := env.upload(biaToken, http.MethodPut, ipath+"/photos/teto", jpegBytes, nil); status != http.StatusBadRequest {
		t.Fatalf("ângulo inválido: %d", status)
	}
	_ = json.Unmarshal(env.must(biaToken, http.MethodPost, ipath+"/submit", nil, http.StatusOK), &inspection)
	if inspection.Status != "submitted" || len(inspection.Photos) != 6 {
		t.Fatalf("enviada = %+v", inspection)
	}
	if status, _ := env.upload(biaToken, http.MethodPut, ipath+"/photos/front", jpegBytes, nil); status != http.StatusBadRequest {
		t.Fatalf("trocar foto da enviada: %d", status)
	}
	// Devolvida sem motivo, não; com motivo, volta uma nova para refazer (com as fotos).
	opath := "/api/org/inspections/" + inspection.ID.String()
	env.must(ivo, http.MethodPost, opath+"/review", map[string]any{"approve": false}, http.StatusBadRequest)
	_ = json.Unmarshal(env.must(ivo, http.MethodPost, opath+"/review", map[string]any{"approve": false, "note": "O chassi saiu tremido."},
		http.StatusOK), &inspection)
	if inspection.Status != "rejected" {
		t.Fatalf("devolvida = %+v", inspection)
	}
	var mine []struct {
		ID     uuid.UUID `json:"id"`
		Status string    `json:"status"`
		Photos []struct{ Slot string }
	}
	_ = json.Unmarshal(env.must(biaToken, http.MethodGet, "/api/me/inspections", nil, http.StatusOK), &mine)
	if len(mine) != 2 || mine[0].Status != "pending" || len(mine[0].Photos) != 6 {
		t.Fatalf("vistorias da associada = %+v", mine)
	}
	redo := "/api/me/inspections/" + mine[0].ID.String()
	if status, _ := env.upload(biaToken, http.MethodPut, redo+"/photos/chassis", jpegBytes, nil); status != http.StatusOK {
		t.Fatalf("refazer o chassi: %d", status)
	}
	env.must(biaToken, http.MethodPost, redo+"/submit", nil, http.StatusOK)
	_ = json.Unmarshal(env.must(ivo, http.MethodPost, "/api/org/inspections/"+mine[0].ID.String()+"/review", map[string]any{"approve": true},
		http.StatusOK), &inspection)
	if inspection.Status != "approved" {
		t.Fatalf("aprovada = %+v", inspection)
	}
	if body := env.must(ivo, http.MethodGet, "/api/org/inspections/"+mine[0].ID.String()+"/photos/front", nil, http.StatusOK); !bytes.Equal(body, jpegBytes) {
		t.Fatalf("foto da vistoria (%d bytes)", len(body))
	}
	env.must(zeca, http.MethodGet, redo, nil, http.StatusNotFound)
}

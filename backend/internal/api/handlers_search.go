package api

import (
	"context"
	"net/http"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// O máximo de cada grupo na busca geral: o resto se acha na tela do grupo.
const searchLimit = 6

// searchResults é a busca geral do painel da central: os clientes, os
// veículos, os rastreadores e os chips livres que casam com o texto.
type searchResults struct {
	Customers []searchCustomer `json:"customers"`
	Vehicles  []searchVehicle  `json:"vehicles"`
	Devices   []searchDevice   `json:"devices"`
	Chips     []searchChip     `json:"chips"`
}

type searchCustomer struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Phone    string    `json:"phone"`
	Document string    `json:"document"`
	Active   bool      `json:"active"`
}

type searchVehicle struct {
	ID           uuid.UUID  `json:"id"`
	Name         string     `json:"name"`
	Plate        string     `json:"plate"`
	CustomerID   *uuid.UUID `json:"customerId"`
	CustomerName string     `json:"customerName"`
	DeviceIMEI   string     `json:"deviceImei"`
}

type searchDevice struct {
	ID           uuid.UUID  `json:"id"`
	IMEI         string     `json:"imei"`
	Model        string     `json:"model"`
	Status       string     `json:"status"`
	VehicleID    *uuid.UUID `json:"vehicleId"`
	VehicleName  string     `json:"vehicleName"`
	CustomerID   *uuid.UUID `json:"customerId"`
	CustomerName string     `json:"customerName"`
}

type searchChip struct {
	ID          uuid.UUID `json:"id"`
	ICCID       string    `json:"iccid"`
	PhoneNumber string    `json:"phoneNumber"`
	Carrier     string    `json:"carrier"`
}

// searchTerms: o texto (minúsculo), só os dígitos e só as letras e os
// dígitos (a placa "ABC-1D23" e o CPF com pontos casam como forem digitados).
func searchTerms(q string) (text, digits, plate string) {
	text = strings.ToLower(strings.TrimSpace(q))
	var d, p strings.Builder
	for _, r := range strings.ToUpper(text) {
		switch {
		case r >= '0' && r <= '9':
			d.WriteRune(r)
			p.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			p.WriteRune(r)
		}
	}
	return text, d.String(), p.String()
}

// handleSearch: a busca geral do painel (?q=). Por nome, e-mail, telefone ou
// CPF/CNPJ do cliente; placa ou nome do veículo; IMEI, linha ou ICCID do
// rastreador; ICCID ou linha do chip que não está em nenhum rastreador. Os
// números valem a partir de 4 dígitos; o texto, de 2 letras.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	out, err := s.search(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		handleStoreError(w, err, "busca indisponível")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) search(ctx context.Context, q string) (*searchResults, error) {
	out := &searchResults{Customers: []searchCustomer{}, Vehicles: []searchVehicle{}, Devices: []searchDevice{}, Chips: []searchChip{}}
	text, digits, plate := searchTerms(q)
	if len(digits) < 4 {
		digits = ""
	}
	// Texto com menos de 2 letras (ou só números curtos) não busca nada.
	letters := 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	if letters < 2 && digits == "" {
		return out, nil
	}
	if len(plate) < 3 {
		plate = ""
	}
	if letters < 2 {
		text = ""
	}

	// Os clientes, como na lista: sem a equipe da conta e sem a contratação
	// pelo site ainda não paga. Quem começa com o texto vem primeiro.
	rows, err := s.DB.Query(ctx, `
		SELECT u.id, u.name, u.email, COALESCE(u.phone, ''), COALESCE(u.document, ''), u.active
		FROM users u
		WHERE u.role = 'customer'
			AND NOT EXISTS (SELECT 1 FROM account_members am WHERE am.user_id = u.id)
			AND NOT checkout_pending(u.id)
			AND (($1 <> '' AND (strpos(lower(u.name), $1) > 0 OR strpos(lower(u.email), $1) > 0))
				OR ($2 <> '' AND (strpos(regexp_replace(COALESCE(u.phone, ''), '\D', '', 'g'), $2) > 0
					OR strpos(regexp_replace(COALESCE(u.document, ''), '\D', '', 'g'), $2) > 0)))
		ORDER BY ($1 <> '' AND left(lower(u.name), length($1)) = $1) DESC, lower(u.name), u.id
		LIMIT $3`, text, digits, searchLimit)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var c searchCustomer
		if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.Document, &c.Active); err != nil {
			rows.Close()
			return nil, database.MapError(err)
		}
		out.Customers = append(out.Customers, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}

	// Os veículos, pela placa (sem a pontuação) ou pelo nome.
	rows, err = s.DB.Query(ctx, `
		SELECT v.id, v.name, COALESCE(v.plate, ''), u.id, COALESCE(u.name, ''), COALESCE(d.imei, '')
		FROM vehicles v
		LEFT JOIN users u ON u.id = v.owner_id
		LEFT JOIN devices d ON d.id = v.device_id
		WHERE ($1 <> '' AND strpos(regexp_replace(upper(COALESCE(v.plate, '')), '[^A-Z0-9]', '', 'g'), $1) > 0)
			OR ($2 <> '' AND strpos(lower(v.name), $2) > 0)
		ORDER BY ($1 <> '' AND regexp_replace(upper(COALESCE(v.plate, '')), '[^A-Z0-9]', '', 'g') = $1) DESC, v.created_at DESC, v.id
		LIMIT $3`, plate, text, searchLimit)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var v searchVehicle
		if err := rows.Scan(&v.ID, &v.Name, &v.Plate, &v.CustomerID, &v.CustomerName, &v.DeviceIMEI); err != nil {
			rows.Close()
			return nil, database.MapError(err)
		}
		out.Vehicles = append(out.Vehicles, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}

	if digits == "" {
		return out, nil
	}

	// Os rastreadores, pelo IMEI, pela linha ou pelo ICCID do chip.
	rows, err = s.DB.Query(ctx, `
		SELECT d.id, d.imei, COALESCE(d.model, ''), COALESCE(d.status, ''), v.id, COALESCE(v.name, ''), u.id, COALESCE(u.name, '')
		FROM devices d
		LEFT JOIN vehicles v ON v.device_id = d.id
		LEFT JOIN users u ON u.id = v.owner_id
		WHERE strpos(d.imei, $1) > 0
			OR strpos(regexp_replace(COALESCE(d.phone_number, ''), '\D', '', 'g'), $1) > 0
			OR strpos(COALESCE(d.iccid, ''), $1) > 0
		ORDER BY (d.imei = $1) DESC, d.imei
		LIMIT $2`, digits, searchLimit)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var d searchDevice
		if err := rows.Scan(&d.ID, &d.IMEI, &d.Model, &d.Status, &d.VehicleID, &d.VehicleName, &d.CustomerID, &d.CustomerName); err != nil {
			rows.Close()
			return nil, database.MapError(err)
		}
		out.Devices = append(out.Devices, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}

	// Os chips livres: o que está num rastreador já aparece nele.
	rows, err = s.DB.Query(ctx, `
		SELECT c.id, c.iccid, c.phone_number, c.carrier
		FROM chips c
		WHERE NOT EXISTS (SELECT 1 FROM devices d WHERE d.iccid = c.iccid)
			AND (strpos(c.iccid, $1) > 0 OR strpos(regexp_replace(c.phone_number, '\D', '', 'g'), $1) > 0)
		ORDER BY c.iccid
		LIMIT $2`, digits, searchLimit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c searchChip
		if err := rows.Scan(&c.ID, &c.ICCID, &c.PhoneNumber, &c.Carrier); err != nil {
			return nil, database.MapError(err)
		}
		out.Chips = append(out.Chips, c)
	}
	return out, database.MapError(rows.Err())
}

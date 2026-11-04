// Package installers mantém os prestadores de instalação recomendados. A
// instalação é paga direto a eles; a plataforma só divulga a lista.
package installers

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

type Installer struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	City        string    `json:"city"`
	ServiceArea string    `json:"serviceArea"`
	// WhatsApp só com dígitos, já com o 55 (ex.: 5511999990000).
	WhatsApp       string    `json:"whatsapp"`
	ServesMoto     bool      `json:"servesMoto"`
	ServesCar      bool      `json:"servesCar"`
	PriceMotoCents *int      `json:"priceMotoCents"`
	PriceCarCents  *int      `json:"priceCarCents"`
	Description    string    `json:"description"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// Public é o que a landing page vê: só os ativos e sem dados de controle.
type Public struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	City           string    `json:"city"`
	ServiceArea    string    `json:"serviceArea"`
	WhatsApp       string    `json:"whatsapp"`
	ServesMoto     bool      `json:"servesMoto"`
	ServesCar      bool      `json:"servesCar"`
	PriceMotoCents *int      `json:"priceMotoCents"`
	PriceCarCents  *int      `json:"priceCarCents"`
	Description    string    `json:"description"`
}

func (i *Installer) Public() Public {
	return Public{
		ID: i.ID, Name: i.Name, City: i.City, ServiceArea: i.ServiceArea, WhatsApp: i.WhatsApp,
		ServesMoto: i.ServesMoto, ServesCar: i.ServesCar,
		PriceMotoCents: i.PriceMotoCents, PriceCarCents: i.PriceCarCents, Description: i.Description,
	}
}

// Input é o cadastro vindo da central.
type Input struct {
	Name           string `json:"name"`
	City           string `json:"city"`
	ServiceArea    string `json:"serviceArea"`
	WhatsApp       string `json:"whatsapp"`
	ServesMoto     bool   `json:"servesMoto"`
	ServesCar      bool   `json:"servesCar"`
	PriceMotoCents *int   `json:"priceMotoCents"`
	PriceCarCents  *int   `json:"priceCarCents"`
	Description    string `json:"description"`
	Active         bool   `json:"active"`
}

// ValidationError descreve um cadastro recusado; a mensagem vai para a tela.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

// Normalize limpa os campos e confere as regras. Devolve o cadastro pronto
// para gravar (WhatsApp só com dígitos e com 55).
func Normalize(in Input) (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.City = strings.TrimSpace(in.City)
	in.ServiceArea = strings.TrimSpace(in.ServiceArea)
	in.Description = strings.TrimSpace(in.Description)

	switch {
	case in.Name == "" || len([]rune(in.Name)) > 100:
		return in, ValidationError{"informe o nome do prestador (até 100 caracteres)"}
	case len([]rune(in.City)) > 100:
		return in, ValidationError{"cidade longa demais (até 100 caracteres)"}
	case len([]rune(in.ServiceArea)) > 200:
		return in, ValidationError{"regiões atendidas: até 200 caracteres"}
	case len([]rune(in.Description)) > 300:
		return in, ValidationError{"descrição: até 300 caracteres"}
	case !in.ServesMoto && !in.ServesCar:
		return in, ValidationError{"marque se o prestador atende moto, carro ou os dois"}
	}
	for _, price := range []*int{in.PriceMotoCents, in.PriceCarCents} {
		if price != nil && (*price < 0 || *price > 100_000_00) {
			return in, ValidationError{"valor de instalação fora da faixa (R$ 0 a R$ 100.000)"}
		}
	}

	phone, ok := normalizeWhatsApp(in.WhatsApp)
	if !ok {
		return in, ValidationError{"WhatsApp inválido: informe DDD e número, ex.: (11) 99999-0000"}
	}
	in.WhatsApp = phone
	return in, nil
}

// NormalizeWhatsApp é o normalizeWhatsApp para os outros cadastros públicos
// (o representante oficial na landing).
func NormalizeWhatsApp(raw string) (string, bool) { return normalizeWhatsApp(raw) }

// normalizeWhatsApp aceita número brasileiro com ou sem 55, com qualquer
// pontuação, e devolve só os dígitos com o 55 na frente.
func normalizeWhatsApp(raw string) (string, bool) {
	trimmed := strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	// Com "+", só o código do Brasil: "+1 415…" não é um celular daqui,
	// mesmo tendo a mesma quantidade de dígitos.
	if strings.HasPrefix(trimmed, "+") && !strings.HasPrefix(trimmed, "+55") {
		return "", false
	}
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if (len(d) == 12 || len(d) == 13) && strings.HasPrefix(d, "55") {
		d = d[2:]
	}
	if !validLocalNumber(d) {
		return "", false
	}
	return "55" + d, true
}

// validLocalNumber confere DDD + número: celular com 9 dígitos começando em 9
// ou fixo com 8 dígitos começando de 2 a 5.
func validLocalNumber(d string) bool {
	if len(d) != 10 && len(d) != 11 {
		return false
	}
	if d[0] == '0' || d[1] == '0' {
		return false
	}
	number := d[2:]
	switch len(number) {
	case 9:
		return number[0] == '9'
	case 8:
		return number[0] >= '2' && number[0] <= '5'
	}
	return false
}

// ---------------------------------------------------------------------------
// Repositório
// ---------------------------------------------------------------------------

const columns = `id, name, city, service_area, whatsapp, serves_moto, serves_car,
	price_moto_cents, price_car_cents, description, active, created_at, updated_at`

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

func scan(row database.Scanner) (*Installer, error) {
	var i Installer
	if err := row.Scan(&i.ID, &i.Name, &i.City, &i.ServiceArea, &i.WhatsApp, &i.ServesMoto,
		&i.ServesCar, &i.PriceMotoCents, &i.PriceCarCents, &i.Description, &i.Active,
		&i.CreatedAt, &i.UpdatedAt); err != nil {
		return nil, database.MapError(err)
	}
	return &i, nil
}

func (r *Repository) list(ctx context.Context, where string) ([]*Installer, error) {
	rows, err := r.db.Query(ctx, `SELECT `+columns+` FROM installers `+where+`
		ORDER BY active DESC, lower(city), lower(name)`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Installer{}
	for rows.Next() {
		i, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (r *Repository) List(ctx context.Context) ([]*Installer, error) { return r.list(ctx, "") }

func (r *Repository) ListActive(ctx context.Context) ([]*Installer, error) {
	return r.list(ctx, "WHERE active")
}

func (r *Repository) Create(ctx context.Context, in Input) (*Installer, error) {
	return scan(r.db.QueryRow(ctx, `
		INSERT INTO installers (name, city, service_area, whatsapp, serves_moto, serves_car,
			price_moto_cents, price_car_cents, description, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+columns,
		in.Name, in.City, in.ServiceArea, in.WhatsApp, in.ServesMoto, in.ServesCar,
		in.PriceMotoCents, in.PriceCarCents, in.Description, in.Active))
}

func (r *Repository) Update(ctx context.Context, id uuid.UUID, in Input) (*Installer, error) {
	return scan(r.db.QueryRow(ctx, `
		UPDATE installers SET name = $2, city = $3, service_area = $4, whatsapp = $5,
			serves_moto = $6, serves_car = $7, price_moto_cents = $8, price_car_cents = $9,
			description = $10, active = $11, updated_at = NOW()
		WHERE id = $1
		RETURNING `+columns,
		id, in.Name, in.City, in.ServiceArea, in.WhatsApp, in.ServesMoto, in.ServesCar,
		in.PriceMotoCents, in.PriceCarCents, in.Description, in.Active))
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM installers WHERE id = $1`, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

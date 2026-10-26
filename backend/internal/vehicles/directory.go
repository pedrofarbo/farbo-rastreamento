package vehicles

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// O tamanho da página da listagem de veículos da central.
const (
	DirectoryPageSize    = 25
	DirectoryMaxPageSize = 100
)

// DirectoryFilter são os filtros da listagem de veículos da central. Os de
// texto valem por pedaço (sem diferença de maiúsculas); a placa e o CPF/CNPJ
// ignoram a pontuação.
type DirectoryFilter struct {
	Plate    string
	Customer string
	Document string
	Email    string
	// Organization: os veículos dos associados dela; NoOrganization, os de
	// quem não é associado.
	Organization   *uuid.UUID
	NoOrganization bool
	Page           int
	PageSize       int
}

// DirectoryEntry é um veículo na listagem da central: o dono, a associação
// que paga por ele e o rastreador.
type DirectoryEntry struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Plate string    `json:"plate"`
	Kind  string    `json:"kind"`
	Brand string    `json:"brand"`
	Model string    `json:"model"`
	Year  *int      `json:"year"`

	CustomerID       *uuid.UUID `json:"customerId"`
	CustomerName     string     `json:"customerName"`
	CustomerEmail    string     `json:"customerEmail"`
	CustomerDocument string     `json:"customerDocument"`

	OrganizationID   *uuid.UUID `json:"organizationId"`
	OrganizationName string     `json:"organizationName"`

	DeviceID     *uuid.UUID `json:"deviceId"`
	DeviceIMEI   string     `json:"deviceImei"`
	DeviceStatus string     `json:"deviceStatus"`
	LastSeenAt   *time.Time `json:"lastSeenAt"`

	CreatedAt time.Time `json:"createdAt"`
}

// DirectoryPage é uma página da listagem, com o total que passa nos filtros.
type DirectoryPage struct {
	Items    []DirectoryEntry `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
}

// onlyDigitsAndLetters tira a pontuação e os espaços (a placa "ABC-1D23" e o
// CPF "123.456.789-09" casam do jeito que forem digitados).
func onlyDigitsAndLetters(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Directory lista os veículos de todas as contas, os mais novos primeiro.
func (s *Service) Directory(ctx context.Context, f DirectoryFilter) (*DirectoryPage, error) {
	if f.PageSize <= 0 {
		f.PageSize = DirectoryPageSize
	}
	f.PageSize = min(f.PageSize, DirectoryMaxPageSize)
	f.Page = max(f.Page, 1)

	var where []string
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if plate := onlyDigitsAndLetters(f.Plate); plate != "" {
		where = append(where, `strpos(regexp_replace(upper(COALESCE(v.plate, '')), '[^A-Z0-9]', '', 'g'), `+arg(plate)+`) > 0`)
	}
	if name := strings.TrimSpace(f.Customer); name != "" {
		where = append(where, `strpos(lower(COALESCE(u.name, '')), lower(`+arg(name)+`)) > 0`)
	}
	if doc := onlyDigitsAndLetters(f.Document); doc != "" {
		where = append(where, `strpos(regexp_replace(upper(COALESCE(u.document, '')), '[^A-Z0-9]', '', 'g'), `+arg(doc)+`) > 0`)
	}
	if email := strings.TrimSpace(f.Email); email != "" {
		where = append(where, `strpos(lower(COALESCE(u.email, '')), lower(`+arg(email)+`)) > 0`)
	}
	switch {
	case f.Organization != nil:
		where = append(where, `u.organization_id = `+arg(*f.Organization))
	case f.NoOrganization:
		where = append(where, `u.organization_id IS NULL`)
	}
	cond := ""
	if len(where) > 0 {
		cond = "WHERE " + strings.Join(where, " AND ")
	}
	limit, offset := arg(f.PageSize), arg((f.Page-1)*f.PageSize)

	rows, err := s.repo.db.Query(ctx, `
		SELECT v.id, v.name, COALESCE(v.plate, ''), v.kind, COALESCE(v.brand, ''), COALESCE(v.model, ''), v.year,
			u.id, COALESCE(u.name, ''), COALESCE(u.email, ''), COALESCE(u.document, ''),
			o.id, COALESCE(o.name, ''),
			d.id, COALESCE(d.imei, ''), COALESCE(d.status, ''), d.last_seen_at,
			v.created_at, count(*) OVER ()
		FROM vehicles v
		LEFT JOIN users u ON u.id = v.owner_id
		LEFT JOIN users o ON o.id = u.organization_id
		LEFT JOIN devices d ON d.id = v.device_id
		`+cond+`
		ORDER BY v.created_at DESC, v.id
		LIMIT `+limit+` OFFSET `+offset, args...)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := &DirectoryPage{Items: []DirectoryEntry{}, Page: f.Page, PageSize: f.PageSize}
	for rows.Next() {
		var e DirectoryEntry
		if err := rows.Scan(&e.ID, &e.Name, &e.Plate, &e.Kind, &e.Brand, &e.Model, &e.Year,
			&e.CustomerID, &e.CustomerName, &e.CustomerEmail, &e.CustomerDocument,
			&e.OrganizationID, &e.OrganizationName,
			&e.DeviceID, &e.DeviceIMEI, &e.DeviceStatus, &e.LastSeenAt,
			&e.CreatedAt, &out.Total); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Uma página depois da última não traz linha nenhuma, e com ela o total.
	if len(out.Items) == 0 && f.Page > 1 {
		if err := s.repo.db.QueryRow(ctx, `
			SELECT count(*) FROM vehicles v LEFT JOIN users u ON u.id = v.owner_id `+cond, args[:len(args)-2]...).
			Scan(&out.Total); err != nil {
			return nil, database.MapError(err)
		}
	}
	return out, nil
}

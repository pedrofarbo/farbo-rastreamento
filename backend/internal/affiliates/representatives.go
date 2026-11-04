package affiliates

import (
	"context"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Representative é o representante oficial autorizado, na landing: o que a
// central marcou para aparecer (e nada dos números nem do cadastro dele).
type Representative struct {
	Name   string `json:"name"`
	Handle string `json:"handle"`
	// Code é o do link dele: contratar com ele leva à indicação dele.
	Code string `json:"code"`
	City string `json:"city"`
	Area string `json:"area"`
	// WhatsApp só com dígitos, com o 55 (vazio: sem o botão).
	WhatsApp string `json:"whatsapp"`
	// HandDelivery: entrega o rastreador em mãos; Installs: ele mesmo instala.
	HandDelivery bool `json:"handDelivery"`
	Installs     bool `json:"installs"`
}

// Representatives são os representantes oficiais na landing: marcados pela
// central e ativos, pela cidade.
func (s *Service) Representatives(ctx context.Context) ([]Representative, error) {
	rows, err := s.db.Query(ctx, `
		SELECT name, handle, code, listing_city, listing_area, listing_whatsapp, hand_delivery,
			manage_installs OR install_mode = 'direct'
		FROM affiliates WHERE listed AND active AND customer_id IS NULL
		ORDER BY lower(listing_city), lower(name) LIMIT 200`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []Representative{}
	for rows.Next() {
		var r Representative
		if err := rows.Scan(&r.Name, &r.Handle, &r.Code, &r.City, &r.Area, &r.WhatsApp, &r.HandDelivery, &r.Installs); err != nil {
			return nil, database.MapError(err)
		}
		out = append(out, r)
	}
	return out, database.MapError(rows.Err())
}

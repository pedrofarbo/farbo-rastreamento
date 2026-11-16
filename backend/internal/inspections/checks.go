package inspections

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// As conferências que o envio faz (o analista vê no painel e no laudo).
const (
	CheckOK   = "ok"   // conferiu
	CheckWarn = "warn" // olhar com atenção
	CheckInfo = "info" // só para saber
)

// Check é uma conferência da vistoria enviada.
type Check struct {
	Code  string `json:"code"`
	Level string `json:"level"`
	Text  string `json:"text"`
}

const (
	// samePlaceM: as fotos de uma vistoria ficam em volta do veículo; mais
	// longe que isso uma da outra, alguma não foi tirada ali.
	samePlaceM = 300
	// sameSessionMin: uma vistoria leva minutos; mais que isso, as fotos
	// podem ser de dias diferentes.
	sameSessionMin = 60
	// trackerWindow: a posição do rastreador vale se for desse tanto antes ou
	// depois das fotos.
	trackerWindow = 20 * time.Minute
)

// distanceM é a distância em metros entre dois pontos (haversine).
func distanceM(lat1, lon1, lat2, lon2 float64) float64 {
	const earth = 6_371_000.0
	rad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earth * math.Asin(math.Min(1, math.Sqrt(a)))
}

// distanceText: "80 m", "1,2 km", "35 km".
func distanceText(m float64) string {
	switch {
	case m < 1000:
		return fmt.Sprintf("%.0f m", math.Round(m/10)*10)
	case m < 10_000:
		return strings.Replace(fmt.Sprintf("%.1f km", m/1000), ".", ",", 1)
	}
	return fmt.Sprintf("%.0f km", m/1000)
}

// durationText: "4 min", "1 h 20 min", "3 dias".
func durationText(d time.Duration) string {
	mins := int(math.Round(d.Minutes()))
	switch {
	case mins < 60:
		return fmt.Sprintf("%d min", max(mins, 1))
	case mins < 48*60:
		if mins%60 == 0 {
			return fmt.Sprintf("%d h", mins/60)
		}
		return fmt.Sprintf("%d h %d min", mins/60, mins%60)
	}
	return fmt.Sprintf("%d dias", mins/(24*60))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// photoCheck é uma foto vista pela conferência.
type photoCheck struct {
	Photo
	kept bool // aproveitada da vistoria devolvida (tirada antes desta)
}

// placeAndTime confere as fotos tiradas nesta vistoria: com a localização,
// no mesmo lugar e na mesma ocasião, e pela câmera.
func placeAndTime(photos []photoCheck) []Check {
	var out []Check
	var fresh []photoCheck
	kept := 0
	for _, p := range photos {
		if p.kept {
			kept++
			continue
		}
		fresh = append(fresh, p)
	}
	if kept > 0 {
		out = append(out, Check{Code: "kept", Level: CheckInfo,
			Text: plural(kept, "foto aproveitada", "fotos aproveitadas") + " da vistoria devolvida"})
	}
	if len(fresh) == 0 {
		return out
	}

	var located []photoCheck
	for _, p := range fresh {
		if p.Latitude != nil && p.Longitude != nil {
			located = append(located, p)
		}
	}
	if missing := len(fresh) - len(located); missing > 0 {
		out = append(out, Check{Code: "no_location", Level: CheckWarn,
			Text: plural(missing, "foto sem a localização", "fotos sem a localização") + " (o celular não deixou)"})
	}
	if len(located) >= 2 {
		farthest := 0.0
		for i := range located {
			for j := i + 1; j < len(located); j++ {
				farthest = max(farthest, distanceM(*located[i].Latitude, *located[i].Longitude, *located[j].Latitude, *located[j].Longitude))
			}
		}
		if farthest > samePlaceM {
			out = append(out, Check{Code: "spread_place", Level: CheckWarn,
				Text: "As fotos foram tiradas em lugares até " + distanceText(farthest) + " longe um do outro"})
		} else {
			out = append(out, Check{Code: "same_place", Level: CheckOK, Text: "Todas as fotos no mesmo lugar"})
		}
	}

	first, last := fresh[0].TakenAt, fresh[0].TakenAt
	for _, p := range fresh {
		if p.TakenAt.Before(first) {
			first = p.TakenAt
		}
		if p.TakenAt.After(last) {
			last = p.TakenAt
		}
	}
	span := last.Sub(first)
	if span > sameSessionMin*time.Minute {
		out = append(out, Check{Code: "spread_time", Level: CheckWarn, Text: "As fotos foram tiradas ao longo de " + durationText(span)})
	} else if len(fresh) >= 2 {
		out = append(out, Check{Code: "same_time", Level: CheckOK, Text: "Fotos tiradas em " + durationText(span)})
	}

	files := 0
	for _, p := range fresh {
		if p.Source != SourceCamera {
			files++
		}
	}
	if files > 0 {
		out = append(out, Check{Code: "file_source", Level: CheckWarn,
			Text: plural(files, "foto escolhida", "fotos escolhidas") + " entre as do aparelho, e não pela câmera da vistoria: podem não ser de agora"})
	} else {
		out = append(out, Check{Code: "live_camera", Level: CheckOK, Text: "Todas as fotos pela câmera da vistoria, na hora"})
	}
	return out
}

// center é o ponto do meio das fotos com localização (a mediana de cada
// eixo, que ignora uma foto fora) e a hora do meio.
func center(photos []photoCheck) (lat, lon float64, at time.Time, ok bool) {
	var lats, lons []float64
	var times []time.Time
	for _, p := range photos {
		if p.kept || p.Latitude == nil || p.Longitude == nil {
			continue
		}
		lats, lons, times = append(lats, *p.Latitude), append(lons, *p.Longitude), append(times, p.TakenAt)
	}
	if len(lats) == 0 {
		return 0, 0, time.Time{}, false
	}
	slices.Sort(lats)
	slices.Sort(lons)
	slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
	mid := len(lats) / 2
	return lats[mid], lons[mid], times[mid], true
}

// runChecks são as conferências da vistoria no envio.
func (s *Service) runChecks(ctx context.Context, in *Inspection, photos []photoCheck) ([]Check, error) {
	out := placeAndTime(photos)

	// A foto repetida: o mesmo arquivo numa vistoria de fora desta série.
	root := in.ID
	if in.RedoOf != nil {
		root = *in.RedoOf
	}
	var dupSlots []string
	var dupPlate string
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT mine.slot, COALESCE(NULLIF(v.plate, ''), o.plate)
		FROM inspection_photos mine
		JOIN inspection_photos other ON other.sha256 = mine.sha256 AND other.inspection_id <> mine.inspection_id
		JOIN inspections o ON o.id = other.inspection_id
		LEFT JOIN vehicles v ON v.id = o.vehicle_id
		WHERE mine.inspection_id = $1 AND mine.sha256 IS NOT NULL
			AND o.id <> $2 AND (o.redo_of IS NULL OR o.redo_of <> $2)`, in.ID, root)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var slot, plate string
		if err := rows.Scan(&slot, &plate); err != nil {
			rows.Close()
			return nil, database.MapError(err)
		}
		if !slices.Contains(dupSlots, slot) {
			dupSlots = append(dupSlots, slot)
		}
		if dupPlate == "" {
			dupPlate = plate
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}
	if len(dupSlots) > 0 {
		labels := make([]string, len(dupSlots))
		for i, slot := range dupSlots {
			labels[i] = strings.ToLower(catalog[slot].Label)
		}
		text := "Foto igual à de outra vistoria: " + strings.Join(labels, ", ")
		if dupPlate != "" {
			text += " (placa " + dupPlate + ")"
		}
		out = append(out, Check{Code: "duplicate", Level: CheckWarn, Text: text})
	}

	// O rastreador: onde ele estava na hora das fotos.
	if in.VehicleID == nil {
		return out, nil
	}
	lat, lon, at, ok := center(photos)
	if !ok {
		return out, nil
	}
	var device *uuid.UUID
	if err := s.db.QueryRow(ctx, `SELECT device_id FROM vehicles WHERE id = $1`, *in.VehicleID).Scan(&device); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, nil
		}
		return nil, database.MapError(err)
	}
	if device == nil {
		return out, nil
	}
	var pLat, pLon float64
	err = s.db.QueryRow(ctx, `
		SELECT latitude, longitude FROM positions
		WHERE device_id = $1 AND gps_timestamp BETWEEN $2 AND $3
		ORDER BY abs(extract(epoch FROM gps_timestamp - $4::timestamptz)) LIMIT 1`,
		*device, at.Add(-trackerWindow), at.Add(trackerWindow), at).Scan(&pLat, &pLon)
	if errors.Is(err, pgx.ErrNoRows) {
		return append(out, Check{Code: "tracker_silent", Level: CheckInfo,
			Text: "O rastreador não mandou posição na hora das fotos"}), nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	d := distanceM(lat, lon, pLat, pLon)
	if d > samePlaceM {
		return append(out, Check{Code: "tracker_far", Level: CheckWarn,
			Text: "As fotos foram tiradas a " + distanceText(d) + " de onde o rastreador do veículo estava naquela hora"}), nil
	}
	return append(out, Check{Code: "tracker_match", Level: CheckOK,
		Text: "As fotos foram tiradas onde o rastreador do veículo estava naquela hora (" + distanceText(d) + ")"}), nil
}

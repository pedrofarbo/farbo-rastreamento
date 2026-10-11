package reports

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Sheet monta as planilhas dos relatórios — as que a tela baixa e as que vão
// anexas no relatório por e-mail: separadas por ponto e vírgula, com vírgula
// decimal e o BOM, para o Excel em português abrir direto, com acentos.
type Sheet struct {
	// Loc é o fuso das datas e horas.
	Loc *time.Location
	// Groups são os nomes dos grupos da conta; Label1 e Label2, os nomes que
	// a conta deu aos campos personalizados (vazios: a coluna não sai).
	Groups         map[uuid.UUID]string
	Label1, Label2 string
}

// ExtraHeader e ExtraCells: o grupo e os campos personalizados que a conta
// nomeou.
func (s Sheet) ExtraHeader() []string {
	out := []string{"Grupo"}
	if s.Label1 != "" {
		out = append(out, s.Label1)
	}
	if s.Label2 != "" {
		out = append(out, s.Label2)
	}
	return out
}

func (s Sheet) ExtraCells(v Vehicle) []string {
	group := ""
	if v.GroupID != nil {
		group = s.Groups[*v.GroupID]
	}
	out := []string{group}
	if s.Label1 != "" {
		out = append(out, v.Custom1)
	}
	if s.Label2 != "" {
		out = append(out, v.Custom2)
	}
	return out
}

// Summary é a planilha do resumo de cada veículo.
func (s Sheet) Summary(rows []SummaryRow, vehicles []Vehicle) [][]string {
	byID := vehicleIndex(vehicles)
	out := [][]string{append(append([]string{"Veículo", "Placa"}, s.ExtraHeader()...), "Km", "Tempo andando", "Parado ligado",
		"Viagens", "Velocidade máxima (km/h)", "Excessos de velocidade", "Dias com movimento", "Primeira saída", "Última chegada")}
	for _, row := range rows {
		out = append(out, append(append([]string{row.Name, row.Plate}, s.ExtraCells(byID[row.VehicleID])...),
			Decimal(row.DistanceKm, 1), Hours(row.MovingSeconds), Hours(row.IdleSeconds), strconv.Itoa(row.Trips),
			Decimal(row.MaxSpeedKmh, 0), strconv.Itoa(row.Overspeeds), strconv.Itoa(row.ActiveDays),
			s.When(row.FirstMoveAt), s.When(row.LastMoveAt)))
	}
	return out
}

// Overspeeds é a planilha dos excessos de velocidade.
func (s Sheet) Overspeeds(rows []Overspeed, vehicles []Vehicle) [][]string {
	byID := vehicleIndex(vehicles)
	out := [][]string{{"Veículo", "Placa", "Início", "Fim", "Duração", "Velocidade máxima (km/h)", "Limite (km/h)", "Local (lat, lon)"}}
	for _, o := range rows {
		v := byID[o.VehicleID]
		duration, limit, where := "", "", ""
		if o.End != nil {
			duration = MinutesSeconds(o.End.Sub(o.Start))
		}
		if o.LimitKmh != nil {
			limit = Decimal(*o.LimitKmh, 0)
		}
		if o.Latitude != nil && o.Longitude != nil {
			where = Coords(*o.Latitude, *o.Longitude)
		}
		out = append(out, []string{v.Name, v.Plate, s.When(&o.Start), s.When(o.End), duration,
			Decimal(o.MaxSpeedKmh, 0), limit, where})
	}
	return out
}

func vehicleIndex(vehicles []Vehicle) map[uuid.UUID]Vehicle {
	out := make(map[uuid.UUID]Vehicle, len(vehicles))
	for _, v := range vehicles {
		out[v.ID] = v
	}
	return out
}

// When: data e hora no fuso da planilha; Clock, só a hora.
func (s Sheet) When(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(s.Loc).Format("02/01/2006 15:04")
}

func (s Sheet) Clock(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(s.Loc).Format("15:04")
}

// WriteCSV grava a planilha: o BOM, ponto e vírgula e as células protegidas.
func WriteCSV(w io.Writer, rows [][]string) error {
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil { // BOM: o Excel reconhece o UTF-8
		return err
	}
	out := csv.NewWriter(w)
	out.Comma = ';'
	for _, row := range rows {
		if err := out.Write(SafeCells(row)); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// SafeCells evita que um nome de veículo vire fórmula na planilha
// ("=HYPERLINK(...)"): começando por = + - @, ganha um apóstrofo.
func SafeCells(row []string) []string {
	out := make([]string, len(row))
	for i, cell := range row {
		if cell != "" && strings.ContainsRune("=+-@\t\r", rune(cell[0])) && !isNumber(cell) {
			cell = "'" + cell
		}
		out[i] = cell
	}
	return out
}

func isNumber(s string) bool {
	_, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
	return err == nil
}

// Decimal: 12.34 com 1 casa → "12,3".
func Decimal(v float64, places int) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', places, 64), ".", ",", 1)
}

// Hours: 3725 s → "1:02" (horas:minutos).
func Hours(seconds int) string {
	m := seconds / 60
	return fmt.Sprintf("%d:%02d", m/60, m%60)
}

// MinutesSeconds: 95 s → "1:35" (minutos:segundos).
func MinutesSeconds(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func Coords(lat, lon float64) string { return fmt.Sprintf("%.5f, %.5f", lat, lon) }

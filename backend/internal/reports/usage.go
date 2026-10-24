package reports

import (
	"math"
	"strconv"
	"time"
)

// UsageRow é a utilização de um veículo no período: quantos dias ele podia
// rodar (do dia em que entrou no sistema, e até hoje), em quantos andou, e
// quanto do tempo ligado foi parado (a ociosidade: combustível queimado sem
// sair do lugar).
type UsageRow struct {
	SummaryRow
	// PeriodDays: os dias do período em que o veículo já estava no sistema.
	PeriodDays int `json:"periodDays"`
	// StoppedDays: desses, os dias sem nenhum movimento.
	StoppedDays int `json:"stoppedDays"`
	// UsePercent: dias com movimento sobre PeriodDays (0 a 100).
	UsePercent float64 `json:"usePercent"`
	// IdlePercent: o tempo parado ligado sobre o tempo ligado (0 a 100).
	IdlePercent float64 `json:"idlePercent"`
	// KmPerActiveDay: a média de km nos dias em que andou.
	KmPerActiveDay float64 `json:"kmPerActiveDay"`
}

// Usage calcula a utilização a partir do resumo do período [from, to] (dias:
// a meia-noite no fuso dos relatórios); today corta o que ainda não chegou.
func (s *Service) Usage(rows []SummaryRow, vehicles []Vehicle, from, to, today time.Time) []UsageRow {
	byID := vehicleIndex(vehicles)
	if to.After(today) {
		to = today
	}
	out := make([]UsageRow, 0, len(rows))
	for _, r := range rows {
		u := UsageRow{SummaryRow: r}
		start := from
		if v, ok := byID[r.VehicleID]; ok && !v.CreatedAt.IsZero() {
			created := v.CreatedAt.In(s.loc)
			if day := time.Date(created.Year(), created.Month(), created.Day(), 0, 0, 0, 0, s.loc); day.After(start) {
				start = day
			}
		}
		if !start.After(to) {
			u.PeriodDays = int(math.Round(to.Sub(start).Hours()/24)) + 1
		}
		u.StoppedDays = max(u.PeriodDays-r.ActiveDays, 0)
		if u.PeriodDays > 0 {
			u.UsePercent = round1(100 * float64(min(r.ActiveDays, u.PeriodDays)) / float64(u.PeriodDays))
		}
		if on := r.MovingSeconds + r.IdleSeconds; on > 0 {
			u.IdlePercent = round1(100 * float64(r.IdleSeconds) / float64(on))
		}
		if r.ActiveDays > 0 {
			u.KmPerActiveDay = round1(r.DistanceKm / float64(r.ActiveDays))
		}
		out = append(out, u)
	}
	return out
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// Usage é a planilha da utilização de cada veículo.
func (s Sheet) Usage(rows []UsageRow, vehicles []Vehicle) [][]string {
	byID := vehicleIndex(vehicles)
	out := [][]string{append(append([]string{"Veículo", "Placa"}, s.ExtraHeader()...), "Dias no período", "Dias com movimento",
		"Dias parado", "Uso (%)", "Km", "Km por dia com movimento", "Tempo andando", "Parado ligado", "Ociosidade (%)",
		"Última chegada")}
	for _, row := range rows {
		out = append(out, append(append([]string{row.Name, row.Plate}, s.ExtraCells(byID[row.VehicleID])...),
			strconv.Itoa(row.PeriodDays), strconv.Itoa(row.ActiveDays), strconv.Itoa(row.StoppedDays),
			Decimal(row.UsePercent, 1), Decimal(row.DistanceKm, 1), Decimal(row.KmPerActiveDay, 1),
			Hours(row.MovingSeconds), Hours(row.IdleSeconds), Decimal(row.IdlePercent, 1), s.When(row.LastMoveAt)))
	}
	return out
}

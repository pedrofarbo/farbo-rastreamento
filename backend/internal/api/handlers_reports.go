package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/reports"
)

// Os relatórios da frota: o resumo de cada veículo no período, os km de cada
// dia, as viagens de um veículo e os excessos de velocidade — em JSON ou, com
// ?formato=csv, na planilha que o Excel abre direto.
//
// O cliente vê os da conta dele (titular e equipe); a central, os de um
// cliente (?cliente=). O período vai de ?de= a ?ate= (AAAA-MM-DD, no fuso da
// central), até reports.MaxDays dias; sem eles, os últimos 7 dias.

type reportQuery struct {
	vehicles []reports.Vehicle
	from, to time.Time // dias (meia-noite no fuso da central)
	csv      bool
}

// reportScope lê a conta, o período e o veículo (?veiculo=, opcional) do pedido.
func (s *Server) reportScope(w http.ResponseWriter, r *http.Request) (reportQuery, bool) {
	if s.Reports == nil {
		writeError(w, http.StatusNotFound, "relatórios indisponíveis")
		return reportQuery{}, false
	}
	owner, isCustomer := customerOf(r)
	if !isCustomer {
		id, err := uuid.Parse(r.URL.Query().Get("cliente"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "informe o cliente (?cliente=)")
			return reportQuery{}, false
		}
		owner = id
	}
	loc := s.Reports.Location()
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	parse := func(name string, fallback time.Time) (time.Time, error) {
		raw := r.URL.Query().Get(name)
		if raw == "" {
			return fallback, nil
		}
		return time.ParseInLocation(time.DateOnly, raw, loc)
	}
	to, err := parse("ate", today)
	if err != nil {
		writeError(w, http.StatusBadRequest, "data final inválida (use AAAA-MM-DD)")
		return reportQuery{}, false
	}
	from, err := parse("de", to.AddDate(0, 0, -6))
	if err != nil {
		writeError(w, http.StatusBadRequest, "data inicial inválida (use AAAA-MM-DD)")
		return reportQuery{}, false
	}
	if to.Before(from) {
		writeError(w, http.StatusBadRequest, "o início do período precisa ser antes do fim")
		return reportQuery{}, false
	}
	if to.Sub(from) >= reports.MaxDays*24*time.Hour {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("o período vai até %d dias", reports.MaxDays))
		return reportQuery{}, false
	}
	list, err := s.Reports.Vehicles(r.Context(), owner)
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return reportQuery{}, false
	}
	if raw := r.URL.Query().Get("veiculo"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "veículo inválido")
			return reportQuery{}, false
		}
		i := slices.IndexFunc(list, func(v reports.Vehicle) bool { return v.ID == id })
		if i < 0 {
			writeError(w, http.StatusNotFound, "veículo não encontrado")
			return reportQuery{}, false
		}
		list = list[i : i+1]
	}
	return reportQuery{vehicles: list, from: from, to: to, csv: r.URL.Query().Get("formato") == "csv"}, true
}

func (s *Server) handleReportSummary(w http.ResponseWriter, r *http.Request) {
	q, ok := s.reportScope(w, r)
	if !ok {
		return
	}
	rows, err := s.Reports.Summary(r.Context(), q.vehicles, q.from, q.to)
	if err != nil {
		handleStoreError(w, err, "relatório não encontrado")
		return
	}
	if q.csv {
		out := [][]string{{"Veículo", "Placa", "Km", "Tempo andando", "Parado ligado", "Viagens", "Velocidade máxima (km/h)",
			"Excessos de velocidade", "Dias com movimento", "Primeira saída", "Última chegada"}}
		for _, row := range rows {
			out = append(out, []string{row.Name, row.Plate, decimal(row.DistanceKm, 1), hours(row.MovingSeconds),
				hours(row.IdleSeconds), strconv.Itoa(row.Trips), decimal(row.MaxSpeedKmh, 0), strconv.Itoa(row.Overspeeds),
				strconv.Itoa(row.ActiveDays), s.when(row.FirstMoveAt), s.when(row.LastMoveAt)})
		}
		writeCSV(w, "resumo", q, out)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": day(q.from), "to": day(q.to), "rows": rows})
}

func (s *Server) handleReportDaily(w http.ResponseWriter, r *http.Request) {
	q, ok := s.reportScope(w, r)
	if !ok {
		return
	}
	rows, err := s.Reports.Daily(r.Context(), q.vehicles, q.from, q.to)
	if err != nil {
		handleStoreError(w, err, "relatório não encontrado")
		return
	}
	if q.csv {
		names := map[uuid.UUID]reports.Vehicle{}
		for _, v := range q.vehicles {
			names[v.ID] = v
		}
		out := [][]string{{"Veículo", "Placa", "Dia", "Km", "Tempo andando", "Parado ligado", "Viagens",
			"Velocidade máxima (km/h)", "Excessos de velocidade", "Primeira saída", "Última chegada"}}
		for _, row := range rows {
			v := names[row.VehicleID]
			d, _ := time.Parse(time.DateOnly, row.Day)
			out = append(out, []string{v.Name, v.Plate, d.Format("02/01/2006"), decimal(row.DistanceKm, 1),
				hours(row.MovingSeconds), hours(row.IdleSeconds), strconv.Itoa(row.Trips), decimal(row.MaxSpeedKmh, 0),
				strconv.Itoa(row.Overspeeds), s.clock(row.FirstMoveAt), s.clock(row.LastMoveAt)})
		}
		writeCSV(w, "km-por-dia", q, out)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": day(q.from), "to": day(q.to), "vehicles": q.vehicles, "rows": rows})
}

func (s *Server) handleReportTrips(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("veiculo") == "" {
		writeError(w, http.StatusBadRequest, "escolha o veículo (?veiculo=)")
		return
	}
	q, ok := s.reportScope(w, r)
	if !ok {
		return
	}
	v := q.vehicles[0]
	from, to := q.from, q.to.AddDate(0, 0, 1)
	// As viagens vêm das posições: só até onde o histórico do veículo guarda.
	if vehicle, err := s.Vehicles.Get(r.Context(), v.ID); err == nil {
		from = s.clampToRetention(r.Context(), vehicle, from)
	}
	trips := []reports.Trip{}
	if from.Before(to) {
		var err error
		if trips, err = s.Reports.Trips(r.Context(), v, from, to); err != nil {
			handleStoreError(w, err, "relatório não encontrado")
			return
		}
	}
	if q.csv {
		out := [][]string{{"Veículo", "Placa", "Saída", "Chegada", "Duração", "Km", "Velocidade máxima (km/h)",
			"Saída (lat, lon)", "Chegada (lat, lon)"}}
		for _, t := range trips {
			arrival := s.when(&t.End)
			if t.Ongoing {
				arrival = "andando"
			}
			out = append(out, []string{v.Name, v.Plate, s.when(&t.Start), arrival, hours(t.Seconds()),
				decimal(t.DistanceM/1000, 1), decimal(t.MaxSpeed, 0), coords(t.StartLat, t.StartLon), coords(t.EndLat, t.EndLon)})
		}
		writeCSV(w, "viagens", q, out)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": day(q.from), "to": day(q.to), "vehicle": v, "trips": trips,
		// Desde quando há posições (a retenção do histórico).
		"availableFrom": from,
	})
}

func (s *Server) handleReportOverspeed(w http.ResponseWriter, r *http.Request) {
	q, ok := s.reportScope(w, r)
	if !ok {
		return
	}
	rows, err := s.Reports.Overspeeds(r.Context(), q.vehicles, q.from, q.to.AddDate(0, 0, 1))
	if err != nil {
		handleStoreError(w, err, "relatório não encontrado")
		return
	}
	if q.csv {
		names := map[uuid.UUID]reports.Vehicle{}
		for _, v := range q.vehicles {
			names[v.ID] = v
		}
		out := [][]string{{"Veículo", "Placa", "Início", "Fim", "Duração", "Velocidade máxima (km/h)", "Limite (km/h)", "Local (lat, lon)"}}
		for _, o := range rows {
			v := names[o.VehicleID]
			duration, limit, where := "", "", ""
			if o.End != nil {
				duration = minutesSeconds(o.End.Sub(o.Start))
			}
			if o.LimitKmh != nil {
				limit = decimal(*o.LimitKmh, 0)
			}
			if o.Latitude != nil && o.Longitude != nil {
				where = coords(*o.Latitude, *o.Longitude)
			}
			out = append(out, []string{v.Name, v.Plate, s.when(&o.Start), s.when(o.End), duration,
				decimal(o.MaxSpeedKmh, 0), limit, where})
		}
		writeCSV(w, "excessos-de-velocidade", q, out)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": day(q.from), "to": day(q.to), "vehicles": q.vehicles, "rows": rows,
		"truncated": len(rows) >= reports.MaxOverspeeds})
}

// ---------------------------------------------------------------------------
// A planilha: separada por ponto e vírgula, vírgula decimal e com o BOM, para
// o Excel em português abrir direto, com acentos.
// ---------------------------------------------------------------------------

func writeCSV(w http.ResponseWriter, name string, q reportQuery, rows [][]string) {
	filename := fmt.Sprintf("farbo-%s-%s-a-%s.csv", name, day(q.from), day(q.to))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM: o Excel reconhece o UTF-8
	out := csv.NewWriter(w)
	out.Comma = ';'
	for _, row := range rows {
		_ = out.Write(safeCells(row))
	}
	out.Flush()
}

// safeCells evita que um nome de veículo vire fórmula na planilha
// ("=HYPERLINK(...)"): começando por = + - @, ganha um apóstrofo.
func safeCells(row []string) []string {
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

func day(t time.Time) string { return t.Format(time.DateOnly) }

func decimal(v float64, places int) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', places, 64), ".", ",", 1)
}

// hours: 3725 s → "1:02" (horas:minutos).
func hours(seconds int) string {
	m := seconds / 60
	return fmt.Sprintf("%d:%02d", m/60, m%60)
}

// minutesSeconds: 95 s → "1:35" (minutos:segundos).
func minutesSeconds(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func coords(lat, lon float64) string { return fmt.Sprintf("%.5f, %.5f", lat, lon) }

// when: data e hora no fuso da central; clock, só a hora.
func (s *Server) when(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(s.Reports.Location()).Format("02/01/2006 15:04")
}

func (s *Server) clock(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(s.Reports.Location()).Format("15:04")
}

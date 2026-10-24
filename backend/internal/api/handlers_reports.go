package api

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/reports"
)

// Os relatórios da frota: o resumo de cada veículo no período, os km de cada
// dia, as viagens de um veículo e os excessos de velocidade — em JSON ou, com
// ?formato=csv, na planilha que o Excel abre direto.
//
// O cliente vê os da conta dele (titular e equipe); a central, os de um
// cliente (?cliente=); a associação ou seguradora, os dos associados (um só,
// com ?associado=). O período vai de ?de= a ?ate= (AAAA-MM-DD, no fuso da
// central), até reports.MaxDays dias; sem eles, os últimos 7 dias.

type reportQuery struct {
	// account: a conta do cliente (vazia na organização).
	account  uuid.UUID
	vehicles []reports.Vehicle
	from, to time.Time // dias (meia-noite no fuso da central)
	csv      bool
	// sheet monta as planilhas: o fuso, os nomes dos grupos e dos campos
	// personalizados da conta (as colunas a mais).
	sheet reports.Sheet
}

// reportScope lê a conta, o período e o veículo (?veiculo=, opcional) do pedido.
func (s *Server) reportScope(w http.ResponseWriter, r *http.Request) (reportQuery, bool) {
	if s.Reports == nil {
		writeError(w, http.StatusNotFound, "relatórios indisponíveis")
		return reportQuery{}, false
	}
	owner, isCustomer := customerOf(r)
	orgID, isOrg := orgOf(r)
	if !isCustomer && !isOrg {
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
	var list []reports.Vehicle
	if isOrg {
		list, err = s.Reports.OrganizationVehicles(r.Context(), orgID)
	} else {
		list, err = s.Reports.Vehicles(r.Context(), owner)
	}
	if err != nil {
		handleStoreError(w, err, "veículos não encontrados")
		return reportQuery{}, false
	}
	// ?associado=: só os veículos de um associado (a organização).
	if raw := r.URL.Query().Get("associado"); raw != "" && isOrg {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "associado inválido")
			return reportQuery{}, false
		}
		list = slices.DeleteFunc(list, func(v reports.Vehicle) bool { return v.OwnerID == nil || *v.OwnerID != id })
	}
	// O corretor: só os veículos dos associados dele.
	if broker := brokerOf(r); broker != nil && isOrg {
		mine, err := s.Organizations.Associates(r.Context(), orgID, broker)
		if err != nil {
			handleStoreError(w, err, "associados não encontrados")
			return reportQuery{}, false
		}
		ids := map[uuid.UUID]bool{}
		for _, a := range mine {
			ids[a.ID] = true
		}
		list = slices.DeleteFunc(list, func(v reports.Vehicle) bool { return v.OwnerID == nil || !ids[*v.OwnerID] })
	}
	// Quem da equipe está restrito a grupos vê só os veículos deles.
	if principal := restrictedPrincipal(r); principal != nil {
		list = slices.DeleteFunc(list, func(v reports.Vehicle) bool { return !principal.SeesGroup(v.GroupID) })
	}
	// ?grupo=: só os veículos de um grupo da conta.
	if raw := r.URL.Query().Get("grupo"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "grupo inválido")
			return reportQuery{}, false
		}
		kept := list[:0:0]
		for _, v := range list {
			if v.GroupID != nil && *v.GroupID == id {
				kept = append(kept, v)
			}
		}
		list = kept
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
	if isOrg {
		owner = uuid.Nil
	}
	q := reportQuery{account: owner, vehicles: list, from: from, to: to, csv: r.URL.Query().Get("formato") == "csv",
		sheet: reports.Sheet{Loc: loc, Groups: map[uuid.UUID]string{}, Owners: isOrg}}
	if q.csv && !isOrg {
		groups, err := s.Vehicles.Groups(r.Context(), owner)
		if err != nil {
			handleStoreError(w, err, "veículos não encontrados")
			return reportQuery{}, false
		}
		for _, g := range groups {
			q.sheet.Groups[g.ID] = g.Name
		}
		labels, err := s.Vehicles.Labels(r.Context(), owner)
		if err != nil {
			handleStoreError(w, err, "veículos não encontrados")
			return reportQuery{}, false
		}
		q.sheet.Label1, q.sheet.Label2 = labels.Label1, labels.Label2
	}
	return q, true
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
		writeCSV(w, "resumo", q, q.sheet.Summary(rows, q.vehicles))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": day(q.from), "to": day(q.to), "rows": rows})
}

// handleReportUsage: a utilização e a ociosidade de cada veículo (dias
// parados, o tempo ligado sem andar).
func (s *Server) handleReportUsage(w http.ResponseWriter, r *http.Request) {
	q, ok := s.reportScope(w, r)
	if !ok {
		return
	}
	summary, err := s.Reports.Summary(r.Context(), q.vehicles, q.from, q.to)
	if err != nil {
		handleStoreError(w, err, "relatório não encontrado")
		return
	}
	loc := s.Reports.Location()
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	rows := s.Reports.Usage(summary, q.vehicles, q.from, q.to, today)
	if q.csv {
		writeCSV(w, "utilizacao", q, q.sheet.Usage(rows, q.vehicles))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": day(q.from), "to": day(q.to), "rows": rows})
}

// handleReportDriving: a nota de direção e os dados de uso de cada veículo
// (na associação ou seguradora, por associado; a planilha é o que vai para a
// análise de risco).
func (s *Server) handleReportDriving(w http.ResponseWriter, r *http.Request) {
	q, ok := s.reportScope(w, r)
	if !ok {
		return
	}
	rows, err := s.Reports.Driving(r.Context(), q.vehicles, q.from, q.to)
	if err != nil {
		handleStoreError(w, err, "relatório não encontrado")
		return
	}
	if q.csv {
		writeCSV(w, "direcao", q, q.sheet.Driving(rows, q.vehicles))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": day(q.from), "to": day(q.to), "rows": rows,
		"minScoreKm": reports.MinScoreKm})
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
		out := [][]string{append(append([]string{"Veículo", "Placa"}, q.sheet.ExtraHeader()...), "Dia", "Km", "Tempo andando",
			"Parado ligado", "Viagens", "Velocidade máxima (km/h)", "Excessos de velocidade", "Primeira saída", "Última chegada")}
		for _, row := range rows {
			v := names[row.VehicleID]
			d, _ := time.Parse(time.DateOnly, row.Day)
			out = append(out, append(append([]string{v.Name, v.Plate}, q.sheet.ExtraCells(v)...), d.Format("02/01/2006"),
				reports.Decimal(row.DistanceKm, 1), reports.Hours(row.MovingSeconds), reports.Hours(row.IdleSeconds),
				strconv.Itoa(row.Trips), reports.Decimal(row.MaxSpeedKmh, 0), strconv.Itoa(row.Overspeeds),
				q.sheet.Clock(row.FirstMoveAt), q.sheet.Clock(row.LastMoveAt)))
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
			arrival := q.sheet.When(&t.End)
			if t.Ongoing {
				arrival = "andando"
			}
			out = append(out, []string{v.Name, v.Plate, q.sheet.When(&t.Start), arrival, reports.Hours(t.Seconds()),
				reports.Decimal(t.DistanceM/1000, 1), reports.Decimal(t.MaxSpeed, 0),
				reports.Coords(t.StartLat, t.StartLon), reports.Coords(t.EndLat, t.EndLon)})
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
		out := q.sheet.Overspeeds(rows, q.vehicles)
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
	_ = reports.WriteCSV(w, rows)
}

func day(t time.Time) string { return t.Format(time.DateOnly) }

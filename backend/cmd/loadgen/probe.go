package main

import (
	"bufio"
	"context"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// sample é o retrato de um instante: os contadores do servidor (/metrics),
// da máquina (/proc/stat), do próprio loadgen e do banco. O relatório mostra
// a diferença entre dois retratos.
type sample struct {
	at time.Time

	serverCPU   float64 // segundos de CPU do processo do servidor
	serverRSS   float64 // bytes
	ingest      map[float64]float64
	ingestCount float64
	serverOK    bool

	hostBusy, hostTotal float64 // jiffies da máquina inteira
	selfCPU             float64 // jiffies do loadgen

	commits, inserted, updated, deleted, blksRead, blksHit float64
	active                                                 float64
	dbOK                                                   bool
}

type prober struct {
	metricsURL string
	db         *pgxpool.Pool
	client     *http.Client
}

func (p *prober) take(ctx context.Context) sample {
	s := sample{at: time.Now()}
	if p.metricsURL != "" {
		p.scrape(ctx, &s)
	}
	s.hostBusy, s.hostTotal = hostJiffies()
	s.selfCPU = selfJiffies()
	if p.db != nil {
		err := p.db.QueryRow(ctx, `
			SELECT xact_commit + xact_rollback, tup_inserted, tup_updated, tup_deleted, blks_read, blks_hit,
				(SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND state = 'active' AND pid <> pg_backend_pid())
			FROM pg_stat_database WHERE datname = current_database()`).
			Scan(&s.commits, &s.inserted, &s.updated, &s.deleted, &s.blksRead, &s.blksHit, &s.active)
		s.dbOK = err == nil
	}
	return s
}

// scrape lê do /metrics a CPU e a memória do servidor e o histograma do
// tempo de cada posição.
func (p *prober) scrape(ctx context.Context, s *sample) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.metricsURL, nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	s.ingest = map[float64]float64{}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		name, value, ok := metricLine(line)
		if !ok {
			continue
		}
		switch {
		case name == "process_cpu_seconds_total":
			s.serverCPU = value
		case name == "process_resident_memory_bytes":
			s.serverRSS = value
		case name == "tracker_position_ingest_seconds_count":
			s.ingestCount = value
		case strings.HasPrefix(name, `tracker_position_ingest_seconds_bucket{le="`):
			le := strings.TrimSuffix(strings.TrimPrefix(name, `tracker_position_ingest_seconds_bucket{le="`), `"}`)
			if bound, err := strconv.ParseFloat(le, 64); err == nil {
				s.ingest[bound] = value
			} else if le == "+Inf" {
				s.ingest[math.Inf(1)] = value
			}
		}
	}
	s.serverOK = sc.Err() == nil
}

func metricLine(line string) (string, float64, bool) {
	if line == "" || line[0] == '#' {
		return "", 0, false
	}
	i := strings.LastIndexByte(line, ' ')
	if i < 0 {
		return "", 0, false
	}
	v, err := strconv.ParseFloat(line[i+1:], 64)
	return line[:i], v, err == nil
}

// hostJiffies é a CPU da máquina inteira (todos os núcleos) desde o boot.
func hostJiffies() (busy, total float64) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, 0
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0
	}
	for i, field := range fields[1:] {
		v, _ := strconv.ParseFloat(field, 64)
		total += v
		// idle e iowait são o tempo parado.
		if i != 3 && i != 4 {
			busy += v
		}
	}
	return busy, total
}

// selfJiffies é a CPU do próprio loadgen (utime + stime).
func selfJiffies() float64 {
	raw, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}
	// O nome do processo vem entre parênteses e pode ter espaços.
	rest := string(raw)
	if i := strings.LastIndexByte(rest, ')'); i >= 0 {
		rest = rest[i+1:]
	}
	fields := strings.Fields(rest)
	if len(fields) < 13 {
		return 0
	}
	utime, _ := strconv.ParseFloat(fields[11], 64)
	stime, _ := strconv.ParseFloat(fields[12], 64)
	return utime + stime
}

// clockTicks é o relógio do /proc (USER_HZ), 100 no Linux.
const clockTicks = 100

// diff resume dois retratos no relatório.
func diff(prev, cur sample, line map[string]any) {
	secs := cur.at.Sub(prev.at).Seconds()
	if secs <= 0 {
		return
	}
	if prev.serverOK && cur.serverOK {
		line["serverCpuPct"] = round1((cur.serverCPU - prev.serverCPU) / secs * 100)
		line["serverRssMB"] = math.Round(cur.serverRSS / (1 << 20))
		if n := cur.ingestCount - prev.ingestCount; n > 0 {
			line["ingestP50ms"] = histogramQuantile(prev.ingest, cur.ingest, n, 0.5)
			line["ingestP99ms"] = histogramQuantile(prev.ingest, cur.ingest, n, 0.99)
		}
	}
	if total := cur.hostTotal - prev.hostTotal; total > 0 {
		// Em núcleos: 1,0 é um núcleo inteiro ocupado.
		line["hostCores"] = round1((cur.hostBusy - prev.hostBusy) / total * float64(cores()))
		line["loadgenCores"] = round1((cur.selfCPU - prev.selfCPU) / clockTicks / secs)
	}
	if prev.dbOK && cur.dbOK {
		line["dbCommitsPerSec"] = math.Round((cur.commits - prev.commits) / secs)
		line["dbInsertsPerSec"] = math.Round((cur.inserted - prev.inserted) / secs)
		line["dbUpdatesPerSec"] = math.Round((cur.updated - prev.updated) / secs)
		line["dbDeletesPerSec"] = math.Round((cur.deleted - prev.deleted) / secs)
		line["dbDiskReadsPerSec"] = math.Round((cur.blksRead - prev.blksRead) / secs)
		line["dbActive"] = cur.active
	}
}

// histogramQuantile devolve o limite (em ms) do primeiro balde que alcança o
// quantil, entre dois retratos do histograma.
func histogramQuantile(prev, cur map[float64]float64, n, q float64) float64 {
	bounds := make([]float64, 0, len(cur))
	for b := range cur {
		bounds = append(bounds, b)
	}
	sort.Float64s(bounds)
	for _, b := range bounds {
		if cur[b]-prev[b] >= q*n {
			if math.IsInf(b, 1) {
				return -1
			}
			return b * 1000
		}
	}
	return -1
}

func cores() int {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 1
	}
	n := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9' {
			n++
		}
	}
	return max(n, 1)
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

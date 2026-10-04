package infra

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Service junta tudo o que o painel de infraestrutura mostra.
type Service struct {
	db        *database.DB
	monitor   *Monitor
	capture   *Capture
	backupDir string
	redisPing func(context.Context) error
	started   time.Time
	now       func() time.Time
}

func NewService(db *database.DB, monitor *Monitor, capture *Capture, backupDir string) *Service {
	return &Service{db: db, monitor: monitor, capture: capture, backupDir: backupDir, started: time.Now(), now: time.Now}
}

// SetRedis liga a verificação do Redis (nulo: Redis desligado).
func (s *Service) SetRedis(ping func(context.Context) error) { s.redisPing = ping }

// Status é o painel.
type Status struct {
	Host     Sample      `json:"host"`
	History  []Point     `json:"history"`
	Process  Process     `json:"process"`
	Database Database    `json:"database"`
	Redis    Check       `json:"redis"`
	Backups  Backups     `json:"backups"`
	Logs     LogCounts   `json:"logs"`
	Live     LiveCounter `json:"live"`
}

// Process é o servidor da API.
type Process struct {
	StartedAt  time.Time `json:"startedAt"`
	GoVersion  string    `json:"goVersion"`
	Goroutines int       `json:"goroutines"`
	HeapBytes  uint64    `json:"heapBytes"`
	// RSS: memória que o processo ocupa de fato na máquina.
	RSSBytes uint64 `json:"rssBytes"`
}

// Check é um serviço que responde ou não.
type Check struct {
	Enabled   bool    `json:"enabled"`
	OK        bool    `json:"ok"`
	LatencyMs float64 `json:"latencyMs"`
	Error     string  `json:"error,omitempty"`
}

// Database é o Postgres.
type Database struct {
	Check
	SizeBytes      int64   `json:"sizeBytes"`
	Connections    int     `json:"connections"`
	MaxConnections int     `json:"maxConnections"`
	Tables         []Table `json:"tables"`
}

// Table é uma das maiores tabelas.
type Table struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	Rows      int64  `json:"rows"`
}

// Backups são as cópias diárias do banco.
type Backups struct {
	Available  bool    `json:"available"`
	Count      int     `json:"count"`
	TotalBytes int64   `json:"totalBytes"`
	Latest     *Backup `json:"latest"`
	// Offsite é a cópia fora da VPS, como o postgres-backup a deixou em
	// offsite-status.json (nil antes da primeira volta do script).
	Offsite *Offsite `json:"offsite"`
}

// Offsite: o último envio da cópia criptografada para fora da VPS.
type Offsite struct {
	Configured bool      `json:"configured"`
	OK         bool      `json:"ok"`
	At         time.Time `json:"at"`
	File       string    `json:"file"`
	Error      string    `json:"error"`
}

// offsiteStatusFile é escrito pelo deploy/postgres-backup.sh.
const offsiteStatusFile = "offsite-status.json"

// Backup é um arquivo de cópia.
type Backup struct {
	Name      string    `json:"name"`
	At        time.Time `json:"at"`
	SizeBytes int64     `json:"sizeBytes"`
}

// LogCounts: avisos e erros das últimas 24 horas, e os que se perderam.
type LogCounts struct {
	Errors24h   int   `json:"errors24h"`
	Warnings24h int   `json:"warnings24h"`
	Dropped     int64 `json:"dropped"`
	Failed      int64 `json:"failed"`
}

// LiveCounter é preenchido por quem chama (conexões abertas no momento).
type LiveCounter struct {
	TrackerConnections int `json:"trackerConnections"`
	RealtimeClients    int `json:"realtimeClients"`
	OnlineDevices      int `json:"onlineDevices"`
}

// Status lê tudo agora.
func (s *Service) Status(ctx context.Context) *Status {
	out := &Status{
		Host:     s.monitor.Current(),
		History:  s.monitor.History(),
		Process:  s.process(),
		Database: s.database(ctx),
		Redis:    s.redis(ctx),
		Backups:  s.backups(),
	}
	out.Logs = s.logCounts(ctx)
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM devices WHERE status = 'ONLINE'`).Scan(&out.Live.OnlineDevices); err != nil {
		out.Live.OnlineDevices = -1
	}
	return out
}

func (s *Service) process() Process {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return Process{
		StartedAt: s.started, GoVersion: runtime.Version(), Goroutines: runtime.NumGoroutine(),
		HeapBytes: mem.HeapAlloc, RSSBytes: selfRSS(),
	}
}

// selfRSS lê o VmRSS do próprio processo.
func selfRSS() uint64 {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "VmRSS:" {
			v, _ := strconv.ParseUint(fields[1], 10, 64)
			return v * 1024
		}
	}
	return 0
}

func (s *Service) database(ctx context.Context) Database {
	out := Database{Check: Check{Enabled: true}}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	start := time.Now()
	if err := s.db.Ping(ctx); err != nil {
		out.Error = err.Error()
		return out
	}
	out.OK, out.LatencyMs = true, ms(time.Since(start))
	_ = s.db.QueryRow(ctx, `
		SELECT pg_database_size(current_database()),
			(SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()),
			current_setting('max_connections')::int`).Scan(&out.SizeBytes, &out.Connections, &out.MaxConnections)
	rows, err := s.db.Query(ctx, `
		SELECT c.relname, pg_total_relation_size(c.oid), GREATEST(c.reltuples, 0)::bigint
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r' AND n.nspname = current_schema()
		ORDER BY 2 DESC LIMIT 6`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Name, &t.SizeBytes, &t.Rows); err == nil {
			out.Tables = append(out.Tables, t)
		}
	}
	return out
}

func (s *Service) redis(ctx context.Context) Check {
	if s.redisPing == nil {
		return Check{}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := s.redisPing(ctx); err != nil {
		return Check{Enabled: true, Error: err.Error()}
	}
	return Check{Enabled: true, OK: true, LatencyMs: ms(time.Since(start))}
}

// backups lê a pasta das cópias do banco (montada só para leitura).
func (s *Service) backups() Backups {
	if s.backupDir == "" {
		return Backups{}
	}
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		return Backups{}
	}
	out := Backups{Available: true}
	var files []Backup
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".dump") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, Backup{Name: e.Name(), At: info.ModTime(), SizeBytes: info.Size()})
		out.TotalBytes += info.Size()
	}
	out.Count = len(files)
	sort.Slice(files, func(i, j int) bool { return files[i].At.After(files[j].At) })
	if len(files) > 0 {
		latest := files[0]
		latest.Name = filepath.Base(latest.Name)
		out.Latest = &latest
	}
	if body, err := os.ReadFile(filepath.Join(s.backupDir, offsiteStatusFile)); err == nil {
		var offsite Offsite
		if json.Unmarshal(body, &offsite) == nil {
			out.Offsite = &offsite
		}
	}
	return out
}

func (s *Service) logCounts(ctx context.Context) LogCounts {
	out := LogCounts{}
	if s.capture != nil {
		out.Dropped, out.Failed = s.capture.Dropped(), s.capture.Failed()
	}
	_ = s.db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE level = 'ERROR'), count(*) FILTER (WHERE level = 'WARN')
		FROM system_logs WHERE logged_at > $1`, s.now().Add(-24*time.Hour)).Scan(&out.Errors24h, &out.Warnings24h)
	return out
}

// ---------------------------------------------------------------------------
// Logs
// ---------------------------------------------------------------------------

// LogQuery filtra os registros.
type LogQuery struct {
	Hours  int    // período (1..720)
	Level  string // "", "ERROR" ou "WARN"
	Search string
	Limit  int
}

// LogGroup é uma mensagem repetida: quantas vezes e a última.
type LogGroup struct {
	Level     string    `json:"level"`
	Component string    `json:"component"`
	Message   string    `json:"message"`
	Count     int       `json:"count"`
	LastAt    time.Time `json:"lastAt"`
}

// LogPage é a resposta dos logs: os mais frequentes e os mais recentes.
type LogPage struct {
	Groups  []LogGroup `json:"groups"`
	Entries []Entry    `json:"entries"`
}

// Logs devolve os avisos e erros do período, agrupados e um a um.
func (s *Service) Logs(ctx context.Context, q LogQuery) (*LogPage, error) {
	if q.Hours < 1 || q.Hours > 720 {
		q.Hours = 24
	}
	if q.Limit < 1 || q.Limit > 500 {
		q.Limit = 100
	}
	level := strings.ToUpper(strings.TrimSpace(q.Level))
	if level != "ERROR" && level != "WARN" {
		level = ""
	}
	since := s.now().Add(-time.Duration(q.Hours) * time.Hour)
	search := strings.TrimSpace(q.Search)
	where := `logged_at > $1 AND ($2 = '' OR level = $2)
		AND ($3 = '' OR message ILIKE '%' || $3 || '%' OR component ILIKE '%' || $3 || '%' OR attrs::text ILIKE '%' || $3 || '%')`

	out := &LogPage{Groups: []LogGroup{}, Entries: []Entry{}}
	rows, err := s.db.Query(ctx, `
		SELECT level, component, message, count(*), max(logged_at)
		FROM system_logs WHERE `+where+`
		GROUP BY level, component, message
		ORDER BY count(*) DESC, max(logged_at) DESC LIMIT 15`, since, level, search)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var g LogGroup
		if err := rows.Scan(&g.Level, &g.Component, &g.Message, &g.Count, &g.LastAt); err != nil {
			rows.Close()
			return nil, err
		}
		out.Groups = append(out.Groups, g)
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `
		SELECT id, logged_at, level, component, message, attrs
		FROM system_logs WHERE `+where+`
		ORDER BY logged_at DESC, id DESC LIMIT $4`, since, level, search, q.Limit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var e Entry
		var attrs []byte
		if err := rows.Scan(&e.ID, &e.At, &e.Level, &e.Component, &e.Message, &attrs); err != nil {
			return nil, err
		}
		e.Attrs = map[string]any{}
		_ = json.Unmarshal(attrs, &e.Attrs)
		out.Entries = append(out.Entries, e)
	}
	return out, rows.Err()
}

// Cleanup apaga os registros mais velhos que LogRetention.
func (s *Service) Cleanup(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM system_logs WHERE logged_at < $1`, s.now().Add(-LogRetention))
	if err != nil {
		return 0, database.MapError(err)
	}
	return tag.RowsAffected(), nil
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

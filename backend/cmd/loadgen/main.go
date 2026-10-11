// Command loadgen é o teste de carga: simula muitos rastreadores GT06 (o
// protocolo dos J16) mandando posição no intervalo configurado e, se pedido,
// painéis abertos no WebSocket recebendo tudo em tempo real.
//
// Os IMEIs são prefixo + número sequencial (000001, 000002, ...) e precisam
// estar cadastrados no servidor, senão a conexão é recusada. Com -db, o
// -setup cadastra (e gera o histórico de -history-days) e o -teardown apaga.
//
//	go run ./cmd/loadgen -db "$DSN" -setup -devices 1000 -history-days 35
//	go run ./cmd/loadgen -host 127.0.0.1 -port 5000 -devices 5000 -interval 10s -duration 10m
//
// Com -api e -ws, mede também o atraso de ponta a ponta: do envio da posição
// à chegada no painel. Com -api, a CPU e a memória do servidor e o tempo de
// cada posição lá dentro (/metrics); com -db, o ritmo do banco.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols/gt06"
)

type counters struct {
	connected    atomic.Int64
	loggedIn     atomic.Int64
	sent         atomic.Int64
	sendErrors   atomic.Int64
	dialErrors   atomic.Int64
	loginErrors  atomic.Int64
	reconnects   atomic.Int64
	wsMessages   atomic.Int64
	wsClients    atomic.Int64
	loginLatency latencies

	// sentAt é o último envio de posição de cada IMEI; deviceIMEI liga o id
	// do rastreador (no tempo real) ao IMEI. e2e é o atraso até o painel: a
	// janela do relatório e o total.
	sentAt     sync.Map
	deviceIMEI map[string]string
	e2eWindow  latencies
	e2eAll     latencies
}

// latencies guarda amostras para percentis.
type latencies struct {
	mu      sync.Mutex
	samples []time.Duration
}

func (l *latencies) add(d time.Duration) {
	l.mu.Lock()
	l.samples = append(l.samples, d)
	l.mu.Unlock()
}

// drain devolve os percentis da janela e a esvazia.
func (l *latencies) drain() (p50, p99, worst time.Duration, n int) {
	l.mu.Lock()
	samples := l.samples
	l.samples = nil
	l.mu.Unlock()
	if len(samples) == 0 {
		return 0, 0, 0, 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	at := func(p float64) time.Duration {
		return samples[int(math.Min(float64(len(samples)-1), p*float64(len(samples))))]
	}
	return at(0.5), at(0.99), samples[len(samples)-1], len(samples)
}

func (l *latencies) percentile(p float64) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.samples) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), l.samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[int(math.Min(float64(len(sorted)-1), p*float64(len(sorted))))]
}

func main() {
	host := flag.String("host", "127.0.0.1", "servidor TCP dos rastreadores")
	port := flag.Int("port", 5000, "porta TCP dos rastreadores")
	devices := flag.Int("devices", 5000, "quantos rastreadores simular")
	prefix := flag.String("imei-prefix", "869", "prefixo dos IMEIs (completados até 15 dígitos)")
	interval := flag.Duration("interval", 10*time.Second, "intervalo entre posições de cada rastreador")
	heartbeat := flag.Duration("heartbeat", 3*time.Minute, "intervalo do heartbeat")
	ramp := flag.Duration("ramp", time.Minute, "tempo para conectar todos (0 = todos de uma vez)")
	duration := flag.Duration("duration", 10*time.Minute, "duração total, contando a rampa")
	apiURL := flag.String("api", "", "URL do painel (ex.: http://127.0.0.1:8080) para os clientes WebSocket")
	wsClients := flag.Int("ws", 0, "quantos painéis abertos simular no WebSocket")
	email := flag.String("email", "", "login de quem abre o painel")
	password := flag.String("password", "", "senha de quem abre o painel")
	origin := flag.String("origin", "http://localhost:5173", "Origin aceito pelo WebSocket")
	dsn := flag.String("db", "", "banco do servidor (postgres://...): -setup/-teardown e o ritmo do banco no relatório")
	doSetup := flag.Bool("setup", false, "cadastra os rastreadores de teste (um veículo cada) e sai")
	historyDays := flag.Int("history-days", 0, "com -setup, dias de histórico gerados (ritmo do J16: 30 s andando, 1 h parado)")
	owner := flag.String("owner", "", "com -setup, id do cliente dono dos veículos (vazio: da central)")
	doTeardown := flag.Bool("teardown", false, "apaga os rastreadores de teste, os veículos e o histórico deles, e sai")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var db *pgxpool.Pool
	if *dsn != "" {
		var err error
		if db, err = pgxpool.New(ctx, *dsn); err != nil {
			fmt.Fprintln(os.Stderr, "banco:", err)
			os.Exit(1)
		}
		defer db.Close()
	}
	if *doSetup || *doTeardown {
		if db == nil {
			fmt.Fprintln(os.Stderr, "-setup e -teardown precisam de -db")
			os.Exit(2)
		}
		var err error
		if *doSetup {
			err = setup(ctx, db, *prefix, *devices, *owner, *historyDays)
		} else {
			err = teardown(ctx, db, *prefix)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := context.WithTimeout(ctx, *duration)
	defer stop()

	c := &counters{}
	start := time.Now()
	var wg sync.WaitGroup

	probe := &prober{db: db, client: &http.Client{Timeout: 5 * time.Second}}
	if *apiURL != "" {
		probe.metricsURL = strings.TrimRight(*apiURL, "/") + "/metrics"
	}

	if *wsClients > 0 {
		token, err := login(*apiURL, *email, *password)
		if err != nil {
			fmt.Fprintln(os.Stderr, "login do painel:", err)
			os.Exit(1)
		}
		if c.deviceIMEI, err = deviceIMEIs(ctx, db, *prefix, *apiURL, token); err != nil {
			fmt.Fprintln(os.Stderr, "rastreadores do painel:", err)
			os.Exit(1)
		}
		for i := 0; i < *wsClients; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				runPanel(ctx, *apiURL, token, *origin, c)
			}()
		}
	}

	addr := net.JoinHostPort(*host, fmt.Sprint(*port))
	for i := 1; i <= *devices; i++ {
		imei := *prefix + fmt.Sprintf("%0*d", 15-len(*prefix), i)
		delay := time.Duration(0)
		if *ramp > 0 {
			delay = time.Duration(float64(*ramp) * float64(i-1) / float64(*devices))
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			runDevice(ctx, addr, imei, *interval, *heartbeat, c)
		}()
	}

	// Um resumo por intervalo, em JSON por linha (fácil de juntar depois).
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	var lastSent, lastWS int64
	lastAt := time.Now()
	first := probe.take(ctx)
	lastProbe := first
	report := func(final bool) {
		now := time.Now()
		sent, ws := c.sent.Load(), c.wsMessages.Load()
		secs := now.Sub(lastAt).Seconds()
		line := map[string]any{
			"t": math.Round(now.Sub(start).Seconds()), "connected": c.connected.Load(), "loggedIn": c.loggedIn.Load(),
			"sentPerSec": math.Round(float64(sent-lastSent) / secs), "sentTotal": sent,
			"sendErrors": c.sendErrors.Load(), "dialErrors": c.dialErrors.Load(), "loginErrors": c.loginErrors.Load(),
			"reconnects": c.reconnects.Load(), "wsClients": c.wsClients.Load(),
			"wsMsgPerSec": math.Round(float64(ws-lastWS) / secs),
			"loginP50ms":  c.loginLatency.percentile(0.5).Milliseconds(),
			"loginP99ms":  c.loginLatency.percentile(0.99).Milliseconds(),
		}
		cur := probe.take(context.Background())
		if final {
			// O resumo do teste inteiro: a média de cada medida e o atraso
			// de todas as posições.
			line["final"] = true
			diff(first, cur, line)
			if c.e2eAll.count() > 0 {
				line["e2eP50ms"] = c.e2eAll.percentile(0.5).Milliseconds()
				line["e2eP99ms"] = c.e2eAll.percentile(0.99).Milliseconds()
				line["e2eSamples"] = c.e2eAll.count()
			}
		} else {
			diff(lastProbe, cur, line)
			if p50, p99, worst, n := c.e2eWindow.drain(); n > 0 {
				line["e2eP50ms"], line["e2eP99ms"], line["e2eMaxMs"] = p50.Milliseconds(), p99.Milliseconds(), worst.Milliseconds()
			}
		}
		lastProbe = cur
		out, _ := json.Marshal(line)
		fmt.Println(string(out))
		lastSent, lastWS, lastAt = sent, ws, now
	}
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			report(true)
			return
		case <-ticker.C:
			report(false)
		}
	}
}

// runDevice mantém um rastreador conectado, reconectando se cair.
func runDevice(ctx context.Context, addr, imei string, interval, heartbeat time.Duration, c *counters) {
	backoff := time.Second
	first := true
	for ctx.Err() == nil {
		if !first {
			c.reconnects.Add(1)
		}
		first = false
		err := session(ctx, addr, imei, interval, heartbeat, c)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff + time.Duration(rand.Int64N(int64(time.Second)))):
			}
			backoff = min(backoff*2, 30*time.Second)
		}
	}
}

func session(ctx context.Context, addr, imei string, interval, heartbeat time.Duration, c *counters) error {
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		c.dialErrors.Add(1)
		return err
	}
	defer conn.Close()
	c.connected.Add(1)
	defer c.connected.Add(-1)

	var serial uint16 = 1
	next := func() uint16 { serial++; return serial }
	// Prazo renovado a cada envio: um prazo antigo derrubaria um heartbeat
	// que saísse logo depois dele.
	write := func(frame []byte) error {
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, err := conn.Write(frame)
		return err
	}

	frame, err := gt06.BuildLoginFrame(imei, next())
	if err != nil {
		return err
	}
	began := time.Now()
	if err := write(frame); err != nil {
		c.sendErrors.Add(1)
		return err
	}
	// O servidor confirma o login; sem confirmação, o IMEI foi recusado.
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	reader := bufio.NewReader(conn)
	if _, err := reader.Peek(10); err != nil {
		c.loginErrors.Add(1)
		return fmt.Errorf("login sem resposta: %w", err)
	}
	c.loginLatency.add(time.Since(began))
	c.loggedIn.Add(1)
	defer c.loggedIn.Add(-1)
	_ = conn.SetReadDeadline(time.Time{})

	// Esvazia o que o servidor mandar (confirmações, comandos).
	go func() { _, _ = io.Copy(io.Discard, reader) }()

	// Cada rastreador começa num ponto aleatório da Grande São Paulo e anda.
	lat := -23.55 + (rand.Float64()-0.5)*0.4
	lon := -46.63 + (rand.Float64()-0.5)*0.4
	course := rand.Float64() * 360
	status := gt06.DeviceStatus{ACC: true, BatteryLevel: 6, GSMLevel: 4}

	// Fase aleatória: os envios se espalham pelo intervalo, como na vida real.
	positionTimer := time.NewTimer(time.Duration(rand.Int64N(int64(interval))))
	defer positionTimer.Stop()
	heartbeatTicker := time.NewTicker(heartbeat)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-heartbeatTicker.C:
			if err := write(gt06.BuildHeartbeatFrame(status, next())); err != nil {
				c.sendErrors.Add(1)
				return err
			}
		case <-positionTimer.C:
			speed := 20 + rand.Float64()*60
			course = math.Mod(course+(rand.Float64()-0.5)*30+360, 360)
			step := speed / 3600 * interval.Seconds() / 111
			lat += step * math.Cos(course*math.Pi/180)
			lon += step * math.Sin(course*math.Pi/180)
			fix := gt06.Fix{Time: time.Now(), Latitude: lat, Longitude: lon, SpeedKmh: speed,
				CourseDeg: course, Satellites: 9, Valid: true}
			c.sentAt.Store(imei, time.Now())
			if err := write(gt06.BuildPositionFrame(fix, status, next())); err != nil {
				c.sendErrors.Add(1)
				return err
			}
			c.sent.Add(1)
			positionTimer.Reset(interval)
		}
	}
}

func login(api, email, password string) (string, error) {
	body := strings.NewReader(fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
	resp, err := http.Post(strings.TrimRight(api, "/")+"/api/auth/login", "application/json", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("login recusado (%d)", resp.StatusCode)
	}
	return out.AccessToken, nil
}

// runPanel é um painel aberto: recebe todas as posições pelo WebSocket.
func runPanel(ctx context.Context, api, token, origin string, c *counters) {
	u, _ := url.Parse(strings.TrimRight(api, "/"))
	u.Scheme = strings.Replace(u.Scheme, "http", "ws", 1)
	u.Path = "/ws"
	u.RawQuery = "token=" + url.QueryEscape(token)
	for ctx.Err() == nil {
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), http.Header{"Origin": {origin}})
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		c.wsClients.Add(1)
		go func() {
			<-ctx.Done()
			conn.Close()
		}()
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				break
			}
			c.wsMessages.Add(1)
			c.observe(payload)
		}
		c.wsClients.Add(-1)
		conn.Close()
	}
}

func (l *latencies) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.samples)
}

// observe mede o atraso de uma posição que chegou ao painel: do último envio
// daquele rastreador até agora.
func (c *counters) observe(payload []byte) {
	var msg struct {
		Type     string `json:"type"`
		DeviceID string `json:"deviceId"`
	}
	if json.Unmarshal(payload, &msg) != nil || msg.Type != "position.updated" {
		return
	}
	imei, ok := c.deviceIMEI[msg.DeviceID]
	if !ok {
		return
	}
	if at, ok := c.sentAt.Load(imei); ok {
		d := time.Since(at.(time.Time))
		c.e2eWindow.add(d)
		c.e2eAll.add(d)
	}
}

// deviceIMEIs liga o id de cada rastreador ao IMEI (o tempo real fala em id):
// pelo banco, com -db; senão, pela lista da equipe (o painel de um cliente
// não vê os IMEIs).
func deviceIMEIs(ctx context.Context, db *pgxpool.Pool, prefix, api, token string) (map[string]string, error) {
	if db != nil {
		rows, err := db.Query(ctx, testDevices, prefix)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[string]string{}
		for rows.Next() {
			var id, imei string
			if err := rows.Scan(&id, &imei); err != nil {
				return nil, err
			}
			out[id] = imei
		}
		return out, rows.Err()
	}
	req, _ := http.NewRequest(http.MethodGet, strings.TrimRight(api, "/")+"/api/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lista recusada (%d)", resp.StatusCode)
	}
	var list []struct {
		ID   string `json:"id"`
		IMEI string `json:"imei"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(list))
	for _, d := range list {
		out[d.ID] = d.IMEI
	}
	return out, nil
}

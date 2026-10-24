// Package webhooks manda os eventos dos veículos para o sistema da empresa
// (ou da seguradora): cada evento gravado vira um POST JSON, assinado com o
// segredo do webhook (HMAC-SHA256 em X-Farbo-Signature), para os endereços
// da conta dona do veículo — e da organização, no veículo de um associado.
// Só HTTPS e nunca para a rede interna (endereço privado, local ou de
// loopback). Falhas seguidas desligam o webhook.
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/events"
)

const (
	// MaxWebhooks por conta.
	MaxWebhooks = 5
	// MaxFailures seguidas desligam o webhook.
	MaxFailures = 20
	queueSize   = 2000
	timeout     = 10 * time.Second
)

// InputError é um pedido recusado; a mensagem vai para a tela.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

func invalid(format string, args ...any) error { return &InputError{fmt.Sprintf(format, args...)} }

// Webhook é um endereço que recebe os eventos.
type Webhook struct {
	ID         uuid.UUID  `json:"id"`
	URL        string     `json:"url"`
	Events     []string   `json:"events"`
	Active     bool       `json:"active"`
	LastStatus *int       `json:"lastStatus"`
	LastError  string     `json:"lastError"`
	LastSentAt *time.Time `json:"lastSentAt"`
	Failures   int        `json:"failures"`
	CreatedAt  time.Time  `json:"createdAt"`
	// Secret só vem na criação.
	Secret string `json:"secret,omitempty"`
}

// Input é o webhook pedido.
type Input struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
	Active *bool    `json:"active"`
}

// Payload é o que vai no corpo do POST.
type Payload struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Timestamp   time.Time      `json:"timestamp"`
	VehicleID   string         `json:"vehicleId"`
	VehicleName string         `json:"vehicleName"`
	Plate       string         `json:"plate"`
	Latitude    *float64       `json:"latitude"`
	Longitude   *float64       `json:"longitude"`
	SpeedKmh    *float64       `json:"speedKmh"`
	Metadata    map[string]any `json:"metadata"`
}

type delivery struct {
	hook    target
	payload []byte
	event   string
}

type target struct {
	id     uuid.UUID
	url    string
	secret string
}

type Service struct {
	db     *database.DB
	client *http.Client
	queue  chan *events.Event
	log    *slog.Logger
	// allowPrivate deixa mandar para a rede local (testes).
	allowPrivate bool
}

func NewService(db *database.DB, log *slog.Logger) *Service {
	s := &Service{db: db, queue: make(chan *events.Event, queueSize), log: log.With("component", "webhooks")}
	s.client = s.newClient()
	return s
}

// AllowPrivate deixa mandar para endereços locais e HTTP (só nos testes).
func (s *Service) AllowPrivate() {
	s.allowPrivate = true
	s.client = s.newClient()
}

// newClient é o cliente que recusa conectar na rede interna (a checagem é
// no IP já resolvido, na hora de conectar: o DNS não engana).
func (s *Service) newClient() *http.Client {
	dialer := &net.Dialer{Timeout: timeout, Control: func(_, address string, _ syscall.RawConn) error {
		if s.allowPrivate {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || blocked(ip) {
			return fmt.Errorf("endereço não permitido: %s", host)
		}
		return nil
	}}
	transport := &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: timeout,
		ResponseHeaderTimeout: timeout, MaxIdleConnsPerHost: 2, Proxy: nil}
	return &http.Client{Timeout: timeout, Transport: transport,
		// Não segue redirecionamento (seria um jeito de chegar na rede interna).
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// blocked: os endereços da rede interna, de loopback, locais e os especiais.
func blocked(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() ||
		ip.Equal(net.ParseIP("169.254.169.254")) ||
		(ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xC0 == 64) // 100.64.0.0/10 (CGNAT)
}

func newSecret() string {
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	return "whsec_" + hex.EncodeToString(raw)
}

// checkURL: HTTPS (HTTP só nos testes), sem usuário e senha, até 500 caracteres.
func (s *Service) checkURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || len(raw) > 500 || u.User != nil {
		return "", invalid("endereço inválido: use https://seu-sistema/caminho")
	}
	if u.Scheme != "https" && !(s.allowPrivate && u.Scheme == "http") {
		return "", invalid("o endereço precisa ser HTTPS")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && blocked(ip) && !s.allowPrivate {
		return "", invalid("o endereço não pode ser da rede interna")
	}
	if strings.EqualFold(u.Hostname(), "localhost") && !s.allowPrivate {
		return "", invalid("o endereço não pode ser da rede interna")
	}
	return u.String(), nil
}

func checkEvents(list []string) ([]string, error) {
	out := []string{}
	for _, e := range list {
		e = strings.ToUpper(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if len(e) > 40 || strings.ContainsFunc(e, func(r rune) bool { return !(r == '_' || (r >= 'A' && r <= 'Z')) }) {
			return nil, invalid("tipo de evento inválido: %s", e)
		}
		if !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out, nil
}

const selectWebhook = `SELECT id, url, events, active, last_status, last_error, last_sent_at, failures, created_at FROM webhooks`

func scan(row database.Scanner) (*Webhook, error) {
	var w Webhook
	if err := row.Scan(&w.ID, &w.URL, &w.Events, &w.Active, &w.LastStatus, &w.LastError, &w.LastSentAt, &w.Failures,
		&w.CreatedAt); err != nil {
		return nil, database.MapError(err)
	}
	return &w, nil
}

// List são os webhooks da conta.
func (s *Service) List(ctx context.Context, owner uuid.UUID) ([]*Webhook, error) {
	rows, err := s.db.Query(ctx, selectWebhook+` WHERE owner_id = $1 ORDER BY created_at`, owner)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []*Webhook{}
	for rows.Next() {
		w, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, database.MapError(rows.Err())
}

// Create cadastra o webhook e devolve o segredo da assinatura (só agora).
func (s *Service) Create(ctx context.Context, owner, by uuid.UUID, in Input) (*Webhook, error) {
	address, err := s.checkURL(in.URL)
	if err != nil {
		return nil, err
	}
	kinds, err := checkEvents(in.Events)
	if err != nil {
		return nil, err
	}
	var count int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM webhooks WHERE owner_id = $1`, owner).Scan(&count); err != nil {
		return nil, database.MapError(err)
	}
	if count >= MaxWebhooks {
		return nil, invalid("são até %d webhooks por conta", MaxWebhooks)
	}
	secret := newSecret()
	w, err := scan(s.db.QueryRow(ctx, `
		INSERT INTO webhooks (owner_id, url, secret, events, created_by) VALUES ($1, $2, $3, $4, $5)
		RETURNING id, url, events, active, last_status, last_error, last_sent_at, failures, created_at`,
		owner, address, secret, kinds, by))
	if err != nil {
		return nil, err
	}
	w.Secret = secret
	return w, nil
}

// Update troca o endereço, os eventos e liga ou desliga (ligar zera as falhas).
func (s *Service) Update(ctx context.Context, owner, id uuid.UUID, in Input) (*Webhook, error) {
	address, err := s.checkURL(in.URL)
	if err != nil {
		return nil, err
	}
	kinds, err := checkEvents(in.Events)
	if err != nil {
		return nil, err
	}
	return scan(s.db.QueryRow(ctx, `
		UPDATE webhooks SET url = $3, events = $4, active = COALESCE($5, active),
			failures = CASE WHEN COALESCE($5, active) AND NOT active THEN 0 ELSE failures END
		WHERE id = $1 AND owner_id = $2
		RETURNING id, url, events, active, last_status, last_error, last_sent_at, failures, created_at`,
		id, owner, address, kinds, in.Active))
}

// Delete apaga o webhook da conta.
func (s *Service) Delete(ctx context.Context, owner, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM webhooks WHERE id = $1 AND owner_id = $2`, id, owner)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// Test manda um evento PING agora e devolve como foi.
func (s *Service) Test(ctx context.Context, owner, id uuid.UUID) (*Webhook, error) {
	var t target
	if err := s.db.QueryRow(ctx, `SELECT id, url, secret FROM webhooks WHERE id = $1 AND owner_id = $2`, id, owner).
		Scan(&t.id, &t.url, &t.secret); err != nil {
		return nil, database.MapError(err)
	}
	body, _ := json.Marshal(Payload{ID: "ping-" + uuid.NewString(), Type: "PING", Timestamp: time.Now().UTC(),
		Metadata: map[string]any{"message": "Teste do webhook da Farbo Rastreadores"}})
	s.send(ctx, delivery{hook: t, payload: body, event: "PING"})
	return scan(s.db.QueryRow(ctx, selectWebhook+` WHERE id = $1`, id))
}

// OnEvent põe o evento na fila (não bloqueia a gravação dos eventos).
func (s *Service) OnEvent(ev *events.Event) {
	select {
	case s.queue <- ev:
	default:
		s.log.Warn("fila dos webhooks cheia: evento descartado", "type", ev.Type, "device", ev.DeviceID)
	}
}

// Run entrega os eventos da fila.
func (s *Service) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-s.queue:
			s.Dispatch(ctx, ev)
		}
	}
}

// Dispatch manda o evento aos webhooks ativos da conta dona do veículo e da
// organização dela.
func (s *Service) Dispatch(ctx context.Context, ev *events.Event) {
	var vehicleID uuid.UUID
	var name, plate string
	var owner, org *uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT v.id, v.name, COALESCE(v.plate, ''), v.owner_id, u.organization_id FROM vehicles v
		LEFT JOIN users u ON u.id = v.owner_id WHERE v.device_id = $1`, ev.DeviceID).Scan(&vehicleID, &name, &plate, &owner, &org)
	if err != nil || owner == nil {
		return
	}
	owners := []uuid.UUID{*owner}
	if org != nil {
		owners = append(owners, *org)
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, url, secret FROM webhooks
		WHERE owner_id = ANY($1) AND active AND (cardinality(events) = 0 OR $2 = ANY(events))`, owners, ev.Type)
	if err != nil {
		s.log.Error("falha ao listar os webhooks", "err", err)
		return
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.url, &t.secret); err != nil {
			rows.Close()
			return
		}
		targets = append(targets, t)
	}
	rows.Close()
	if len(targets) == 0 {
		return
	}
	body, _ := json.Marshal(Payload{
		ID: strconv.FormatInt(ev.ID, 10), Type: ev.Type, Timestamp: ev.Timestamp, VehicleID: vehicleID.String(),
		VehicleName: name, Plate: plate, Latitude: ev.Latitude, Longitude: ev.Longitude, SpeedKmh: ev.SpeedKmh,
		Metadata: ev.Metadata,
	})
	for _, t := range targets {
		s.send(ctx, delivery{hook: t, payload: body, event: ev.Type})
	}
}

// Sign é a assinatura do corpo: hex(HMAC-SHA256(segredo, "<timestamp>.<corpo>")).
func Sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// send faz o POST e guarda o resultado (2xx zera as falhas; MaxFailures
// seguidas desligam o webhook).
func (s *Service) send(ctx context.Context, d delivery) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	status, errText := 0, ""
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.hook.url, bytes.NewReader(d.payload))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Farbo-Webhooks/1")
		req.Header.Set("X-Farbo-Event", d.event)
		req.Header.Set("X-Farbo-Timestamp", timestamp)
		req.Header.Set("X-Farbo-Signature", Sign(d.hook.secret, timestamp, d.payload))
		var res *http.Response
		res, err = s.client.Do(req)
		if err == nil {
			status = res.StatusCode
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
			res.Body.Close()
			if status < 200 || status > 299 {
				errText = "resposta " + strconv.Itoa(status)
			}
		}
	}
	if err != nil {
		errText = err.Error()
		var opErr *net.OpError
		if errors.As(err, &opErr) && strings.Contains(opErr.Error(), "endereço não permitido") {
			errText = "o endereço aponta para a rede interna"
		}
		if len(errText) > 200 {
			errText = errText[:200]
		}
	}
	ok := errText == ""
	if _, err := s.db.Exec(context.Background(), `
		UPDATE webhooks SET last_status = $2, last_error = $3, last_sent_at = NOW(),
			failures = CASE WHEN $4 THEN 0 ELSE failures + 1 END,
			active = CASE WHEN NOT $4 AND failures + 1 >= $5 THEN FALSE ELSE active END
		WHERE id = $1`, d.hook.id, status, errText, ok, MaxFailures); err != nil {
		s.log.Error("falha ao guardar a entrega do webhook", "webhook", d.hook.id, "err", err)
	}
}

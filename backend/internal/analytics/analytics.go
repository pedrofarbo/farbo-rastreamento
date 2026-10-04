// Package analytics conta as visitas da landing page sem cookies e sem dados
// pessoais. Cada visitante vira um código — o hash do IP e do navegador com
// um sal que muda todo dia —, o IP não é gravado e o sal do dia anterior é
// apagado: ninguém refaz o código nem segue a pessoa de um dia para o outro.
// Por isso "visitantes" de um período é a soma dos visitantes únicos de cada
// dia.
package analytics

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// RetentionDays: as visitas ficam guardadas por pouco mais de um ano (dá para
// comparar com o mesmo mês do ano anterior).
const RetentionDays = 400

// Events são os eventos que a landing manda (o resto é ignorado).
var Events = map[string]bool{
	"pageview":        true, // abriu a landing
	"section_view":    true, // chegou a uma seção (label = id da seção)
	"cta_click":       true, // clicou num botão (label = qual)
	"outbound_click":  true, // saiu por um link (Instagram, e-mail)
	"lead_open":       true, // abriu o pré-cadastro (label = plano)
	"lead_submit":     true, // mandou o pré-cadastro
	"waitlist_submit": true, // entrou na lista de lançamento
	"installers_open": true, // abriu os prestadores
}

const (
	maxLabel = 80
	maxPath  = 200
	maxUTM   = 100
)

// Hit é o que a landing manda a cada visita ou evento.
type Hit struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Path        string `json:"path"`
	Referrer    string `json:"referrer"`
	UTMSource   string `json:"utmSource"`
	UTMMedium   string `json:"utmMedium"`
	UTMCampaign string `json:"utmCampaign"`
	// ScreenWidth desempata o aparelho quando o navegador não diz (iPad).
	ScreenWidth int `json:"screenWidth"`
}

// bots: robôs de busca, prévias de link, monitores e navegadores sem tela.
// "bot" só como palavra ou com versão (Googlebot/2.1): celular Cubot é gente.
var bots = regexp.MustCompile(`(?i)\bbot\b|bot/|bot;|crawl|spider|slurp|headless|lighthouse|pagespeed|preview|facebookexternalhit|` +
	`curl|wget|python|go-http|java/|monitor|uptime|phantom|selenium|playwright`)

type Service struct {
	db  *database.DB
	loc *time.Location
	log *slog.Logger
	now func() time.Time

	mu      sync.Mutex
	saltDay string
	salt    []byte
}

func NewService(db *database.DB, log *slog.Logger) *Service {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.FixedZone("BRT", -3*60*60)
	}
	return &Service{db: db, loc: loc, log: log.With("component", "analytics"), now: time.Now}
}

// SetClock troca o relógio (testes).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Record grava uma visita ou um evento. Robôs, eventos desconhecidos e
// pedidos sem IP ou navegador são ignorados sem erro: a landing não precisa
// saber. ownHost é o domínio do próprio site (a origem interna não conta).
func (s *Service) Record(ctx context.Context, hit Hit, ip, userAgent, ownHost string) error {
	if !Events[hit.Name] || ip == "" || strings.TrimSpace(userAgent) == "" || bots.MatchString(userAgent) {
		return nil
	}
	day := s.now().In(s.loc).Format(time.DateOnly)
	salt, err := s.saltFor(ctx, day)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(string(salt) + "\x00" + ip + "\x00" + userAgent))
	visitor := hex.EncodeToString(sum[:12])

	// A origem e a campanha são da chegada à página.
	var referrer, source, medium, campaign string
	if hit.Name == "pageview" {
		referrer = ReferrerHost(hit.Referrer, ownHost)
		source = clip(strings.ToLower(strings.TrimSpace(hit.UTMSource)), maxUTM)
		medium = clip(strings.ToLower(strings.TrimSpace(hit.UTMMedium)), maxUTM)
		campaign = clip(strings.ToLower(strings.TrimSpace(hit.UTMCampaign)), maxUTM)
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO analytics_events (day, visitor, name, label, path, referrer_host, utm_source, utm_medium,
			utm_campaign, device, browser, os)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		day, visitor, hit.Name, clip(strings.TrimSpace(hit.Label), maxLabel), clip(hit.Path, maxPath),
		referrer, source, medium, campaign, Device(userAgent, hit.ScreenWidth), Browser(userAgent), OS(userAgent))
	return database.MapError(err)
}

// saltFor devolve o sal do dia, criando-o na primeira visita. O de ontem é
// apagado aqui: a partir de então, ninguém refaz os códigos antigos.
func (s *Service) saltFor(ctx context.Context, day string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saltDay == day {
		return s.salt, nil
	}
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	// Outra instância pode ter criado o do dia: vale o que estiver gravado.
	var salt []byte
	err := s.db.QueryRow(ctx, `
		WITH created AS (
			INSERT INTO analytics_salts (day, salt) VALUES ($1, $2) ON CONFLICT (day) DO NOTHING RETURNING salt
		)
		SELECT salt FROM created
		UNION ALL
		SELECT salt FROM analytics_salts WHERE day = $1
		LIMIT 1`, day, fresh).Scan(&salt)
	if err != nil {
		return nil, database.MapError(err)
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM analytics_salts WHERE day < $1`, day); err != nil {
		s.log.Warn("falha ao apagar o sal de ontem", "err", err)
	}
	s.saltDay, s.salt = day, salt
	return salt, nil
}

// Cleanup apaga as visitas mais antigas que RetentionDays e os sais
// passados.
func (s *Service) Cleanup(ctx context.Context) (int64, error) {
	today := s.now().In(s.loc).Format(time.DateOnly)
	tag, err := s.db.Exec(ctx, `DELETE FROM analytics_events WHERE day < $1::date - $2::int`, today, RetentionDays)
	if err != nil {
		return 0, database.MapError(err)
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM analytics_salts WHERE day < $1`, today); err != nil {
		return 0, database.MapError(err)
	}
	return tag.RowsAffected(), nil
}

// ---------------------------------------------------------------------------
// Resumo para o painel
// ---------------------------------------------------------------------------

// Count é uma linha de um ranking: quantos visitantes (únicos por dia) e
// quantas vezes.
type Count struct {
	Key      string `json:"key"`
	Visitors int    `json:"visitors"`
	Count    int    `json:"count"`
}

// Day é um dia do gráfico.
type Day struct {
	Day       string `json:"day"`
	Visitors  int    `json:"visitors"`
	Pageviews int    `json:"pageviews"`
}

// Summary é o painel das visitas de um período.
type Summary struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Visitors  int    `json:"visitors"`
	Pageviews int    `json:"pageviews"`
	// ActiveNow: visitantes nos últimos 10 minutos.
	ActiveNow int `json:"activeNow"`
	// Conversões: quantos visitantes fizeram cada coisa.
	LeadOpens      int `json:"leadOpens"`
	Leads          int `json:"leads"`
	Waitlist       int `json:"waitlist"`
	InstallerOpens int `json:"installerOpens"`

	Days      []Day   `json:"days"`
	Referrers []Count `json:"referrers"`
	Campaigns []Count `json:"campaigns"`
	Devices   []Count `json:"devices"`
	Browsers  []Count `json:"browsers"`
	Systems   []Count `json:"systems"`
	Sections  []Count `json:"sections"`
	Clicks    []Count `json:"clicks"`
}

// Summary resume os últimos days dias (hoje incluso).
func (s *Service) Summary(ctx context.Context, days int) (*Summary, error) {
	if days < 1 || days > RetentionDays {
		days = 30
	}
	today := s.now().In(s.loc)
	from := today.AddDate(0, 0, -(days - 1)).Format(time.DateOnly)
	to := today.Format(time.DateOnly)
	out := &Summary{From: from, To: to}

	err := s.db.QueryRow(ctx, `
		SELECT
			count(DISTINCT (day, visitor)) FILTER (WHERE name = 'pageview'),
			count(*) FILTER (WHERE name = 'pageview'),
			count(DISTINCT (day, visitor)) FILTER (WHERE name = 'lead_open'),
			count(DISTINCT (day, visitor)) FILTER (WHERE name = 'lead_submit'),
			count(DISTINCT (day, visitor)) FILTER (WHERE name = 'waitlist_submit'),
			count(DISTINCT (day, visitor)) FILTER (WHERE name = 'installers_open')
		FROM analytics_events WHERE day BETWEEN $1 AND $2`, from, to).
		Scan(&out.Visitors, &out.Pageviews, &out.LeadOpens, &out.Leads, &out.Waitlist, &out.InstallerOpens)
	if err != nil {
		return nil, database.MapError(err)
	}
	if err := s.db.QueryRow(ctx, `
		SELECT count(DISTINCT visitor) FROM analytics_events WHERE occurred_at > NOW() - interval '10 minutes'`).
		Scan(&out.ActiveNow); err != nil {
		return nil, database.MapError(err)
	}

	rows, err := s.db.Query(ctx, `
		SELECT d::date::text, count(DISTINCT e.visitor), count(e.id)
		FROM generate_series($1::date, $2::date, interval '1 day') d
		LEFT JOIN analytics_events e ON e.day = d::date AND e.name = 'pageview'
		GROUP BY d ORDER BY d`, from, to)
	if err != nil {
		return nil, database.MapError(err)
	}
	for rows.Next() {
		var d Day
		if err := rows.Scan(&d.Day, &d.Visitors, &d.Pageviews); err != nil {
			rows.Close()
			return nil, err
		}
		out.Days = append(out.Days, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Os rankings: a expressão da chave é fixa, daqui (nunca do pedido).
	for _, r := range []struct {
		dst   *[]Count
		key   string
		where string
		limit int
	}{
		{&out.Referrers, `referrer_host`, `name = 'pageview'`, 10},
		{&out.Campaigns, `concat_ws(' · ', utm_source, NULLIF(utm_campaign, ''))`, `name = 'pageview' AND utm_source <> ''`, 10},
		{&out.Devices, `device`, `name = 'pageview'`, 5},
		{&out.Browsers, `browser`, `name = 'pageview'`, 8},
		{&out.Systems, `os`, `name = 'pageview'`, 8},
		{&out.Sections, `label`, `name = 'section_view'`, 20},
		{&out.Clicks, `label`, `name IN ('cta_click', 'outbound_click')`, 20},
	} {
		list, err := s.counts(ctx, r.key, r.where, r.limit, from, to)
		if err != nil {
			return nil, err
		}
		*r.dst = list
	}
	return out, nil
}

func (s *Service) counts(ctx context.Context, key, where string, limit int, from, to string) ([]Count, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+key+` AS key, count(DISTINCT (day, visitor)) AS visitors, count(*)
		FROM analytics_events
		WHERE day BETWEEN $1 AND $2 AND `+where+`
		GROUP BY 1 ORDER BY 2 DESC, 3 DESC, 1
		LIMIT $3`, from, to, limit)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := []Count{}
	for rows.Next() {
		var c Count
		if err := rows.Scan(&c.Key, &c.Visitors, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// O navegador e a origem
// ---------------------------------------------------------------------------

// ReferrerHost é o domínio de fora de onde a pessoa veio ("" se direto ou do
// próprio site), sem "www." nem porta.
func ReferrerHost(raw, ownHost string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := bareHost(u.Host)
	own := bareHost(ownHost)
	if host == "" || host == own || strings.HasSuffix(host, "."+own) || strings.HasSuffix(own, "."+host) {
		return ""
	}
	return clip(host, maxLabel)
}

func bareHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	return strings.TrimPrefix(h, "www.")
}

// Device: celular, tablet ou computador.
func Device(ua string, screenWidth int) string {
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "ipad") || strings.Contains(l, "tablet") ||
		(strings.Contains(l, "android") && !strings.Contains(l, "mobile")):
		return "tablet"
	case strings.Contains(l, "mobi") || strings.Contains(l, "iphone") || strings.Contains(l, "android"):
		return "mobile"
	case screenWidth > 0 && screenWidth < 768:
		return "mobile"
	}
	return "desktop"
}

// Browser: o navegador, incluindo os de dentro do Instagram e do Facebook.
func Browser(ua string) string {
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "instagram"):
		return "Instagram"
	case strings.Contains(l, "fban") || strings.Contains(l, "fbav"):
		return "Facebook"
	case strings.Contains(l, "edg/") || strings.Contains(l, "edga/") || strings.Contains(l, "edgios/"):
		return "Edge"
	case strings.Contains(l, "samsungbrowser"):
		return "Samsung Internet"
	case strings.Contains(l, "opr/") || strings.Contains(l, "opera"):
		return "Opera"
	case strings.Contains(l, "firefox/") || strings.Contains(l, "fxios"):
		return "Firefox"
	case strings.Contains(l, "crios") || strings.Contains(l, "chrome/"):
		return "Chrome"
	case strings.Contains(l, "safari/"):
		return "Safari"
	}
	return "Outro"
}

// OS: o sistema do aparelho.
func OS(ua string) string {
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "iphone") || strings.Contains(l, "ipad") || strings.Contains(l, "ipod"):
		return "iOS"
	case strings.Contains(l, "android"):
		return "Android"
	case strings.Contains(l, "windows"):
		return "Windows"
	case strings.Contains(l, "cros"):
		return "ChromeOS"
	case strings.Contains(l, "mac os x") || strings.Contains(l, "macintosh"):
		return "macOS"
	case strings.Contains(l, "linux"):
		return "Linux"
	}
	return "Outro"
}

// clip corta o texto em n caracteres (sem quebrar um acento no meio).
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

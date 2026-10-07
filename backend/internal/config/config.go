// Package config carrega toda a configuração a partir de variáveis de ambiente.
package config

import (
	"fmt"
	netmail "net/mail"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env string
	// Warnings são problemas de segurança tolerados só porque o ambiente é
	// de desenvolvimento (ver validateSecrets). O main registra no log.
	Warnings []string

	HTTP      HTTP
	TCP       TCP
	Postgres  Postgres
	Redis     Redis
	Auth      Auth
	Tracking  Tracking
	Commands  Commands
	Telemetry Telemetry
	Bootstrap Bootstrap
	Mail      Mail
	Billing   Billing
	Payments  Payments
	Catalog   Catalog
	Shipping  Shipping
	Alerts    Alerts
	Push      Push
	StepUp    StepUp
	WhatsApp  WhatsApp
	Leads     Leads
	Infra     Infra
	SMS       SMS
	Theft     Theft
	Company   Company
}

// Company é a empresa no contrato com o cliente (a CONTRATADA).
type Company struct {
	Name      string
	LegalName string
	CNPJ      string
	Address   string
	Email     string
}

// SMS configura o envio pelo Twilio e a configuração do rastreador por SMS
// na ativação (APN, servidor, fuso e intervalo, para o número do chip).
type SMS struct {
	TwilioAccountSID          string
	TwilioAuthToken           string
	TwilioFrom                string
	TwilioMessagingServiceSID string
	TwilioBaseURL             string
	// WebhookBaseURL é o endereço público em que o Twilio avisa o status dos
	// SMS e entrega as respostas; o padrão é o APP_URL.
	WebhookBaseURL string
	// TrackerHost e TrackerPort são para onde o rastreador manda as posições
	// (o padrão é o host do APP_URL e a TCP_PORT).
	TrackerHost string
	TrackerPort int
	// O APN dos chips, quando o cadastro do rastreador não diz.
	APN         string
	APNUser     string
	APNPassword string
	// ReportSeconds e ParkedSeconds são os intervalos das posições com a
	// ignição ligada e desligada (TIMER); o cadastro do rastreador pode ter
	// o próprio intervalo ligado.
	ReportSeconds int
	ParkedSeconds int
}

type HTTP struct {
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	CORSOrigins     []string
	RateLimitRPS    float64
	RateLimitBurst  int
	// TrustedProxies são os proxies (nginx do painel, balanceador) de quem
	// se aceita o X-Forwarded-For. De qualquer outro endereço o cabeçalho é
	// ignorado — senão o cliente inventa o próprio IP e burla o limite de
	// tentativas de login.
	TrustedProxies []netip.Prefix
}

type TCP struct {
	Port int
	// MaxPacketSize limita o buffer por conexão, evitando exaustão de memória (§7).
	MaxPacketSize int
	// ReadTimeout derruba conexões mudas.
	ReadTimeout time.Duration
	// WriteTimeout limita o envio de um comando.
	WriteTimeout time.Duration
	// MaxConnections limita o total de sessões simultâneas (no teste de
	// carga, ~30 KB de memória cada).
	MaxConnections int
	// KeepAlive do socket TCP.
	KeepAlive time.Duration
	// IdentifyTimeout é o prazo para uma conexão nova mandar o login (IMEI).
	// Rastreador de verdade manda no primeiro pacote; quem só abre o socket
	// e fica parado é derrubado, em vez de ocupar a vaga por ReadTimeout.
	IdentifyTimeout time.Duration
	// MaxPendingPerIP limita as conexões ainda não identificadas de um mesmo
	// IP (0 desliga). Folgado de propósito: chips M2M saem por NAT da
	// operadora, com muitos rastreadores atrás de um IP.
	MaxPendingPerIP int
	// ProxyProtocol lê o cabeçalho PROXY (v1/v2) que o balanceador (Traefik)
	// manda com o IP real do rastreador. Só vale de TrustedProxies, e quem
	// chega sem cabeçalho segue com o IP do socket.
	ProxyProtocol bool
	// TrustedProxies: os mesmos de HTTP.TrustedProxies (TRUSTED_PROXIES).
	TrustedProxies []netip.Prefix
}

type Postgres struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
	SSLMode  string
	MaxConns int32
	// TelemetryMaxConns é o pool separado das gravações dos rastreadores
	// (posição, estado, último contato).
	TelemetryMaxConns int32
	// TelemetrySyncCommit: com false (padrão) essas gravações não esperam o
	// fsync do disco — no pior caso, uma queda do PostgreSQL perde menos de
	// 1 s de posições. É o que deixa a ingestão independente da velocidade
	// do disco. Faturas, pagamentos e cadastros seguem síncronos.
	TelemetrySyncCommit bool
}

func (p Postgres) DSN() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(p.User, p.Password),
		Host:   fmt.Sprintf("%s:%d", p.Host, p.Port),
		Path:   "/" + p.Database,
	}
	q := u.Query()
	q.Set("sslmode", p.SSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}

type Redis struct {
	Enabled  bool
	Host     string
	Port     int
	Password string
	DB       int
}

func (r Redis) Addr() string { return fmt.Sprintf("%s:%d", r.Host, r.Port) }

type Auth struct {
	JWTSecret       []byte
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	BcryptCost      int
	// PasswordResetTTL é a validade do link de redefinição de senha.
	PasswordResetTTL time.Duration
	// InviteTTL é a validade do link de boas-vindas mandado ao cliente novo
	// para ele criar a senha.
	InviteTTL time.Duration
}

type Tracking struct {
	// StaleAfter: sem pacotes por este tempo o dispositivo vira STALE (§17).
	StaleAfter time.Duration
	// OfflineAfter: sem pacotes por este tempo o dispositivo vira OFFLINE.
	OfflineAfter time.Duration
	// StatusSweepInterval é a frequência da varredura de status.
	StatusSweepInterval time.Duration
	// MaxHistoryPoints limita o retorno da API de histórico (§12).
	MaxHistoryPoints int
	// SimplifyToleranceMeters é a tolerância do Douglas-Peucker no histórico.
	SimplifyToleranceMeters float64
	// DefaultSpeedLimitKmh usado quando o veículo não define o próprio limite.
	DefaultSpeedLimitKmh float64
	// OverspeedHysteresisKmh evita oscilação do estado de excesso (§21).
	OverspeedHysteresisKmh float64
	// HistoryRetentionDays é o padrão da central para guardar posições e
	// eventos (7, 14 ou 30); cliente e veículo podem ter o próprio.
	HistoryRetentionDays int
	// HistoryCleanupInterval é de quanto em quanto tempo o histórico vencido
	// é apagado.
	HistoryCleanupInterval time.Duration
	// StoreRawPayload guarda o pacote bruto em cada posição. Desligado por
	// padrão: custa ~25% do disco das posições; ligue só para investigar um
	// modelo novo (pacotes com problema já vão para raw_packets).
	StoreRawPayload bool
}

type Commands struct {
	// AckTimeout: sem ACK neste prazo o comando vira TIMEOUT (§16).
	AckTimeout time.Duration
	// EngineCutMaxSpeedKmh é o limite de segurança para o corte (§14).
	EngineCutMaxSpeedKmh float64
	// EngineCutMaxPositionAge recusa o corte se a última posição for antiga demais.
	EngineCutMaxPositionAge time.Duration
	// SweepInterval é a frequência da varredura de comandos vencidos.
	SweepInterval time.Duration
}

type Telemetry struct {
	ServiceName  string
	LogLevel     string
	LogFormat    string // json | text
	OTLPEndpoint string // vazio desliga o tracing
	TraceRatio   float64
	MaskIMEI     bool
}

type Bootstrap struct {
	AdminEmail    string
	AdminPassword string
	AdminName     string
}

// Modos de TLS aceitos em SMTP_TLS.
const (
	SMTPStartTLS = "starttls" // porta 587: conexão sobe para TLS com STARTTLS
	SMTPTLS      = "tls"      // porta 465: TLS desde o primeiro byte
	SMTPNoTLS    = "none"     // só para servidor local de testes (ex.: Mailpit)
)

type Mail struct {
	// AppURL é o endereço público do painel; os links dos e-mails apontam
	// para ele.
	AppURL string
	// From é o remetente, ex.: "Farbo Rastreadores <nao-responda@exemplo.com>".
	From string

	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPTLS      string
}

type Billing struct {
	// InvoiceLeadDays: a fatura é gerada esta quantidade de dias antes do
	// vencimento, para o cliente ver e pagar com antecedência.
	InvoiceLeadDays int
	// SuspendAfterDays: com fatura vencida há mais que isso, o cliente perde o
	// acesso ao mapa e aos veículos até pagar. 0 desliga a suspensão.
	SuspendAfterDays int
	// Timezone define o "hoje" dos vencimentos.
	Timezone string
	// PriceAdjustment liga o reajuste anual pelo IPCA (aviso em 1º de julho,
	// vale em agosto); IPCAURL troca a API do Banco Central (testes).
	PriceAdjustment bool
	IPCAURL         string
}

// Alerts regula os alertas por e-mail (internal/alerts).
type Alerts struct {
	Enabled bool
	// Cooldown é o intervalo mínimo entre dois e-mails do mesmo alerta, do
	// mesmo veículo, para o mesmo destinatário; os repetidos no meio viram
	// contagem no próximo e-mail.
	Cooldown time.Duration
	// MaxEventAge: evento mais velho que isso ao chegar não vira e-mail — é o
	// rastreador descarregando o que guardou enquanto estava sem sinal.
	MaxEventAge time.Duration
	// MaxPerHour é o teto de e-mails de alerta por destinatário por hora.
	MaxPerHour int
	// OfflineParkedAfter: rastreador que para de comunicar com o veículo
	// parado só vira alerta depois desse tempo sem voltar (garagem
	// subterrânea é normal; horas sem sinal, não).
	OfflineParkedAfter time.Duration
	// TowingDistanceMeters: deslocamento com a ignição desligada, a partir do
	// ponto em que o veículo estacionou, que conta como reboque/furto.
	TowingDistanceMeters float64
	// CentralEmails recebem os alertas dos veículos da central (sem dono) e
	// os alertas de segurança (SOS, bateria desconectada, reboque) de todos.
	CentralEmails []string
	// Timezone é o fuso do horário de vigilância e das datas nos e-mails.
	Timezone string
}

// Push regula as notificações no celular do app do cliente (Web Push).
type Push struct {
	Enabled bool
	// VAPIDPrivateKey (base64url, 32 bytes) fixa as chaves; vazio gera um
	// par na primeira subida e guarda no banco, cifrado.
	VAPIDPrivateKey string
	// VAPIDSubject identifica a central aos serviços de push (mailto: ou
	// https:). Vazio usa o endereço de MAIL_FROM ou o APP_URL.
	VAPIDSubject string
	// ExtraHosts acrescenta serviços de push aos conhecidos (host:porta; http
	// só para o próprio computador). Serve para teste.
	ExtraHosts []string
}

// StepUp é a confirmação extra (biometria ou senha) antes de ações
// sensíveis, como o cliente desligar o motor pelo app.
type StepUp struct {
	// RPID é o domínio das credenciais de biometria (WebAuthn). Para valerem
	// no app e no painel, use o domínio comum aos dois (ex.:
	// farborastreadores.com.br). Vazio usa o host do APP_URL.
	RPID string
	// Origins são os endereços de onde a biometria é aceita. Vazio usa o
	// APP_URL e o CORS_ORIGINS que pertencem ao RPID.
	Origins []string
}

// Catalog é a tabela de preços usada quando o próprio cliente contrata um
// rastreador (ele não escolhe preço). Os padrões são os da landing page. A
// central pode ajustar os valores caso a caso. A instalação não entra: ela é
// paga direto ao prestador (ver o pacote installers).
type Catalog struct {
	PlanName            string
	PlanPriceCents      int
	DefaultDueDay       int
	EquipmentName       string
	EquipmentPriceCents int
	// SetupDueDays: prazo da fatura do equipamento.
	SetupDueDays int
	// LaunchPromo é a promoção de pré-lançamento.
	LaunchPromo LaunchPromo
}

// LaunchPromo: quem está na lista de lançamento paga menos no primeiro
// rastreador e na mensalidade dele por um período, enquanto houver vaga.
type LaunchPromo struct {
	Enabled bool
	// EquipmentCents é o valor do rastreador na promoção.
	EquipmentCents int
	// MonthlyCents é a mensalidade durante Months meses; depois, a do plano.
	MonthlyCents int
	// InsanosMonthlyCents é a mensalidade da promoção no plano dos
	// integrantes do Insanos MC (InsanosPlanName); depois, a desse plano.
	InsanosMonthlyCents int
	InsanosPlanName     string
	Months              int
	// Slots é quantos clientes podem usar (os primeiros a contratar).
	Slots int
}

func (p LaunchPromo) validate() error {
	if !p.Enabled {
		return nil
	}
	switch {
	case p.EquipmentCents < 0 || p.EquipmentCents > 100_000_00 || p.MonthlyCents < 0 || p.MonthlyCents > 100_000_00 ||
		p.InsanosMonthlyCents < 0 || p.InsanosMonthlyCents > 100_000_00:
		return fmt.Errorf("LAUNCH_PROMO_*_CENTS fora da faixa (0..10000000 centavos)")
	case p.Months < 1 || p.Months > 60:
		return fmt.Errorf("LAUNCH_PROMO_MONTHS fora da faixa (1..60)")
	case p.Slots < 1 || p.Slots > 1_000_000:
		return fmt.Errorf("LAUNCH_PROMO_SLOTS fora da faixa (1..1000000)")
	}
	return nil
}

// MonthlyFor é a mensalidade da promoção para o plano da assinatura: o do
// Insanos MC tem a sua; os demais, MonthlyCents.
func (p LaunchPromo) MonthlyFor(planName string) int {
	if p.InsanosPlanName != "" && strings.EqualFold(strings.TrimSpace(planName), p.InsanosPlanName) {
		return p.InsanosMonthlyCents
	}
	return p.MonthlyCents
}

func (c Catalog) validate() error {
	if err := c.LaunchPromo.validate(); err != nil {
		return err
	}
	for name, value := range map[string]int{
		"CATALOG_PLAN_PRICE_CENTS": c.PlanPriceCents, "CATALOG_EQUIPMENT_PRICE_CENTS": c.EquipmentPriceCents,
	} {
		if value < 0 || value > 100_000_00 {
			return fmt.Errorf("%s fora da faixa (0..10000000 centavos)", name)
		}
	}
	if c.DefaultDueDay < 1 || c.DefaultDueDay > 28 {
		return fmt.Errorf("CATALOG_DEFAULT_DUE_DAY fora da faixa (1..28)")
	}
	if c.SetupDueDays < 0 || c.SetupDueDays > 30 {
		return fmt.Errorf("CATALOG_SETUP_DUE_DAYS fora da faixa (0..30)")
	}
	if strings.TrimSpace(c.PlanName) == "" || strings.TrimSpace(c.EquipmentName) == "" {
		return fmt.Errorf("CATALOG_PLAN_NAME e CATALOG_EQUIPMENT_NAME não podem ficar vazios")
	}
	return nil
}

// Payments configura o pagamento online das faturas (AbacatePay).
type Payments struct {
	// AbacatePayAPIKey liga o Pix. Chave abc_dev_... opera em modo de testes.
	AbacatePayAPIKey  string
	AbacatePayBaseURL string
	// WebhookSecret é o valor que a AbacatePay manda em ?webhookSecret= em
	// cada notificação. Vazio recusa todos os webhooks.
	WebhookSecret string
	// WebhookPublicKey assina os webhooks (HMAC); o padrão é a chave pública
	// publicada pela AbacatePay.
	WebhookPublicKey string
	// PixExpiresIn é a validade de cada Pix gerado.
	PixExpiresIn time.Duration
	// SupplierPix liga o pagamento de fornecedores por Pix (sai do saldo da
	// AbacatePay; a chave precisa da permissão WITHDRAW:CREATE).
	SupplierPix bool
	// TransferAPIKey é uma chave só para os envios; vazia usa AbacatePayAPIKey.
	TransferAPIKey string
}

func (p Payments) Enabled() bool { return p.AbacatePayAPIKey != "" }

// TransferKey é a chave dos envios a fornecedores ("" se desligado).
func (p Payments) TransferKey() string {
	if !p.SupplierPix {
		return ""
	}
	if p.TransferAPIKey != "" {
		return p.TransferAPIKey
	}
	return p.AbacatePayAPIKey
}

// Shipping configura o envio do rastreador pelo Melhor Envios: etiqueta
// comprada pelo sistema e rastreio até a casa do cliente.
type Shipping struct {
	Sandbox bool
	// BaseURL sobrescreve o endereço da API (testes); vazio usa sandbox ou
	// produção conforme Sandbox.
	BaseURL string
	// ClientID e ClientSecret são do aplicativo criado em Integrações > Área
	// Dev. O Secret também assina os webhooks.
	ClientID     string
	ClientSecret string
	// Token pessoal opcional; com ele não é preciso "Conectar" pelo painel.
	Token string
	// RedirectURL é o callback cadastrado no aplicativo; o padrão é
	// APP_URL + /api/integrations/melhorenvio/callback.
	RedirectURL string
	// ContactEmail vai no User-Agent, exigido pela API.
	ContactEmail string
	// From é o remetente (a base da central).
	From ShippingAddress
	// Pacote de um rastreador.
	WeightKg float64
	HeightCm int
	WidthCm  int
	LengthCm int
	// InsuranceCents é o valor declarado (seguro); o padrão é o do equipamento.
	InsuranceCents int
	// NonCommercial envia com declaração de conteúdo em vez de nota fiscal.
	NonCommercial bool
	// Services filtra os serviços cotados (ex.: "1,2" = PAC e SEDEX); vazio
	// cota todos.
	Services string
	// SyncInterval é de quanto em quanto tempo o rastreio é consultado.
	SyncInterval time.Duration
	// ArrangeCities são as cidades ("São Paulo/SP") onde o cliente pode
	// combinar a entrega com a central, sem frete; o padrão é a Grande São
	// Paulo.
	ArrangeCities []string
}

// Theft é o modo roubo.
type Theft struct {
	// ParkedSeconds: a posição com o veículo parado, no modo roubo (o normal
	// é SMS_TRACKER_PARKED_SECONDS).
	ParkedSeconds int
	// Duration: desliga sozinho depois disso (poupa a bateria do veículo).
	Duration time.Duration
	// Reminder: de quanto em quanto tempo o dono é lembrado.
	Reminder time.Duration
}

// GreaterSaoPaulo são os 39 municípios da Região Metropolitana de São Paulo
// (a Grande São Paulo): onde o cliente pode, por padrão, combinar a entrega
// com a central (SHIPPING_ARRANGE_CITIES).
const GreaterSaoPaulo = "Arujá/SP,Barueri/SP,Biritiba-Mirim/SP,Caieiras/SP,Cajamar/SP,Carapicuíba/SP,Cotia/SP," +
	"Diadema/SP,Embu das Artes/SP,Embu-Guaçu/SP,Ferraz de Vasconcelos/SP,Francisco Morato/SP," +
	"Franco da Rocha/SP,Guararema/SP,Guarulhos/SP,Itapecerica da Serra/SP,Itapevi/SP," +
	"Itaquaquecetuba/SP,Jandira/SP,Juquitiba/SP,Mairiporã/SP,Mauá/SP,Mogi das Cruzes/SP," +
	"Osasco/SP,Pirapora do Bom Jesus/SP,Poá/SP,Ribeirão Pires/SP,Rio Grande da Serra/SP," +
	"Salesópolis/SP,Santa Isabel/SP,Santana de Parnaíba/SP,Santo André/SP," +
	"São Bernardo do Campo/SP,São Caetano do Sul/SP,São Lourenço da Serra/SP,São Paulo/SP," +
	"Suzano/SP,Taboão da Serra/SP,Vargem Grande Paulista/SP"

// ShippingAddress é o remetente das etiquetas.
type ShippingAddress struct {
	Name            string
	Phone           string
	Email           string
	Document        string
	CompanyDocument string
	StateRegister   string
	PostalCode      string
	Address         string
	Number          string
	Complement      string
	District        string
	City            string
	State           string
}

// Enabled diz se há credencial do Melhor Envios.
func (s Shipping) Enabled() bool { return s.Token != "" || (s.ClientID != "" && s.ClientSecret != "") }

// MissingOrigin lista o que falta no remetente para comprar etiqueta.
func (s Shipping) MissingOrigin() []string {
	missing := []string{}
	for env, value := range map[string]string{
		"MELHORENVIO_FROM_NAME": s.From.Name, "MELHORENVIO_FROM_PHONE": s.From.Phone,
		"MELHORENVIO_FROM_POSTAL_CODE": s.From.PostalCode, "MELHORENVIO_FROM_ADDRESS": s.From.Address,
		"MELHORENVIO_FROM_NUMBER": s.From.Number, "MELHORENVIO_FROM_DISTRICT": s.From.District,
		"MELHORENVIO_FROM_CITY": s.From.City, "MELHORENVIO_FROM_STATE": s.From.State,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, env)
		}
	}
	if s.From.Document == "" && s.From.CompanyDocument == "" {
		missing = append(missing, "MELHORENVIO_FROM_DOCUMENT ou MELHORENVIO_FROM_COMPANY_DOCUMENT")
	}
	sort.Strings(missing)
	return missing
}

// Infra configura o painel de infraestrutura (Diagnóstico → Servidor).
type Infra struct {
	// BackupDir é a pasta das cópias do banco (montada só para leitura);
	// vazia, o painel não mostra os backups.
	BackupDir string
}

// Leads configura os pré-clientes (cadastro de interesse da landing).
type Leads struct {
	// NotifyEmails recebem o aviso de cada pré-cliente novo.
	NotifyEmails []string
}

// WhatsApp configura o atendimento pelo WhatsApp (API oficial da Meta, a
// Cloud API) e o atendente de IA que responde os contatos.
type WhatsApp struct {
	// AccessToken é o token do usuário do sistema do Business Manager, com
	// whatsapp_business_messaging.
	AccessToken string
	// PhoneNumberID é o identificador do número (não o número em si), em
	// WhatsApp > Configuração da API.
	PhoneNumberID string
	// AppSecret é a chave secreta do aplicativo da Meta: assina os webhooks.
	AppSecret string
	// VerifyToken é o que se digita na Meta ao cadastrar o webhook.
	VerifyToken string
	// GraphVersion e GraphBaseURL apontam a Graph API (a URL só em testes).
	GraphVersion string
	GraphBaseURL string

	// AnthropicAPIKey liga o atendente de IA (Claude). Sem ela, as conversas
	// chegam ao painel e só a equipe responde.
	AnthropicAPIKey string
	AIEnabled       bool
	AIModel         string
	// AIEffort é quanto o modelo pensa antes de responder: low, medium ou
	// high. Atendimento é conversa curta; low responde mais rápido e gasta
	// menos.
	AIEffort string
	// AIDebounce é a espera antes de responder: quem manda três mensagens
	// seguidas recebe uma resposta só.
	AIDebounce time.Duration
	// AIMaxRepliesPerDay limita as respostas da IA por contato em 24 h;
	// passou disso, a conversa vai para a equipe.
	AIMaxRepliesPerDay int
	// AIHistory é quantas mensagens da conversa a IA lê.
	AIHistory int
	// HandoffEmails recebem o aviso de conversa transferida para a equipe.
	HandoffEmails []string
}

// Enabled diz se o número do WhatsApp está configurado.
func (w WhatsApp) Enabled() bool {
	return w.AccessToken != "" && w.PhoneNumberID != "" && w.AppSecret != ""
}

// AIReady diz se o atendente de IA responde os contatos.
func (w WhatsApp) AIReady() bool { return w.Enabled() && w.AIEnabled && w.AnthropicAPIKey != "" }

// minVerifyTokenLen: o token do webhook é um segredo como outro qualquer.
const minVerifyTokenLen = 16

func (w WhatsApp) validate() error {
	if !w.Enabled() {
		return nil
	}
	switch {
	case len(w.VerifyToken) < minVerifyTokenLen:
		return fmt.Errorf("WHATSAPP_VERIFY_TOKEN precisa de ao menos %d caracteres: gere com %s", minVerifyTokenLen, genPassword)
	case IsPlaceholder(w.VerifyToken) || IsPlaceholder(w.AppSecret) || IsPlaceholder(w.AccessToken):
		return fmt.Errorf("WHATSAPP_ACCESS_TOKEN, WHATSAPP_APP_SECRET e WHATSAPP_VERIFY_TOKEN não podem ser valores de exemplo")
	}
	switch w.AIEffort {
	case "low", "medium", "high":
	default:
		return fmt.Errorf("WHATSAPP_AI_EFFORT inválido: use low, medium ou high")
	}
	if strings.TrimSpace(w.AIModel) == "" {
		return fmt.Errorf("WHATSAPP_AI_MODEL vazio")
	}
	if w.AIDebounce < 0 || w.AIDebounce > time.Minute {
		return fmt.Errorf("WHATSAPP_AI_DEBOUNCE fora da faixa aceitável (0..1m)")
	}
	if w.AIMaxRepliesPerDay < 1 || w.AIMaxRepliesPerDay > 1000 {
		return fmt.Errorf("WHATSAPP_AI_MAX_REPLIES_PER_DAY fora da faixa aceitável (1..1000)")
	}
	if w.AIHistory < 4 || w.AIHistory > 200 {
		return fmt.Errorf("WHATSAPP_AI_HISTORY fora da faixa aceitável (4..200)")
	}
	for _, email := range w.HandoffEmails {
		if _, err := netmail.ParseAddress(email); err != nil {
			return fmt.Errorf("WHATSAPP_HANDOFF_EMAILS: endereço inválido %q", email)
		}
	}
	return nil
}

// Enabled diz se há servidor SMTP configurado. Sem ele os e-mails só vão
// para o log.
func (m Mail) Enabled() bool { return m.SMTPHost != "" }

func Load() (*Config, error) {
	cfg := &Config{
		Env: str("APP_ENV", "development"),
		HTTP: HTTP{
			Port:            num("HTTP_PORT", 8080),
			ReadTimeout:     dur("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    dur("HTTP_WRITE_TIMEOUT", 30*time.Second),
			ShutdownTimeout: dur("HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
			CORSOrigins:     csv("CORS_ORIGINS", "http://localhost:5173,http://localhost:3000"),
			RateLimitRPS:    flt("RATE_LIMIT_RPS", 20),
			RateLimitBurst:  num("RATE_LIMIT_BURST", 40),
		},
		TCP: TCP{
			Port:            num("TCP_PORT", 5000),
			MaxPacketSize:   num("TCP_MAX_PACKET_SIZE", 8192),
			ReadTimeout:     dur("TCP_READ_TIMEOUT", 10*time.Minute),
			WriteTimeout:    dur("TCP_WRITE_TIMEOUT", 10*time.Second),
			MaxConnections:  num("TCP_MAX_CONNECTIONS", 20000),
			IdentifyTimeout: dur("TCP_IDENTIFY_TIMEOUT", 30*time.Second),
			MaxPendingPerIP: num("TCP_MAX_PENDING_PER_IP", 100),
			KeepAlive:       dur("TCP_KEEPALIVE", 60*time.Second),
			ProxyProtocol:   bl("TCP_PROXY_PROTOCOL", false),
		},
		Postgres: Postgres{
			Host:     str("POSTGRES_HOST", "localhost"),
			Port:     num("POSTGRES_PORT", 5432),
			Database: str("POSTGRES_DB", "tracker"),
			User:     str("POSTGRES_USER", "tracker"),
			Password: str("POSTGRES_PASSWORD", ""),
			SSLMode:  str("POSTGRES_SSLMODE", "disable"),
			MaxConns: int32(num("POSTGRES_MAX_CONNS", 15)),

			TelemetryMaxConns:   int32(num("POSTGRES_TELEMETRY_MAX_CONNS", 10)),
			TelemetrySyncCommit: bl("TELEMETRY_SYNCHRONOUS_COMMIT", false),
		},
		Redis: Redis{
			Enabled:  bl("REDIS_ENABLED", false),
			Host:     str("REDIS_HOST", "localhost"),
			Port:     num("REDIS_PORT", 6379),
			Password: str("REDIS_PASSWORD", ""),
			DB:       num("REDIS_DB", 0),
		},
		Auth: Auth{
			JWTSecret:       []byte(str("JWT_SECRET", "")),
			AccessTokenTTL:  dur("JWT_ACCESS_TTL", 15*time.Minute),
			RefreshTokenTTL: dur("JWT_REFRESH_TTL", 720*time.Hour),
			BcryptCost:      num("BCRYPT_COST", 12),

			PasswordResetTTL: dur("PASSWORD_RESET_TTL", time.Hour),
			InviteTTL:        dur("CUSTOMER_INVITE_TTL", 72*time.Hour),
		},
		Tracking: Tracking{
			StaleAfter:              dur("DEVICE_STALE_AFTER", 2*time.Minute),
			OfflineAfter:            dur("DEVICE_OFFLINE_AFTER", 5*time.Minute),
			StatusSweepInterval:     dur("DEVICE_STATUS_SWEEP", 30*time.Second),
			MaxHistoryPoints:        num("HISTORY_MAX_POINTS", 5000),
			SimplifyToleranceMeters: flt("HISTORY_SIMPLIFY_TOLERANCE_M", 5),
			DefaultSpeedLimitKmh:    flt("DEFAULT_SPEED_LIMIT_KMH", 0),
			OverspeedHysteresisKmh:  flt("OVERSPEED_HYSTERESIS_KMH", 5),
			HistoryRetentionDays:    num("HISTORY_RETENTION_DAYS", 30),
			HistoryCleanupInterval:  dur("HISTORY_CLEANUP_INTERVAL", time.Hour),
			StoreRawPayload:         bl("POSITIONS_STORE_RAW", false),
		},
		Commands: Commands{
			AckTimeout:              dur("COMMAND_ACK_TIMEOUT", 15*time.Second),
			EngineCutMaxSpeedKmh:    flt("ENGINE_CUT_MAX_SPEED_KMH", 5),
			EngineCutMaxPositionAge: dur("ENGINE_CUT_MAX_POSITION_AGE", 10*time.Minute),
			SweepInterval:           dur("COMMAND_SWEEP_INTERVAL", 5*time.Second),
		},
		Telemetry: Telemetry{
			ServiceName:  str("OTEL_SERVICE_NAME", "tracker-platform"),
			LogLevel:     str("LOG_LEVEL", "info"),
			LogFormat:    str("LOG_FORMAT", "json"),
			OTLPEndpoint: str("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
			TraceRatio:   flt("OTEL_TRACE_SAMPLE_RATIO", 1),
			MaskIMEI:     bl("LOG_MASK_IMEI", true),
		},
		Bootstrap: Bootstrap{
			AdminEmail:    str("ADMIN_EMAIL", ""),
			AdminPassword: str("ADMIN_PASSWORD", ""),
			AdminName:     str("ADMIN_NAME", "Administrador"),
		},
		Mail: Mail{
			AppURL:       strings.TrimRight(str("APP_URL", ""), "/"),
			From:         str("MAIL_FROM", ""),
			SMTPHost:     str("SMTP_HOST", ""),
			SMTPPort:     num("SMTP_PORT", 587),
			SMTPUsername: str("SMTP_USERNAME", ""),
			SMTPPassword: str("SMTP_PASSWORD", ""),
			SMTPTLS:      strings.ToLower(str("SMTP_TLS", SMTPStartTLS)),
		},
		Catalog: Catalog{
			PlanName:            str("CATALOG_PLAN_NAME", "Plano Mensal"),
			PlanPriceCents:      num("CATALOG_PLAN_PRICE_CENTS", 6990),
			DefaultDueDay:       num("CATALOG_DEFAULT_DUE_DAY", 10),
			EquipmentName:       str("CATALOG_EQUIPMENT_NAME", "Rastreador J16 GT06"),
			EquipmentPriceCents: num("CATALOG_EQUIPMENT_PRICE_CENTS", 15000),
			SetupDueDays:        num("CATALOG_SETUP_DUE_DAYS", 3),
			LaunchPromo: LaunchPromo{
				Enabled:        bl("LAUNCH_PROMO_ENABLED", true),
				EquipmentCents: num("LAUNCH_PROMO_EQUIPMENT_CENTS", 12000),
				MonthlyCents:   num("LAUNCH_PROMO_MONTHLY_CENTS", 3490),
				// O nome é o do plano que a central escolhe para os integrantes.
				InsanosMonthlyCents: num("LAUNCH_PROMO_INSANOS_MONTHLY_CENTS", 2790),
				InsanosPlanName:     strings.TrimSpace(str("LAUNCH_PROMO_INSANOS_PLAN_NAME", "Especial Insanos MC")),
				Months:              num("LAUNCH_PROMO_MONTHS", 12),
				Slots:               num("LAUNCH_PROMO_SLOTS", 500),
			},
		},
		Payments: Payments{
			AbacatePayAPIKey:  str("ABACATEPAY_API_KEY", ""),
			AbacatePayBaseURL: str("ABACATEPAY_BASE_URL", ""),
			WebhookSecret:     str("ABACATEPAY_WEBHOOK_SECRET", ""),
			WebhookPublicKey:  str("ABACATEPAY_WEBHOOK_PUBLIC_KEY", ""),
			PixExpiresIn:      dur("PIX_EXPIRES_IN", 24*time.Hour),
			SupplierPix:       bl("ABACATEPAY_SUPPLIER_PIX", true),
			TransferAPIKey:    str("ABACATEPAY_TRANSFER_API_KEY", ""),
		},
		Shipping: Shipping{
			Sandbox:      bl("MELHORENVIO_SANDBOX", true),
			BaseURL:      strings.TrimRight(str("MELHORENVIO_BASE_URL", ""), "/"),
			ClientID:     str("MELHORENVIO_CLIENT_ID", ""),
			ClientSecret: str("MELHORENVIO_CLIENT_SECRET", ""),
			Token:        str("MELHORENVIO_TOKEN", ""),
			RedirectURL:  str("MELHORENVIO_REDIRECT_URL", ""),
			ContactEmail: str("MELHORENVIO_CONTACT_EMAIL", ""),
			From: ShippingAddress{
				Name:            str("MELHORENVIO_FROM_NAME", ""),
				Phone:           str("MELHORENVIO_FROM_PHONE", ""),
				Email:           str("MELHORENVIO_FROM_EMAIL", ""),
				Document:        str("MELHORENVIO_FROM_DOCUMENT", ""),
				CompanyDocument: str("MELHORENVIO_FROM_COMPANY_DOCUMENT", ""),
				StateRegister:   str("MELHORENVIO_FROM_STATE_REGISTER", ""),
				PostalCode:      str("MELHORENVIO_FROM_POSTAL_CODE", ""),
				Address:         str("MELHORENVIO_FROM_ADDRESS", ""),
				Number:          str("MELHORENVIO_FROM_NUMBER", ""),
				Complement:      str("MELHORENVIO_FROM_COMPLEMENT", ""),
				District:        str("MELHORENVIO_FROM_DISTRICT", ""),
				City:            str("MELHORENVIO_FROM_CITY", ""),
				State:           str("MELHORENVIO_FROM_STATE", ""),
			},
			WeightKg:       flt("MELHORENVIO_PACKAGE_WEIGHT_KG", 0.3),
			HeightCm:       num("MELHORENVIO_PACKAGE_HEIGHT_CM", 5),
			WidthCm:        num("MELHORENVIO_PACKAGE_WIDTH_CM", 12),
			LengthCm:       num("MELHORENVIO_PACKAGE_LENGTH_CM", 16),
			InsuranceCents: num("MELHORENVIO_INSURANCE_CENTS", -1),
			NonCommercial:  bl("MELHORENVIO_NON_COMMERCIAL", true),
			Services:       str("MELHORENVIO_SERVICES", ""),
			SyncInterval:   dur("MELHORENVIO_SYNC_INTERVAL", 15*time.Minute),
			ArrangeCities:  csv("SHIPPING_ARRANGE_CITIES", GreaterSaoPaulo),
		},
		Billing: Billing{
			InvoiceLeadDays:  num("BILLING_INVOICE_LEAD_DAYS", 10),
			SuspendAfterDays: num("BILLING_SUSPEND_AFTER_DAYS", 10),
			Timezone:         str("BILLING_TIMEZONE", "America/Sao_Paulo"),
			PriceAdjustment:  bl("PRICE_ADJUSTMENT_ENABLED", true),
			IPCAURL:          str("IPCA_API_URL", ""),
		},
		Alerts: Alerts{
			Enabled:              bl("ALERTS_ENABLED", true),
			Cooldown:             dur("ALERTS_COOLDOWN", 30*time.Minute),
			MaxEventAge:          dur("ALERTS_MAX_EVENT_AGE", 10*time.Minute),
			MaxPerHour:           num("ALERTS_MAX_PER_HOUR", 20),
			OfflineParkedAfter:   dur("ALERTS_OFFLINE_PARKED_AFTER", 2*time.Hour),
			TowingDistanceMeters: float64(num("ALERTS_TOWING_DISTANCE_M", 300)),
			CentralEmails:        csv("ALERTS_CENTRAL_EMAILS", ""),
		},
	}
	cfg.Push = Push{
		Enabled:         bl("PUSH_ENABLED", true),
		VAPIDPrivateKey: str("VAPID_PRIVATE_KEY", ""),
		VAPIDSubject:    str("VAPID_SUBJECT", ""),
		ExtraHosts:      csv("PUSH_EXTRA_HOSTS", ""),
	}
	// O horário de vigilância segue o fuso da central, o mesmo das faturas.
	cfg.Alerts.Timezone = str("ALERTS_TIMEZONE", cfg.Billing.Timezone)
	cfg.StepUp = StepUp{
		RPID:    strings.ToLower(str("WEBAUTHN_RP_ID", "")),
		Origins: csv("WEBAUTHN_ORIGINS", ""),
	}
	cfg.WhatsApp = WhatsApp{
		AccessToken:   str("WHATSAPP_ACCESS_TOKEN", ""),
		PhoneNumberID: str("WHATSAPP_PHONE_NUMBER_ID", ""),
		AppSecret:     str("WHATSAPP_APP_SECRET", ""),
		VerifyToken:   str("WHATSAPP_VERIFY_TOKEN", ""),
		GraphVersion:  str("WHATSAPP_GRAPH_VERSION", "v24.0"),
		GraphBaseURL:  strings.TrimRight(str("WHATSAPP_GRAPH_BASE_URL", "https://graph.facebook.com"), "/"),

		AnthropicAPIKey:    str("ANTHROPIC_API_KEY", ""),
		AIEnabled:          bl("WHATSAPP_AI_ENABLED", true),
		AIModel:            str("WHATSAPP_AI_MODEL", "claude-opus-5"),
		AIEffort:           strings.ToLower(str("WHATSAPP_AI_EFFORT", "low")),
		AIDebounce:         dur("WHATSAPP_AI_DEBOUNCE", 4*time.Second),
		AIMaxRepliesPerDay: num("WHATSAPP_AI_MAX_REPLIES_PER_DAY", 40),
		AIHistory:          num("WHATSAPP_AI_HISTORY", 40),
		// Sem lista própria: os e-mails da central; sem eles, o do admin.
		HandoffEmails: csv("WHATSAPP_HANDOFF_EMAILS", centralEmails()),
	}
	cfg.Leads = Leads{NotifyEmails: csv("LEADS_NOTIFY_EMAILS", centralEmails())}
	cfg.Infra = Infra{BackupDir: strings.TrimSpace(str("BACKUP_DIR", ""))}
	cfg.SMS = SMS{
		TwilioAccountSID:          strings.TrimSpace(str("TWILIO_ACCOUNT_SID", "")),
		TwilioAuthToken:           strings.TrimSpace(str("TWILIO_AUTH_TOKEN", "")),
		TwilioFrom:                strings.TrimSpace(str("TWILIO_FROM", "")),
		TwilioMessagingServiceSID: strings.TrimSpace(str("TWILIO_MESSAGING_SERVICE_SID", "")),
		TwilioBaseURL:             strings.TrimRight(str("TWILIO_BASE_URL", ""), "/"),
		WebhookBaseURL:            strings.TrimRight(str("TWILIO_WEBHOOK_BASE_URL", ""), "/"),
		TrackerHost:               strings.TrimSpace(str("TRACKER_PUBLIC_HOST", "")),
		TrackerPort:               num("TRACKER_PUBLIC_PORT", cfg.TCP.Port),
		APN:                       strings.TrimSpace(str("TRACKER_APN", "")),
		APNUser:                   strings.TrimSpace(str("TRACKER_APN_USER", "")),
		APNPassword:               strings.TrimSpace(str("TRACKER_APN_PASSWORD", "")),
		// Testado em campo com o J16: 10 s com a ignição ligada.
		ReportSeconds: num("TRACKER_REPORT_INTERVAL_SECONDS", 10),
		ParkedSeconds: num("TRACKER_PARKED_INTERVAL_SECONDS", 3600),
	}
	cfg.Company = Company{
		Name:      str("COMPANY_NAME", "Farbo Rastreadores"),
		LegalName: str("COMPANY_LEGAL_NAME", "FARBO TECNOLOGIA DE SISTEMAS E CLOUD LTDA"),
		CNPJ:      str("COMPANY_CNPJ", "49.757.084/0001-00"),
		Address:   str("COMPANY_ADDRESS", ""),
		Email:     str("COMPANY_EMAIL", "contato@farborastreadores.com.br"),
	}
	cfg.Theft = Theft{
		ParkedSeconds: num("THEFT_PARKED_INTERVAL_SECONDS", 30),
		Duration:      dur("THEFT_DURATION", 72*time.Hour),
		Reminder:      dur("THEFT_REMINDER_INTERVAL", 24*time.Hour),
	}

	// Sem APP_URL, usa a primeira origem do CORS: ela já é o endereço em que
	// o navegador abre o painel.
	if cfg.Mail.AppURL == "" && len(cfg.HTTP.CORSOrigins) > 0 {
		cfg.Mail.AppURL = strings.TrimRight(cfg.HTTP.CORSOrigins[0], "/")
	}
	// SMS: os avisos do Twilio e o servidor dos rastreadores no endereço do painel.
	if cfg.SMS.WebhookBaseURL == "" {
		cfg.SMS.WebhookBaseURL = cfg.Mail.AppURL
	}
	if cfg.SMS.TrackerHost == "" {
		if u, err := url.Parse(cfg.Mail.AppURL); err == nil && u.Hostname() != "localhost" {
			cfg.SMS.TrackerHost = strings.ToLower(u.Hostname())
		}
	}
	// Os serviços de push pedem um contato de quem envia (RFC 8292).
	if cfg.Push.VAPIDSubject == "" {
		cfg.Push.VAPIDSubject = cfg.Mail.AppURL
		if from, err := netmail.ParseAddress(cfg.Mail.From); err == nil {
			cfg.Push.VAPIDSubject = "mailto:" + from.Address
		}
	}
	// Biometria: sem domínio, o do painel; sem origens, as do painel e do
	// CORS que pertencem ao domínio.
	if cfg.StepUp.RPID == "" {
		if u, err := url.Parse(cfg.Mail.AppURL); err == nil {
			cfg.StepUp.RPID = strings.ToLower(u.Hostname())
		}
	}
	if len(cfg.StepUp.Origins) == 0 {
		for _, origin := range append([]string{cfg.Mail.AppURL}, cfg.HTTP.CORSOrigins...) {
			origin = strings.TrimRight(origin, "/")
			if originInDomain(origin, cfg.StepUp.RPID) && !slices.Contains(cfg.StepUp.Origins, origin) {
				cfg.StepUp.Origins = append(cfg.StepUp.Origins, origin)
			}
		}
	}

	// O callback do OAuth passa pelo mesmo endereço do painel (o /api dele
	// chega ao backend).
	if cfg.Shipping.RedirectURL == "" {
		cfg.Shipping.RedirectURL = cfg.Mail.AppURL + "/api/integrations/melhorenvio/callback"
	}
	if cfg.Shipping.InsuranceCents < 0 {
		cfg.Shipping.InsuranceCents = cfg.Catalog.EquipmentPriceCents
	}
	if cfg.Shipping.ContactEmail == "" {
		cfg.Shipping.ContactEmail = cfg.Bootstrap.AdminEmail
	}

	proxies, err := parseTrustedProxies(csv("TRUSTED_PROXIES",
		"127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"))
	if err != nil {
		return nil, err
	}
	cfg.HTTP.TrustedProxies = proxies
	cfg.TCP.TrustedProxies = proxies

	if err := cfg.validateSecrets(); err != nil {
		return nil, err
	}
	if cfg.TCP.MaxPacketSize < 512 || cfg.TCP.MaxPacketSize > 1<<20 {
		return nil, fmt.Errorf("TCP_MAX_PACKET_SIZE fora da faixa aceitável (512..1048576)")
	}
	if cfg.Auth.PasswordResetTTL < 5*time.Minute || cfg.Auth.PasswordResetTTL > 24*time.Hour {
		return nil, fmt.Errorf("PASSWORD_RESET_TTL fora da faixa aceitável (5m..24h)")
	}
	if cfg.Auth.InviteTTL < time.Hour || cfg.Auth.InviteTTL > 30*24*time.Hour {
		return nil, fmt.Errorf("CUSTOMER_INVITE_TTL fora da faixa aceitável (1h..720h)")
	}
	if err := cfg.Mail.validate(); err != nil {
		return nil, err
	}
	if err := cfg.Billing.validate(); err != nil {
		return nil, err
	}
	if err := cfg.Catalog.validate(); err != nil {
		return nil, err
	}
	if err := cfg.Alerts.validate(); err != nil {
		return nil, err
	}
	if err := cfg.StepUp.validate(); err != nil {
		return nil, err
	}
	if err := cfg.WhatsApp.validate(); err != nil {
		return nil, err
	}
	for _, email := range cfg.Leads.NotifyEmails {
		if _, err := netmail.ParseAddress(email); err != nil {
			return nil, fmt.Errorf("LEADS_NOTIFY_EMAILS: endereço inválido %q", email)
		}
	}
	if cfg.Payments.PixExpiresIn < 5*time.Minute || cfg.Payments.PixExpiresIn > 30*24*time.Hour {
		return nil, fmt.Errorf("PIX_EXPIRES_IN fora da faixa aceitável (5m..720h)")
	}
	if cfg.TCP.IdentifyTimeout < time.Second || cfg.TCP.IdentifyTimeout > cfg.TCP.ReadTimeout {
		return nil, fmt.Errorf("TCP_IDENTIFY_TIMEOUT fora da faixa (1s..TCP_READ_TIMEOUT)")
	}
	if cfg.TCP.MaxPendingPerIP < 0 {
		return nil, fmt.Errorf("TCP_MAX_PENDING_PER_IP não pode ser negativo")
	}
	switch cfg.Tracking.HistoryRetentionDays {
	case 7, 14, 30:
	default:
		return nil, fmt.Errorf("HISTORY_RETENTION_DAYS precisa ser 7, 14 ou 30")
	}
	if cfg.Tracking.HistoryCleanupInterval < time.Minute || cfg.Tracking.HistoryCleanupInterval > 24*time.Hour {
		return nil, fmt.Errorf("HISTORY_CLEANUP_INTERVAL fora da faixa aceitável (1m..24h)")
	}
	if cfg.Shipping.SyncInterval < time.Minute || cfg.Shipping.SyncInterval > 24*time.Hour {
		return nil, fmt.Errorf("MELHORENVIO_SYNC_INTERVAL fora da faixa aceitável (1m..24h)")
	}
	if cfg.Shipping.WeightKg <= 0 || cfg.Shipping.HeightCm <= 0 || cfg.Shipping.WidthCm <= 0 || cfg.Shipping.LengthCm <= 0 {
		return nil, fmt.Errorf("MELHORENVIO_PACKAGE_*: peso e medidas precisam ser positivos")
	}
	return cfg, nil
}

func (a Alerts) validate() error {
	if a.Cooldown < time.Minute || a.Cooldown > 24*time.Hour {
		return fmt.Errorf("ALERTS_COOLDOWN fora da faixa aceitável (1m..24h)")
	}
	if a.MaxEventAge < time.Minute || a.MaxEventAge > 24*time.Hour {
		return fmt.Errorf("ALERTS_MAX_EVENT_AGE fora da faixa aceitável (1m..24h)")
	}
	if a.MaxPerHour < 1 || a.MaxPerHour > 1000 {
		return fmt.Errorf("ALERTS_MAX_PER_HOUR fora da faixa aceitável (1..1000)")
	}
	if a.OfflineParkedAfter < time.Minute || a.OfflineParkedAfter > 7*24*time.Hour {
		return fmt.Errorf("ALERTS_OFFLINE_PARKED_AFTER fora da faixa aceitável (1m..168h)")
	}
	if a.TowingDistanceMeters < 50 || a.TowingDistanceMeters > 10000 {
		return fmt.Errorf("ALERTS_TOWING_DISTANCE_M fora da faixa aceitável (50..10000)")
	}
	for _, email := range a.CentralEmails {
		if _, err := netmail.ParseAddress(email); err != nil {
			return fmt.Errorf("ALERTS_CENTRAL_EMAILS: endereço inválido %q", email)
		}
	}
	if _, err := time.LoadLocation(a.Timezone); err != nil {
		return fmt.Errorf("ALERTS_TIMEZONE inválido: %w", err)
	}
	return nil
}

func (s StepUp) validate() error {
	if s.RPID == "" {
		return fmt.Errorf("WEBAUTHN_RP_ID vazio: defina o domínio do painel (ou o APP_URL)")
	}
	for _, origin := range s.Origins {
		if !originInDomain(origin, s.RPID) {
			return fmt.Errorf("WEBAUTHN_ORIGINS: %q não pertence ao domínio %q (WEBAUTHN_RP_ID)", origin, s.RPID)
		}
	}
	return nil
}

// originInDomain diz se a origem (https://app.exemplo.com) é do domínio
// (exemplo.com) ou dele mesmo: é a regra do WebAuthn para o RP ID.
func originInDomain(origin, domain string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || domain == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func (b Billing) validate() error {
	if b.InvoiceLeadDays < 0 || b.InvoiceLeadDays > 60 {
		return fmt.Errorf("BILLING_INVOICE_LEAD_DAYS fora da faixa aceitável (0..60)")
	}
	if b.SuspendAfterDays < 0 || b.SuspendAfterDays > 365 {
		return fmt.Errorf("BILLING_SUSPEND_AFTER_DAYS fora da faixa aceitável (0..365; 0 desliga)")
	}
	if _, err := time.LoadLocation(b.Timezone); err != nil {
		return fmt.Errorf("BILLING_TIMEZONE inválido: %w", err)
	}
	return nil
}

func (m Mail) validate() error {
	appURL, err := url.Parse(m.AppURL)
	if err != nil || (appURL.Scheme != "http" && appURL.Scheme != "https") || appURL.Host == "" {
		return fmt.Errorf("APP_URL precisa ser o endereço completo do painel, ex.: https://painel.exemplo.com")
	}
	switch m.SMTPTLS {
	case SMTPStartTLS, SMTPTLS, SMTPNoTLS:
	default:
		return fmt.Errorf("SMTP_TLS inválido: use %q, %q ou %q", SMTPStartTLS, SMTPTLS, SMTPNoTLS)
	}
	if m.Enabled() && m.From == "" {
		return fmt.Errorf("MAIL_FROM é obrigatório quando SMTP_HOST está definido")
	}
	return nil
}

// centralEmails é o padrão de quem recebe os avisos da equipe: os e-mails da
// central (ALERTS_CENTRAL_EMAILS) ou, sem eles, o do admin.
func centralEmails() string {
	return strings.Join(csv("ALERTS_CENTRAL_EMAILS", str("ADMIN_EMAIL", "")), ",")
}

func str(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func num(key string, def int) int {
	if v, err := strconv.Atoi(str(key, "")); err == nil {
		return v
	}
	return def
}

func flt(key string, def float64) float64 {
	if v, err := strconv.ParseFloat(str(key, ""), 64); err == nil {
		return v
	}
	return def
}

func bl(key string, def bool) bool {
	if v, err := strconv.ParseBool(str(key, "")); err == nil {
		return v
	}
	return def
}

func dur(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(str(key, "")); err == nil {
		return v
	}
	return def
}

func csv(key, def string) []string {
	raw := strings.Split(str(key, def), ",")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// parseTrustedProxies aceita CIDRs ("10.0.0.0/8") e IPs soltos. O padrão é
// a própria máquina e as redes privadas, onde ficam os contêineres.
func parseTrustedProxies(list []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(list))
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(item); err == nil {
			out = append(out, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES: %q não é IP nem CIDR", item)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

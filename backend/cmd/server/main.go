// Comando server: sobe a API HTTP, o servidor TCP de rastreadores e as
// rotinas de manutenção da plataforma.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/alerts"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/analytics"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/api"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/commands"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/events"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/geocoding"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/geofences"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/infra"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/installers"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leads"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/melhorenvio"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments/abacatepay"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols/gt06"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols/h02"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols/tkstar"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/realtime"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/retention"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/shares"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/support"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tcp"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
	ws "github.com/pedrofarbo/farbo-rastreamento/backend/internal/websocket"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/whatsapp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "erro fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Avisos e erros também vão para o painel de infraestrutura (gravados no
	// banco depois que ele estiver pronto; até lá, esperam na fila).
	logCapture := infra.NewCapture(telemetry.NewLogger(cfg.Telemetry.LogLevel, cfg.Telemetry.LogFormat).Handler())
	log := slog.New(logCapture)
	slog.SetDefault(log)
	telemetry.SetMaskIMEI(cfg.Telemetry.MaskIMEI)
	// Segredo de exemplo, óbvio ou repetido: fora do desenvolvimento
	// config.Load já recusou; aqui só chega com APP_ENV=development/test.
	for _, problem := range cfg.Warnings {
		log.Warn("SEGREDO INSEGURO — aceito só porque APP_ENV="+cfg.Env+"; nunca use assim em produção",
			"problema", problem)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := telemetry.SetupTracing(ctx, cfg.Telemetry.ServiceName,
		cfg.Telemetry.OTLPEndpoint, cfg.Env, cfg.Telemetry.TraceRatio)
	if err != nil {
		return fmt.Errorf("configurando tracing: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(shutdownCtx)
	}()

	metrics := telemetry.NewMetrics()

	// ---- Banco ----
	db, err := database.Connect(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.Migrate(ctx, log); err != nil {
		return fmt.Errorf("aplicando migrations: %w", err)
	}
	logCapture.Persist(ctx, db)

	// Pool separado para o que os rastreadores gravam (posição, estado,
	// último contato): por padrão sem esperar o fsync do disco — ver
	// database.ConnectTelemetry. Faturas, pagamentos e cadastros ficam no
	// pool principal, com commit síncrono.
	telemetryDB, err := database.ConnectTelemetry(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer telemetryDB.Close()

	// ---- Protocolos ----
	//
	// A ordem importa: a detecção usa o primeiro adaptador que reconhecer o
	// tráfego, então os formatos com assinatura forte vêm antes, e o modo de
	// captura (que aceita qualquer coisa) vem por último.
	registry := protocols.NewRegistry(
		gt06.New(envBool("GT06_ACK_GPS", false)),
		h02.New(envInt("H02_BINARY_FRAME_LENGTH", 0)),
		tkstar.NewV1(),
		tkstar.NewV1Extended(),
		tkstar.NewV4(envBool("TKSTAR_V4_CAPTURE", false)),
	)
	logProtocols(log, registry)

	// ---- WebSocket ----
	hub := ws.NewHub(log, func(total int) {
		metrics.WebsocketClients.Set(float64(total))
	})

	var redisClient *redis.Client
	if cfg.Redis.Enabled {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr(),
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		if err := redisClient.Ping(ctx).Err(); err != nil {
			// Redis é opcional: sem ele o hub continua servindo esta instância.
			log.Warn("Redis indisponível; seguindo sem replicação entre instâncias", "err", err)
			_ = redisClient.Close()
			redisClient = nil
		} else {
			bridge := ws.NewBridge(redisClient, hub, cfg.Auth.JWTSecret, realtime.Decode, log)
			go bridge.Run(ctx)
			defer func() { _ = redisClient.Close() }()
		}
	}

	// ---- Repositórios e serviços ----
	deviceRepo := devices.NewRepository(db)
	deviceRepo.UseTelemetry(telemetryDB)
	vehicleRepo := vehicles.NewRepository(db)
	positionRepo := tracking.NewRepository(telemetryDB)
	stateRepo := tracking.NewStateRepository(telemetryDB)
	eventRepo := events.NewRepository(db)
	commandRepo := commands.NewRepository(db)
	geofenceRepo := geofences.NewRepository(db)
	auditRepo := audit.NewRepository(db)
	userRepo := auth.NewRepository(db)
	rawRepo := tracking.NewRawPacketRepository(db)

	mailer, err := newMailSender(cfg, log)
	if err != nil {
		return err
	}
	authSvc := auth.NewService(userRepo, cfg.Auth, mail.NewAccountMailer(mailer, cfg.Mail.AppURL), log)
	// JWT_SECRET trocado: as sessões abertas com o anterior acabam aqui.
	if err := authSvc.RevokeSessionsFromOldKeys(ctx); err != nil {
		return err
	}
	if err := authSvc.EnsureBootstrapUser(ctx, cfg.Bootstrap); err != nil {
		return err
	}

	billingSvc, err := billing.NewService(billing.NewRepository(db), cfg.Billing, log)
	if err != nil {
		return err
	}

	// Quem é dono de cada veículo: o WebSocket filtra por aqui o que cada
	// cliente recebe.
	ownerIndex := vehicles.NewOwnerIndex(db, log)
	if err := ownerIndex.Refresh(ctx); err != nil {
		return fmt.Errorf("carregando os donos dos veículos: %w", err)
	}
	go ownerIndex.Run(ctx, 15*time.Second)

	deviceSvc := devices.NewService(deviceRepo, registry)
	vehicleSvc := vehicles.NewService(vehicleRepo)
	eventSvc := events.NewService(eventRepo, hub, log)
	auditSvc := audit.NewService(auditRepo, log)
	paymentsSvc := payments.NewService(payments.NewRepository(db), billingSvc,
		newPaymentGateway(cfg, log), auditSvc, cfg.Payments, log)
	geofenceSvc := geofences.NewService(geofenceRepo)
	if err := geofenceSvc.Refresh(ctx); err != nil {
		return fmt.Errorf("carregando cercas: %w", err)
	}

	// Reaproveita o e-mail do admin como contato do User-Agent exigido pela
	// política de uso do Nominatim — evita mais uma variável de ambiente só
	// para isso.
	geocodingSvc := geocoding.NewService(cfg.Bootstrap.AdminEmail, log)

	// Envio do rastreador pelo Melhor Envios. Sem credencial, o acompanhamento
	// funciona igual; só a compra de etiqueta e o rastreio automático ficam
	// desligados.
	carrierStore, err := melhorenvio.NewDBStore(db, string(cfg.Auth.JWTSecret))
	if err != nil {
		return err
	}
	var carrier *melhorenvio.Client
	if cfg.Shipping.Enabled() {
		carrier = melhorenvio.New(melhorenvio.Config{
			BaseURL:  melhorenvio.BaseURLFor(cfg.Shipping.Sandbox, cfg.Shipping.BaseURL),
			ClientID: cfg.Shipping.ClientID, ClientSecret: cfg.Shipping.ClientSecret,
			RedirectURL: cfg.Shipping.RedirectURL, ContactEmail: cfg.Shipping.ContactEmail,
			StaticToken: cfg.Shipping.Token,
		}, carrierStore)
		log.Info("envio pelo Melhor Envios ligado", "sandbox", cfg.Shipping.Sandbox)
	}
	retentionSvc := retention.NewService(db, cfg.Tracking.HistoryRetentionDays, log)
	fulfillmentSvc := fulfillment.NewService(db, fulfillment.NewRepository(db), carrier,
		fulfillment.MailNotifier{Mailer: mail.NewShipmentMailer(mailer, cfg.Mail.AppURL)},
		cfg.Shipping, cfg.Catalog.EquipmentName, log)

	installerRepo := installers.NewRepository(db)
	supportSvc, err := newSupport(ctx, cfg, db, fulfillmentSvc.Repo(), installerRepo, mailer, log)
	if err != nil {
		return err
	}

	stateStore := tracking.NewStateStore(stateRepo)
	if err := stateStore.Load(ctx); err != nil {
		return fmt.Errorf("carregando estado dos dispositivos: %w", err)
	}

	connManager := tcp.NewManager(func(total int) {
		metrics.Connections.Set(float64(total))
	})

	commandSvc := commands.NewService(
		commandRepo, registry, commandSender{connManager},
		tracking.NewSnapshotProvider(positionRepo),
		eventSvc, auditSvc, hub, cfg.Commands, metrics, log,
	)

	ingestor := tracking.NewIngestor(
		deviceSvc, vehicleSvc, positionRepo, stateStore, eventSvc,
		geofenceSvc, commandSvc, rawRepo, hub, cfg.Tracking, metrics, log,
	)

	// ---- Alertas por e-mail ----
	// Os ganchos só enfileiram: avaliar e enviar corre em segundo plano.
	alertStore := alerts.NewDBStore(db, billingSvc.IsSuspended)
	alertEngine := alerts.NewEngine(cfg.Alerts, cfg.Mail.AppURL, alertStore, mail.NewAlertMailer(mailer),
		stateStore, geocodingSvc, log)
	if cfg.Alerts.Enabled {
		eventSvc.SetObserver(alertEngine.OnEvent)
		ingestor.SetPositionObserver(alertEngine.OnPosition)
	}
	// Notificações no celular do app do cliente: o mesmo alerta, outro canal.
	pushSvc, err := push.NewService(ctx, db, cfg.Push, cfg.Auth.JWTSecret, log)
	if err != nil {
		return fmt.Errorf("notificações no celular: %w", err)
	}
	if pushSvc.Enabled() {
		alertEngine.SetPusher(pushSvc)
	}

	// Visitas da landing page, sem cookies.
	analyticsSvc := analytics.NewService(db, log)

	// Infraestrutura: CPU, memória e disco da máquina a cada 10 s (o
	// histórico de 24 h fica em memória), banco, Redis, backups e erros.
	hostMonitor := infra.NewMonitor("/proc", "/")
	go hostMonitor.Run(ctx, 10*time.Second)
	infraSvc := infra.NewService(db, hostMonitor, logCapture, cfg.Infra.BackupDir)
	if redisClient != nil {
		infraSvc.SetRedis(func(ctx context.Context) error { return redisClient.Ping(ctx).Err() })
	}

	// Acessos de terceiros aos veículos (acompanhar e bloqueio de emergência).
	sharesSvc := shares.NewService(db, authSvc, mail.NewShareMailer(mailer, cfg.Mail.AppURL), log)
	if pushSvc.Enabled() {
		sharesSvc.SetPusher(pushSvc)
	}

	// ---- Servidores ----
	tcpServer := tcp.NewServer(cfg.TCP, registry, connManager, ingestor, log, metrics)

	apiServer := api.NewServer(api.Deps{
		Config: cfg, Log: log, Metrics: metrics, DB: db,
		Auth: authSvc, Devices: deviceSvc, Vehicles: vehicleSvc,
		Geofences: geofenceSvc, Geocoder: geocodingSvc, Events: eventSvc, Commands: commandSvc,
		Audit: auditSvc, Billing: billingSvc, Payments: paymentsSvc, Owners: ownerIndex,
		Orders:      orders.NewService(db, billingSvc, vehicleSvc, cfg.Catalog, log),
		Installers:  installerRepo,
		Addresses:   addresses.NewRepository(db),
		Fulfillment: fulfillmentSvc, Carrier: carrier, CarrierStore: carrierStore,
		Retention: retentionSvc,
		Support:   supportSvc,
		Leads: leads.NewService(leads.NewRepository(db),
			leads.MailNotifier{Mailer: mail.NewLeadMailer(mailer, cfg.Mail.AppURL), To: cfg.Leads.NotifyEmails}, log),
		Alerts: alertEngine, AlertStore: alertStore, Push: pushSvc, Shares: sharesSvc, Analytics: analyticsSvc,
		Infra:     infraSvc,
		StepUp:    stepup.NewService(db, cfg.StepUp, authSvc),
		Positions: positionRepo, States: stateStore,
		Raw: rawRepo, Ingestor: ingestor, Conns: connManager, Registry: registry,
		WS: ws.NewHandler(hub, cfg.HTTP.CORSOrigins), Hub: hub,
	})

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := apiServer.ListenAndServe(ctx); err != nil {
			errCh <- fmt.Errorf("API HTTP: %w", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		addr := fmt.Sprintf(":%d", cfg.TCP.Port)
		if err := tcpServer.ListenAndServe(ctx, addr); err != nil {
			errCh <- fmt.Errorf("servidor TCP: %w", err)
		}
	}()

	if cfg.Alerts.Enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			alertEngine.Run(ctx)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		runWorkers(ctx, cfg, ingestor, commandSvc, authSvc, billingSvc, paymentsSvc, fulfillmentSvc, retentionSvc, rawRepo, geocodingSvc, analyticsSvc, infraSvc, log)
	}()

	log.Info("plataforma no ar",
		"env", cfg.Env, "http", cfg.HTTP.Port, "tcp", cfg.TCP.Port,
		"redis", redisClient != nil)

	select {
	case err := <-errCh:
		stop()
		wg.Wait()
		return err
	case <-ctx.Done():
		log.Info("sinal de encerramento recebido, finalizando")
		wg.Wait()
		return nil
	}
}

// runWorkers concentra as rotinas periódicas.
func runWorkers(
	ctx context.Context,
	cfg *config.Config,
	ingestor *tracking.Ingestor,
	commandSvc *commands.Service,
	authSvc *auth.Service,
	billingSvc *billing.Service,
	paymentsSvc *payments.Service,
	fulfillmentSvc *fulfillment.Service,
	retentionSvc *retention.Service,
	rawRepo *tracking.RawPacketRepository,
	geocodingSvc *geocoding.Service,
	analyticsSvc *analytics.Service,
	infraSvc *infra.Service,
	log *slog.Logger,
) {
	statusTicker := time.NewTicker(cfg.Tracking.StatusSweepInterval)
	commandTicker := time.NewTicker(cfg.Commands.SweepInterval)
	cleanupTicker := time.NewTicker(6 * time.Hour)
	// As faturas vencem por dia; conferir de hora em hora basta para a fatura
	// nova aparecer no mesmo dia em que entra na antecedência configurada.
	billingTicker := time.NewTicker(time.Hour)
	// Reconsulta os Pix pendentes: dá baixa mesmo quando o webhook não chega.
	paymentsTicker := time.NewTicker(time.Minute)
	defer paymentsTicker.Stop()
	// Rastreio das etiquetas a caminho (o webhook, quando configurado,
	// adianta; isto garante mesmo sem ele).
	shippingTicker := time.NewTicker(cfg.Shipping.SyncInterval)
	defer shippingTicker.Stop()
	// Apaga o histórico que passou do prazo de cada veículo (7, 14 ou 30 dias).
	historyTicker := time.NewTicker(cfg.Tracking.HistoryCleanupInterval)
	defer historyTicker.Stop()
	// Em segundo plano e uma de cada vez: uma limpeza grande (depois de muito
	// tempo parado, por exemplo) não pode atrasar as outras rotinas.
	var cleaning atomic.Bool
	cleanHistory := func() {
		if !cleaning.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer cleaning.Store(false)
			if _, err := retentionSvc.Cleanup(ctx); err != nil && ctx.Err() == nil {
				log.Error("falha ao apagar o histórico vencido", "err", err)
			}
		}()
	}
	cleanHistory()
	defer statusTicker.Stop()
	defer commandTicker.Stop()
	defer cleanupTicker.Stop()
	defer billingTicker.Stop()

	// Primeira varredura imediata para o painel já abrir com o status correto.
	ingestor.SweepStatuses(ctx)
	billingSvc.GenerateInvoices(ctx)

	for {
		select {
		case <-ctx.Done():
			return

		case <-statusTicker.C:
			ingestor.SweepStatuses(ctx)

		case <-commandTicker.C:
			commandSvc.SweepTimeouts(ctx)

		case <-billingTicker.C:
			billingSvc.GenerateInvoices(ctx)

		case <-paymentsTicker.C:
			paymentsSvc.SyncPending(ctx)

		case <-historyTicker.C:
			cleanHistory()

		case <-shippingTicker.C:
			if changed, err := fulfillmentSvc.Sync(ctx); err != nil {
				if errors.Is(err, melhorenvio.ErrNotConnected) {
					log.Warn("rastreio pausado: Melhor Envios não conectado")
				} else {
					log.Error("falha ao consultar o rastreio no Melhor Envios", "err", err)
				}
			} else if changed > 0 {
				log.Info("rastreio atualizado", "count", changed)
			}

		case <-cleanupTicker.C:
			authSvc.CleanupExpiredTokens(ctx)
			if removed, err := infraSvc.Cleanup(ctx); err != nil {
				log.Warn("falha ao limpar os registros antigos de erros", "err", err)
			} else if removed > 0 {
				log.Info("registros antigos de erros removidos", "count", removed)
			}
			if removed, err := analyticsSvc.Cleanup(ctx); err != nil {
				log.Warn("falha ao limpar as visitas antigas da landing", "err", err)
			} else if removed > 0 {
				log.Info("visitas antigas da landing removidas", "count", removed)
			}
			geocodingSvc.PurgeExpired()
			cutoff := time.Now().Add(-30 * 24 * time.Hour)
			if removed, err := rawRepo.DeleteOlderThan(ctx, cutoff); err != nil {
				log.Warn("falha ao limpar pacotes crus", "err", err)
			} else if removed > 0 {
				log.Info("pacotes crus antigos removidos", "count", removed)
			}
		}
	}
}

// newPaymentGateway liga o Pix pela AbacatePay quando há chave. A chave nunca
// vai para o log; só o modo (testes ou produção).
func newPaymentGateway(cfg *config.Config, log *slog.Logger) payments.Gateway {
	p := cfg.Payments
	if !p.Enabled() {
		log.Warn("ABACATEPAY_API_KEY não definida: pagamento das faturas por Pix desligado")
		return nil
	}
	mode := "produção"
	if abacatepay.IsDevKey(p.AbacatePayAPIKey) {
		mode = "testes (devMode)"
	}
	if p.WebhookSecret == "" {
		log.Warn("ABACATEPAY_WEBHOOK_SECRET vazio: webhooks recusados; a baixa vem só da consulta periódica")
	}
	log.Info("pagamento por Pix via AbacatePay", "modo", mode, "webhook", p.WebhookSecret != "",
		"validade_pix", p.PixExpiresIn.String())
	return abacatepay.NewClient(p.AbacatePayBaseURL, p.AbacatePayAPIKey)
}

// newSupport monta o atendimento pelo WhatsApp. Sem o número configurado, o
// painel mostra o que falta; sem ANTHROPIC_API_KEY, as conversas chegam e só
// a equipe responde.
func newSupport(ctx context.Context, cfg *config.Config, db *database.DB, fulfillments *fulfillment.Repository,
	installerRepo *installers.Repository, mailer mail.Sender, log *slog.Logger) (*support.Service, error) {
	w := cfg.WhatsApp
	tz, err := time.LoadLocation(cfg.Billing.Timezone)
	if err != nil {
		return nil, err
	}
	var wa *whatsapp.Client
	if w.Enabled() {
		wa = whatsapp.New(w.GraphBaseURL, w.GraphVersion, w.PhoneNumberID, w.AccessToken)
	} else {
		log.Info("WhatsApp não configurado (WHATSAPP_ACCESS_TOKEN, WHATSAPP_PHONE_NUMBER_ID e WHATSAPP_APP_SECRET)")
	}
	var ai *support.Assistant
	switch {
	case w.AIReady():
		ai, err = support.NewAssistant(w.AnthropicAPIKey, w.AIModel, w.AIEffort,
			support.FactsFrom(cfg.Catalog, cfg.Billing, cfg.Mail.AppURL), tz)
		if err != nil {
			return nil, err
		}
		log.Info("atendente de IA no WhatsApp ligado", "model", w.AIModel, "effort", w.AIEffort,
			"avisos_para", len(w.HandoffEmails))
	case w.Enabled():
		log.Warn("atendente de IA desligado (ANTHROPIC_API_KEY vazia ou WHATSAPP_AI_ENABLED=false): só a equipe responde")
	}
	notifier := support.MailNotifier{Mailer: mail.NewSupportMailer(mailer, cfg.Mail.AppURL), To: w.HandoffEmails}
	return support.NewService(ctx, w, support.NewRepository(db), wa, ai, fulfillments, installerRepo, notifier, tz, log), nil
}

// newMailSender escolhe como os e-mails saem: por SMTP quando configurado;
// senão só para o log (com o conteúdo apenas em desenvolvimento).
func newMailSender(cfg *config.Config, log *slog.Logger) (mail.Sender, error) {
	if !cfg.Mail.Enabled() {
		log.Warn("SMTP_HOST não definido: e-mails de redefinição de senha não serão enviados",
			"app_url", cfg.Mail.AppURL)
		return mail.NewLogSender(log, cfg.Env == "development"), nil
	}
	sender, err := mail.NewSMTPSender(cfg.Mail)
	if err != nil {
		return nil, err
	}
	log.Info("envio de e-mail por SMTP",
		"host", cfg.Mail.SMTPHost, "port", cfg.Mail.SMTPPort, "tls", cfg.Mail.SMTPTLS,
		"app_url", cfg.Mail.AppURL)
	return sender, nil
}

// commandSender adapta o gerenciador de conexões à interface do módulo de
// comandos, traduzindo o erro de "não conectado".
type commandSender struct {
	manager *tcp.Manager
}

func (s commandSender) Send(imei string, payload []byte) error {
	err := s.manager.Send(imei, payload)
	if errors.Is(err, tcp.ErrNotConnected) {
		return commands.ErrNotConnected
	}
	return err
}

func logProtocols(log *slog.Logger, registry *protocols.ProtocolRegistry) {
	for _, descriptor := range registry.Descriptors() {
		level := slog.LevelInfo
		if descriptor.Confidence != protocols.Documented {
			// Quem opera precisa saber que há adaptador não confirmado ativo.
			level = slog.LevelWarn
		}
		log.Log(context.Background(), level, "protocolo registrado",
			"name", descriptor.Name,
			"confidence", descriptor.Confidence,
			"commands", len(descriptor.Commands))
	}
}

func envInt(key string, def int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return def
	}
	return value
}

func envBool(key string, def bool) bool {
	switch os.Getenv(key) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return def
	}
}

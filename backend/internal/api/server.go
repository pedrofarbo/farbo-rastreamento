// Package api expõe a API REST e monta as rotas do painel.
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/accounts"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/adjustment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/affiliates"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/alerts"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/analytics"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/apikeys"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/appointments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/audit"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/auth"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/billing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/busroutes"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/claims"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/closing"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/commands"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/contract"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/dunning"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/events"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/finance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fleet"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/geocoding"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/geofences"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/infra"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/inspections"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/installers"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leads"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/livelinks"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/maintenance"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/melhorenvio"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/organizations"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/reportmail"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/reports"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/retention"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/shares"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/smssetup"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/support"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tcp"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/theft"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/topups"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/twofactor"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/webhooks"
	ws "github.com/pedrofarbo/farbo-rastreamento/backend/internal/websocket"
)

// Deps reúne tudo o que os handlers precisam.
type Deps struct {
	Config     *config.Config
	Log        *slog.Logger
	Metrics    *telemetry.Metrics
	DB         *database.DB
	Auth       *auth.Service
	Devices    *devices.Service
	Vehicles   *vehicles.Service
	Geofences  *geofences.Service
	Geocoder   *geocoding.Service
	Events     *events.Service
	Commands   *commands.Service
	Audit      *audit.Service
	Billing    *billing.Service
	Payments   *payments.Service
	Orders     *orders.Service
	Installers *installers.Repository
	Addresses  *addresses.Repository
	// Acompanhamento do pedido e envio pelo Melhor Envios (Carrier nil =
	// envio não configurado).
	Fulfillment *fulfillment.Service
	// Retention decide por quantos dias guardar o histórico de cada veículo.
	Retention *retention.Service
	// Atendimento pelo WhatsApp (IA e equipe). Nil: rotas sem efeito.
	Support *support.Service
	// Pré-clientes: cadastro de interesse da landing.
	Leads *leads.Service
	// Alertas por e-mail: o motor (e-mail de teste) e as preferências.
	Alerts     *alerts.Engine
	AlertStore *alerts.DBStore
	// Push: notificações no celular do app do cliente.
	Push *push.Service
	// Shares: acessos de terceiros aos veículos dos clientes.
	Shares *shares.Service
	// Accounts: a equipe da conta empresa de cada cliente.
	Accounts *accounts.Service
	// Organizations: as associações e seguradoras (o painel delas, os
	// associados e a fatura do mês). Nil: rotas fora.
	Organizations *organizations.Service
	// Closing: o fechamento do mês e as notas fiscais. Nil: rotas fora.
	Closing *closing.Service
	// Analytics: visitas da landing page (sem cookies).
	Analytics *analytics.Service
	// Infra: a saúde do sistema (máquina, banco, backups, erros).
	Infra *infra.Service
	// Finance: a gestão da empresa (contas, caixa, resultado, estoque).
	Finance *finance.Service
	// Affiliates: o programa de afiliados (links de indicação e comissões).
	Affiliates *affiliates.Service
	// SMSSetup: a configuração do rastreador por SMS (SMS Market) na ativação.
	SMSSetup *smssetup.Service
	// Contract: o contrato que o cliente aceita no primeiro acesso. Nil: sem
	// exigência (testes).
	Contract *contract.Service
	// Dunning: a régua de cobrança (lembretes de fatura e o link de
	// pagamento sem login). Nil: rotas respondem 404.
	Dunning *dunning.Service
	// Reports: os relatórios da frota (resumo, km por dia, viagens,
	// excessos). Nil: as rotas respondem 404.
	Reports *reports.Service
	// ReportMail: os relatórios da frota por e-mail. Nil: rotas fora.
	ReportMail *reportmail.Service
	// Maintenance: o hodômetro e a manutenção dos veículos. Nil: rotas fora.
	Maintenance *maintenance.Service
	// Theft: o modo roubo (o cliente avisa; o rastreador acelera; o link
	// público mostra a posição à polícia). Nil: rotas respondem 404.
	Theft *theft.Service
	// LiveLinks: a localização ao vivo por um tempo, pelo link. Nil: rotas fora.
	LiveLinks *livelinks.Service
	// Claims e Inspections: as ocorrências e as vistorias dos associados.
	// Nil: rotas fora.
	Claims      *claims.Service
	Inspections *inspections.Service
	// Fleet: a gestão da frota (motoristas, custos, multas, locações). Nil:
	// rotas fora.
	Fleet *fleet.Service
	// APIKeys e Webhooks: as integrações (a API /api/v1 e os eventos por
	// POST). Nil: rotas fora.
	APIKeys  *apikeys.Service
	Webhooks *webhooks.Service
	// Appointments: a agenda de instalação e o link do instalador. Nil: rotas fora.
	Appointments *appointments.Service
	// BusRoutes: os itinerários (linhas de ônibus) e o link do passageiro. Nil: rotas fora.
	BusRoutes *busroutes.Service
	// StepUp: confirmação extra (biometria ou senha) antes de ações
	// sensíveis. Nil desliga a exigência (só em testes).
	StepUp       *stepup.Service
	Carrier      *melhorenvio.Client
	CarrierStore *melhorenvio.DBStore
	// TopUps: as recargas da carteira do Melhor Envios (nil sem o envio).
	TopUps *topups.Service
	// TwoFactor: a verificação em duas etapas (nil a desliga, nos testes).
	TwoFactor *twofactor.Service
	// Adjustment: o reajuste anual pelo IPCA (nil: sem a tela).
	Adjustment *adjustment.Service
	Owners     *vehicles.OwnerIndex
	Positions  *tracking.Repository
	States     *tracking.StateStore
	Raw        *tracking.RawPacketRepository
	Ingestor   *tracking.Ingestor
	Conns      *tcp.Manager
	Registry   *protocols.ProtocolRegistry
	WS         *ws.Handler
	Hub        *ws.Hub
}

type Server struct {
	Deps
	router chi.Router
}

func NewServer(deps Deps) *Server {
	s := &Server{Deps: deps}
	if deps.Config != nil {
		trustedProxies = deps.Config.HTTP.TrustedProxies
	}
	if s.WS != nil {
		// O WebSocket entrega a cada cliente só o que é dos veículos dele.
		s.WS.Scope = s.websocketScope
	}
	if s.Geofences != nil {
		s.Geofences.Baseline = s.fenceBaseline
	}
	s.router = s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) routes() chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	// Sem middleware.RealIP: ele troca o RemoteAddr pelo X-Forwarded-For de
	// qualquer cliente. O IP de origem sai de clientIP, que só aceita o
	// cabeçalho vindo de um proxy confiável (TRUSTED_PROXIES).
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(loggingMiddleware(s.Log))
	r.Use(metricsMiddleware(s.Metrics))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.Config.HTTP.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", stepUpHeader},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Observabilidade sem autenticação: fica atrás da rede interna (§28).
	r.Get("/health", s.handleHealth)
	r.Get("/ready", s.handleReady)
	r.Handle("/metrics", s.Metrics.Handler())

	limiter := newRateLimiter(s.Config.HTTP.RateLimitRPS, s.Config.HTTP.RateLimitBurst)
	// O login recebe um limite bem mais apertado que o resto da API.
	loginLimiter := newRateLimiter(0.5, 5)
	// "Esqueci a senha" dispara e-mail: o limite por IP é o mais apertado de
	// todos (além do intervalo mínimo por conta, aplicado no serviço).
	forgotLimiter := newRateLimiter(0.1, 3)
	resetLimiter := newRateLimiter(0.5, 10)
	// Cadastro de interesse da landing: público, poucos envios por IP.
	leadLimiter := newRateLimiter(0.05, 5)
	// Lista de lançamento: mais folgado que o pré-cadastro, porque num evento
	// (QR Code) muita gente se inscreve pela mesma rede, ou no mesmo tablet.
	launchLimiter := newRateLimiter(0.5, 20)
	// A tela do link de indicação e a página do afiliado.
	affiliateLimiter := newRateLimiter(0.5, 30)
	// Visitas da landing: uma página manda a visita, as seções e os cliques.
	analyticsLimiter := newRateLimiter(1, 40)
	// Senha da confirmação extra: além do teto por usuário (stepup).
	stepUpLimiter := newRateLimiter(0.2, 5)
	// O código da verificação em duas etapas: errar uma ou duas vezes não
	// trava; a força bruta esbarra nas 5 tentativas por desafio.
	twoFactorLimiter := newRateLimiter(0.5, 10)
	// O link do modo roubo: a página pergunta a cada 10 s; numa delegacia,
	// várias pessoas podem abrir pela mesma rede.
	theftLinkLimiter := newRateLimiter(2, 60)
	// A API de integração: 5 por segundo, com folga para rajadas.
	apiLimiter := newRateLimiter(5, 60)
	// O link de pagamento: a página pergunta o status do Pix a cada 4 s.
	payLinkLimiter := newRateLimiter(1, 40)

	r.Route("/api", func(r chi.Router) {
		r.Use(limiter.middleware)

		r.Route("/auth", func(r chi.Router) {
			r.With(loginLimiter.middleware).Post("/login", s.handleLogin)
			// Entrar com a biometria do aparelho (Face ID, digital), no app.
			if s.StepUp != nil {
				r.With(loginLimiter.middleware).Post("/biometric/options", s.handleBiometricLoginOptions)
				r.With(loginLimiter.middleware).Post("/biometric", s.handleBiometricLogin)
			}
			r.Post("/refresh", s.handleRefresh)
			r.Post("/logout", s.handleLogout)
			// O segundo fator depois da senha, e a ativação obrigatória da equipe.
			if s.TwoFactor != nil {
				r.Route("/2fa", func(r chi.Router) {
					r.Use(twoFactorLimiter.middleware)
					r.Post("/verify", s.handleTwoFactorVerify)
					r.Post("/resend", s.handleTwoFactorResend)
					r.Post("/setup/email", s.handleTwoFactorSetupEmail)
					r.Post("/setup/confirm", s.handleTwoFactorSetupConfirm)
				})
			}

			r.With(forgotLimiter.middleware).Post("/forgot-password", s.handleForgotPassword)
			r.With(resetLimiter.middleware).Post("/reset-password", s.handleResetPassword)
			r.With(resetLimiter.middleware).Post("/reset-password/validate", s.handleCheckResetToken)
			r.With(s.Auth.Middleware).Get("/me", s.handleMe)
			// Os aparelhos com a conta aberta e sair dos outros.
			r.With(s.Auth.Middleware).Get("/sessions", s.handleMySessions)
			r.With(s.Auth.Middleware).Post("/sessions/sign-out-others", s.handleSignOutOthers)
		})

		// Notificações de pagamento da AbacatePay: sem login, autenticadas
		// pelo segredo e pela assinatura (ver o handler).
		r.Post("/payments/abacatepay/webhook", s.handleAbacatePayWebhook)

		// Prestadores recomendados para a landing page (público).
		r.Get("/public/installers", s.handlePublicInstallers)
		// Visitas da landing (sem cookies nem dados pessoais).
		if s.Analytics != nil {
			r.With(analyticsLimiter.middleware).Post("/public/analytics", s.handlePublicAnalytics)
		}
		// Cadastro de interesse (pré-cliente) da landing.
		// E o "me avise quando lançar" (lista de lançamento).
		if s.Leads != nil {
			r.With(leadLimiter.middleware).Post("/public/leads", s.handlePublicCreateLead)
			r.With(launchLimiter.middleware).Post("/public/launch", s.handlePublicJoinWaitlist)
		}
		// Afiliados: o "Indicado por" da tela de cadastro e a página do
		// afiliado (link secreto).
		if s.Affiliates != nil {
			r.With(affiliateLimiter.middleware).Get("/public/affiliates/{code}", s.handlePublicAffiliate)
			r.With(affiliateLimiter.middleware).Get("/public/partner/{token}", s.handlePartnerReport)
			r.With(affiliateLimiter.middleware).Get("/public/partner/{token}/qr", s.handlePartnerQR)
		}
		// O contrato em vigor, para ler antes de contratar.
		if s.Contract != nil {
			r.Get("/public/contract", s.handlePublicContract)
		}
		// Link de pagamento da fatura (sem login): o lembrete abre o Pix.
		if s.Dunning != nil {
			r.Route("/public/invoices/{token}", func(r chi.Router) {
				r.Use(payLinkLimiter.middleware)
				r.Get("/", s.handlePublicInvoice)
				r.Post("/pix", s.handlePublicInvoicePix)
				r.Post("/pix-group", s.handlePublicInvoiceGroupPix)
				r.Post("/boleto", s.handlePublicInvoiceBoleto)
				r.Post("/boleto-group", s.handlePublicInvoiceGroupBoleto)
				r.Get("/charges/{chargeId}", s.handlePublicCharge)
				r.Post("/charges/{chargeId}/simulate", s.handlePublicChargeSimulate)
			})
		}
		// Modo roubo: a posição ao vivo pelo link secreto (a tela atualiza a
		// cada poucos segundos, aberta por quem recebeu o link).
		if s.Theft != nil {
			r.With(theftLinkLimiter.middleware).Get("/public/theft/{token}", s.handlePublicTheft)
		}
		// A API de integração: lê os veículos, as posições e os eventos da
		// conta da chave.
		if s.APIKeys != nil {
			r.Route("/v1", func(r chi.Router) {
				r.Use(apiLimiter.middleware, s.requireAPIKey)
				r.Get("/vehicles", s.handleAPIVehicles)
				r.Get("/vehicles/{id}/positions", s.handleAPIPositions)
				r.Get("/events", s.handleAPIEvents)
			})
		}
		// O link do passageiro: onde o ônibus da linha está (sem login).
		if s.BusRoutes != nil {
			r.With(theftLinkLimiter.middleware).Get("/public/routes/{token}", s.handlePublicRoute)
		}
		// O link do instalador: o passo a passo da instalação (sem login).
		if s.Appointments != nil {
			r.Route("/public/install/{token}", func(r chi.Router) {
				r.Use(theftLinkLimiter.middleware)
				r.Get("/", s.handlePublicInstall)
				r.Post("/arrive", s.handlePublicInstallArrive)
				r.Post("/check", s.handlePublicInstallCheck)
				r.Put("/photos/{slot}", s.handlePublicInstallPhoto)
				r.Get("/photos/{slot}", s.handlePublicInstallPhotoFile)
				r.Post("/complete", s.handlePublicInstallComplete)
			})
		}
		// A localização ao vivo por um tempo, pelo link que o cliente mandou.
		if s.LiveLinks != nil {
			r.With(theftLinkLimiter.middleware).Get("/public/live/{token}", s.handlePublicLive)
		}

		// Melhor Envios: volta da autorização (o navegador, sem o token do
		// painel; vale pelo state) e webhooks de etiqueta (assinatura HMAC).
		r.Get("/integrations/melhorenvio/callback", s.handleShippingCallback)
		r.Post("/shipping/melhorenvio/webhook", s.handleShippingWebhook)

		// WhatsApp: verificação do webhook (token combinado) e mensagens dos
		// contatos (assinatura HMAC da chave do aplicativo).
		r.Get("/whatsapp/webhook", s.handleWhatsAppVerify)
		r.Post("/whatsapp/webhook", s.handleWhatsAppWebhook)

		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Middleware)
			// Quem é de uma associação ou seguradora só passa nas rotas do
			// painel dela (ver orgGate).
			r.Use(s.orgGate)

			r.Get("/geocoding/reverse", s.handleReverseGeocode)
			r.Get("/catalog", s.handleCatalog)

			// A verificação em duas etapas de quem está conectado (Minha
			// conta → Segurança): fora do contrato e da inadimplência.
			if s.TwoFactor != nil {
				r.Route("/security/two-factor", func(r chi.Router) {
					r.Get("/", s.handleTwoFactorStatus)
					r.With(stepUpLimiter.middleware).Post("/start", s.handleTwoFactorStart)
					r.With(stepUpLimiter.middleware).Post("/confirm", s.handleTwoFactorConfirm)
					r.With(stepUpLimiter.middleware).Post("/code", s.handleTwoFactorActionCode)
					r.With(stepUpLimiter.middleware).Post("/disable", s.handleTwoFactorDisable)
					r.With(stepUpLimiter.middleware).Post("/recovery-codes", s.handleTwoFactorRecoveryCodes)
					r.Delete("/devices", s.handleTwoFactorRevokeDevice)
					r.Delete("/devices/{id}", s.handleTwoFactorRevokeDevice)
				})
			}

			// Confirmação extra de ações sensíveis (desligar o motor): a
			// biometria do aparelho (WebAuthn) ou a senha.
			r.Route("/step-up", func(r chi.Router) {
				if s.StepUp == nil {
					return
				}
				r.Get("/biometrics", s.handleListBiometrics)
				r.With(stepUpLimiter.middleware).Post("/biometrics/options", s.handleBiometricOptions)
				r.Post("/biometrics", s.handleRegisterBiometric)
				r.Delete("/biometrics/{id}", s.handleDeleteBiometric)
				r.Post("/options", s.handleStepUpOptions)
				r.Post("/biometric", s.handleStepUpBiometric)
				r.With(stepUpLimiter.middleware).Post("/password", s.handleStepUpPassword)
			})

			// Veículos: a equipe vê todos; o cliente só os dele (o filtro
			// fica nos handlers, em vehicleFromURL) e perde o acesso se
			// tiver fatura atrasada além do limite.
			r.Route("/vehicles", func(r chi.Router) {
				r.Use(s.requireContract)
				r.Use(s.requireActiveCustomer)
				r.Use(s.requireActiveOrg)

				r.Get("/", s.handleListVehicles)
				// Só veículos da central; os de cliente entram pelo fluxo
				// único (/me/trackers e /customers/{id}/trackers).
				r.With(auth.RequireRole(auth.RoleAdmin)).Post("/", s.handleCreateVehicle)

				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.handleGetVehicle)
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleCustomer), s.requireAccountOperator).Patch("/", s.handleUpdateVehicle)
					r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/", s.handleDeleteVehicle)
					r.With(auth.RequireRole(auth.RoleAdmin)).Put("/history-retention", s.handleSetVehicleRetention)

					r.Get("/position", s.handleVehiclePosition)
					r.Get("/positions", s.handleVehiclePositions)
					r.Get("/events", s.handleVehicleEvents)
					r.Get("/commands", s.handleVehicleCommands)
					// O rastreio de bloqueio e desbloqueio (quem, de qual aparelho, como confirmou).
					r.Get("/engine-history", s.handleEngineHistory)
					// O hodômetro e a manutenção: a leitura, quem mexe na conta.
					if s.Maintenance != nil {
						r.Get("/maintenance", s.handleVehicleMaintenance)
						r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleCustomer), s.requireAccountOperator).Put("/odometer", s.handleSetOdometer)
						r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleCustomer), s.requireAccountOperator).Put("/engine-hours", s.handleSetEngineHours)
					}

					// Bloqueio, desbloqueio e pedidos de posição/status: equipe
					// operacional e o próprio dono do veículo, com a mesma
					// trava de segurança do corte.
					r.Group(func(r chi.Router) {
						r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator, auth.RoleCustomer, auth.RoleOrgMember))
						// Da equipe de uma conta, só o Gestor (o Visualizador acompanha);
						// da organização, o Administrador e o Operador.
						r.Use(s.requireAccountOperator, s.requireOrgOperator)
						r.Post("/commands/engine-cut", s.handleEngineCut)
						r.Get("/commands/engine-cut/check", s.handleEngineCutCheck)
						r.Post("/commands/engine-resume", s.handleEngineResume)
						r.Post("/commands/request-position", s.handleRequestPosition)
						r.Post("/commands/request-status", s.handleRequestStatus)
					})
					// Comandos de configuração e texto livre ficam com a central.
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)).Post("/commands", s.handleGenericCommand)

					// Modo roubo: o dono e quem pode bloquear ligam; só o dono
					// (com a biometria ou a senha) desliga.
					r.Get("/theft", s.handleTheftStatus)
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator, auth.RoleCustomer, auth.RoleOrgMember),
						s.requireAccountOperator, s.requireOrgOperator).Post("/theft", s.handleTheftActivate)
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleCustomer), s.requireAccountOperator).Post("/theft/end", s.handleTheftEnd)

					// A localização ao vivo por um tempo: o dono (e o Gestor da
					// conta) gera e encerra; a central vê e encerra.
					if s.LiveLinks != nil {
						r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator, auth.RoleCustomer)).Get("/live-links", s.handleVehicleLiveLinks)
						r.With(auth.RequireRole(auth.RoleCustomer), s.requireAccountOperator).Post("/live-links", s.handleCreateLiveLink)
						r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleCustomer), s.requireAccountOperator).Delete("/live-links/{linkId}", s.handleRevokeLiveLink)
					}
				})
			})

			// Cercas: a equipe vê as da central (só o admin mexe); o cliente
			// cria e vê as dele, para os veículos dele (o filtro de dono fica
			// nos handlers, em fenceOwner).
			r.Route("/geofences", func(r chi.Router) {
				r.Use(s.requireContract)
				r.Use(s.requireActiveCustomer)
				r.Get("/", s.handleListGeofences)
				r.Get("/limit", s.handleGeofenceLimit)
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleCustomer))
					r.Use(s.requireAccountOperator)
					r.Post("/", s.handleCreateGeofence)
					r.Patch("/{id}", s.handleUpdateGeofence)
					r.Delete("/{id}", s.handleDeleteGeofence)
				})
			})

			// Relatórios da frota: o cliente (titular e equipe) vê os da conta;
			// a central, os de um cliente (?cliente=).
			r.Route("/reports", func(r chi.Router) {
				r.Use(s.requireContract)
				r.Use(s.requireActiveCustomer)
				r.Use(s.requireActiveOrg)
				r.Get("/summary", s.handleReportSummary)
				r.Get("/daily", s.handleReportDaily)
				r.Get("/trips", s.handleReportTrips)
				r.Get("/overspeed", s.handleReportOverspeed)
				r.Get("/usage", s.handleReportUsage)
				r.Get("/driving", s.handleReportDriving)
				r.Get("/journey", s.handleReportJourney)
				r.Get("/costs", s.handleReportCosts)
			})

			// Área do cliente: conta e faturas continuam acessíveis mesmo com
			// o acesso suspenso — é por aqui que ele vê o que pagar.
			r.Route("/me", func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleCustomer))
				// Sem o aceite do contrato, só o próprio contrato (ler e aceitar).
				r.Use(s.requireContract)
				// Conta empresa: o titular faz tudo; o Gestor, menos a própria
				// conta (equipe, cadastro, contrato, pedidos); o Visualizador só
				// acompanha (sem faturas).
				r.Get("/contract", s.handleMyContract)
				r.With(s.requireAccountOwner).Post("/contract/accept", s.handleAcceptContract)
				if s.Affiliates != nil {
					// Indique e ganhe: o link do cliente e os créditos.
					r.With(s.requireAccountOwner).Get("/referral", s.handleMyReferral)
				}
				// As integrações (chaves da API e webhooks): o titular.
				if s.APIKeys != nil && s.Webhooks != nil {
					r.Group(func(r chi.Router) {
						r.Use(s.requireAccountOwner)
						s.integrationRoutes(r)
					})
				}
				r.Get("/account", s.handleMyAccount)
				r.Get("/subscriptions", s.handleMySubscriptions)
				if s.Appointments != nil {
					r.Get("/installations", s.handleMyInstallations)
				}
				// O associado: as ocorrências e as vistorias (o handler confere
				// que é associado e que é dele).
				if s.Claims != nil {
					r.Get("/claims", s.handleMyClaims)
					r.Post("/claims", s.handleOpenClaim)
					r.Get("/claims/{claimId}", s.handleGetClaim)
					r.Post("/claims/{claimId}/files", s.handleAddClaimFile)
					r.Get("/claims/{claimId}/files/{fileId}", s.handleClaimFile)
					r.Post("/claims/{claimId}/notes", s.handleClaimNote)
				}
				if s.Inspections != nil {
					r.Get("/inspections", s.handleMyInspections)
					r.Get("/inspections/{inspectionId}", s.handleGetInspection)
					r.Put("/inspections/{inspectionId}/photos/{slot}", s.handleSaveInspectionPhoto)
					r.Get("/inspections/{inspectionId}/photos/{slot}", s.handleInspectionPhoto)
					r.Post("/inspections/{inspectionId}/submit", s.handleSubmitInspection)
				}
				r.Group(func(r chi.Router) {
					r.Use(s.requireAccountOperator)
					// Da conta inteira: quem está restrito a grupos não vê.
					r.Use(s.requireWholeAccount)
					r.Get("/invoices", s.handleMyInvoices)
					r.Post("/invoices/{id}/pix", s.handleMyInvoicePix)
					r.Post("/invoices/{id}/boleto", s.handleMyInvoiceBoleto)
					// Várias de uma vez: a fatura do mês da frota, ou as vencidas.
					r.Post("/invoices/pix", s.handleMyInvoicesPix)
					r.Post("/invoices/boleto", s.handleMyInvoicesBoleto)
					// O cartão: pagar, o cartão salvo e o débito automático.
					r.Post("/invoices/{id}/card", s.handleMyInvoiceCard)
					r.Post("/invoices/card", s.handleMyInvoicesCard)
					r.Get("/card", s.handleMyCard)
					r.Put("/card/auto-debit", s.handleMyCardAutoDebit)
					r.Delete("/card", s.handleMyCardRemove)
					r.Get("/charges/{id}", s.handleMyCharge)
					r.Post("/charges/{id}/simulate", s.handleMyChargeSimulate)
					// Acompanhamento do chip e do rastreador até a casa dele.
					r.Get("/fulfillments", s.handleMyFulfillments)
					// As notas fiscais do fechamento de cada mês.
					if s.Closing != nil {
						r.Get("/fiscal-notes", s.handleMyFiscalNotes)
						r.Get("/fiscal-notes/{id}/file", s.handleMyFiscalNoteFile)
					}
				})
				r.Group(func(r chi.Router) {
					r.Use(s.requireAccountOwner)
					r.Put("/address", s.handleSaveMyAddress)
					// Meus dados: nome, telefone e CPF/CNPJ.
					r.Put("/profile", s.handleUpdateMyProfile)
					// Novo veículo (veículo → rastreador → assinatura): bloqueado com o
					// acesso suspenso.
					r.With(s.requireActiveCustomer).Post("/trackers", s.handleMyOrderTracker)
					r.Get("/shipping-quote", s.handleMyShippingQuote)
					// Assinatura antiga sem veículo: o cliente informa qual é.
					r.Post("/subscriptions/{subscriptionId}/vehicle", s.handleMyAttachVehicle)
					// A equipe da conta empresa.
					if s.Accounts != nil {
						r.Get("/team", s.handleMyTeam)
						r.Get("/team/limit", s.handleMyTeamLimit)
						r.With(s.requireActiveCustomer).Post("/team", s.handleAddTeamMember)
						r.Patch("/team/{id}", s.handleUpdateTeamMember)
						r.Delete("/team/{id}", s.handleRemoveTeamMember)
					}
				})
				// Alertas por e-mail: escolhas, histórico e e-mail de teste.
				r.Get("/alerts", s.handleMyAlerts)
				r.Put("/alerts", s.handleSaveMyAlerts)
				r.Post("/alerts/test", s.handleMyAlertsTest)
				r.With(s.requireAccountOperator, s.requireWholeAccount).Put("/alerts/recipients", s.handleSaveMyAlertRecipients)
				// A organização da frota: grupos e campos personalizados (quem
				// está restrito a grupos vê só os dele e não mexe).
				r.Get("/fleet", s.handleMyFleet)
				r.Group(func(r chi.Router) {
					r.Use(s.requireAccountOperator, s.requireWholeAccount)
					r.Post("/fleet/groups", s.handleCreateFleetGroup)
					r.Patch("/fleet/groups/{id}", s.handleRenameFleetGroup)
					r.Delete("/fleet/groups/{id}", s.handleDeleteFleetGroup)
					r.Put("/fleet/labels", s.handleSetFleetLabels)
				})
				// A manutenção da frota: o que vence, por km ou por data.
				if s.Maintenance != nil {
					r.Get("/maintenance", s.handleMyMaintenance)
					r.Group(func(r chi.Router) {
						r.Use(s.requireAccountOperator)
						r.Post("/maintenance", s.handleCreateMaintenance)
						r.Post("/maintenance/done", s.handleMaintenanceDone)
						r.Patch("/maintenance/{id}", s.handleUpdateMaintenance)
						r.Delete("/maintenance/{id}", s.handleDeleteMaintenance)
					})
				}
				// A gestão da frota: motoristas, custos, multas e locações (cada
				// um com os veículos que vê; lançar é de quem opera a conta).
				if s.Fleet != nil {
					r.Get("/drivers", s.handleDrivers)
					r.Get("/expenses", s.handleExpenses)
					r.Get("/fines", s.handleFines)
					r.Get("/rentals", s.handleRentals)
					if s.BusRoutes != nil {
						r.Get("/routes", s.handleRoutes)
						r.Get("/routes/{id}", s.handleGetRoute)
						r.Get("/routes/{id}/day", s.handleRouteDay)
						r.Group(func(r chi.Router) {
							r.Use(s.requireAccountOperator)
							r.Post("/routes", s.handleSaveRoute)
							r.Put("/routes/{id}", s.handleSaveRoute)
							r.Delete("/routes/{id}", s.handleDeleteRoute)
							r.Post("/routes/{id}/trips", s.handleCreateTrips)
							r.Post("/trips/{tripId}/cancel", s.handleCancelTrip)
						})
					}
					r.Group(func(r chi.Router) {
						r.Use(s.requireAccountOperator)
						r.Post("/drivers", s.handleCreateDriver)
						r.Patch("/drivers/{id}", s.handleUpdateDriver)
						r.Put("/vehicles/{vehicleId}/driver", s.handleAssignDriver)
						r.Post("/expenses", s.handleCreateExpense)
						r.Delete("/expenses/{id}", s.handleDeleteExpense)
						r.Post("/fines", s.handleCreateFine)
						r.Patch("/fines/{id}", s.handleUpdateFine)
						r.Delete("/fines/{id}", s.handleDeleteFine)
						r.Post("/rentals", s.handleCreateRental)
						r.Post("/rentals/{id}/return", s.handleReturnRental)
						r.Post("/rentals/{id}/extend", s.handleExtendRental)
					})
				}
				// Os relatórios da frota por e-mail (semanal ou mensal).
				if s.ReportMail != nil {
					r.Get("/report-schedules", s.handleMyReportSchedules)
					r.Post("/report-schedules", s.handleCreateReportSchedule)
					r.Delete("/report-schedules/{id}", s.handleDeleteReportSchedule)
				}
				// Notificações no celular (app do cliente).
				r.Get("/push", s.handleMyPush)
				r.Post("/push/subscriptions", s.handleMyPushSubscribe)
				r.Post("/push/unsubscribe", s.handleMyPushUnsubscribe)
				r.Post("/push/test", s.handleMyPushTest)
				// A área de notificações do app: o que foi avisado, lido ou não.
				r.Get("/notifications", s.handleMyNotifications)
				r.Post("/notifications/read", s.handleMyNotificationsRead)

				// Acessos de terceiros aos veículos do cliente.
				if s.Shares != nil {
					r.Get("/shares", s.handleMyShares)
					r.With(s.requireAccountOwner, s.requireActiveCustomer).Post("/shares", s.handleCreateShare)
					r.With(s.requireAccountOwner).Patch("/shares/{id}", s.handleUpdateShare)
					r.Delete("/shares/{id}", s.handleRemoveShare)
				}
			})

			// O painel da associação ou seguradora: os associados, a equipe e
			// as faturas (o mapa e os veículos vêm de /vehicles, filtrados).
			if s.Organizations != nil {
				r.Route("/org", func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleOrgMember), s.requireOrgMember)
					r.Get("/", s.handleOrgOverview)
					r.Group(func(r chi.Router) {
						r.Use(s.requireActiveOrg)
						// Os associados: o corretor vê e cadastra só os dele.
						r.Get("/associates", s.handleOrgAssociates)
						r.Get("/associates/{associateId}", s.handleOrgAssociate)
						r.Group(func(r chi.Router) {
							r.Use(s.requireOrgRegistrar)
							r.Post("/associates", s.handleOrgCreateAssociate)
							r.Patch("/associates/{associateId}", s.handleOrgUpdateAssociate)
							r.Post("/associates/{associateId}/invite", s.handleOrgInviteAssociate)
							r.Post("/associates/{associateId}/vehicles", s.handleOrgRegisterVehicle)
						})
						// O resto do painel é da equipe (não do corretor).
						r.Group(func(r chi.Router) {
							r.Use(s.requireOrgStaff)
							r.Get("/recoveries", s.handleOrgRecoveries)
							r.Get("/announcements", s.handleOrgAnnouncements)
							if s.Appointments != nil {
								r.Get("/installations", s.handleOrgInstallations)
							}
							if s.Claims != nil {
								r.Get("/claims", s.handleOrgClaims)
								r.Get("/claims/{claimId}", s.handleGetClaim)
								r.Get("/claims/{claimId}/files/{fileId}", s.handleClaimFile)
							}
							if s.Inspections != nil {
								r.Get("/inspections", s.handleOrgInspections)
								r.Get("/inspections/{inspectionId}", s.handleGetInspection)
								r.Get("/inspections/{inspectionId}/photos/{slot}", s.handleInspectionPhoto)
							}
						})
						r.Group(func(r chi.Router) {
							r.Use(s.requireOrgOperator)
							r.Post("/associates/import", s.handleOrgImportAssociates)
							// O pedido de desinstalação (a central agenda a retirada).
							if s.Appointments != nil {
								r.Post("/vehicles/{vehicleId}/uninstall", s.handleOrgRequestUninstall)
								r.Delete("/vehicles/{vehicleId}/uninstall", s.handleOrgCancelUninstall)
							}
							r.Post("/announcements", s.handleOrgAnnounce)
							if s.Claims != nil {
								r.Post("/claims/{claimId}/status", s.handleOrgClaimStatus)
								r.Post("/claims/{claimId}/notes", s.handleClaimNote)
								r.Post("/claims/{claimId}/files", s.handleAddClaimFile)
							}
							if s.Inspections != nil {
								r.Post("/inspections", s.handleOrgRequestInspection)
								r.Post("/inspections/{inspectionId}/review", s.handleOrgReviewInspection)
							}
						})
					})
					// A equipe e as faturas: o Administrador (pagar continua
					// aberto com o painel suspenso).
					r.Group(func(r chi.Router) {
						r.Use(s.requireOrgAdmin)
						if s.APIKeys != nil && s.Webhooks != nil {
							r.With(s.requireActiveOrg).Group(s.integrationRoutes)
						}
						r.Get("/team", s.handleOrgTeam)
						r.With(s.requireActiveOrg).Post("/team", s.handleOrgAddMember)
						r.Patch("/team/{userId}", s.handleOrgSetMemberRole)
						r.Delete("/team/{userId}", s.handleOrgRemoveMember)
						r.Post("/team/{userId}/invite", s.handleOrgInviteMember)
						r.Get("/invoices", s.handleOrgInvoices)
						r.Post("/invoices/{id}/pix", s.handleOrgInvoicePix)
						r.Post("/invoices/{id}/boleto", s.handleOrgInvoiceBoleto)
						r.Get("/charges/{id}", s.handleOrgCharge)
						r.Post("/charges/{id}/simulate", s.handleOrgChargeSimulate)
						if s.Closing != nil {
							r.Get("/fiscal-notes", s.handleOrgFiscalNotes)
							r.Get("/fiscal-notes/{id}/file", s.handleOrgFiscalNoteFile)
						}
					})
				})
			}

			// Daqui para baixo, só a equipe da central.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator, auth.RoleViewer))

				r.Get("/protocols", s.handleListProtocols)
				r.Get("/events", s.handleRecentEvents)

				r.Route("/devices", func(r chi.Router) {
					r.Get("/", s.handleListDevices)
					r.With(auth.RequireRole(auth.RoleAdmin)).Post("/", s.handleCreateDevice)
					r.With(auth.RequireRole(auth.RoleAdmin)).Post("/bulk", s.handleCreateDevicesBulk)
					r.With(auth.RequireRole(auth.RoleAdmin)).Get("/unknown", s.handleUnknownDevices)
					if s.SMSSetup != nil {
						r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)).Post("/bulk/sms-setup", s.handleStartSMSSetupBulk)
					}

					r.Route("/{id}", func(r chi.Router) {
						r.Get("/", s.handleGetDevice)
						r.With(auth.RequireRole(auth.RoleAdmin)).Patch("/", s.handleUpdateDevice)
						r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/", s.handleDeleteDevice)
						r.Get("/status", s.handleDeviceStatus)
						r.Get("/commands", s.handleDeviceCommands)
						// Configuração do aparelho: só o admin (a tela de
						// rastreadores também é só dele).
						r.With(auth.RequireRole(auth.RoleAdmin)).Get("/provisioning", s.handleDeviceProvisioning)
						// Configuração por SMS na ativação: quem avança os pedidos.
						if s.SMSSetup != nil {
							r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)).Get("/sms-setup", s.handleDeviceSMSSetup)
							r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)).Post("/sms-setup", s.handleStartSMSSetup)
						}
					})
				})

				// Infraestrutura: máquina, banco, backups e erros (admin).
				if s.Infra != nil {
					r.Route("/infra", func(r chi.Router) {
						r.Use(auth.RequireRole(auth.RoleAdmin))
						r.Get("/status", s.handleInfraStatus)
						r.Get("/logs", s.handleInfraLogs)
					})
				}

				// Gestão da empresa: contas a pagar e receitas, caixa,
				// resultado do mês e estoque (admin).
				if s.Finance != nil {
					r.Route("/finance", func(r chi.Router) {
						r.Use(auth.RequireRole(auth.RoleAdmin))
						r.Get("/alerts", s.handleFinanceAlerts)
						r.Get("/overview", s.handleFinanceOverview)
						r.Get("/cashflow", s.handleFinanceCashFlow)
						r.Get("/dre", s.handleFinanceDRE)
						r.Get("/customer-payments", s.handleCustomerPayments)
						r.Put("/settings", s.handleFinanceSettings)

						r.Get("/entries", s.handleListEntries)
						r.Post("/entries", s.handleCreateEntries)
						r.Route("/entries/{id}", func(r chi.Router) {
							r.Patch("/", s.handleUpdateEntry)
							r.Delete("/", s.handleDeleteEntry)
							r.Post("/pay", s.handlePayEntry)
							r.Post("/reopen", s.handleReopenEntry)
							r.Post("/cancel", s.handleCancelEntry)
							r.Post("/attachments", s.handleAddAttachment)
							r.Get("/pix", s.handleEntryPixPlan)
							r.Post("/pix", s.handleSendEntryPix)
						})
						r.Get("/pix", s.handleFinancePixInfo)
						r.Get("/balance", s.handleGatewayBalance)
						r.Post("/pix/{id}/resolve", s.handleResolvePix)
						r.Get("/attachments/{id}", s.handleGetAttachment)
						r.Delete("/attachments/{id}", s.handleDeleteAttachment)

						r.Get("/recurrences", s.handleListRecurrences)
						r.Post("/recurrences", s.handleCreateRecurrence)
						r.Patch("/recurrences/{id}", s.handleUpdateRecurrence)
						r.Post("/recurrences/{id}/end", s.handleEndRecurrence)

						r.Get("/categories", s.handleListFinanceCategories)
						r.Post("/categories", s.handleSaveFinanceCategory)
						r.Patch("/categories/{id}", s.handleSaveFinanceCategory)
						r.Get("/suppliers", s.handleListSuppliers)
						r.Post("/suppliers", s.handleSaveSupplier)
						r.Patch("/suppliers/{id}", s.handleSaveSupplier)

						r.Get("/stock/items", s.handleListStockItems)
						r.Post("/stock/items", s.handleSaveStockItem)
						r.Patch("/stock/items/{id}", s.handleSaveStockItem)
						r.Get("/stock/movements", s.handleListStockMovements)
						r.Post("/stock/movements", s.handleMoveStock)
					})
				}

				// Programa de afiliados (admin): links, valores e fechamentos.
				if s.Affiliates != nil {
					r.Route("/affiliates", func(r chi.Router) {
						r.Use(auth.RequireRole(auth.RoleAdmin))
						r.Get("/", s.handleListAffiliates)
						r.Post("/", s.handleCreateAffiliate)
						r.Get("/settings", s.handleAffiliateSettings)
						r.Put("/settings", s.handleSaveAffiliateSettings)
						r.Get("/closing", s.handleAffiliateClosingPreview)
						r.Post("/closing", s.handleAffiliateClose)
						r.Get("/payouts", s.handleAffiliatePayouts)
						r.Delete("/payouts/{id}", s.handleUndoAffiliatePayout)
						r.Patch("/{id}", s.handleUpdateAffiliate)
						r.Post("/{id}/report-token", s.handleAffiliateToken)
					})
				}

				// Visitas da landing page (admin).
				if s.Analytics != nil {
					r.With(auth.RequireRole(auth.RoleAdmin)).Get("/analytics/landing", s.handleLandingAnalytics)
				}

				// Equipe da central: quem entra no painel e com que perfil.
				r.Route("/users", func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin))
					r.Get("/", s.handleListUsers)
					r.Post("/", s.handleCreateUser)
					r.Patch("/{id}", s.handleUpdateUser)
					r.Post("/{id}/invite", s.handleInviteUser)
					if s.TwoFactor != nil {
						r.Post("/{id}/two-factor/reset", s.handleResetTwoFactor)
					}
				})

				// Clientes, assinaturas e faturas (administração).
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin))

					// As opções do pedido do cliente: o comodato e a instalação Farbo.
					r.Get("/order-options", s.handleOrderOptions)
					r.Put("/order-options", s.handleSetOrderOptions)

					// O reajuste anual pelo IPCA: o próximo, os anteriores e o veto.
					if s.Adjustment != nil {
						r.Get("/price-adjustments", s.handlePriceAdjustments)
						r.Post("/price-adjustments/{year}/cancel", s.handleCancelPriceAdjustment)
					}

					r.Route("/customers", func(r chi.Router) {
						r.Get("/", s.handleListCustomers)
						r.Post("/", s.handleCreateCustomer)
						r.Get("/mrr", s.handleCustomersMRR)
						r.Route("/{id}", func(r chi.Router) {
							r.Get("/", s.handleGetCustomer)
							r.Patch("/", s.handleUpdateCustomer)
							if s.TwoFactor != nil {
								r.Post("/two-factor/reset", s.handleResetTwoFactor)
							}
							r.Put("/address", s.handleSaveCustomerAddress)
							r.Put("/history-retention", s.handleSetCustomerRetention)
							r.Get("/fleet", s.handleCustomerFleet)
							r.Get("/alerts", s.handleGetCustomerAlerts)
							// A equipe da conta empresa do cliente (só leitura).
							if s.Accounts != nil {
								r.Get("/team", s.handleCustomerTeam)
							}
							r.Put("/alerts", s.handleSaveCustomerAlerts)
							r.Post("/invite", s.handleInviteCustomer)
							r.Post("/invite-link", s.handleCustomerInviteLink)
							// As sessões do cliente: ver, desconectar todas e a senha nova.
							r.Get("/sessions", s.handleCustomerSessions)
							r.Post("/sessions/sign-out", s.handleCustomerSignOut)
							r.Post("/password", s.handleSetCustomerPassword)
							r.Post("/invoices", s.handleCreateInvoice)
							r.Post("/trackers", s.handleAdminOrderTracker)
							r.Post("/trackers/import", s.handleAdminImportFleet)
							r.Get("/shipping-quote", s.handleCustomerShippingQuote)
							r.Get("/launch-promo", s.handleCustomerLaunchPromo)
							r.Post("/launch-promo/grant", s.handleGrantLaunchPromo)
							r.Put("/plan", s.handleSetCustomerPlan)
							// A associação ou seguradora que paga o rastreamento do cliente.
							if s.Organizations != nil {
								r.Put("/organization", s.handleSetCustomerOrganization)
							}
							r.Delete("/plan", s.handleClearCustomerPlan)
							// O cartão salvo: a central só desliga o débito ou apaga.
							r.Put("/card/auto-debit", s.handleCustomerCardAutoDebit)
							r.Delete("/card", s.handleCustomerCardRemove)
							r.Delete("/launch-promo/grant", s.handleRevokeLaunchPromo)
							if s.Affiliates != nil {
								r.Put("/affiliate", s.handleSetCustomerAffiliate)
							}
							r.Post("/subscriptions/{subscriptionId}/vehicle", s.handleAdminAttachVehicle)
							r.Post("/vehicles/{vehicleId}/subscription", s.handleReactivateSubscription)
						})
					})
					// O fechamento do mês e as notas fiscais.
					r.Route("/closing", func(r chi.Router) {
						if s.Closing == nil {
							return
						}
						r.Get("/", s.handleClosing)
						r.Get("/export.csv", s.handleClosingCSV)
						r.Post("/notes", s.handleSaveFiscalNote)
						r.Delete("/notes/{id}", s.handleDeleteFiscalNote)
						r.Get("/notes/{id}/file", s.handleFiscalNoteFile)
					})
					// As associações e seguradoras.
					r.Route("/organizations", func(r chi.Router) {
						if s.Organizations == nil {
							return
						}
						r.Get("/", s.handleListOrganizations)
						r.Post("/", s.handleCreateOrganization)
						r.Route("/{orgId}", func(r chi.Router) {
							r.Get("/", s.handleGetOrganization)
							r.Patch("/", s.handleUpdateOrganization)
							r.Post("/members", s.handleAdminOrgAddMember)
							r.Patch("/members/{userId}", s.handleAdminOrgSetMemberRole)
							r.Delete("/members/{userId}", s.handleAdminOrgRemoveMember)
							r.Post("/members/{userId}/invite", s.handleAdminOrgInviteMember)
							r.Post("/associates", s.handleAdminOrgCreateAssociate)
							r.Post("/associates/import", s.handleAdminOrgImportAssociates)
							r.Post("/associates/{associateId}/vehicles", s.handleAdminOrgRegisterVehicle)
							r.Post("/invoices/generate", s.handleGenerateOrgInvoice)
						})
					})
					r.Route("/leads", func(r chi.Router) {
						if s.Leads == nil {
							return
						}
						r.Get("/", s.handleListLeads)
						r.Get("/stats", s.handleLeadStats)
						r.Get("/waitlist", s.handleListWaitlist)
						r.Get("/promo", s.handleLaunchPromoUsage)
						r.Get("/qr", s.handleLeadQR)
						r.Delete("/waitlist/{id}", s.handleRemoveFromWaitlist)
						r.Delete("/{id}", s.handleDeleteLead)
						r.Delete("/{id}/launch-list", s.handleRemoveLeadFromLaunch)
						r.Patch("/{id}", s.handleUpdateLead)
					})
					r.Patch("/subscriptions/{id}", s.handleUpdateSubscription)
					r.Post("/subscriptions/{id}/cancel", s.handleCancelSubscription)
					r.Post("/subscriptions/{id}/due-day", s.handleChangeDueDay)
					r.Patch("/invoices/{id}", s.handleUpdateInvoice)
					r.Post("/invoices/{id}/due-date", s.handleChangeInvoiceDueDate)
					r.Post("/invoices/{id}/installments", s.handleFinanceInvoice)
					r.Post("/invoices/{id}/pay", s.handlePayInvoice)
					r.Post("/invoices/{id}/cancel", s.handleCancelInvoice)
					r.Post("/invoices/{id}/pix", s.handleAdminInvoicePix)
					r.Post("/invoices/{id}/boleto", s.handleAdminInvoiceBoleto)
					r.Post("/invoices/{id}/remind", s.handleRemindInvoice)
					r.Get("/charges/{id}", s.handleAdminCharge)
					r.Post("/charges/{id}/simulate", s.handleAdminChargeSimulate)
					r.Post("/charges/{id}/refund", s.handleAdminChargeRefund)
				})

				// Pedidos: o chip e o rastreador até o cliente. A equipe
				// operacional avança as etapas; comprar etiqueta (gasta saldo)
				// e conectar o Melhor Envios é do admin.
				if s.SMSSetup != nil {
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)).Post("/sms-setup/{id}/cancel", s.handleCancelSMSSetup)
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)).Get("/sms/usage", s.handleSMSUsage)
				}
				r.Route("/fulfillments", func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator))
					r.Get("/", s.handleListFulfillments)
					r.Get("/{id}", s.handleGetFulfillment)
					r.Post("/{id}/status", s.handleChangeFulfillment)
					r.Post("/{id}/shipping/sync", s.handleSyncShipping)
					// A etiqueta comprada, em PDF.
					r.Get("/{id}/shipping/label.pdf", s.handleLabelPDF)
					r.With(auth.RequireRole(auth.RoleAdmin)).Post("/{id}/shipping/quote", s.handleQuoteShipping)
					r.With(auth.RequireRole(auth.RoleAdmin)).Post("/{id}/shipping/label", s.handleBuyLabel)
				})
				r.Route("/integrations/melhorenvio", func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin))
					r.Get("/", s.handleShippingIntegration)
					r.Post("/connect", s.handleConnectShipping)
					r.Post("/disconnect", s.handleDisconnectShipping)
					// A carteira: o saldo que paga as etiquetas e a recarga.
					r.Get("/balance", s.handleShippingBalance)
					r.Post("/balance", s.handleAddShippingBalance)
					// A recarga por Pix vira conta a pagar, paga pela AbacatePay.
					r.Post("/balance/{id}/entry", s.handleTopUpEntry)
				})

				// Atendimento pelo WhatsApp: as conversas da IA e da equipe.
				r.Route("/whatsapp", func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator))
					r.Get("/", s.handleWhatsAppStatus)
					if s.Support == nil {
						return
					}
					r.Get("/conversations", s.handleListConversations)
					r.Get("/conversations/{id}", s.handleGetConversation)
					r.Post("/conversations/{id}/messages", s.handleSendWhatsApp)
					r.Put("/conversations/{id}/mode", s.handleSetConversationMode)
				})

				// A agenda de instalação: a equipe operacional agenda e acompanha.
				if s.Appointments != nil {
					r.Route("/installations", func(r chi.Router) {
						r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator))
						r.Get("/", s.handleInstallations)
						r.Get("/awaiting", s.handleAwaitingInstall)
						r.Get("/awaiting-uninstall", s.handleAwaitingUninstall)
						r.Post("/", s.handleCreateInstallation)
						r.Patch("/{id}", s.handleRescheduleInstallation)
						r.Post("/{id}/close", s.handleCloseInstallation)
						r.Get("/{id}/photos/{slot}", s.handleInstallationPhoto)
					})
				}

				r.Route("/installers", func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin))
					r.Get("/", s.handleListInstallers)
					r.Post("/", s.handleCreateInstaller)
					r.Patch("/{id}", s.handleUpdateInstaller)
					r.Delete("/{id}", s.handleDeleteInstaller)
				})

				r.Route("/diagnostics", func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin))
					r.Get("/connections", s.handleConnections)
					r.Get("/raw-packets", s.handleRawPackets)
					r.Get("/audit-logs", s.handleAuditLogs)
				})
			})
		})
	})

	// O WebSocket autentica pelo mesmo middleware, lendo ?token= porque o
	// navegador não permite header na abertura da conexão.
	// Cliente suspenso também não recebe o tempo real.
	r.With(s.Auth.Middleware, s.requireActiveCustomer, s.requireActiveOrg).Handle("/ws", s.WS)

	return r
}

// ListenAndServe sobe o servidor HTTP e encerra junto com o contexto.
func (s *Server) ListenAndServe(ctx context.Context) error {
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", s.Config.HTTP.Port),
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       s.Config.HTTP.ReadTimeout,
		WriteTimeout:      s.Config.HTTP.WriteTimeout,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.Log.Info("API HTTP no ar", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx), s.Config.HTTP.ShutdownTimeout)
		defer cancel()
		s.Log.Info("encerrando API HTTP")
		return server.Shutdown(shutdownCtx)
	}
}

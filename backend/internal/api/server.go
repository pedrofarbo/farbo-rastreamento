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

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/addresses"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/alerts"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/analytics"
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
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/installers"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/leads"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/melhorenvio"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/payments"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/push"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/retention"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/shares"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/stepup"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/support"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tcp"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/telemetry"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/tracking"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/vehicles"
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
	// Analytics: visitas da landing page (sem cookies).
	Analytics *analytics.Service
	// StepUp: confirmação extra (biometria ou senha) antes de ações
	// sensíveis. Nil desliga a exigência (só em testes).
	StepUp       *stepup.Service
	Carrier      *melhorenvio.Client
	CarrierStore *melhorenvio.DBStore
	Owners       *vehicles.OwnerIndex
	Positions    *tracking.Repository
	States       *tracking.StateStore
	Raw          *tracking.RawPacketRepository
	Ingestor     *tracking.Ingestor
	Conns        *tcp.Manager
	Registry     *protocols.ProtocolRegistry
	WS           *ws.Handler
	Hub          *ws.Hub
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
	// Visitas da landing: uma página manda a visita, as seções e os cliques.
	analyticsLimiter := newRateLimiter(1, 40)
	// Senha da confirmação extra: além do teto por usuário (stepup).
	stepUpLimiter := newRateLimiter(0.2, 5)

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

			r.With(forgotLimiter.middleware).Post("/forgot-password", s.handleForgotPassword)
			r.With(resetLimiter.middleware).Post("/reset-password", s.handleResetPassword)
			r.With(resetLimiter.middleware).Post("/reset-password/validate", s.handleCheckResetToken)
			r.With(s.Auth.Middleware).Get("/me", s.handleMe)
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
			r.With(leadLimiter.middleware).Post("/public/launch", s.handlePublicJoinWaitlist)
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

			r.Get("/geocoding/reverse", s.handleReverseGeocode)
			r.Get("/catalog", s.handleCatalog)

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
				r.Use(s.requireActiveCustomer)

				r.Get("/", s.handleListVehicles)
				// Só veículos da central; os de cliente entram pelo fluxo
				// único (/me/trackers e /customers/{id}/trackers).
				r.With(auth.RequireRole(auth.RoleAdmin)).Post("/", s.handleCreateVehicle)

				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.handleGetVehicle)
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleCustomer)).Patch("/", s.handleUpdateVehicle)
					r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/", s.handleDeleteVehicle)
					r.With(auth.RequireRole(auth.RoleAdmin)).Put("/history-retention", s.handleSetVehicleRetention)

					r.Get("/position", s.handleVehiclePosition)
					r.Get("/positions", s.handleVehiclePositions)
					r.Get("/events", s.handleVehicleEvents)
					r.Get("/commands", s.handleVehicleCommands)

					// Bloqueio, desbloqueio e pedidos de posição/status: equipe
					// operacional e o próprio dono do veículo, com a mesma
					// trava de segurança do corte.
					r.Group(func(r chi.Router) {
						r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator, auth.RoleCustomer))
						r.Post("/commands/engine-cut", s.handleEngineCut)
						r.Get("/commands/engine-cut/check", s.handleEngineCutCheck)
						r.Post("/commands/engine-resume", s.handleEngineResume)
						r.Post("/commands/request-position", s.handleRequestPosition)
						r.Post("/commands/request-status", s.handleRequestStatus)
					})
					// Comandos de configuração e texto livre ficam com a central.
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)).Post("/commands", s.handleGenericCommand)
				})
			})

			// Cercas: a equipe vê as da central (só o admin mexe); o cliente
			// cria e vê as dele, para os veículos dele (o filtro de dono fica
			// nos handlers, em fenceOwner).
			r.Route("/geofences", func(r chi.Router) {
				r.Use(s.requireActiveCustomer)
				r.Get("/", s.handleListGeofences)
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleCustomer))
					r.Post("/", s.handleCreateGeofence)
					r.Patch("/{id}", s.handleUpdateGeofence)
					r.Delete("/{id}", s.handleDeleteGeofence)
				})
			})

			// Área do cliente: conta e faturas continuam acessíveis mesmo com
			// o acesso suspenso — é por aqui que ele vê o que pagar.
			r.Route("/me", func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleCustomer))
				r.Get("/account", s.handleMyAccount)
				r.Get("/subscriptions", s.handleMySubscriptions)
				r.Get("/invoices", s.handleMyInvoices)
				r.Post("/invoices/{id}/pix", s.handleMyInvoicePix)
				r.Get("/charges/{id}", s.handleMyCharge)
				r.Post("/charges/{id}/simulate", s.handleMyChargeSimulate)
				r.Put("/address", s.handleSaveMyAddress)
				// Novo veículo (veículo → rastreador → assinatura): bloqueado com o
				// acesso suspenso.
				r.With(s.requireActiveCustomer).Post("/trackers", s.handleMyOrderTracker)
				// Assinatura antiga sem veículo: o cliente informa qual é.
				r.Post("/subscriptions/{subscriptionId}/vehicle", s.handleMyAttachVehicle)
				// Acompanhamento do chip e do rastreador até a casa dele.
				r.Get("/fulfillments", s.handleMyFulfillments)
				// Alertas por e-mail: escolhas, histórico e e-mail de teste.
				r.Get("/alerts", s.handleMyAlerts)
				r.Put("/alerts", s.handleSaveMyAlerts)
				r.Post("/alerts/test", s.handleMyAlertsTest)
				// Notificações no celular (app do cliente).
				r.Get("/push", s.handleMyPush)
				r.Post("/push/subscriptions", s.handleMyPushSubscribe)
				r.Post("/push/unsubscribe", s.handleMyPushUnsubscribe)
				r.Post("/push/test", s.handleMyPushTest)

				// Acessos de terceiros aos veículos do cliente.
				if s.Shares != nil {
					r.Get("/shares", s.handleMyShares)
					r.With(s.requireActiveCustomer).Post("/shares", s.handleCreateShare)
					r.Patch("/shares/{id}", s.handleUpdateShare)
					r.Delete("/shares/{id}", s.handleRemoveShare)
				}
			})

			// Daqui para baixo, só a equipe da central.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator, auth.RoleViewer))

				r.Get("/protocols", s.handleListProtocols)
				r.Get("/events", s.handleRecentEvents)

				r.Route("/devices", func(r chi.Router) {
					r.Get("/", s.handleListDevices)
					r.With(auth.RequireRole(auth.RoleAdmin)).Post("/", s.handleCreateDevice)

					r.Route("/{id}", func(r chi.Router) {
						r.Get("/", s.handleGetDevice)
						r.With(auth.RequireRole(auth.RoleAdmin)).Patch("/", s.handleUpdateDevice)
						r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/", s.handleDeleteDevice)
						r.Get("/status", s.handleDeviceStatus)
						r.Get("/commands", s.handleDeviceCommands)
						// Configuração do aparelho: só o admin (a tela de
						// rastreadores também é só dele).
						r.With(auth.RequireRole(auth.RoleAdmin)).Get("/provisioning", s.handleDeviceProvisioning)
					})
				})

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
				})

				// Clientes, assinaturas e faturas (administração).
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin))

					r.Route("/customers", func(r chi.Router) {
						r.Get("/", s.handleListCustomers)
						r.Post("/", s.handleCreateCustomer)
						r.Route("/{id}", func(r chi.Router) {
							r.Get("/", s.handleGetCustomer)
							r.Patch("/", s.handleUpdateCustomer)
							r.Put("/address", s.handleSaveCustomerAddress)
							r.Put("/history-retention", s.handleSetCustomerRetention)
							r.Get("/alerts", s.handleGetCustomerAlerts)
							r.Put("/alerts", s.handleSaveCustomerAlerts)
							r.Post("/invite", s.handleInviteCustomer)
							r.Post("/invoices", s.handleCreateInvoice)
							r.Post("/trackers", s.handleAdminOrderTracker)
							r.Get("/launch-promo", s.handleCustomerLaunchPromo)
							r.Post("/subscriptions/{subscriptionId}/vehicle", s.handleAdminAttachVehicle)
							r.Post("/vehicles/{vehicleId}/subscription", s.handleReactivateSubscription)
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
						r.Delete("/waitlist/{id}", s.handleRemoveFromWaitlist)
						r.Patch("/{id}", s.handleUpdateLead)
					})
					r.Patch("/subscriptions/{id}", s.handleUpdateSubscription)
					r.Post("/subscriptions/{id}/cancel", s.handleCancelSubscription)
					r.Patch("/invoices/{id}", s.handleUpdateInvoice)
					r.Post("/invoices/{id}/pay", s.handlePayInvoice)
					r.Post("/invoices/{id}/cancel", s.handleCancelInvoice)
					r.Post("/invoices/{id}/pix", s.handleAdminInvoicePix)
					r.Get("/charges/{id}", s.handleAdminCharge)
					r.Post("/charges/{id}/simulate", s.handleAdminChargeSimulate)
					r.Post("/charges/{id}/refund", s.handleAdminChargeRefund)
				})

				// Pedidos: o chip e o rastreador até o cliente. A equipe
				// operacional avança as etapas; comprar etiqueta (gasta saldo)
				// e conectar o Melhor Envios é do admin.
				r.Route("/fulfillments", func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator))
					r.Get("/", s.handleListFulfillments)
					r.Get("/{id}", s.handleGetFulfillment)
					r.Post("/{id}/status", s.handleChangeFulfillment)
					r.Post("/{id}/shipping/sync", s.handleSyncShipping)
					r.With(auth.RequireRole(auth.RoleAdmin)).Post("/{id}/shipping/quote", s.handleQuoteShipping)
					r.With(auth.RequireRole(auth.RoleAdmin)).Post("/{id}/shipping/label", s.handleBuyLabel)
				})
				r.Route("/integrations/melhorenvio", func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin))
					r.Get("/", s.handleShippingIntegration)
					r.Post("/connect", s.handleConnectShipping)
					r.Post("/disconnect", s.handleDisconnectShipping)
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
	r.With(s.Auth.Middleware, s.requireActiveCustomer).Handle("/ws", s.WS)

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

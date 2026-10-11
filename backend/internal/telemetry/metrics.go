package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

// Metrics agrupa os coletores expostos em /metrics (§28).
type Metrics struct {
	registry *prometheus.Registry

	Connections      prometheus.Gauge
	OnlineDevices    prometheus.Gauge
	PacketsReceived  *prometheus.CounterVec
	PacketsInvalid   *prometheus.CounterVec
	PositionsStored  *prometheus.CounterVec
	PositionIngest   prometheus.Histogram
	CommandsSent     *prometheus.CounterVec
	CommandsFailed   *prometheus.CounterVec
	CommandLatency   *prometheus.HistogramVec
	WebsocketClients prometheus.Gauge
	HTTPRequests     *prometheus.CounterVec
	HTTPDuration     *prometheus.HistogramVec
}

func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	m := &Metrics{
		registry: reg,
		Connections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tracker_connections",
			Help: "Conexões TCP de rastreadores atualmente abertas.",
		}),
		OnlineDevices: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tracker_online_devices",
			Help: "Dispositivos com status ONLINE.",
		}),
		PacketsReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tracker_packets_received_total",
			Help: "Pacotes recebidos e decodificados, por protocolo e tipo.",
		}, []string{"protocol", "kind"}),
		PacketsInvalid: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tracker_packets_invalid_total",
			Help: "Pacotes descartados, por motivo.",
		}, []string{"reason"}),
		PositionsStored: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tracker_positions_received_total",
			Help: "Posições persistidas, por protocolo.",
		}, []string{"protocol"}),
		PositionIngest: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "tracker_position_ingest_seconds",
			Help:    "Tempo de uma posição dentro do servidor: do pacote decodificado à publicação no tempo real.",
			Buckets: []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}),
		CommandsSent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tracker_commands_sent_total",
			Help: "Comandos escritos na conexão do rastreador, por tipo.",
		}, []string{"command"}),
		CommandsFailed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tracker_commands_failed_total",
			Help: "Comandos que terminaram em falha, por tipo e status final.",
		}, []string{"command", "status"}),
		CommandLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tracker_command_latency_seconds",
			Help:    "Tempo entre o envio do comando e o ACK do rastreador.",
			Buckets: []float64{0.5, 1, 2, 5, 10, 15, 30, 60},
		}, []string{"command"}),
		WebsocketClients: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tracker_websocket_clients",
			Help: "Clientes WebSocket conectados ao painel.",
		}),
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Requisições HTTP atendidas.",
		}, []string{"method", "route", "status"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duração das requisições HTTP.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
	}

	reg.MustRegister(
		m.Connections, m.OnlineDevices, m.PacketsReceived, m.PacketsInvalid,
		m.PositionsStored, m.PositionIngest, m.CommandsSent, m.CommandsFailed, m.CommandLatency,
		m.WebsocketClients, m.HTTPRequests, m.HTTPDuration,
	)
	return m
}

// Handler devolve o endpoint /metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

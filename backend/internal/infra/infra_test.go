package infra

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeProc(t *testing.T, dir, stat string) {
	t.Helper()
	files := map[string]string{
		"stat":    stat,
		"meminfo": "MemTotal:       8000000 kB\nMemFree:         500000 kB\nMemAvailable:   2000000 kB\n",
		"loadavg": "0.52 0.48 0.40 1/234 5678\n",
		"uptime":  "86400.50 170000.00\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// Duas leituras de /proc/stat: a CPU é a fatia ocupada entre elas.
func TestMonitorSample(t *testing.T) {
	dir := t.TempDir()
	// total 1000, ocioso (idle + iowait) 800
	writeProc(t, dir, "cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 50 0 50 350 50 0 0 0 0 0\ncpu1 50 0 50 350 50 0 0 0 0 0\nintr 1\n")
	m := NewMonitor(dir, dir)
	start := time.Date(2026, 10, 4, 12, 0, 5, 0, time.UTC)
	m.Sample(start)

	// +1000 no total, +250 ocioso: 75% ocupada.
	writeProc(t, dir, "cpu  500 0 450 850 200 0 0 0 0 0\ncpu0 1 0 1 1 1 0 0 0 0 0\ncpu1 1 0 1 1 1 0 0 0 0 0\n")
	s := m.Sample(start.Add(10 * time.Second))
	if s.CPUPercent != 75 || s.Cores != 2 {
		t.Fatalf("cpu = %v%% em %d núcleos", s.CPUPercent, s.Cores)
	}
	if s.MemTotal != 8000000*1024 || s.MemUsed != 6000000*1024 {
		t.Errorf("memória = %d de %d", s.MemUsed, s.MemTotal)
	}
	if s.Load1 != 0.52 || s.Load15 != 0.40 || s.HostUptime != 86400.5 {
		t.Errorf("carga/uptime = %+v", s)
	}
	if s.DiskTotal == 0 {
		t.Error("disco sem total")
	}

	// As duas leituras são do mesmo minuto: um ponto, com a média da CPU.
	h := m.History()
	if len(h) != 1 || h[0].CPU != 37.5 || h[0].Mem != 75 {
		t.Fatalf("histórico = %+v", h)
	}
	m.Sample(start.Add(time.Minute))
	if len(m.History()) != 2 {
		t.Fatalf("minuto novo não abriu ponto: %+v", m.History())
	}
}

func TestMonitorHistoryCap(t *testing.T) {
	dir := t.TempDir()
	writeProc(t, dir, "cpu  1 0 1 1 1 0 0 0 0 0\n")
	m := NewMonitor(dir, dir)
	start := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	for i := 0; i < historyMinutes+30; i++ {
		m.Sample(start.Add(time.Duration(i) * time.Minute))
	}
	h := m.History()
	if len(h) != historyMinutes || !h[len(h)-1].At.Equal(start.Add(time.Duration(historyMinutes+29)*time.Minute)) {
		t.Fatalf("histórico com %d pontos, último %v", len(h), h[len(h)-1].At)
	}
}

// O Capture deixa o log seguir normalmente e copia só avisos e erros, com o
// componente, os grupos e o erro em texto; fila cheia descarta sem travar.
func TestCapture(t *testing.T) {
	var out bytes.Buffer
	capture := NewCapture(slog.NewJSONHandler(&out, nil))
	log := slog.New(capture).With("component", "auth")

	log.Info("tudo certo")
	log.WithGroup("pedido").Error("falhou", "id", 7, "err", errors.New("sem conexão"), "tempo", 1500*time.Millisecond)
	log.Warn("cuidado")

	if !strings.Contains(out.String(), "tudo certo") || !strings.Contains(out.String(), "falhou") {
		t.Fatalf("o log normal não saiu: %s", out.String())
	}
	if len(capture.state.queue) != 2 {
		t.Fatalf("na fila: %d, quer 2 (aviso e erro)", len(capture.state.queue))
	}
	e := <-capture.state.queue
	if e.Level != "ERROR" || e.Component != "auth" || e.Message != "falhou" {
		t.Fatalf("entrada = %+v", e)
	}
	if e.Attrs["pedido.id"] != int64(7) || e.Attrs["pedido.err"] != "sem conexão" || e.Attrs["pedido.tempo"] != "1.5s" {
		t.Errorf("atributos = %v", e.Attrs)
	}
	if w := <-capture.state.queue; w.Level != "WARN" || w.Component != "auth" {
		t.Errorf("aviso = %+v", w)
	}

	for i := 0; i < queueSize+5; i++ {
		log.Error("muitos")
	}
	if capture.Dropped() != 5 {
		t.Errorf("descartados = %d, quer 5", capture.Dropped())
	}
}

func TestClipText(t *testing.T) {
	if got := clipText("ação", 2); got != "aç…" {
		t.Errorf("clipText = %q", got)
	}
}

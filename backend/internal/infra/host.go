// Package infra mostra a saúde do sistema para o painel (Diagnóstico →
// Servidor): CPU, memória e disco da máquina, o banco, o Redis, os backups
// e os avisos e erros do próprio servidor.
package infra

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Leitura da máquina. Dentro do contêiner, /proc mostra a CPU, a memória e a
// carga da máquina toda (o Docker não isola esses arquivos), e o disco de "/"
// é o do Docker na máquina — é isso que interessa vigiar.

// Sample é uma leitura.
type Sample struct {
	At         time.Time `json:"at"`
	CPUPercent float64   `json:"cpuPercent"`
	Cores      int       `json:"cores"`
	Load1      float64   `json:"load1"`
	Load5      float64   `json:"load5"`
	Load15     float64   `json:"load15"`
	MemUsed    uint64    `json:"memUsed"`
	MemTotal   uint64    `json:"memTotal"`
	DiskUsed   uint64    `json:"diskUsed"`
	DiskTotal  uint64    `json:"diskTotal"`
	// HostUptime: há quanto tempo a máquina está ligada (segundos).
	HostUptime float64 `json:"hostUptime"`
}

// Point é um minuto do histórico, em porcentagem.
type Point struct {
	At   time.Time `json:"at"`
	CPU  float64   `json:"cpu"`
	Mem  float64   `json:"mem"`
	Disk float64   `json:"disk"`
}

// historyMinutes: o gráfico cobre 24 horas, um ponto por minuto.
const historyMinutes = 24 * 60

// Monitor lê a máquina a cada intervalo e guarda o histórico em memória
// (recomeça quando o servidor reinicia).
type Monitor struct {
	proc     string // "/proc" (os testes usam uma pasta falsa)
	diskPath string

	mu        sync.RWMutex
	last      Sample
	history   []Point
	minute    time.Time
	cpuSum    float64
	cpuCount  int
	prevTotal uint64
	prevIdle  uint64
}

func NewMonitor(proc, diskPath string) *Monitor {
	if proc == "" {
		proc = "/proc"
	}
	if diskPath == "" {
		diskPath = "/"
	}
	return &Monitor{proc: proc, diskPath: diskPath}
}

// Run lê a máquina até o contexto acabar.
func (m *Monitor) Run(ctx context.Context, every time.Duration) {
	m.Sample(time.Now())
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			m.Sample(now)
		}
	}
}

// Sample faz uma leitura e a acrescenta ao minuto corrente do histórico.
func (m *Monitor) Sample(now time.Time) Sample {
	s := Sample{At: now}
	total, idle, cores := m.readCPU()
	s.Cores = cores
	s.Load1, s.Load5, s.Load15 = m.readLoad()
	s.MemUsed, s.MemTotal = m.readMem()
	s.DiskUsed, s.DiskTotal = readDisk(m.diskPath)
	s.HostUptime = m.readUptime()

	m.mu.Lock()
	defer m.mu.Unlock()
	// A CPU é a fatia ocupada desde a leitura anterior.
	if m.prevTotal > 0 && total > m.prevTotal {
		busy := float64((total - m.prevTotal) - (idle - m.prevIdle))
		s.CPUPercent = clampPercent(busy / float64(total-m.prevTotal) * 100)
	} else {
		s.CPUPercent = m.last.CPUPercent
	}
	m.prevTotal, m.prevIdle = total, idle
	m.last = s

	minute := now.Truncate(time.Minute)
	if !minute.Equal(m.minute) {
		m.minute, m.cpuSum, m.cpuCount = minute, 0, 0
		m.history = append(m.history, Point{At: minute})
		if len(m.history) > historyMinutes {
			m.history = m.history[len(m.history)-historyMinutes:]
		}
	}
	m.cpuSum += s.CPUPercent
	m.cpuCount++
	point := &m.history[len(m.history)-1]
	point.CPU = round1(m.cpuSum / float64(m.cpuCount))
	point.Mem = round1(percent(s.MemUsed, s.MemTotal))
	point.Disk = round1(percent(s.DiskUsed, s.DiskTotal))
	return s
}

// Current é a última leitura.
func (m *Monitor) Current() Sample {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.last
}

// History é uma cópia do histórico (do mais antigo para o mais novo).
func (m *Monitor) History() []Point {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Point(nil), m.history...)
}

// readCPU: os contadores da primeira linha de /proc/stat (total e ociosos) e
// quantos núcleos há.
func (m *Monitor) readCPU() (total, idle uint64, cores int) {
	f, err := os.Open(filepath.Join(m.proc, "stat"))
	if err != nil {
		return 0, 0, 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		if fields[0] != "cpu" {
			cores++
			continue
		}
		// user nice system idle iowait irq softirq steal (guest já está em user).
		for i, field := range fields[1:] {
			if i >= 8 {
				break
			}
			v, _ := strconv.ParseUint(field, 10, 64)
			total += v
			if i == 3 || i == 4 { // idle e iowait
				idle += v
			}
		}
	}
	return total, idle, cores
}

func (m *Monitor) readLoad() (l1, l5, l15 float64) {
	data, err := os.ReadFile(filepath.Join(m.proc, "loadavg"))
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	l1, _ = strconv.ParseFloat(fields[0], 64)
	l5, _ = strconv.ParseFloat(fields[1], 64)
	l15, _ = strconv.ParseFloat(fields[2], 64)
	return l1, l5, l15
}

// readMem: em uso = total - disponível (o cache que o sistema libera conta
// como disponível).
func (m *Monitor) readMem() (used, total uint64) {
	f, err := os.Open(filepath.Join(m.proc, "meminfo"))
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var available uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = v * 1024
		case "MemAvailable:":
			available = v * 1024
		}
	}
	if total == 0 || available > total {
		return 0, total
	}
	return total - available, total
}

func (m *Monitor) readUptime() float64 {
	data, err := os.ReadFile(filepath.Join(m.proc, "uptime"))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

// readDisk: total e em uso do sistema de arquivos do caminho.
func readDisk(path string) (used, total uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bsize := uint64(st.Bsize)
	total = st.Blocks * bsize
	free := st.Bfree * bsize
	if free > total {
		return 0, total
	}
	return total - free, total
}

func percent(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func clampPercent(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	}
	return round1(v)
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

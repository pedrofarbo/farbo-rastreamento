package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Avisos e erros do servidor. O Capture embrulha o handler do log: tudo
// segue para a saída de sempre, e o que é aviso ou erro também vai para uma
// fila que um gravador passa ao banco (system_logs). Nada aqui bloqueia quem
// está logando: fila cheia descarta (e conta o descarte).

// LogRetention: os registros ficam 30 dias.
const LogRetention = 30 * 24 * time.Hour

const (
	queueSize = 1024
	maxAttrs  = 30
	maxValue  = 500
)

// Entry é um aviso ou erro.
type Entry struct {
	ID        int64          `json:"id"`
	At        time.Time      `json:"at"`
	Level     string         `json:"level"` // WARN ou ERROR
	Component string         `json:"component"`
	Message   string         `json:"message"`
	Attrs     map[string]any `json:"attrs"`
}

// captureState é o que todas as cópias do handler (With/WithGroup) dividem.
type captureState struct {
	queue   chan Entry
	dropped atomic.Int64
	failed  atomic.Int64
	start   sync.Once
}

// Capture é um slog.Handler que copia avisos e erros para o banco.
type Capture struct {
	inner  slog.Handler
	state  *captureState
	attrs  []slog.Attr
	groups []string
}

func NewCapture(inner slog.Handler) *Capture {
	return &Capture{inner: inner, state: &captureState{queue: make(chan Entry, queueSize)}}
}

func (c *Capture) Enabled(ctx context.Context, level slog.Level) bool {
	return c.inner.Enabled(ctx, level)
}

func (c *Capture) Handle(ctx context.Context, r slog.Record) error {
	err := c.inner.Handle(ctx, r)
	if r.Level >= slog.LevelWarn {
		select {
		case c.state.queue <- c.entry(r):
		default:
			c.state.dropped.Add(1)
		}
	}
	return err
}

func (c *Capture) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *c
	next.inner = c.inner.WithAttrs(attrs)
	// Os atributos herdados levam o prefixo dos grupos abertos até aqui.
	next.attrs = append([]slog.Attr(nil), c.attrs...)
	for _, a := range attrs {
		next.attrs = append(next.attrs, prefixed(c.groups, a))
	}
	return &next
}

func (c *Capture) WithGroup(name string) slog.Handler {
	if name == "" {
		return c
	}
	next := *c
	next.inner = c.inner.WithGroup(name)
	next.groups = append(append([]string(nil), c.groups...), name)
	return &next
}

// Dropped: quantos avisos e erros não couberam na fila; Failed: quantos o
// banco recusou.
func (c *Capture) Dropped() int64 { return c.state.dropped.Load() }
func (c *Capture) Failed() int64  { return c.state.failed.Load() }

// Persist passa a gravar a fila no banco (uma vez só; o que chegou antes do
// banco ficar pronto espera na fila).
func (c *Capture) Persist(ctx context.Context, db *database.DB) {
	c.state.start.Do(func() {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case e := <-c.state.queue:
					attrs, _ := json.Marshal(e.Attrs)
					// O próprio gravador nunca loga (um erro aqui viraria outro
					// registro na fila): só conta.
					wctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					_, err := db.Exec(wctx, `
						INSERT INTO system_logs (logged_at, level, component, message, attrs)
						VALUES ($1, $2, $3, $4, $5)`, e.At, e.Level, e.Component, e.Message, attrs)
					cancel()
					if err != nil {
						c.state.failed.Add(1)
					}
				}
			}
		}()
	})
}

func (c *Capture) entry(r slog.Record) Entry {
	e := Entry{At: r.Time, Message: clipText(r.Message, maxValue), Attrs: map[string]any{}}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	e.Level = "WARN"
	if r.Level >= slog.LevelError {
		e.Level = "ERROR"
	}
	add := func(a slog.Attr) {
		// Já com o prefixo dos grupos: "component" puro é o da raiz do logger.
		if a.Key == "component" {
			e.Component = clipText(a.Value.Resolve().String(), 60)
			return
		}
		if len(e.Attrs) < maxAttrs {
			e.Attrs[a.Key] = value(a.Value)
		}
	}
	for _, a := range c.attrs {
		add(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		add(prefixed(c.groups, a))
		return true
	})
	return e
}

// prefixed põe o caminho dos grupos na chave ("pedido.id").
func prefixed(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 {
		return a
	}
	return slog.Attr{Key: strings.Join(groups, ".") + "." + a.Key, Value: a.Value}
}

// value converte o valor do log em algo que o JSON guarda.
func value(v slog.Value) any {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return clipText(v.String(), maxValue)
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format(time.RFC3339)
	case slog.KindGroup:
		out := map[string]any{}
		for _, a := range v.Group() {
			out[a.Key] = value(a.Value)
		}
		return out
	}
	if err, ok := v.Any().(error); ok {
		return clipText(err.Error(), maxValue)
	}
	return clipText(fmt.Sprint(v.Any()), maxValue)
}

func clipText(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

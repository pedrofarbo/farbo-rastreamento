package dunning

import (
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
)

// A régua com 10 dias até a suspensão (a do sistema): gerada, 3 dias antes,
// no dia, 3 dias depois e 2 dias antes de suspender — e nada depois.
func TestStage(t *testing.T) {
	for late, want := range map[int]string{
		-10: KindIssued, -4: KindIssued, -3: KindDueSoon, -1: KindDueSoon, 0: KindDueToday,
		1: "", 2: "", 3: KindOverdue, 8: KindOverdue, 9: KindSuspensionSoon, 10: KindSuspensionSoon, 11: "", 40: "",
	} {
		if got := Stage(late, 10); got != want {
			t.Errorf("Stage(%d, 10) = %q, quer %q", late, got, want)
		}
	}
	// Sem suspensão: o atraso é lembrado uma vez, com 3 dias.
	if Stage(3, 0) != KindOverdue || Stage(30, 0) != KindOverdue {
		t.Error("sem suspensão")
	}
	// Suspensão curta: o aviso de suspensão vem antes do de atraso.
	if Stage(2, 3) != KindSuspensionSoon || Stage(4, 3) != "" {
		t.Error("suspensão com 3 dias")
	}
}

func TestPaymentLink(t *testing.T) {
	s := NewService(nil, nil, nil, Config{AppURL: "https://farbo.test/", LinkSecret: []byte("segredo")},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	id := uuid.New()
	url := s.PayURL(id)
	token := url[len("https://farbo.test/pagar/"):]
	if got, ok := s.InvoiceID(token); !ok || got != id {
		t.Fatalf("link %s → %v %v", url, got, ok)
	}
	// Outro segredo, link adulterado ou inventado: nada.
	other := NewService(nil, nil, nil, Config{LinkSecret: []byte("outro")}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, ok := other.InvoiceID(token); ok {
		t.Error("o link valeu com outro segredo")
	}
	if _, ok := s.InvoiceID(token[:len(token)-1] + "A"); ok && token[len(token)-1] != 'A' {
		t.Error("link adulterado valeu")
	}
	if _, ok := s.InvoiceID("nada"); ok {
		t.Error("link inventado valeu")
	}
}

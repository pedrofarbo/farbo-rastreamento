package mail

import (
	"context"
	"strings"
	"testing"
	"time"
)

// O código da verificação: no assunto e em destaque, com a validade; e um
// aviso para cada mudança.
func TestTwoFactorMails(t *testing.T) {
	sender := &captureSender{}
	m := NewTwoFactorMailer(sender, "https://painel.farbo.test/")
	ctx := context.Background()

	if err := m.Code(ctx, "lia@farbo.test", "Lia Martins", "482913", 10*time.Minute, "login"); err != nil {
		t.Fatal(err)
	}
	msg := sender.last
	if msg.To != "lia@farbo.test" || !strings.Contains(msg.Subject, "482913") || !strings.Contains(msg.Subject, "Seu código para entrar") ||
		!strings.Contains(msg.HTML, ">482913<") || !strings.Contains(msg.Text, "10 minutos") || !strings.Contains(msg.HTML, "Olá, Lia!") ||
		!strings.Contains(msg.Text, "não pede este código") {
		t.Errorf("código = %+v", msg)
	}
	if err := m.Code(ctx, "lia@farbo.test", "Lia", "000111", 10*time.Minute, "setup"); err != nil || !strings.Contains(sender.last.Subject, "Confirme") {
		t.Errorf("código da ativação = %q (%v)", sender.last.Subject, err)
	}

	at := time.Date(2026, 10, 7, 15, 30, 0, 0, time.UTC)
	for event, want := range map[string]string{
		"enabled_totp": "app autenticador", "enabled_email": "código para este e-mail", "disabled": "basta a senha",
		"reset": "redefiniu", "recovery_used": "códigos de recuperação",
	} {
		if err := m.Changed(ctx, "lia@farbo.test", "Lia", event, at); err != nil {
			t.Fatalf("%s: %v", event, err)
		}
		if !strings.Contains(sender.last.Text, want) || !strings.Contains(sender.last.Text, "07/10/2026 às 12:30") ||
			!strings.Contains(sender.last.HTML, "https://painel.farbo.test/login") {
			t.Errorf("%s = %s", event, sender.last.Text)
		}
	}
	if err := m.Changed(ctx, "lia@farbo.test", "Lia", "outro", at); err == nil {
		t.Error("aviso desconhecido aceito")
	}
}

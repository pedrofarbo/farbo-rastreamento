package mail

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	netmail "net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
)

// fakeSMTP é um servidor SMTP mínimo, suficiente para o net/smtp conversar:
// guarda o envelope, a autenticação e a mensagem recebida.
type fakeSMTP struct {
	listener net.Listener

	mu        sync.Mutex
	from      string
	rcpt      []string
	authPlain string
	data      string
	done      chan struct{}
}

func startFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("abrindo porta: %v", err)
	}
	server := &fakeSMTP{listener: listener, done: make(chan struct{})}
	t.Cleanup(func() { _ = listener.Close() })
	go server.serve()
	return server
}

func (f *fakeSMTP) port() int { return f.listener.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) serve() {
	conn, err := f.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	defer close(f.done)

	reader := bufio.NewReader(conn)
	reply := func(line string) { _, _ = io.WriteString(conn, line+"\r\n") }

	reply("220 fake ESMTP")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])

		switch verb {
		case "EHLO", "HELO":
			reply("250-fake")
			reply("250 AUTH PLAIN")
		case "AUTH":
			f.mu.Lock()
			f.authPlain = strings.TrimPrefix(line, "AUTH PLAIN ")
			f.mu.Unlock()
			reply("235 ok")
		case "MAIL":
			f.mu.Lock()
			f.from = line
			f.mu.Unlock()
			reply("250 ok")
		case "RCPT":
			f.mu.Lock()
			f.rcpt = append(f.rcpt, line)
			f.mu.Unlock()
			reply("250 ok")
		case "DATA":
			reply("354 manda")
			var body strings.Builder
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if dataLine == ".\r\n" {
					break
				}
				body.WriteString(strings.TrimPrefix(dataLine, "."))
			}
			f.mu.Lock()
			f.data = body.String()
			f.mu.Unlock()
			reply("250 ok")
		case "QUIT":
			reply("221 tchau")
			return
		default:
			reply("250 ok")
		}
	}
}

func TestSMTPSenderDeliversMultipartMessage(t *testing.T) {
	server := startFakeSMTP(t)

	sender, err := NewSMTPSender(config.Mail{
		From:         "Farbo Rastreadores <nao-responda@farbo.test>",
		SMTPHost:     "localhost", // PlainAuth só aceita sem TLS em localhost
		SMTPPort:     server.port(),
		SMTPUsername: "usuario",
		SMTPPassword: "segredo",
		SMTPTLS:      config.SMTPNoTLS,
	})
	if err != nil {
		t.Fatalf("NewSMTPSender: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = sender.Send(ctx, Message{
		To:      "maria@exemplo.com",
		ToName:  "Maria José",
		Subject: "Redefinição de senha — Farbo",
		Text:    "Olá, Maria! Abra o link: https://painel.test/redefinir-senha#token=abc",
		HTML:    `<p>Olá, Maria!</p><a href="https://painel.test/redefinir-senha#token=abc">Criar nova senha</a>`,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	<-server.done

	server.mu.Lock()
	defer server.mu.Unlock()

	if server.from != "MAIL FROM:<nao-responda@farbo.test>" {
		t.Errorf("MAIL FROM = %q", server.from)
	}
	if len(server.rcpt) != 1 || server.rcpt[0] != "RCPT TO:<maria@exemplo.com>" {
		t.Errorf("RCPT TO = %v", server.rcpt)
	}
	decodedAuth, _ := base64.StdEncoding.DecodeString(server.authPlain)
	if string(decodedAuth) != "\x00usuario\x00segredo" {
		t.Errorf("AUTH PLAIN = %q", decodedAuth)
	}

	parsed, err := netmail.ReadMessage(strings.NewReader(server.data))
	if err != nil {
		t.Fatalf("mensagem recebida não é um e-mail válido: %v", err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subject != "Redefinição de senha — Farbo" {
		t.Errorf("Subject = %q (err %v)", subject, err)
	}
	to, err := parsed.Header.AddressList("To")
	if err != nil || len(to) != 1 || to[0].Name != "Maria José" || to[0].Address != "maria@exemplo.com" {
		t.Errorf("To = %v (err %v)", to, err)
	}
	if parsed.Header.Get("Message-ID") == "" || parsed.Header.Get("Date") == "" {
		t.Error("faltam Message-ID ou Date")
	}

	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("Content-Type = %q (err %v)", parsed.Header.Get("Content-Type"), err)
	}
	reader := multipart.NewReader(parsed.Body, params["boundary"])
	var types []string
	var texts []string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("lendo parte: %v", err)
		}
		content, _ := io.ReadAll(part) // NextPart já decodifica quoted-printable
		types = append(types, part.Header.Get("Content-Type"))
		texts = append(texts, string(content))
	}
	if len(types) != 2 || !strings.HasPrefix(types[0], "text/plain") || !strings.HasPrefix(types[1], "text/html") {
		t.Fatalf("partes = %v, esperado texto e HTML", types)
	}
	if !strings.Contains(texts[0], "Olá, Maria!") || !strings.Contains(texts[1], "#token=abc") {
		t.Errorf("conteúdo das partes não confere: %q", texts)
	}
}

func TestSMTPSenderRejectsHeaderInjection(t *testing.T) {
	sender, err := NewSMTPSender(config.Mail{
		From: "nao-responda@farbo.test", SMTPHost: "localhost", SMTPPort: 1, SMTPTLS: config.SMTPNoTLS,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = sender.Send(context.Background(), Message{To: "maria@exemplo.com\r\nBcc: intruso@mal.test"})
	if err == nil {
		t.Fatal("destinatário com quebra de linha deveria ser recusado")
	}
}

func TestSMTPSenderRefusesPlainConnectionWithoutStartTLS(t *testing.T) {
	server := startFakeSMTP(t)
	sender, _ := NewSMTPSender(config.Mail{
		From: "nao-responda@farbo.test", SMTPHost: "localhost", SMTPPort: server.port(),
		SMTPTLS: config.SMTPStartTLS,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := sender.Send(ctx, Message{To: "maria@exemplo.com", Subject: "x", Text: "x", HTML: "x"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("sem STARTTLS o envio deveria falhar; err=%v", err)
	}
}

type captureSender struct{ last Message }

func (c *captureSender) Send(_ context.Context, msg Message) error {
	c.last = msg
	return nil
}

func TestAccountMailerPasswordReset(t *testing.T) {
	capture := &captureSender{}
	mailer := NewAccountMailer(capture, "https://painel.farbo.test/")

	token := "tok_ABC-123"
	if err := mailer.PasswordReset(context.Background(), "maria@exemplo.com", "Maria <b>Silva</b>", token, time.Hour); err != nil {
		t.Fatal(err)
	}
	msg := capture.last
	link := "https://painel.farbo.test/redefinir-senha#token=" + token

	if msg.To != "maria@exemplo.com" || !strings.Contains(msg.Subject, "Redefinição de senha") {
		t.Fatalf("destinatário/assunto: %q / %q", msg.To, msg.Subject)
	}
	for name, body := range map[string]string{"texto": msg.Text, "HTML": msg.HTML} {
		if !strings.Contains(body, link) {
			t.Errorf("%s sem o link %q", name, link)
		}
		if !strings.Contains(body, "1 hora") {
			t.Errorf("%s sem a validade do link", name)
		}
	}
	// O nome vem do cadastro: no HTML tem de sair escapado.
	if strings.Contains(msg.HTML, "<b>Silva</b>") || !strings.Contains(msg.HTML, "Olá, Maria") {
		t.Errorf("nome não escapado ou saudação ausente no HTML")
	}
	if !strings.Contains(msg.HTML, "https://painel.farbo.test/assets/email-header.png") {
		t.Error("HTML sem a logo")
	}
}

func TestAccountMailerPasswordChanged(t *testing.T) {
	capture := &captureSender{}
	mailer := NewAccountMailer(capture, "https://painel.farbo.test")

	at := time.Date(2026, 9, 29, 17, 5, 0, 0, time.UTC)
	if err := mailer.PasswordChanged(context.Background(), "maria@exemplo.com", "", at); err != nil {
		t.Fatal(err)
	}
	msg := capture.last
	if !strings.Contains(msg.Text, "29/09/2026 às 14:05") {
		t.Errorf("horário de Brasília ausente: %q", msg.Text)
	}
	if !strings.Contains(msg.Text, "Olá!") {
		t.Errorf("sem nome, a saudação deveria ser só \"Olá!\": %q", msg.Text)
	}
	if !strings.Contains(msg.HTML, "https://painel.farbo.test/login") {
		t.Error("HTML sem o link para o login")
	}
}

func TestAccountMailerInvite(t *testing.T) {
	capture := &captureSender{}
	mailer := NewAccountMailer(capture, "https://painel.farbo.test")

	if err := mailer.Invite(context.Background(), "joao@exemplo.com", "João Souza", "customer", "tok123", 72*time.Hour); err != nil {
		t.Fatal(err)
	}
	msg := capture.last
	link := "https://painel.farbo.test/redefinir-senha#token=tok123&boasvindas=1"
	if !strings.Contains(msg.Subject, "Bem-vindo") {
		t.Errorf("assunto = %q", msg.Subject)
	}
	for name, body := range map[string]string{"texto": msg.Text, "HTML": msg.HTML} {
		if !strings.Contains(body, "3 dias") {
			t.Errorf("%s sem a validade do convite", name)
		}
	}
	if !strings.Contains(msg.Text, link) {
		t.Errorf("texto sem o link %q", link)
	}
	// No HTML o & do link é escapado como &amp;, o que o navegador desfaz.
	if !strings.Contains(msg.HTML, "#token=tok123&amp;boasvindas=1") {
		t.Errorf("HTML sem o link de convite")
	}
}

// O convite de quem entra na equipe diz o perfil e o que ele permite, e não
// fala de "seus veículos" nem de faturas, que são do cliente.
func TestAccountMailerTeamInvite(t *testing.T) {
	capture := &captureSender{}
	mailer := NewAccountMailer(capture, "https://painel.farbo.test")

	if err := mailer.Invite(context.Background(), "bia@farbo.test", "Bia Lima", "operator", "tok456", 72*time.Hour); err != nil {
		t.Fatal(err)
	}
	msg := capture.last
	if !strings.Contains(msg.Subject, "equipe") {
		t.Errorf("assunto = %q", msg.Subject)
	}
	for name, body := range map[string]string{"texto": msg.Text, "HTML": msg.HTML} {
		for _, want := range []string{"Bia", "Operador", "envia comandos", "3 dias"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s sem %q", name, want)
			}
		}
		if strings.Contains(body, "faturas") {
			t.Errorf("%s com o texto do cliente", name)
		}
	}
	if !strings.Contains(msg.Text, "https://painel.farbo.test/redefinir-senha#token=tok456&boasvindas=1") {
		t.Errorf("texto sem o link do convite")
	}
}

func TestHumanizeDuration(t *testing.T) {
	cases := map[time.Duration]string{
		time.Hour:        "1 hora",
		2 * time.Hour:    "2 horas",
		30 * time.Minute: "30 minutos",
		time.Minute:      "1 minuto",
		90 * time.Minute: "90 minutos",
		10 * time.Second: "1 minuto",
		24 * time.Hour:   "1 dia",
		72 * time.Hour:   "3 dias",
		36 * time.Hour:   "36 horas",
	}
	for input, want := range cases {
		if got := humanizeDuration(input); got != want {
			t.Errorf("humanizeDuration(%s) = %q, esperado %q", input, got, want)
		}
	}
}

func TestLogSenderOnlyShowsBodyInDevelopment(t *testing.T) {
	var out strings.Builder
	log := newTestLogger(&out)

	if err := NewLogSender(log, true).Send(context.Background(), Message{To: "a@b", Subject: "s", Text: "link-secreto"}); err != nil {
		t.Fatalf("em desenvolvimento o LogSender não deveria falhar: %v", err)
	}
	if !strings.Contains(out.String(), "link-secreto") {
		t.Error("em desenvolvimento o conteúdo deveria ir para o log")
	}

	out.Reset()
	err := NewLogSender(log, false).Send(context.Background(), Message{To: "a@b", Subject: "s", Text: "link-secreto"})
	if err != ErrNotConfigured {
		t.Fatalf("fora de desenvolvimento deveria devolver ErrNotConfigured; err=%v", err)
	}
	if strings.Contains(out.String(), "link-secreto") {
		t.Error("fora de desenvolvimento o conteúdo (com token) não pode ir para o log")
	}
}

func newTestLogger(out io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(out, nil))
}

func TestShipmentEmails(t *testing.T) {
	sender := &captureSender{}
	m := NewShipmentMailer(sender, "https://painel.farbo.test/")
	if err := m.Shipped(context.Background(), Shipment{To: "ana@x.test", Name: "Ana Souza", Vehicle: "Moto da Ana",
		Carrier: "Correios PAC", TrackingCode: "BR123 45"}); err != nil {
		t.Fatal(err)
	}
	msg := sender.last
	for _, want := range []string{"Moto da Ana", "Correios PAC", "BR123 45", "https://www.melhorrastreio.com.br/rastreio/BR123%2045"} {
		if !strings.Contains(msg.HTML, want) || !strings.Contains(msg.Text, want) {
			t.Errorf("e-mail de envio sem %q", want)
		}
	}
	if !strings.Contains(msg.Text, "Olá, Ana!") || msg.To != "ana@x.test" {
		t.Errorf("saudação/destinatário: %q %q", msg.To, msg.Text[:20])
	}
	if err := m.Delivered(context.Background(), Shipment{To: "ana@x.test", Name: "Ana", Vehicle: "<b>Moto</b>"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sender.last.HTML, "<b>Moto</b>") || !strings.Contains(sender.last.HTML, "https://painel.farbo.test/meus-veiculos") {
		t.Errorf("e-mail de chegada: HTML sem escapar ou sem o link do painel")
	}
}

// Os e-mails dos acessos de terceiros: o convite leva ao link de criar a
// senha, o aviso ao app, o dono recebe o alerta de segurança e o do
// bloqueio — e a permissão de bloqueio aparece só quando foi dada.
func TestShareMailer(t *testing.T) {
	capture := &captureSender{}
	mailer := NewShareMailer(capture, "https://painel.farbo.test")
	n := ShareNotice{
		VehicleID: "v-1", VehicleName: "Carro da Ana", VehiclePlate: "ABC1D23", OwnerName: "Ana Souza",
		GuestName: "Caio Lima", GuestEmail: "caio@exemplo.com", CanBlock: true,
		At: time.Date(2026, 10, 4, 15, 30, 0, 0, time.UTC),
	}
	ctx := context.Background()

	if err := mailer.GuestInvited(ctx, "caio@exemplo.com", n, "tok789", 72*time.Hour); err != nil {
		t.Fatal(err)
	}
	msg := capture.last
	for _, want := range []string{"Olá, Caio!", "Ana Souza compartilhou Carro da Ana (ABC1D23)", "bloquear o motor",
		"https://painel.farbo.test/redefinir-senha#token=tok789&boasvindas=1", "3 dias"} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("convite sem %q:\n%s", want, msg.Text)
		}
	}
	if !strings.Contains(msg.Subject, "Ana Souza compartilhou") {
		t.Errorf("assunto do convite = %q", msg.Subject)
	}

	only := n
	only.CanBlock = false
	if err := mailer.GuestAdded(ctx, "caio@exemplo.com", only); err != nil {
		t.Fatal(err)
	}
	if msg := capture.last; strings.Contains(msg.Text, "bloquear") || !strings.Contains(msg.Text, "https://painel.farbo.test/app/") {
		t.Errorf("aviso só de acompanhar:\n%s", msg.Text)
	}

	if err := mailer.OwnerShared(ctx, "ana@exemplo.com", n); err != nil {
		t.Fatal(err)
	}
	if msg := capture.last; !strings.Contains(msg.Text, "Caio Lima (caio@exemplo.com)") ||
		!strings.Contains(msg.Text, "remova agora") || !strings.Contains(msg.HTML, "Olá, Ana!") {
		t.Errorf("aviso de segurança ao dono:\n%s", msg.Text)
	}

	if err := mailer.GuestBlocked(ctx, "ana@exemplo.com", n); err != nil {
		t.Fatal(err)
	}
	msg = capture.last
	if !strings.Contains(msg.Subject, "Motor bloqueado") || !strings.Contains(msg.Text, "04/10/2026 às 12:30") ||
		!strings.Contains(msg.Text, "https://painel.farbo.test/app/veiculos/v-1") {
		t.Errorf("aviso de bloqueio: %q\n%s", msg.Subject, msg.Text)
	}
}

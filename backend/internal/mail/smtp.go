package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
)

// dialTimeout limita a abertura da conexão; o resto do envio respeita o
// prazo do contexto.
const dialTimeout = 10 * time.Second

// SMTPSender entrega por SMTP autenticado.
type SMTPSender struct {
	cfg  config.Mail
	from *netmail.Address
}

func NewSMTPSender(cfg config.Mail) (*SMTPSender, error) {
	from, err := netmail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("MAIL_FROM inválido: %w", err)
	}
	return &SMTPSender{cfg: cfg, from: from}, nil
}

func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	to, err := netmail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("destinatário inválido: %w", err)
	}
	to.Name = msg.ToName

	raw, err := buildMessage(s.from, to, msg, time.Now())
	if err != nil {
		return err
	}

	client, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Mail(s.from.Address); err != nil {
		return fmt.Errorf("SMTP MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("SMTP RCPT TO: %w", err)
	}
	body, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := body.Write(raw); err != nil {
		return fmt.Errorf("SMTP escrevendo mensagem: %w", err)
	}
	if err := body.Close(); err != nil {
		return fmt.Errorf("SMTP finalizando mensagem: %w", err)
	}
	return client.Quit()
}

// connect abre a sessão SMTP já com TLS e autenticação resolvidos.
func (s *SMTPSender) connect(ctx context.Context) (*smtp.Client, error) {
	host := s.cfg.SMTPHost
	addr := net.JoinHostPort(host, strconv.Itoa(s.cfg.SMTPPort))
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}

	dialer := &net.Dialer{Timeout: dialTimeout}
	var conn net.Conn
	var err error
	if s.cfg.SMTPTLS == config.SMTPTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("conectando ao SMTP %s: %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("SMTP saudação: %w", err)
	}

	if s.cfg.SMTPTLS == config.SMTPStartTLS {
		// Sem STARTTLS a senha e o link iriam em claro: melhor falhar.
		if ok, _ := client.Extension("STARTTLS"); !ok {
			_ = client.Close()
			return nil, fmt.Errorf("o servidor SMTP não oferece STARTTLS; use SMTP_TLS=tls (porta 465)")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("SMTP STARTTLS: %w", err)
		}
	}

	if s.cfg.SMTPUsername != "" {
		// PlainAuth recusa enviar a senha sem TLS (exceto para localhost).
		auth := smtp.PlainAuth("", s.cfg.SMTPUsername, s.cfg.SMTPPassword, host)
		if err := client.Auth(auth); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("SMTP autenticação: %w", err)
		}
	}
	return client, nil
}

// buildMessage monta o e-mail em MIME multipart/alternative (texto + HTML);
// com anexos, num multipart/mixed: o texto e o HTML, depois os arquivos.
func buildMessage(from, to *netmail.Address, msg Message, now time.Time) ([]byte, error) {
	body, contentType, err := alternativeBody(msg)
	if err != nil {
		return nil, err
	}
	if len(msg.Attachments) > 0 {
		if body, contentType, err = mixedBody(body, contentType, msg.Attachments); err != nil {
			return nil, err
		}
	}

	headers := []struct{ key, value string }{
		{"From", from.String()},
		{"To", to.String()},
		{"Subject", mime.QEncoding.Encode("utf-8", msg.Subject)},
		{"Date", now.Format(time.RFC1123Z)},
		{"Message-ID", messageID(from.Address)},
		{"MIME-Version", "1.0"},
		{"Content-Type", contentType},
		// Evita respostas automáticas ("estou de férias") voltando para o remetente.
		{"Auto-Submitted", "auto-generated"},
	}
	var head bytes.Buffer
	for _, h := range headers {
		if strings.ContainsAny(h.value, "\r\n") {
			return nil, fmt.Errorf("cabeçalho %s com quebra de linha", h.key)
		}
		head.WriteString(h.key + ": " + h.value + "\r\n")
	}
	head.WriteString("\r\n")
	return append(head.Bytes(), body...), nil
}

// alternativeBody é o texto e o HTML, em multipart/alternative.
func alternativeBody(msg Message) ([]byte, string, error) {
	var buf bytes.Buffer
	body := multipart.NewWriter(&buf)
	for _, part := range []struct{ contentType, content string }{
		{"text/plain; charset=utf-8", msg.Text},
		{"text/html; charset=utf-8", msg.HTML},
	} {
		w, err := body.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.contentType},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, "", err
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write([]byte(part.content)); err != nil {
			return nil, "", err
		}
		if err := qp.Close(); err != nil {
			return nil, "", err
		}
	}
	if err := body.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), `multipart/alternative; boundary="` + body.Boundary() + `"`, nil
}

// mixedBody junta ao corpo (texto e HTML) os anexos, em base64.
func mixedBody(alternative []byte, alternativeType string, attachments []Attachment) ([]byte, string, error) {
	var buf bytes.Buffer
	body := multipart.NewWriter(&buf)
	w, err := body.CreatePart(textproto.MIMEHeader{"Content-Type": {alternativeType}})
	if err != nil {
		return nil, "", err
	}
	if _, err := w.Write(alternative); err != nil {
		return nil, "", err
	}
	for _, a := range attachments {
		contentType := a.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w, err := body.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {mime.FormatMediaType(contentType, map[string]string{"name": a.Name})},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": a.Name})},
			"Content-Transfer-Encoding": {"base64"},
		})
		if err != nil {
			return nil, "", err
		}
		// Linhas de até 76 caracteres, como pede o MIME.
		encoded := base64.StdEncoding.EncodeToString(a.Data)
		for len(encoded) > 76 {
			if _, err := w.Write([]byte(encoded[:76] + "\r\n")); err != nil {
				return nil, "", err
			}
			encoded = encoded[76:]
		}
		if _, err := w.Write([]byte(encoded + "\r\n")); err != nil {
			return nil, "", err
		}
	}
	if err := body.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), `multipart/mixed; boundary="` + body.Boundary() + `"`, nil
}

// messageID gera um Message-ID único no domínio do remetente.
func messageID(fromAddress string) string {
	domain := "localhost"
	if at := strings.LastIndex(fromAddress, "@"); at >= 0 {
		domain = fromAddress[at+1:]
	}
	random := make([]byte, 16)
	_, _ = rand.Read(random)
	return "<" + hex.EncodeToString(random) + "@" + domain + ">"
}

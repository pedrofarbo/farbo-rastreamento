package mail

import (
	"bytes"
	"context"
	"fmt"
)

// CardMailer avisa quando o débito automático no cartão não passa.
type CardMailer struct {
	sender Sender
	appURL string
}

func NewCardMailer(sender Sender, appURL string) *CardMailer {
	return &CardMailer{sender: sender, appURL: appURL}
}

// CardDeclined é a cobrança do vencimento que o cartão recusou. Retry diz se
// ainda há tentativa (no dia seguinte); sem ela, a fatura segue para o Pix ou
// o boleto.
type CardDeclined struct {
	To          string
	Name        string
	Brand       string
	Last4       string
	AmountCents int
	DueDate     string // dd/mm/aaaa
	Retry       bool
}

type cardDeclinedData struct {
	CardDeclined
	FirstName string
	Amount    string
	AppURL    string
	ActionURL string
}

func (m *CardMailer) Declined(ctx context.Context, d CardDeclined) error {
	msg, err := renderCardDeclined(d, m.appURL)
	if err != nil {
		return err
	}
	return m.sender.Send(ctx, msg)
}

func renderCardDeclined(d CardDeclined, appURL string) (Message, error) {
	data := cardDeclinedData{CardDeclined: d, FirstName: firstName(d.Name), Amount: brl(d.AmountCents), AppURL: appURL,
		ActionURL: appURL + "/faturas"}
	var text, html bytes.Buffer
	if err := cardTemplates.text.Execute(&text, data); err != nil {
		return Message{}, fmt.Errorf("montando o aviso do cartão (texto): %w", err)
	}
	if err := cardTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return Message{}, fmt.Errorf("montando o aviso do cartão (HTML): %w", err)
	}
	subject := "Não conseguimos cobrar a fatura no seu cartão"
	if !d.Retry {
		subject = "Pague a fatura por Pix ou boleto: o cartão foi recusado"
	}
	return Message{To: d.To, ToName: d.Name, Subject: subject, Text: text.String(), HTML: html.String()}, nil
}

const cardHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#b45309;">Débito automático</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">O cartão recusou a fatura de {{.Amount}}</h1>
<p style="margin:0 0 16px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .FirstName}}, {{.}}{{end}}! Tentamos cobrar a fatura com vencimento em {{.DueDate}} no cartão {{.Brand}} final {{.Last4}}, e o banco recusou.</p>
<p style="margin:0 0 20px;font-size:15px;line-height:1.6;color:#334155;">{{if .Retry}}Vamos tentar de novo amanhã. Se preferir, pague agora por Pix ou boleto, ou troque o cartão em Faturas.{{else}}Foi a última tentativa: pague a fatura por Pix ou boleto e, se quiser continuar com o débito automático, troque o cartão em Faturas.{{end}}</p>
{{template "button" (button .ActionURL "Ver as faturas")}}
<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">O banco não informa o motivo da recusa: limite, cartão vencido ou bloqueado são os mais comuns.</p>
{{end}}`

const cardText = `Olá{{with .FirstName}}, {{.}}{{end}}!

Tentamos cobrar a fatura de {{.Amount}}, com vencimento em {{.DueDate}}, no cartão {{.Brand}} final {{.Last4}}, e o banco recusou.
{{if .Retry}}
Vamos tentar de novo amanhã. Se preferir, pague agora por Pix ou boleto, ou troque o cartão em Faturas.{{else}}
Foi a última tentativa: pague a fatura por Pix ou boleto e, se quiser continuar com o débito automático, troque o cartão em Faturas.{{end}}

Ver as faturas: {{.ActionURL}}

— Farbo Rastreadores
`

var cardTemplates = mustTemplates(cardText, cardHTML)

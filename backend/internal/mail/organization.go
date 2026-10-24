package mail

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// OrgMailer escreve os e-mails do painel das associações e seguradoras: o
// convite de quem entra na equipe de uma organização.
type OrgMailer struct {
	sender Sender
	appURL string
}

func NewOrgMailer(sender Sender, appURL string) *OrgMailer {
	return &OrgMailer{sender: sender, appURL: strings.TrimRight(appURL, "/")}
}

// OrgNotice é o acesso ao painel da organização como o e-mail conta.
type OrgNotice struct {
	OrgName    string
	MemberName string
	// Kind: association ou insurer.
	Kind string
	// Role: admin, operator ou viewer.
	Role string
}

type orgData struct {
	Name      string
	Org       string
	KindLabel string
	RoleLabel string
	What      string
	AppURL    string
	ActionURL string
	Validity  string
}

// orgRoles: o papel na organização, como o convite apresenta.
var orgRoles = map[string]struct{ Label, What string }{
	"admin": {"Administrador", "acompanhar os veículos dos associados no mapa, cadastrar associados e veículos, " +
		"bloquear e desbloquear, cuidar da equipe e das faturas"},
	"operator": {"Operador", "acompanhar os veículos dos associados no mapa, cadastrar associados e veículos, " +
		"bloquear e desbloquear"},
	"viewer": {"Visualizador", "acompanhar os veículos dos associados no mapa, com o histórico e os eventos"},
}

// MemberInvited: o link cria a senha de quem entrou na equipe da organização.
func (m *OrgMailer) MemberInvited(ctx context.Context, to string, n OrgNotice, token string, ttl time.Duration) error {
	role, ok := orgRoles[n.Role]
	if !ok {
		role = orgRoles["viewer"]
	}
	kind := "associação"
	if n.Kind == "insurer" {
		kind = "seguradora"
	}
	data := orgData{
		Name: firstName(n.MemberName), Org: strings.TrimSpace(n.OrgName), KindLabel: kind,
		RoleLabel: role.Label, What: role.What, AppURL: m.appURL,
		ActionURL: m.appURL + ResetPath + "#token=" + url.QueryEscape(token) + "&boasvindas=1",
		Validity:  humanizeDuration(ttl),
	}
	var text, html bytes.Buffer
	if err := orgInviteTemplates.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := orgInviteTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return m.sender.Send(ctx, Message{
		To: to, ToName: n.MemberName, Text: text.String(), HTML: html.String(),
		Subject: "Seu acesso ao painel da " + data.Org + " na Farbo — crie sua senha",
	})
}

const orgInviteHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Painel da {{.KindLabel}}</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">Seu acesso ao painel da {{.Org}}</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
<p style="margin:0;font-size:15px;line-height:1.6;color:#334155;">Você entrou na equipe da <strong>{{.Org}}</strong> na Farbo Rastreadores como <strong>{{.RoleLabel}}</strong>: vai poder {{.What}}. Para começar, crie a sua senha:</p>
{{template "button" (button .ActionURL "Criar minha senha")}}
<p style="margin:0 0 20px;font-size:14px;line-height:1.6;color:#475569;">O link vale por <strong>{{.Validity}}</strong>. Depois disso, use "Esqueci minha senha" na tela de login para receber outro.</p>
<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">Não esperava este e-mail? Ignore-o. Se o botão não funcionar, copie e cole este endereço no navegador:<br><a href="{{.ActionURL}}" style="color:#15803d;word-break:break-all;">{{.ActionURL}}</a></p>
{{end}}`

const orgInviteText = `Olá{{with .Name}}, {{.}}{{end}}!

Você entrou na equipe da {{.Org}} na Farbo Rastreadores como {{.RoleLabel}}: vai poder {{.What}}.

Para começar, crie a sua senha neste link:
{{.ActionURL}}

O link vale por {{.Validity}}. Depois disso, use "Esqueci minha senha" na tela de login para receber outro.

Não esperava este e-mail? Ignore-o.

— Farbo Rastreadores
`

var orgInviteTemplates = mustTemplates(orgInviteText, orgInviteHTML)

// OrgAnnouncement é o comunicado de uma associação ou seguradora ao associado.
type OrgAnnouncement struct {
	OrgName    string
	MemberName string
	Title      string
	Body       string
}

type orgAnnouncementData struct {
	Name, Org, Title string
	Paragraphs       []string
	AppURL           string
}

// Announcement: o comunicado, com o nome de quem mandou (a organização) e o
// link do app.
func (m *OrgMailer) Announcement(ctx context.Context, to string, n OrgAnnouncement) error {
	data := orgAnnouncementData{
		Name: firstName(n.MemberName), Org: strings.TrimSpace(n.OrgName), Title: strings.TrimSpace(n.Title),
		AppURL: m.appURL + "/app/",
	}
	for _, p := range strings.Split(strings.ReplaceAll(n.Body, "\r\n", "\n"), "\n") {
		if p = strings.TrimSpace(p); p != "" {
			data.Paragraphs = append(data.Paragraphs, p)
		}
	}
	var text, html bytes.Buffer
	if err := orgAnnouncementTemplates.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := orgAnnouncementTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return m.sender.Send(ctx, Message{
		To: to, ToName: n.MemberName, Text: text.String(), HTML: html.String(),
		Subject: data.Title + " — " + data.Org,
	})
}

const orgAnnouncementHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#15803d;">Comunicado da {{.Org}}</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Title}}</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}!</p>
{{range .Paragraphs}}<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">{{.}}</p>
{{end}}{{template "button" (button .AppURL "Abrir o app")}}
<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">Você recebe este e-mail porque o rastreamento do seu veículo é pela {{.Org}}. Dúvidas sobre o comunicado, fale com a {{.Org}}.</p>
{{end}}`

const orgAnnouncementText = `Olá{{with .Name}}, {{.}}{{end}}!

Comunicado da {{.Org}}: {{.Title}}
{{range .Paragraphs}}
{{.}}
{{end}}
Abrir o app: {{.AppURL}}

Você recebe este e-mail porque o rastreamento do seu veículo é pela {{.Org}}.

— Farbo Rastreadores
`

var orgAnnouncementTemplates = mustTemplates(orgAnnouncementText, orgAnnouncementHTML)

// ClaimNotice é a ocorrência aberta por um associado, como o aviso à equipe
// da organização conta.
type ClaimNotice struct {
	ClaimID       string
	OrgName       string
	AssociateName string
	VehicleName   string
	Plate         string
	KindLabel     string
	OccurredAt    time.Time
	Description   string
}

type claimData struct {
	Name, Org, Associate, Vehicle, Kind, When, Description, ActionURL string
}

// ClaimOpened avisa quem é da equipe da organização da ocorrência nova.
func (m *OrgMailer) ClaimOpened(ctx context.Context, to, name string, n ClaimNotice) error {
	vehicle := strings.TrimSpace(n.VehicleName)
	if n.Plate != "" {
		vehicle += " (" + n.Plate + ")"
	}
	description := []rune(strings.TrimSpace(n.Description))
	if len(description) > 400 {
		description = append(description[:400], '…')
	}
	data := claimData{
		Name: firstName(name), Org: strings.TrimSpace(n.OrgName), Associate: n.AssociateName, Vehicle: vehicle,
		Kind: n.KindLabel, When: formatBrazilTime(n.OccurredAt), Description: string(description),
		ActionURL: m.appURL + "/organizacao/ocorrencias/" + url.PathEscape(n.ClaimID),
	}
	var text, html bytes.Buffer
	if err := claimTemplates.text.Execute(&text, data); err != nil {
		return fmt.Errorf("montando e-mail (texto): %w", err)
	}
	if err := claimTemplates.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return fmt.Errorf("montando e-mail (HTML): %w", err)
	}
	return m.sender.Send(ctx, Message{
		To: to, ToName: name, Text: text.String(), HTML: html.String(),
		Subject: "Ocorrência nova: " + data.Kind + " — " + data.Associate,
	})
}

const claimHTML = `{{define "content"}}
<p style="margin:0 0 8px;font-size:12px;font-weight:800;letter-spacing:3px;text-transform:uppercase;color:#b45309;">Ocorrência nova</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#0d130e;">{{.Kind}} — {{.Associate}}</h1>
<p style="margin:0 0 12px;font-size:15px;line-height:1.6;color:#334155;">Olá{{with .Name}}, {{.}}{{end}}! {{.Associate}} abriu uma ocorrência na {{.Org}}:</p>
<p style="margin:0 0 4px;font-size:15px;line-height:1.6;color:#334155;"><strong>Quando:</strong> {{.When}}</p>
{{with .Vehicle}}<p style="margin:0 0 4px;font-size:15px;line-height:1.6;color:#334155;"><strong>Veículo:</strong> {{.}}</p>{{end}}
<p style="margin:12px 0 0;font-size:15px;line-height:1.6;color:#334155;white-space:pre-line;">{{.Description}}</p>
{{template "button" (button .ActionURL "Ver a ocorrência")}}
<p style="margin:0;font-size:12px;line-height:1.6;color:#64748b;">As fotos e o boletim de ocorrência ficam no painel. Se o botão não funcionar, copie e cole este endereço no navegador:<br><a href="{{.ActionURL}}" style="color:#15803d;word-break:break-all;">{{.ActionURL}}</a></p>
{{end}}`

const claimText = `Olá{{with .Name}}, {{.}}{{end}}!

{{.Associate}} abriu uma ocorrência na {{.Org}}: {{.Kind}}.

Quando: {{.When}}
{{with .Vehicle}}Veículo: {{.}}
{{end}}
{{.Description}}

Ver a ocorrência (as fotos e o B.O. ficam no painel):
{{.ActionURL}}

— Farbo Rastreadores
`

var claimTemplates = mustTemplates(claimText, claimHTML)

package support

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/installers"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
)

// conversationTools são as ferramentas da IA, presas a uma conversa: os
// pedidos consultados são sempre os do dono do número que escreveu, nunca
// os de quem o contato disser ser.
type conversationTools struct {
	s      *Service
	conv   *Conversation
	reason string
}

func (t *conversationTools) Transfer(ctx context.Context, reason string) (string, error) {
	reason = truncate(strings.TrimSpace(reason), 500)
	if reason == "" {
		reason = "a IA transferiu a conversa"
	}
	if err := t.s.repo.setMode(ctx, t.conv.ID, ModeHuman, reason, true); err != nil {
		return "", err
	}
	t.reason = reason
	return "Conversa transferida para a equipe. Agora avise o contato, numa mensagem curta, que alguém da " +
		"equipe vai continuar a conversa por aqui.", nil
}

var chipLabels = map[string]string{
	fulfillment.ChipRequested: "chip solicitado ao fornecedor",
	fulfillment.ChipShipped:   "chip enviado pelo fornecedor",
	fulfillment.ChipAtBase:    "chip chegou na base da Farbo",
	fulfillment.ChipSeparated: "chip separado para a configuração",
}

var trackerLabels = map[string]string{
	fulfillment.TrackerAwaitingSupplier: "aguardando o rastreador chegar do fornecedor",
	fulfillment.TrackerAtBase:           "rastreador chegou na base da Farbo",
	fulfillment.TrackerAwaitingChip:     "aguardando o chip M2M chegar para configurar",
	fulfillment.TrackerConfiguring:      "rastreador em configuração",
	fulfillment.TrackerConfigured:       "rastreador configurado, pronto para envio",
	fulfillment.TrackerShipped:          "rastreador enviado",
	fulfillment.TrackerInTransit:        "rastreador em trânsito com a transportadora",
	fulfillment.TrackerDelivered:        "rastreador entregue",
}

func (t *conversationTools) Orders(ctx context.Context) (string, error) {
	if t.conv.CustomerID == nil {
		return "Este número não está no cadastro de clientes: não há pedidos para consultar. Se a pessoa já é " +
			"cliente, ela vê os pedidos no painel (Meus veículos) ou a equipe confirma.", nil
	}
	list, err := t.s.fulfillments.ListByCustomer(ctx, *t.conv.CustomerID)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "O cliente não tem pedido de rastreador em andamento nem entregue.", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Pedidos do cliente (%d):\n", len(list))
	for i, f := range list {
		v := f.CustomerView()
		fmt.Fprintf(&b, "%d. Veículo: %s. Pedido em %s.\n", i+1, v.VehicleName, t.date(v.CreatedAt))
		fmt.Fprintf(&b, "   Rastreador: %s%s.\n", trackerLabels[v.TrackerStatus], t.since(v.Events, fulfillment.TrackTracker, v.TrackerStatus))
		if v.TrackerStatus != fulfillment.TrackerDelivered {
			fmt.Fprintf(&b, "   Chip: %s.\n", chipLabels[v.ChipStatus])
		}
		if v.TrackingCode != "" {
			fmt.Fprintf(&b, "   Envio: %s, código de rastreio %s (%s).\n", v.Carrier, v.TrackingCode, mail.TrackingURL(v.TrackingCode))
		}
	}
	return b.String(), nil
}

func (t *conversationTools) date(at time.Time) string { return at.In(t.s.tz).Format("02/01/2006") }

// since diz desde quando o pedido está na etapa atual.
func (t *conversationTools) since(events []fulfillment.Event, track, status string) string {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Track == track && events[i].Status == status {
			return " (desde " + t.date(events[i].CreatedAt) + ")"
		}
	}
	return ""
}

func (t *conversationTools) Installers(ctx context.Context, city string) (string, error) {
	all, err := t.s.installers.ListActive(ctx)
	if err != nil {
		return "", err
	}
	if len(all) == 0 {
		return "Não há prestadores parceiros cadastrados no momento: transfira para a equipe indicar um.", nil
	}
	list, intro := all, fmt.Sprintf("Prestadores parceiros (%d):", len(all))
	if key := fold(city); key != "" {
		var found []*installers.Installer
		for _, in := range all {
			if strings.Contains(fold(in.City+" "+in.ServiceArea), key) {
				found = append(found, in)
			}
		}
		if len(found) > 0 {
			list, intro = found, fmt.Sprintf("Prestadores parceiros que atendem %q (%d):", city, len(found))
		} else {
			intro = fmt.Sprintf("Nenhum prestador cadastrado em %q. Todos os parceiros (%d):", city, len(all))
		}
	}
	var b strings.Builder
	b.WriteString(intro + "\n")
	for _, in := range list {
		var serves []string
		if in.ServesMoto {
			serves = append(serves, "moto")
		}
		if in.ServesCar {
			serves = append(serves, "carro")
		}
		fmt.Fprintf(&b, "- %s — %s", in.Name, in.City)
		if in.ServiceArea != "" {
			fmt.Fprintf(&b, " (atende: %s)", in.ServiceArea)
		}
		fmt.Fprintf(&b, ". Instala em %s.", strings.Join(serves, " e "))
		if in.PriceMotoCents != nil {
			fmt.Fprintf(&b, " Moto: %s.", brl(*in.PriceMotoCents))
		}
		if in.PriceCarCents != nil {
			fmt.Fprintf(&b, " Carro: %s.", brl(*in.PriceCarCents))
		}
		if in.WhatsApp != "" {
			fmt.Fprintf(&b, " WhatsApp: https://wa.me/%s", in.WhatsApp)
		}
		if in.Description != "" {
			fmt.Fprintf(&b, "\n  %s", in.Description)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// fold deixa minúsculo e sem acento: "São Paulo" acha "sao paulo".
func fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, strings.ToLower(strings.TrimSpace(s)))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(s))
	}
	return out
}

// brl formata centavos: 6990 → R$ 69,90.
func brl(cents int) string {
	reais := cents / 100
	var groups []string
	for reais >= 1000 {
		groups = append([]string{fmt.Sprintf("%03d", reais%1000)}, groups...)
		reais /= 1000
	}
	groups = append([]string{fmt.Sprintf("%d", reais)}, groups...)
	return fmt.Sprintf("R$ %s,%02d", strings.Join(groups, "."), cents%100)
}

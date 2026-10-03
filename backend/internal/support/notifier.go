package support

import (
	"context"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
)

// MailNotifier avisa a equipe por e-mail.
type MailNotifier struct {
	Mailer *mail.SupportMailer
	To     []string
}

func (n MailNotifier) Handoff(ctx context.Context, h Handoff) error {
	if len(n.To) == 0 {
		return nil
	}
	return n.Mailer.Handoff(ctx, n.To, mail.Handoff{
		ConversationID: h.ConversationID.String(), Contact: h.Contact, Phone: h.Phone, Reason: h.Reason,
	})
}

package leads

import (
	"context"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/mail"
)

// MailNotifier avisa a equipe por e-mail.
type MailNotifier struct {
	Mailer *mail.LeadMailer
	To     []string
}

func (n MailNotifier) NewLead(ctx context.Context, l *Lead) error {
	if len(n.To) == 0 {
		return nil
	}
	return n.Mailer.NewLead(ctx, n.To, mail.Lead{
		Name: l.Name, Email: l.Email, Phone: l.Phone, City: l.City, Plan: l.Plan,
		VehicleType: l.VehicleType, VehicleCount: l.VehicleCount, Message: l.Message,
	})
}

package smssetup

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/commands"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
)

// seqCommander devolve um desfecho por envio, na ordem.
type seqCommander struct {
	seq  []commands.Command
	sent int
	last commands.Command
}

func (c *seqCommander) Send(context.Context, commands.Request) (*commands.Command, error) {
	c.last = c.seq[min(c.sent, len(c.seq)-1)]
	c.last.ID = uuid.New()
	c.sent++
	if c.last.SentAt == nil {
		return &c.last, nil
	}
	return &commands.Command{ID: c.last.ID, Status: commands.StatusSent}, nil
}

func (c *seqCommander) Get(context.Context, uuid.UUID) (*commands.Command, error) {
	out := c.last
	return &out, nil
}

// O desfecho de um passo pela conexão: a resposta, a recusa (que não vai por
// SMS: lá a resposta seria a mesma) e as novas tentativas com o rastreador fora
// do ar ou sem resposta.
func TestRunOnline(t *testing.T) {
	at := time.Now()
	offline := commands.Command{Status: commands.StatusFailed, Error: commands.ErrNotConnected.Error()}
	silent := commands.Command{Status: commands.StatusTimeout, SentAt: &at}
	ok := commands.Command{Status: commands.StatusAcknowledged, Response: "GMT:W,0,0 OK", SentAt: &at}
	for name, c := range map[string]struct {
		seq    []commands.Command
		status string
		reply  string
		sends  int
	}{
		"responde":               {[]commands.Command{ok}, OnlineConfirmed, "GMT:W,0,0 OK", 1},
		"recusa":                 {[]commands.Command{{Status: commands.StatusFailed, Error: "ERROR:GMT", SentAt: &at}}, OnlineRejected, "ERROR:GMT", 1},
		"fora do ar e volta":     {[]commands.Command{offline, offline, ok}, OnlineConfirmed, "GMT:W,0,0 OK", 3},
		"sem resposta, responde": {[]commands.Command{silent, ok}, OnlineConfirmed, "GMT:W,0,0 OK", 2},
	} {
		cmd := &seqCommander{seq: c.seq}
		s := &Service{commander: cmd, now: time.Now}
		got := s.runOnline(context.Background(), uuid.New(), &devices.Device{}, "+5511999998888", 2,
			built{Step: Step{Kind: StepGMT, Via: ViaConnection}, real: "GMT,W,0,0#"})
		if got.Step != 2 || got.Status != c.status || got.Reply != c.reply || cmd.sent != c.sends {
			t.Errorf("%s: %+v (%d envios)", name, got, cmd.sent)
		}
	}
}

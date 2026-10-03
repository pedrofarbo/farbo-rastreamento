// Package support é o atendimento pelo WhatsApp: as conversas com os
// contatos, respondidas pelo atendente de IA (Claude) ou pela equipe, que
// assume quando a IA transfere ou quando decide entrar na conversa.
package support

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/config"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/fulfillment"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/installers"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/whatsapp"
)

// RuleError é uma ação recusada; a mensagem vai para a tela.
type RuleError struct{ Message string }

func (e RuleError) Error() string { return e.Message }

// maxInboundChars corta mensagem gigante antes de gravar e mandar à IA.
const maxInboundChars = 4000

// Notifier avisa a equipe da conversa transferida pela IA.
type Notifier interface {
	Handoff(ctx context.Context, h Handoff) error
}

// Handoff é o aviso de conversa que precisa de alguém da equipe.
type Handoff struct {
	ConversationID uuid.UUID
	Contact        string
	Phone          string
	Reason         string
}

type Service struct {
	ctx          context.Context
	cfg          config.WhatsApp
	repo         *Repository
	wa           *whatsapp.Client // nil: número não configurado
	ai           *Assistant       // nil: IA desligada
	fulfillments *fulfillment.Repository
	installers   *installers.Repository
	notifier     Notifier
	tz           *time.Location
	log          *slog.Logger

	mu sync.Mutex
	// running: a conversa já tem quem vá respondê-la; again: chegou mensagem
	// enquanto a resposta era gerada.
	running map[uuid.UUID]bool
	again   map[uuid.UUID]bool
}

// NewService monta o atendimento. ctx é o da plataforma: as respostas da IA
// correm em segundo plano e param com ele.
func NewService(ctx context.Context, cfg config.WhatsApp, repo *Repository, wa *whatsapp.Client, ai *Assistant,
	fulfillments *fulfillment.Repository, installerRepo *installers.Repository, notifier Notifier,
	tz *time.Location, log *slog.Logger) *Service {
	return &Service{
		ctx: ctx, cfg: cfg, repo: repo, wa: wa, ai: ai, fulfillments: fulfillments, installers: installerRepo,
		notifier: notifier, tz: tz, log: log.With("component", "whatsapp"),
		running: map[uuid.UUID]bool{}, again: map[uuid.UUID]bool{},
	}
}

func (s *Service) Repo() *Repository { return s.repo }

// Enabled diz se o número do WhatsApp está configurado.
func (s *Service) Enabled() bool { return s.wa != nil }

// AIReady diz se a IA responde os contatos.
func (s *Service) AIReady() bool { return s.wa != nil && s.ai != nil }

// ---------------------------------------------------------------------------
// O que chega
// ---------------------------------------------------------------------------

// Receive grava o que o webhook trouxe e agenda a resposta da IA. Erro faz o
// webhook responder 500 e a Meta mandar de novo (o repetido é ignorado).
func (s *Service) Receive(ctx context.Context, p whatsapp.Payload) error {
	inbound, statuses := p.Events(s.cfg.PhoneNumberID)
	for _, st := range statuses {
		if err := s.repo.setStatus(ctx, st.ID, st.Status, st.Error); err != nil {
			return err
		}
		if st.Status == "failed" {
			s.log.Warn("mensagem do WhatsApp não entregue", "message", st.ID, "error", st.Error)
		}
	}
	for _, in := range inbound {
		// Reação (👍) a uma resposta não pede outra resposta.
		if in.Kind == "reaction" {
			continue
		}
		in.Body = truncate(strings.TrimSpace(in.Body), maxInboundChars)
		customer, err := s.repo.FindCustomer(ctx, in.From)
		if err != nil {
			return err
		}
		conv, err := s.repo.recordInbound(ctx, in, customer)
		if errors.Is(err, errDuplicate) {
			continue
		}
		if err != nil {
			return err
		}
		if conv.Mode == ModeHuman || s.ai == nil {
			if !conv.NeedsAttention {
				if err := s.repo.flagAttention(ctx, conv.ID); err != nil {
					return err
				}
			}
			continue
		}
		s.kick(conv.ID)
	}
	return nil
}

// kick agenda a resposta da conversa. Quem manda várias mensagens seguidas
// recebe uma resposta só, depois que para de escrever.
func (s *Service) kick(id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[id] {
		s.again[id] = true
		return
	}
	s.running[id] = true
	go s.work(id)
}

func (s *Service) work(id uuid.UUID) {
	release := func() {
		delete(s.running, id)
		delete(s.again, id)
	}
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("pânico ao responder no WhatsApp", "conversation", id, "panic", r)
			s.mu.Lock()
			release()
			s.mu.Unlock()
		}
	}()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(s.cfg.AIDebounce):
		}
		s.mu.Lock()
		s.again[id] = false
		s.mu.Unlock()

		if err := s.answer(s.ctx, id); err != nil && s.ctx.Err() == nil {
			s.log.Error("falha ao responder no WhatsApp", "conversation", id, "err", err)
		}

		// Conferir e liberar juntos: uma mensagem que chega no meio não fica
		// sem resposta.
		s.mu.Lock()
		if !s.again[id] {
			release()
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

// answer responde o que o contato mandou desde a última resposta.
func (s *Service) answer(ctx context.Context, id uuid.UUID) error {
	conv, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if conv.Mode != ModeBot || !conv.WindowOpen {
		return nil
	}
	msgs, err := s.repo.Messages(ctx, id, s.cfg.AIHistory)
	if err != nil {
		return err
	}
	if len(msgs) == 0 || msgs[len(msgs)-1].Direction != "IN" {
		return nil
	}
	last := msgs[len(msgs)-1]

	replies, err := s.repo.botRepliesSince(ctx, id, time.Now().Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if replies >= s.cfg.AIMaxRepliesPerDay {
		return s.handOff(ctx, conv, "a IA chegou ao limite de respostas do dia nesta conversa")
	}

	// Lida e "digitando…" enquanto a IA pensa.
	if err := s.wa.MarkRead(ctx, last.WaMessageID, true); err != nil {
		s.log.Debug("não deu para marcar como lida", "conversation", id, "err", err)
	}

	tools := &conversationTools{s: s, conv: conv}
	reply, err := s.ai.Reply(ctx, turnsOf(msgs), Situation{
		ContactName: conv.ContactName, CustomerName: conv.CustomerName, IsCustomer: conv.CustomerID != nil,
		Now: time.Now(),
	}, tools)
	s.log.Info("resposta da IA no WhatsApp", "conversation", id, "rounds", reply.Usage.Rounds,
		"input_tokens", reply.Usage.Input, "output_tokens", reply.Usage.Output,
		"cache_read_tokens", reply.Usage.CacheRead, "cache_write_tokens", reply.Usage.CacheWrite,
		"transferred", reply.Transferred, "refused", reply.Refused)

	text := strings.TrimSpace(reply.Text)
	switch {
	case err != nil && !reply.Transferred:
		s.log.Error("a IA falhou; conversa vai para a equipe", "conversation", id, "err", err)
		return s.handOff(ctx, conv, "a IA não conseguiu responder (falha técnica)")
	case reply.Refused && !reply.Transferred:
		return s.handOff(ctx, conv, "a IA não quis responder esta mensagem")
	case text == "" && !reply.Transferred:
		return s.handOff(ctx, conv, "a IA não produziu resposta")
	case text == "":
		text = s.handoffText()
	}

	// Alguém da equipe assumiu enquanto a IA pensava: a resposta não sai.
	if !reply.Transferred {
		fresh, err := s.repo.Get(ctx, id)
		if err != nil {
			return err
		}
		if fresh.Mode != ModeBot {
			return nil
		}
	}
	if _, err := s.send(ctx, conv, AuthorBot, nil, text); err != nil {
		return err
	}
	if reply.Transferred {
		s.notify(ctx, conv, tools.reason)
	}
	return nil
}

// handOff passa a conversa para a equipe e avisa o contato e a equipe.
func (s *Service) handOff(ctx context.Context, conv *Conversation, reason string) error {
	if err := s.repo.setMode(ctx, conv.ID, ModeHuman, reason, true); err != nil {
		return err
	}
	_, err := s.send(ctx, conv, AuthorBot, nil, s.handoffText())
	s.notify(ctx, conv, reason)
	return err
}

func (s *Service) handoffText() string {
	text := "Recebi sua mensagem! Vou passar a conversa para alguém da equipe, que continua o atendimento por aqui."
	if !inBusinessHours(time.Now().In(s.tz)) {
		text += " Nosso horário é de segunda a sábado, das 8h às 20h — respondemos assim que abrir."
	}
	return text
}

func inBusinessHours(t time.Time) bool {
	return t.Weekday() != time.Sunday && t.Hour() >= 8 && t.Hour() < 20
}

func (s *Service) notify(ctx context.Context, conv *Conversation, reason string) {
	if s.notifier == nil {
		return
	}
	contact := conv.CustomerName
	if contact == "" {
		contact = conv.ContactName
	}
	if err := s.notifier.Handoff(ctx, Handoff{
		ConversationID: conv.ID, Contact: contact, Phone: conv.Phone, Reason: reason,
	}); err != nil {
		s.log.Error("falha ao avisar a equipe da conversa transferida", "conversation", conv.ID, "err", err)
	}
}

// ---------------------------------------------------------------------------
// O que sai
// ---------------------------------------------------------------------------

// send manda o texto (em partes, se passar do limite do WhatsApp) e grava
// cada parte. Devolve a última.
func (s *Service) send(ctx context.Context, conv *Conversation, author string, agentID *uuid.UUID, text string) (*Message, error) {
	var last *Message
	for _, part := range splitText(text, whatsapp.MaxTextLength) {
		waID, err := s.wa.SendText(ctx, conv.WaID, part)
		if err != nil {
			var apiErr *whatsapp.APIError
			if errors.As(err, &apiErr) && apiErr.WindowClosed() {
				return last, RuleError{"passaram 24 horas desde a última mensagem do contato: o WhatsApp só " +
					"deixa responder quando ele escrever de novo"}
			}
			return last, err
		}
		if last, err = s.repo.recordOutbound(ctx, conv.ID, author, agentID, waID, part); err != nil {
			return last, err
		}
	}
	return last, nil
}

// SendAgent: alguém da equipe responde pelo painel. Responder assume a
// conversa (a IA para de responder).
func (s *Service) SendAgent(ctx context.Context, id, agentID uuid.UUID, text string) (*Message, error) {
	if s.wa == nil {
		return nil, RuleError{"WhatsApp não configurado no servidor"}
	}
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return nil, RuleError{"escreva a mensagem"}
	case utf8.RuneCountInString(text) > whatsapp.MaxTextLength:
		return nil, RuleError{fmt.Sprintf("mensagem longa demais: até %d caracteres", whatsapp.MaxTextLength)}
	}
	conv, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !conv.WindowOpen {
		return nil, RuleError{"passaram 24 horas desde a última mensagem do contato: o WhatsApp só deixa " +
			"responder quando ele escrever de novo"}
	}
	if conv.Mode == ModeBot {
		if err := s.repo.setMode(ctx, id, ModeHuman, "assumida pela equipe", false); err != nil {
			return nil, err
		}
	}
	return s.send(ctx, conv, AuthorAgent, &agentID, text)
}

// SetMode: a equipe assume a conversa ou devolve para a IA (que responde o
// que estiver pendente).
func (s *Service) SetMode(ctx context.Context, id uuid.UUID, mode string) (*Conversation, error) {
	switch mode {
	case ModeHuman:
		if err := s.repo.setMode(ctx, id, ModeHuman, "assumida pela equipe", false); err != nil {
			return nil, err
		}
	case ModeBot:
		if s.ai == nil {
			return nil, RuleError{"a IA não está ligada no servidor (ANTHROPIC_API_KEY)"}
		}
		if err := s.repo.setMode(ctx, id, ModeBot, "", false); err != nil {
			return nil, err
		}
		s.kick(id)
	default:
		return nil, RuleError{"modo inválido"}
	}
	return s.repo.Get(ctx, id)
}

// ---------------------------------------------------------------------------
// Conversa → IA
// ---------------------------------------------------------------------------

// turnsOf transforma o histórico em falas. O que a IA não lê (áudio, foto)
// vira um aviso; o que não chegou ao contato fica de fora.
func turnsOf(msgs []*Message) []Turn {
	turns := make([]Turn, 0, len(msgs))
	for _, m := range msgs {
		if m.Direction == "OUT" {
			if m.Status != "failed" {
				turns = append(turns, Turn{Text: m.Body})
			}
			continue
		}
		turns = append(turns, Turn{FromContact: true, Text: inboundText(m.Kind, m.Body)})
	}
	return turns
}

func inboundText(kind, body string) string {
	with := func(notice string) string {
		if body == "" {
			return notice
		}
		return notice + "\n" + body
	}
	switch kind {
	case "text":
		return body
	case "audio":
		return "[O contato mandou um áudio, que você não consegue ouvir.]"
	case "image":
		return with("[O contato mandou uma imagem, que você não consegue ver. Legenda:]")
	case "video":
		return with("[O contato mandou um vídeo, que você não consegue ver. Legenda:]")
	case "document":
		return with("[O contato mandou um documento, que você não consegue abrir. Legenda:]")
	case "sticker":
		return "[O contato mandou uma figurinha.]"
	case "location":
		return "[O contato mandou uma localização: " + body + "]"
	case "contacts":
		return "[O contato compartilhou um contato.]"
	}
	return "[O contato mandou uma mensagem de um tipo que você não consegue ler.]"
}

// splitText quebra em partes de até max caracteres, de preferência entre
// parágrafos ou linhas.
func splitText(text string, max int) []string {
	var parts []string
	for utf8.RuneCountInString(text) > max {
		runes := []rune(text)
		cut := string(runes[:max])
		at := strings.LastIndex(cut, "\n\n")
		if at < len(cut)/2 {
			at = strings.LastIndex(cut, "\n")
		}
		if at < len(cut)/2 {
			at = strings.LastIndex(cut, " ")
		}
		if at <= 0 {
			at = len(cut)
		}
		parts = append(parts, strings.TrimSpace(cut[:at]))
		text = strings.TrimSpace(text[at:])
	}
	if text != "" {
		parts = append(parts, text)
	}
	return parts
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

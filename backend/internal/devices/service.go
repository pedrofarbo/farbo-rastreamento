package devices

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/protocols"
)

type Service struct {
	repo     *Repository
	registry *protocols.ProtocolRegistry
}

func NewService(repo *Repository, registry *protocols.ProtocolRegistry) *Service {
	return &Service{repo: repo, registry: registry}
}

func (s *Service) List(ctx context.Context) ([]*Device, error) { return s.repo.List(ctx) }

func (s *Service) ListByIDs(ctx context.Context, ids []uuid.UUID) ([]*Device, error) {
	return s.repo.ListByIDs(ctx, ids)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Device, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) GetByIMEI(ctx context.Context, imei string) (*Device, error) {
	return s.repo.GetByIMEI(ctx, imei)
}

func (s *Service) Create(ctx context.Context, in Input) (*Device, error) {
	merged, err := mergeInput(nil, in)
	if err != nil {
		return nil, err
	}
	normalized, err := s.validate(merged)
	if err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, normalized)
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (*Device, error) {
	current, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	merged, err := mergeInput(current, in)
	if err != nil {
		return nil, err
	}
	normalized, err := s.validate(merged)
	if err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, id, normalized)
}

// mergeInput aplica a regra de "só escrita" das credenciais (ver Input):
// senha vazia mantém a atual, Clear* apaga, e overrides ausentes ficam como
// estão. current nulo é a criação.
//
// Um override redigido (com ***) que volta igual ao que a leitura mostrou é
// o texto atual reenviado sem mudança: mantém-se o original. Qualquer outro
// texto com *** é recusado — gravá-lo mandaria "***" no lugar da senha.
func mergeInput(current *Device, in Input) (Input, error) {
	if in.ClearAPNPassword && in.APNPassword != "" {
		return in, invalid("informe a nova senha APN ou peça para apagá-la, não os dois")
	}
	if in.ClearCommandPassword && strings.TrimSpace(in.CommandPassword) != "" {
		return in, invalid("informe a nova senha de comando ou peça para apagá-la, não os dois")
	}
	if current == nil {
		current = &Device{}
	}

	switch {
	case in.ClearAPNPassword:
		in.APNPassword = ""
	case in.APNPassword == "":
		in.APNPassword = current.APNPassword
	}
	switch {
	case in.ClearCommandPassword:
		in.CommandPassword = ""
	case strings.TrimSpace(in.CommandPassword) == "":
		in.CommandPassword = current.CommandPassword
	}

	if in.CommandOverrides == nil {
		in.CommandOverrides = current.CommandOverrides
		return in, nil
	}
	secrets := current.Secrets()
	merged := make(map[string]string, len(in.CommandOverrides))
	for key, value := range in.CommandOverrides {
		if hasRedaction(value) {
			previous, ok := current.CommandOverrides[strings.ToUpper(strings.TrimSpace(key))]
			if !ok || RedactText(previous, secrets) != strings.TrimSpace(value) {
				return in, invalid("o texto de %s contém %q: digite o texto completo, "+
					"com a senha, ou envie-o como a leitura mostrou para mantê-lo", key, Redacted)
			}
			value = previous
		}
		merged[key] = value
	}
	in.CommandOverrides = merged
	return in, nil
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

func (s *Service) Touch(ctx context.Context, id uuid.UUID, seenAt time.Time) error {
	return s.repo.Touch(ctx, id, seenAt)
}

func (s *Service) SetProtocol(ctx context.Context, id uuid.UUID, protocol string) error {
	return s.repo.SetProtocol(ctx, id, protocol)
}

func (s *Service) SweepStatuses(ctx context.Context, stale, offline time.Duration) ([]StatusChange, error) {
	return s.repo.SweepStatuses(ctx, stale, offline)
}

func (s *Service) CountOnline(ctx context.Context) (int, error) { return s.repo.CountOnline(ctx) }

// ValidationError descreve um payload recusado.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return ValidationError{Message: fmt.Sprintf(format, args...)}
}

func (s *Service) validate(in Input) (Input, error) {
	in.IMEI = strings.TrimSpace(in.IMEI)
	if err := protocols.ValidateIMEI(in.IMEI); err != nil {
		return in, invalid("IMEI inválido: %v", err)
	}

	in.Protocol = strings.ToLower(strings.TrimSpace(in.Protocol))
	if in.Protocol != "" {
		if _, ok := s.registry.ByName(in.Protocol); !ok {
			return in, invalid("protocolo %q não está registrado", in.Protocol)
		}
	}

	if in.ServerPort != nil && (*in.ServerPort < 1 || *in.ServerPort > 65535) {
		return in, invalid("porta do servidor fora da faixa 1..65535")
	}
	if in.ReportIntervalSeconds != nil && (*in.ReportIntervalSeconds < 5 || *in.ReportIntervalSeconds > 86400) {
		return in, invalid("intervalo de envio fora da faixa 5..86400 segundos")
	}
	if in.HeartbeatIntervalSeconds != nil && (*in.HeartbeatIntervalSeconds < 30 || *in.HeartbeatIntervalSeconds > 86400) {
		return in, invalid("intervalo de heartbeat fora da faixa 30..86400 segundos")
	}

	// Os overrides são texto bruto enviado ao aparelho: sanitizamos aqui para
	// que não seja possível injetar comandos extras pelo cadastro (§27).
	clean := map[string]string{}
	for key, value := range in.CommandOverrides {
		cmdType := protocols.CommandType(strings.ToUpper(strings.TrimSpace(key)))
		if !cmdType.Valid() {
			return in, invalid("override para comando desconhecido: %q", key)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len(value) > 200 {
			return in, invalid("override de %s é longo demais", cmdType)
		}
		for _, r := range value {
			if r < 0x20 || r > 0x7E {
				return in, invalid("override de %s contém caractere de controle", cmdType)
			}
		}
		clean[string(cmdType)] = value
	}
	in.CommandOverrides = clean

	if pwd := strings.TrimSpace(in.CommandPassword); pwd != "" {
		for _, r := range pwd {
			isDigit := r >= '0' && r <= '9'
			isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
			if !isDigit && !isLetter {
				return in, invalid("senha de comando aceita apenas letras e números")
			}
		}
		if len(pwd) > 16 {
			return in, invalid("senha de comando longa demais")
		}
		in.CommandPassword = pwd
	}

	return in, nil
}

// ProvisioningCommands devolve os comandos sugeridos para apontar o aparelho
// para este servidor. Nada é enviado automaticamente (§30): a interface mostra
// o texto e o operador decide.
//
// O texto sai com as credenciais redigidas (***), como todo texto que deixa o
// backend: a tela é só do admin, mas a senha continua sendo só de escrita.
// Quem manda o SMS digita a senha de comando no lugar do *** — Redacted
// avisa quando isso é preciso.
type ProvisioningCommand struct {
	Type        protocols.CommandType `json:"type"`
	Description string                `json:"description"`
	Text        string                `json:"text"`
	Available   bool                  `json:"available"`
	Reason      string                `json:"reason,omitempty"`
	Redacted    bool                  `json:"redacted,omitempty"`
}

func (s *Service) ProvisioningCommands(dev *Device) []ProvisioningCommand {
	out := []ProvisioningCommand{}
	proto, ok := s.registry.ByName(dev.Protocol)
	if !ok {
		return out
	}

	secrets := dev.Secrets()
	build := func(cmdType protocols.CommandType, description string, params map[string]string) {
		entry := ProvisioningCommand{Type: cmdType, Description: description}
		raw, err := proto.EncodeCommand(protocols.Command{
			Type:     cmdType,
			UniqueID: dev.IMEI,
			Password: dev.CommandPassword,
			Params:   params,
			Raw:      dev.CommandOverrides[string(cmdType)],
		})
		if err != nil {
			entry.Reason = RedactText(err.Error(), secrets)
		} else {
			redacted := RedactBytes(raw, secrets)
			entry.Available = true
			entry.Redacted = !bytes.Equal(redacted, raw)
			entry.Text = printableCommand(redacted)
		}
		out = append(out, entry)
	}

	if dev.ServerHost != "" && dev.ServerPort != nil {
		build(protocols.CommandSetServer, "Aponta o rastreador para este servidor",
			map[string]string{"host": dev.ServerHost, "port": fmt.Sprint(*dev.ServerPort)})
	}
	if dev.ReportIntervalSeconds != nil {
		build(protocols.CommandSetInterval, "Define o intervalo de envio de posição",
			map[string]string{"seconds": fmt.Sprint(*dev.ReportIntervalSeconds)})
	}
	if dev.HeartbeatIntervalSeconds != nil {
		build(protocols.CommandSetHeartbeat, "Define o intervalo de heartbeat",
			map[string]string{"minutes": fmt.Sprint(*dev.HeartbeatIntervalSeconds / 60)})
	}
	return out
}

// printableCommand mostra o comando de forma legível, sem exibir bytes crus.
func printableCommand(raw []byte) string {
	var sb strings.Builder
	for _, b := range raw {
		if b >= 0x20 && b <= 0x7E {
			sb.WriteByte(b)
		} else {
			fmt.Fprintf(&sb, "\\x%02X", b)
		}
	}
	return sb.String()
}

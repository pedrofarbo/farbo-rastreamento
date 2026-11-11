package affiliates

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/orders"
)

// A senha da página do afiliado: com ela, o link secreto (/parceiro/<token>)
// só mostra os números e o cadastro de recebedor depois da senha, que a
// central define no cadastro do afiliado. A entrada vira uma sessão assinada
// de 30 dias, que cai quando a senha ou o link da página mudam.

var (
	// ErrPartnerPassword: a página pede a senha (sem sessão que valha).
	ErrPartnerPassword = errors.New("esta página pede a senha: se não tiver, peça à Farbo")
	// ErrWrongPartnerPassword: a senha não confere.
	ErrWrongPartnerPassword = errors.New("senha incorreta")
)

// errManageNeedsPassword: a gestão das instalações pelo representante mostra
// os dados dos clientes na página dele, que então precisa de senha.
var errManageNeedsPassword = invalid("Para o representante cuidar das instalações, a página dele precisa de senha (ela mostra os dados dos clientes).")

const (
	partnerSessionTTL = 30 * 24 * time.Hour
	// minReportPassword e maxReportPassword: o bcrypt só usa 72 bytes.
	minReportPassword = 8
	maxReportPassword = 72
)

// SetSessionKey deriva a chave das sessões da página do afiliado do segredo
// do servidor (sem ela, uma chave aleatória: as sessões caem ao reiniciar).
func (s *Service) SetSessionKey(secret []byte) {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("farbo-partner-session-v1"))
	s.sessionKey = mac.Sum(nil)
}

func randomKey() []byte {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return key
}

// validReportPassword confere a senha nova da página.
func validReportPassword(password string) error {
	switch {
	case len(password) < minReportPassword:
		return invalid("A senha da página precisa de pelo menos %d caracteres.", minReportPassword)
	case len(password) > maxReportPassword:
		return invalid("A senha da página pode ter no máximo %d caracteres.", maxReportPassword)
	}
	return nil
}

// hashReportPassword é o que vai para o banco: o hash da senha nova, ou vazio
// para tirar a senha (nil: não muda).
func hashReportPassword(password *string) (*string, error) {
	if password == nil || *password == "" {
		return password, nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	out := string(hash)
	return &out, nil
}

// reportPassword é o hash da senha da página pelo link secreto (vazio: sem
// senha); database.ErrNotFound se o link não vale.
func (s *Service) reportPassword(ctx context.Context, token string) (string, error) {
	if len(token) < 20 || len(token) > 64 {
		return "", database.ErrNotFound
	}
	var hash string
	err := s.db.QueryRow(ctx, `SELECT report_password_hash FROM affiliates WHERE report_token = $1`, token).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", database.ErrNotFound
	}
	return hash, database.MapError(err)
}

// CheckPartner confere o acesso à página do afiliado: sem senha, o link
// basta; com senha, a sessão da entrada. Um link que não vale passa (a
// página responde que não o achou).
func (s *Service) CheckPartner(ctx context.Context, token, session string) error {
	hash, err := s.reportPassword(ctx, token)
	if errors.Is(err, database.ErrNotFound) || (err == nil && hash == "") {
		return nil
	}
	if err != nil {
		return err
	}
	if !s.validSession(token, hash, session) {
		return ErrPartnerPassword
	}
	return nil
}

// PartnerLogin confere a senha da página e devolve a sessão (e até quando ela vale).
func (s *Service) PartnerLogin(ctx context.Context, token, password string) (string, time.Time, error) {
	hash, err := s.reportPassword(ctx, token)
	if err != nil {
		return "", time.Time{}, err
	}
	if hash == "" {
		return "", time.Time{}, invalid("Esta página não pede senha.")
	}
	if len(password) > maxReportPassword || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", time.Time{}, ErrWrongPartnerPassword
	}
	expires := s.now().Add(partnerSessionTTL).Truncate(time.Second)
	return s.sign(token, hash, expires.Unix()), expires, nil
}

// sign: "<validade>.<assinatura>", ligada ao link e à senha da página.
func (s *Service) sign(token, hash string, expires int64) string {
	mac := hmac.New(sha256.New, s.sessionKey)
	mac.Write([]byte(token + "|" + hash + "|" + strconv.FormatInt(expires, 10)))
	return strconv.FormatInt(expires, 10) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) validSession(token, hash, session string) bool {
	raw, _, ok := strings.Cut(session, ".")
	if !ok {
		return false
	}
	expires, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || s.now().Unix() >= expires {
		return false
	}
	return hmac.Equal([]byte(session), []byte(s.sign(token, hash, expires)))
}

// Manager é o representante que cuida das instalações dos clientes dele.
type Manager struct {
	ID   uuid.UUID
	Name string
}

// Manager é o representante do link secreto com a gestão das instalações
// ligada (ativo e com a senha da página); nil se não é.
func (s *Service) Manager(ctx context.Context, token string) (*Manager, error) {
	if len(token) < 20 || len(token) > 64 {
		return nil, nil
	}
	var m Manager
	err := s.db.QueryRow(ctx, `SELECT id, name FROM affiliates
		WHERE report_token = $1 AND active AND manage_installs AND report_password_hash <> ''`, token).Scan(&m.ID, &m.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return &m, nil
}

// SetPartnerInstall: o representante que recebe direto do cliente define, pela
// página dele, os valores da instalação (sem e, se ele oferece a escolha, com
// o relé do bloqueio — nil: sem a escolha) e da desinstalação (eles não passam
// pela plataforma: só aparecem para o cliente). Só no modo direto.
func (s *Service) SetPartnerInstall(ctx context.Context, token string, installCents, uninstallCents int, blockingCents *int) (*orders.InstallPrices, error) {
	prices := orders.InstallPrices{Mode: orders.InstallDirect, InstallCents: installCents, UninstallCents: uninstallCents,
		BlockingCents: blockingCents}
	if !prices.Valid() {
		return nil, invalid("Valores inválidos: de R$ 0 a R$ 100.000.")
	}
	if len(token) < 20 || len(token) > 64 {
		return nil, database.ErrNotFound
	}
	err := s.db.QueryRow(ctx, `UPDATE affiliates SET install_cents = $2, uninstall_cents = $3, install_blocking_cents = $4,
		updated_at = NOW() WHERE report_token = $1 AND install_mode = 'direct' RETURNING loan_surcharge_cents`,
		token, installCents, uninstallCents, blockingCents).Scan(&prices.GuaranteeCents)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, invalid("Os valores da instalação são da Farbo para o seu link: fale com a Farbo para mudar.")
	}
	if err != nil {
		return nil, database.MapError(err)
	}
	return &prices, nil
}

// PartnerAccount é o afiliado do link secreto da página e se ela tem senha
// (database.ErrNotFound se o link não vale).
func (s *Service) PartnerAccount(ctx context.Context, token string) (uuid.UUID, bool, error) {
	if len(token) < 20 || len(token) > 64 {
		return uuid.Nil, false, database.ErrNotFound
	}
	var id uuid.UUID
	var protected bool
	err := s.db.QueryRow(ctx, `SELECT id, report_password_hash <> '' FROM affiliates WHERE report_token = $1`, token).
		Scan(&id, &protected)
	return id, protected, database.MapError(err)
}

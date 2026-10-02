package stepup

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Verificação do WebAuthn (biometria do aparelho) só com a biblioteca padrão.
//
// O navegador entrega a chave pública pronta em SubjectPublicKeyInfo
// (AuthenticatorAttestationResponse.getPublicKey), então não há CBOR para
// decodificar. O cadastro usa attestation "none": quem cadastra já provou a
// senha da conta, e a partir daí cada uso exige a assinatura da chave que só
// o aparelho tem, com a verificação do usuário (rosto, digital) marcada pelo
// próprio autenticador nos dados assinados.

// Algoritmos COSE aceitos: o ES256 (iPhone, Android, Mac) e o RS256 (Windows
// Hello).
const (
	AlgES256 = -7
	AlgRS256 = -257
)

// Bits de authenticatorData.flags.
const (
	flagUserPresent  = 0x01
	flagUserVerified = 0x04
	flagAttested     = 0x40
)

// ErrVerification é a recusa da biometria (assinatura, origem, desafio...).
var ErrVerification = errors.New("biometria não confirmada")

type clientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

// relyingParty é o que o WebAuthn confere em todo pedido: o domínio das
// credenciais e de onde o pedido pode vir.
type relyingParty struct {
	id      string
	origins []string
}

func refuse(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrVerification, fmt.Sprintf(format, args...))
}

// checkClientData confere o tipo, o desafio e a origem do clientDataJSON.
func (rp relyingParty) checkClientData(raw []byte, wantType string, challenge []byte) error {
	var cd clientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return refuse("clientDataJSON inválido")
	}
	if cd.Type != wantType {
		return refuse("tipo %q, esperado %q", cd.Type, wantType)
	}
	got, err := base64.RawURLEncoding.DecodeString(cd.Challenge)
	if err != nil || subtle.ConstantTimeCompare(got, challenge) != 1 {
		return refuse("desafio diferente do emitido")
	}
	if cd.CrossOrigin || !slices.Contains(rp.origins, cd.Origin) {
		return refuse("origem %q não autorizada", cd.Origin)
	}
	return nil
}

type authData struct {
	flags     byte
	signCount uint32
	// credentialID só vem no cadastro (flag AT).
	credentialID []byte
}

// parseAuthData lê o authenticatorData e confere o domínio e a verificação
// do usuário.
func (rp relyingParty) parseAuthData(raw []byte, attested bool) (*authData, error) {
	if len(raw) < 37 {
		return nil, refuse("authenticatorData curto demais")
	}
	want := sha256.Sum256([]byte(rp.id))
	if subtle.ConstantTimeCompare(raw[:32], want[:]) != 1 {
		return nil, refuse("credencial de outro domínio")
	}
	ad := &authData{flags: raw[32], signCount: binary.BigEndian.Uint32(raw[33:37])}
	if ad.flags&flagUserPresent == 0 || ad.flags&flagUserVerified == 0 {
		return nil, refuse("o aparelho não verificou o usuário (biometria ou código)")
	}
	if !attested {
		return ad, nil
	}
	if ad.flags&flagAttested == 0 || len(raw) < 55 {
		return nil, refuse("cadastro sem dados da credencial")
	}
	size := int(binary.BigEndian.Uint16(raw[53:55]))
	if size == 0 || len(raw) < 55+size {
		return nil, refuse("id da credencial inválido")
	}
	ad.credentialID = raw[55 : 55+size]
	return ad, nil
}

// parsePublicKey lê a chave do cadastro e confere que bate com o algoritmo.
func parsePublicKey(spki []byte, alg int) (crypto.PublicKey, error) {
	key, err := x509.ParsePKIXPublicKey(spki)
	if err != nil {
		return nil, refuse("chave pública ilegível")
	}
	switch k := key.(type) {
	case *ecdsa.PublicKey:
		if alg != AlgES256 || k.Curve != elliptic.P256() {
			return nil, refuse("chave EC fora do ES256")
		}
	case *rsa.PublicKey:
		if alg != AlgRS256 || k.N.BitLen() < 2048 {
			return nil, refuse("chave RSA fora do RS256 de 2048 bits")
		}
	default:
		return nil, refuse("tipo de chave não aceito")
	}
	return key, nil
}

// verifySignature confere a assinatura de authenticatorData || SHA-256(clientDataJSON).
func verifySignature(key crypto.PublicKey, authenticatorData, clientDataJSON, signature []byte) error {
	clientHash := sha256.Sum256(clientDataJSON)
	digest := sha256.Sum256(append(bytes.Clone(authenticatorData), clientHash[:]...))
	switch k := key.(type) {
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(k, digest[:], signature) {
			return refuse("assinatura inválida")
		}
	case *rsa.PublicKey:
		if rsa.VerifyPKCS1v15(k, crypto.SHA256, digest[:], signature) != nil {
			return refuse("assinatura inválida")
		}
	default:
		return refuse("tipo de chave não aceito")
	}
	return nil
}

// RegistrationResponse é o que o navegador devolve do credentials.create,
// em base64url.
type RegistrationResponse struct {
	ChallengeID       string `json:"challengeId"`
	CredentialID      string `json:"credentialId"`
	ClientDataJSON    string `json:"clientDataJSON"`
	AuthenticatorData string `json:"authenticatorData"`
	PublicKey         string `json:"publicKey"`
	Algorithm         int    `json:"algorithm"`
	Name              string `json:"name"`
}

// AssertionResponse é o que o navegador devolve do credentials.get.
type AssertionResponse struct {
	ChallengeID       string `json:"challengeId"`
	CredentialID      string `json:"credentialId"`
	ClientDataJSON    string `json:"clientDataJSON"`
	AuthenticatorData string `json:"authenticatorData"`
	Signature         string `json:"signature"`
}

func decode(field, value string) ([]byte, error) {
	out, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(out) == 0 {
		return nil, refuse("%s inválido", field)
	}
	return out, nil
}

package leads

import (
	"errors"
	"strings"
	"testing"
)

func valid() Input {
	return Input{
		Name: "  Ana   Souza ", Email: " Ana@Exemplo.com.br ", Phone: "(11) 98888-7777", City: " São  Paulo ",
		Plan: "Plano Mensal - R$ 69,90", VehicleType: "Moto", VehicleCount: 2, Consent: true,
	}
}

func TestNormalize(t *testing.T) {
	got, err := Normalize(valid())
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Ana Souza" || got.Email != "ana@exemplo.com.br" || got.City != "São Paulo" || got.VehicleType != "moto" {
		t.Errorf("normalizado = %+v", got)
	}

	noCount := valid()
	noCount.VehicleCount = 0
	if got, err := Normalize(noCount); err != nil || got.VehicleCount != 1 {
		t.Errorf("sem quantidade = %d, %v (quer 1)", got.VehicleCount, err)
	}
	noPhone := valid()
	noPhone.Phone = ""
	if _, err := Normalize(noPhone); err == nil {
		t.Error("WhatsApp é obrigatório: aceitou sem")
	}
}

func TestNormalizeRejects(t *testing.T) {
	cases := map[string]func(*Input){
		"sem nome":            func(in *Input) { in.Name = " " },
		"e-mail sem domínio":  func(in *Input) { in.Email = "ana@exemplo" },
		"e-mail com nome":     func(in *Input) { in.Email = "Ana <ana@exemplo.com>" },
		"e-mail inválido":     func(in *Input) { in.Email = "ana" },
		"telefone curto":      func(in *Input) { in.Phone = "9999-0000" },
		"tipo desconhecido":   func(in *Input) { in.VehicleType = "barco" },
		"frota grande demais": func(in *Input) { in.VehicleCount = 501 },
		"mensagem enorme":     func(in *Input) { in.Message = strings.Repeat("a", 1001) },
		"sem consentimento":   func(in *Input) { in.Consent = false },
	}
	for name, change := range cases {
		in := valid()
		change(&in)
		var invalid ValidationError
		if _, err := Normalize(in); !errors.As(err, &invalid) {
			t.Errorf("%s: aceito (%v)", name, err)
		}
	}
}

func TestNormalizeWaitlist(t *testing.T) {
	const phone = "(11) 98888-7777"
	got, err := normalizeWaitlist(WaitlistInput{Name: "  Bruno ", Email: " Bruno@Exemplo.com ", Phone: " " + phone, Consent: true})
	if err != nil || got.Name != "Bruno" || got.Email != "bruno@exemplo.com" || got.Phone != phone {
		t.Fatalf("normalizado = %+v, %v", got, err)
	}
	if _, err := normalizeWaitlist(WaitlistInput{Email: "bruno@exemplo.com", Phone: phone, Consent: true}); err != nil {
		t.Errorf("nome é opcional: %v", err)
	}
	for name, in := range map[string]WaitlistInput{
		"sem consentimento": {Email: "bruno@exemplo.com", Phone: phone},
		"e-mail inválido":   {Email: "bruno", Phone: phone, Consent: true},
		"sem WhatsApp":      {Email: "bruno@exemplo.com", Consent: true},
		"WhatsApp sem DDD":  {Email: "bruno@exemplo.com", Phone: "98888-7777", Consent: true},
		"nome enorme":       {Name: strings.Repeat("a", 121), Email: "bruno@exemplo.com", Phone: phone, Consent: true},
	} {
		var invalid ValidationError
		if _, err := normalizeWaitlist(in); !errors.As(err, &invalid) {
			t.Errorf("%s: aceito (%v)", name, err)
		}
	}
}

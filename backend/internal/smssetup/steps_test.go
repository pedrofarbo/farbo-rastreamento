package smssetup

import (
	"strings"
	"testing"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
)

func texts(steps []built) (real, shown []string) {
	for _, s := range steps {
		real, shown = append(real, s.real), append(shown, s.Text)
	}
	return real, shown
}

func TestBuildSteps(t *testing.T) {
	defaults := Defaults{ServerHost: "farborastreadores.com.br", ServerPort: 5000, APN: "smart.m2m.vivo.com.br",
		APNUser: "vivo", APNPassword: "Segr3d0", ReportSeconds: 30, ParkedSeconds: 3600}

	// Sem APN no cadastro: o padrão, com a senha redigida na tela.
	steps, problems := buildSteps(&devices.Device{Protocol: "gt06"}, defaults, Options{})
	real, shown := texts(steps)
	if len(problems) > 0 || strings.Join(real, " ") != "APN,smart.m2m.vivo.com.br,vivo,Segr3d0# SERVER,1,farborastreadores.com.br,5000,0# GMT,W,0,0# TIMER,30,3600#" {
		t.Fatalf("padrão = %v %v", real, problems)
	}
	if shown[0] != "APN,smart.m2m.vivo.com.br,vivo,***#" {
		t.Errorf("na tela = %v", shown)
	}
	// O contador da página de rastreadores conta com isso.
	if len(steps) != ActivationSMS {
		t.Errorf("a ativação padrão manda %d SMS, ActivationSMS diz %d", len(steps), ActivationSMS)
	}

	// O cadastro do rastreador manda: outro APN (com o usuário e a senha
	// dele), servidor por IP, intervalo próprio; com os opcionais.
	port, report := 7000, 60
	dev := &devices.Device{Protocol: "", APN: "zap.vivo.com.br", APNUser: "vivo", APNPassword: "vivo", ServerHost: "143.95.169.244",
		ServerPort: &port, ReportIntervalSeconds: &report}
	steps, problems = buildSteps(dev, defaults, Options{Unlock: true, Query: true})
	real, _ = texts(steps)
	want := "CMDLOCK,123456,0# APN,zap.vivo.com.br,vivo,vivo# SERVER,0,143.95.169.244,7000,0# GMT,W,0,0# TIMER,60,3600# PARAM#"
	if len(problems) > 0 || strings.Join(real, " ") != want {
		t.Fatalf("do cadastro = %v %v", real, problems)
	}
	if steps[0].Kind != StepUnlock || steps[len(steps)-1].Kind != StepParam {
		t.Errorf("ordem = %+v", steps)
	}

	// Sem intervalo no padrão nem no cadastro: 10 s ligado (testado em campo).
	noReport := defaults
	noReport.ReportSeconds = 0
	steps, _ = buildSteps(&devices.Device{}, noReport, Options{})
	if real, _ = texts(steps); real[len(real)-1] != "TIMER,10,3600#" {
		t.Errorf("intervalo padrão = %v", real)
	}

	// O mesmo APN do padrão no cadastro, sem usuário e senha: vêm do padrão.
	steps, problems = buildSteps(&devices.Device{APN: "smart.m2m.vivo.com.br"}, defaults, Options{})
	if real, _ = texts(steps); len(problems) > 0 || real[0] != "APN,smart.m2m.vivo.com.br,vivo,Segr3d0#" {
		t.Errorf("mesmo APN do padrão = %v %v", real, problems)
	}

	// O que falta, ou não serve.
	for name, c := range map[string]struct {
		dev  *devices.Device
		d    Defaults
		want string
	}{
		"protocolo": {&devices.Device{Protocol: "h02"}, defaults, "J16"},
		"sem APN":   {&devices.Device{}, Defaults{ServerHost: "x.com", ServerPort: 5000}, "APN"},
		"sem senha": {&devices.Device{APN: "outro.apn", APNUser: "u"}, defaults, "usuário e da senha"},
		"vírgula":   {&devices.Device{APN: "a,b", APNUser: "u", APNPassword: "p"}, defaults, "caracteres"},
		"sem host":  {&devices.Device{APN: "zap"}, Defaults{}, "servidor"},
	} {
		if _, problems := buildSteps(c.dev, c.d, Options{}); len(problems) == 0 || !strings.Contains(strings.Join(problems, " "), c.want) {
			t.Errorf("%s: %v", name, problems)
		}
	}
}

func TestE164(t *testing.T) {
	for in, want := range map[string]string{
		"(11) 99999-8888":   "+5511999998888",
		"11999998888":       "+5511999998888",
		"1133334444":        "+551133334444",
		"55 11 99999-8888":  "+5511999998888",
		"+55 11 99999-8888": "+5511999998888",
		"+1 415 867 5310":   "+14158675310",
		"99999-8888":        "",
		"":                  "",
	} {
		if got := E164(in); got != want {
			t.Errorf("E164(%q) = %q, quero %q", in, got, want)
		}
	}
}

// Package smssetup configura o rastreador por SMS na ativação: manda os
// comandos de configuração (APN, servidor, fuso, intervalo) para o número do
// chip pelo Twilio, um por vez, e espera o rastreador conectar no servidor —
// a prova de que a configuração pegou. A sintaxe é a do J16 (GT06), em
// docs/J16.md.
package smssetup

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/devices"
)

// Os passos possíveis, na ordem em que saem.
const (
	StepUnlock = "UNLOCK" // destrava o canal de comandos (alguns lotes vêm travados)
	StepAPN    = "APN"
	StepServer = "SERVER"
	StepGMT    = "GMT"
	StepTimer  = "TIMER"
	StepParam  = "PARAM" // pede a configuração de volta, por SMS
)

// Defaults é o que vale quando o cadastro do rastreador não diz.
type Defaults struct {
	ServerHost string
	ServerPort int
	// O APN dos chips (todos da mesma operadora); o cadastro do rastreador
	// tem precedência.
	APN         string
	APNUser     string
	APNPassword string
	// Intervalos do TIMER: ligado (o do cadastro, se houver) e desligado.
	ReportSeconds int
	ParkedSeconds int
}

// Options são os passos opcionais.
type Options struct {
	// Unlock manda antes CMDLOCK com a senha de fábrica.
	Unlock bool
	// Query manda PARAM# no fim (a resposta chega se o chip puder responder).
	Query bool
}

// Step é um passo, como a tela mostra (o texto redigido: sem as senhas).
type Step struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Text  string `json:"text"`
}

// built é o passo com o texto de verdade, que só existe na hora de enviar.
type built struct {
	Step
	real string
}

// factoryPassword é a senha de fábrica reportada para o J16 (docs/J16.md).
const factoryPassword = "123456"

// safeValue: o que pode ir dentro de um comando sem quebrar a sintaxe
// (vírgula e # separam os campos).
var safeValue = regexp.MustCompile(`^[A-Za-z0-9._@:/\-]*$`)

// supported diz se o protocolo é configurado por SMS aqui: o do J16 (GT06),
// ou ainda em branco (o aparelho nunca conectou e a detecção é automática).
func supported(protocol string) bool {
	p := strings.ToLower(strings.TrimSpace(protocol))
	return p == "" || p == "gt06"
}

// buildSteps monta os comandos do rastreador. problems diz o que falta no
// cadastro para mandar.
func buildSteps(dev *devices.Device, d Defaults, opts Options) (steps []built, problems []string) {
	if !supported(dev.Protocol) {
		return nil, []string{fmt.Sprintf("A configuração por SMS é do J16 (protocolo GT06); este rastreador é %s.", dev.Protocol)}
	}
	// O APN do cadastro do rastreador; sem ele, o padrão. O usuário e a senha
	// que faltarem no cadastro vêm do padrão quando o APN é o mesmo.
	apn, user, pass := strings.TrimSpace(dev.APN), strings.TrimSpace(dev.APNUser), strings.TrimSpace(dev.APNPassword)
	if apn == "" {
		apn, user, pass = strings.TrimSpace(d.APN), strings.TrimSpace(d.APNUser), strings.TrimSpace(d.APNPassword)
	} else if strings.EqualFold(apn, strings.TrimSpace(d.APN)) {
		if user == "" {
			user = strings.TrimSpace(d.APNUser)
		}
		if pass == "" {
			pass = strings.TrimSpace(d.APNPassword)
		}
	}
	host, port := strings.TrimSpace(dev.ServerHost), 0
	if dev.ServerPort != nil {
		port = *dev.ServerPort
	}
	if host == "" || port == 0 {
		host, port = d.ServerHost, d.ServerPort
	}
	report := d.ReportSeconds
	if dev.ReportIntervalSeconds != nil && *dev.ReportIntervalSeconds > 0 {
		report = *dev.ReportIntervalSeconds
	}

	switch {
	case apn == "":
		problems = append(problems, "Falta o APN do chip: preencha no cadastro do rastreador.")
	case user == "" || pass == "":
		// Testado em campo: o J16 só pega o APN com o usuário e a senha.
		problems = append(problems, "O APN precisa do usuário e da senha: preencha no cadastro do rastreador.")
	case !safeValue.MatchString(apn) || !safeValue.MatchString(user) || !safeValue.MatchString(pass):
		problems = append(problems, "O APN, o usuário ou a senha do APN têm caracteres que o SMS não aceita (vírgula, #, espaço).")
	}
	if host == "" || port <= 0 || port > 65535 || !safeValue.MatchString(host) {
		problems = append(problems, "Falta o endereço do servidor (TRACKER_PUBLIC_HOST e a porta).")
	}
	if report < 10 || report > 86400 {
		report = 10
	}
	parked := d.ParkedSeconds
	if parked < 60 || parked > 86400 {
		parked = 3600
	}
	if len(problems) > 0 {
		return nil, problems
	}

	secrets := dev.Secrets()
	if pass != "" && pass != dev.APNPassword {
		secrets = append(secrets, pass)
	}
	add := func(kind, label, text string) {
		shown := devices.RedactText(text, secrets)
		steps = append(steps, built{Step: Step{Kind: kind, Label: label, Text: shown}, real: text})
	}
	if opts.Unlock {
		add(StepUnlock, "Destravar o canal de comandos", "CMDLOCK,"+factoryPassword+",0#")
	}
	// A ordem testada em campo, um comando por vez: APN (com usuário e
	// senha), servidor, fuso e a frequência das posições.
	add(StepAPN, "APN do chip, com usuário e senha", fmt.Sprintf("APN,%s,%s,%s#", apn, user, pass))
	mode := "1" // domínio
	if net.ParseIP(host) != nil {
		mode = "0"
	}
	add(StepServer, "Servidor da Farbo", fmt.Sprintf("SERVER,%s,%s,%d,0#", mode, host, port))
	// O GT06 manda a hora em UTC: o fuso fica zerado (o servidor converte).
	add(StepGMT, "Fuso horário (UTC)", "GMT,W,0,0#")
	add(StepTimer, fmt.Sprintf("Posição a cada %ds em movimento, %ds parado", report, parked), fmt.Sprintf("TIMER,%d,%d#", report, parked))
	if opts.Query {
		add(StepParam, "Pedir a configuração de volta", "PARAM#")
	}
	return steps, nil
}

// E164 deixa o número do chip no formato do Twilio: +55 e o DDD. Vazio se
// não der.
func E164(phone string) string {
	d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	switch {
	case strings.HasPrefix(strings.TrimSpace(phone), "+") && len(d) >= 10 && len(d) <= 15:
		return "+" + d
	case len(d) == 10 || len(d) == 11:
		return "+55" + d
	case (len(d) == 12 || len(d) == 13) && strings.HasPrefix(d, "55"):
		return "+" + d
	}
	return ""
}

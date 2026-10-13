package billing

import (
	"encoding/json"
	"fmt"
	"time"
)

// Date é uma data de calendário, sem hora nem fuso: vencimento é "dia 10",
// não um instante. Em JSON vira "2026-10-10" — com hora, o navegador
// converteria meia-noite UTC para o dia anterior no horário de Brasília.
type Date struct{ time.Time }

func NewDate(year int, month time.Month, day int) Date {
	return Date{time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// DateIn devolve a data do calendário de t no fuso informado.
func DateIn(t time.Time, loc *time.Location) Date {
	local := t.In(loc)
	return NewDate(local.Year(), local.Month(), local.Day())
}

func (d Date) AddDays(n int) Date { return Date{d.Time.AddDate(0, 0, n)} }

func (d Date) Before(other Date) bool { return d.Time.Before(other.Time) }

func (d Date) String() string { return d.Format(time.DateOnly) }

func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d *Date) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return fmt.Errorf("data inválida %q: use AAAA-MM-DD", raw)
	}
	*d = Date{parsed}
	return nil
}

// nextMonthly devolve o vencimento do mês seguinte, no mesmo dia.
func nextMonthly(d Date, dueDay int) Date {
	return NewDate(d.Year(), d.Month()+1, dueDay)
}

// followingDue é o vencimento depois de `d`: no dia da conta, pelo menos um
// mês depois. Para um vencimento no dia da conta, é o do mês seguinte; para a
// 1ª mensalidade, que vence no dia da contratação, a 2ª nunca vem colada nela
// (contratando dia 28 com a conta no dia 10, vence dia 10 do mês depois do
// seguinte).
func followingDue(d Date, dueDay int) Date {
	return firstDueDate(addMonth(d), dueDay)
}

// addMonth é a mesma data no mês seguinte; sem esse dia (31/01), o último do
// mês (28 ou 29/02).
func addMonth(d Date) Date {
	last := NewDate(d.Year(), d.Month()+2, 0).Day()
	return NewDate(d.Year(), d.Month()+1, min(d.Day(), last))
}

// firstDueDate é o primeiro vencimento no dia escolhido a partir de hoje
// (inclusive): assinando no dia 5 com vencimento dia 10, vence dia 10; no dia
// 15, vence dia 10 do mês seguinte.
func firstDueDate(today Date, dueDay int) Date {
	candidate := NewDate(today.Year(), today.Month(), dueDay)
	if candidate.Before(today) {
		return nextMonthly(candidate, dueDay)
	}
	return candidate
}

var monthNames = [...]string{
	"janeiro", "fevereiro", "março", "abril", "maio", "junho",
	"julho", "agosto", "setembro", "outubro", "novembro", "dezembro",
}

// invoiceDescription: "Plano Mensal — outubro/2026".
func invoiceDescription(planName string, due Date) string {
	return fmt.Sprintf("%s — %s/%d", planName, monthNames[due.Month()-1], due.Year())
}

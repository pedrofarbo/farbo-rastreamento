package billing

import (
	"reflect"
	"testing"
)

// A divisão sem juros: a 1ª leva os centavos que sobram; a permanência vai
// até a mensalidade com a última parcela (a k-ésima vai na mensalidade k-1).
func TestInstallmentPlan(t *testing.T) {
	for _, c := range []struct {
		total, n int
		want     []int
	}{
		{15000, 10, []int{1500, 1500, 1500, 1500, 1500, 1500, 1500, 1500, 1500, 1500}},
		{12000, 7, []int{1716, 1714, 1714, 1714, 1714, 1714, 1714}},
		{10000, 3, []int{3334, 3333, 3333}},
		{999, 1, []int{999}},
	} {
		if got := SplitInstallments(c.total, c.n); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%d em %dx = %v, quer %v", c.total, c.n, got, c.want)
		}
	}

	for _, c := range []struct {
		first Date
		n     int
		want  string
	}{
		{NewDate(2026, 10, 10), 10, "2027-06-10"},
		{NewDate(2026, 10, 10), 2, "2026-10-10"},
		{NewDate(2026, 12, 28), 3, "2027-01-28"},
	} {
		s := &Subscription{NextDueDate: c.first, DueDay: c.first.Day()}
		s.PlanInstallments(c.n)
		if s.Installments != c.n || s.CommitmentUntil == nil || s.CommitmentUntil.Format("2006-01-02") != c.want {
			t.Errorf("%dx a partir de %v: até %v, quer %s", c.n, c.first, s.CommitmentUntil, c.want)
		}
	}
}

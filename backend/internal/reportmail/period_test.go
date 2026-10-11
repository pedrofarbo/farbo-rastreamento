package reportmail

import (
	"testing"
	"time"
)

// O último período fechado e a hora de mandar: a semana de segunda a
// domingo, às segundas às 7h; o mês anterior, no dia 1º às 7h.
func TestLatestPeriod(t *testing.T) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	day := func(v time.Time) string { return v.Format("2006-01-02") }
	cases := []struct {
		frequency, now, first, last, sendAt string
	}{
		{Weekly, "2026-10-12 08:00", "2026-10-05", "2026-10-11", "2026-10-12 07:00"}, // segunda
		{Weekly, "2026-10-12 06:59", "2026-10-05", "2026-10-11", "2026-10-12 07:00"}, // segunda cedo: ainda não
		{Weekly, "2026-10-18 23:00", "2026-10-05", "2026-10-11", "2026-10-12 07:00"}, // domingo
		{Weekly, "2026-11-02 10:00", "2026-10-26", "2026-11-01", "2026-11-02 07:00"}, // virada de mês
		{Monthly, "2026-10-01 07:30", "2026-09-01", "2026-09-30", "2026-10-01 07:00"},
		{Monthly, "2026-10-20 12:00", "2026-09-01", "2026-09-30", "2026-10-01 07:00"},
		{Monthly, "2027-01-01 09:00", "2026-12-01", "2026-12-31", "2027-01-01 07:00"}, // virada de ano
		{Monthly, "2026-03-05 09:00", "2026-02-01", "2026-02-28", "2026-03-01 07:00"},
	}
	for _, c := range cases {
		period, sendAt := latest(c.frequency, at(c.now), loc)
		if day(period.First) != c.first || day(period.Last) != c.last || !sendAt.Equal(at(c.sendAt)) {
			t.Errorf("%s em %s: %s a %s, sai %s (quer %s a %s, %s)", c.frequency, c.now,
				day(period.First), day(period.Last), sendAt.Format("2006-01-02 15:04"), c.first, c.last, c.sendAt)
		}
	}
}

func TestThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1.000", 48312: "48.312", 1234567: "1.234.567"} {
		if got := thousands(n); got != want {
			t.Errorf("thousands(%d) = %q, quer %q", n, got, want)
		}
	}
}

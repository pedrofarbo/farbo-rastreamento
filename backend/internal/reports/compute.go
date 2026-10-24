// Package reports calcula os relatórios da frota: o resumo de cada dia de
// cada rastreador (km, tempo andando, parado com a ignição ligada, viagens,
// velocidade máxima, excessos), guardado em vehicle_daily_stats, e as
// viagens de um veículo num período, a partir das posições.
package reports

import (
	"math"
	"time"
)

// Os critérios do cálculo. Andando: a partir de MovingKmh (abaixo disso é
// ruído do GPS parado). Uma parada de StopAfter encerra a viagem. Um buraco
// de mais de MaxGap sem posição não conta como tempo (o rastreador sumiu).
// Parado, saltos menores que DriftMeters são ruído; mais rápido que MaxKmh,
// salto do GPS. Viagem com menos de MinTripMeters não é viagem.
const (
	MovingKmh     = 5.0
	StopAfter     = 5 * time.Minute
	MaxGap        = 10 * time.Minute
	DriftMeters   = 50.0
	MaxKmh        = 250.0
	MinTripMeters = 200.0
)

// A nota de direção. Brusca: a velocidade caiu (freada) ou subiu (arrancada)
// mais que isso por segundo entre duas posições de até HarshMaxGap — só dá
// para ver com o rastreador mandando a posição a cada poucos segundos (com
// intervalos maiores, fica de fora). Alta velocidade: acima de HighSpeedKmh.
// Noite: das NightFrom às NightUntil horas (no fuso do dia).
const (
	HarshBrakeKmhPerS = 12.0
	HarshAccelKmhPerS = 10.0
	HarshMaxGap       = 15 * time.Second
	HighSpeedKmh      = 120.0
	NightFrom         = 22
	NightUntil        = 6
)

// Point é uma posição do rastreador, só com o que o cálculo usa.
type Point struct {
	At       time.Time
	Lat, Lon float64
	SpeedKmh float64
	// Acc: a ignição; nil quando o rastreador não informa.
	Acc *bool
	// Valid: o GPS tinha sinal; nil quando o rastreador não informa.
	Valid *bool
}

// Stats é o resumo de um rastreador num intervalo (um dia).
type Stats struct {
	DistanceM float64
	// MovingS: andando; IdleS: parado com a ignição ligada (ocioso).
	MovingS  int
	IdleS    int
	Trips    int
	MaxSpeed float64
	// A primeira e a última vez que andou no intervalo.
	FirstMove *time.Time
	LastMove  *time.Time
	Points    int
	// Para a nota de direção: os metros rodados à noite, as freadas e
	// arrancadas bruscas e o tempo acima de HighSpeedKmh.
	NightM      float64
	HarshBrakes int
	HarshAccels int
	HighSpeedS  int
}

// night diz se o instante é de noite no fuso loc.
func night(t time.Time, loc *time.Location) bool {
	h := t.In(loc).Hour()
	return h >= NightFrom || h < NightUntil
}

// Trip é uma viagem: de onde e quando saiu até onde e quando parou.
type Trip struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	StartLat  float64   `json:"startLat"`
	StartLon  float64   `json:"startLon"`
	EndLat    float64   `json:"endLat"`
	EndLon    float64   `json:"endLon"`
	DistanceM float64   `json:"distanceMeters"`
	MaxSpeed  float64   `json:"maxSpeedKmh"`
	// Ongoing: o veículo ainda estava andando na última posição do período.
	Ongoing bool `json:"ongoing"`
}

// Seconds é a duração da viagem.
func (t Trip) Seconds() int { return int(t.End.Sub(t.Start).Seconds()) }

func validPoints(points []Point) []Point {
	out := make([]Point, 0, len(points))
	for _, p := range points {
		if p.Valid != nil && !*p.Valid {
			continue
		}
		if math.Abs(p.Lat) < 1e-6 && math.Abs(p.Lon) < 1e-6 {
			continue
		}
		out = append(out, p)
	}
	return out
}

func moving(p Point) bool { return p.SpeedKmh >= MovingKmh && p.SpeedKmh <= MaxKmh }

// step é o quanto o trecho de a até b soma de distância: nada se for ruído
// do GPS parado ou um salto impossível.
func step(a, b Point) float64 {
	dt := b.At.Sub(a.At).Seconds()
	if dt <= 0 {
		return 0
	}
	d := distance(a, b)
	if d/dt*3.6 > MaxKmh {
		return 0
	}
	if !moving(a) && !moving(b) && d < DriftMeters {
		return 0
	}
	return d
}

// distance em metros entre dois pontos (haversine).
func distance(a, b Point) float64 {
	const rad = math.Pi / 180
	dLat := (b.Lat - a.Lat) * rad
	dLon := (b.Lon - a.Lon) * rad
	h := math.Pow(math.Sin(dLat/2), 2) + math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Pow(math.Sin(dLon/2), 2)
	return 2 * 6371000 * math.Asin(math.Min(1, math.Sqrt(h)))
}

// DayStats resume as posições de um intervalo [from, to). As posições vêm
// em ordem; as anteriores a from só ligam o primeiro trecho (o que andou
// entre a última posição de antes e a primeira do dia conta no dia).
func DayStats(points []Point, from, to time.Time) Stats {
	pts := validPoints(points)
	var s Stats
	var prev *Point
	// As do dia, mais a última de antes (a viagem que vem da véspera começa
	// lá, e conta lá).
	inside := make([]Point, 0, len(pts))
	for i := range pts {
		p := pts[i]
		if !p.At.Before(to) {
			break
		}
		if p.At.Before(from) {
			prev = &pts[i]
			continue
		}
		if len(inside) == 0 && prev != nil {
			inside = append(inside, *prev)
		}
		s.Points++
		inside = append(inside, p)
		if moving(p) {
			at := p.At
			if s.FirstMove == nil {
				s.FirstMove = &at
			}
			s.LastMove = &at
			s.MaxSpeed = math.Max(s.MaxSpeed, p.SpeedKmh)
		}
		if prev != nil {
			d := step(*prev, p)
			s.DistanceM += d
			// À noite só o trecho todo de noite (um buraco desde a tarde não conta).
			if night(prev.At, from.Location()) && night(p.At, from.Location()) {
				s.NightM += d
			}
			if dt := p.At.Sub(prev.At); dt > 0 && dt <= MaxGap {
				switch {
				case moving(*prev) || moving(p):
					s.MovingS += int(dt.Seconds())
				case prev.Acc != nil && *prev.Acc:
					s.IdleS += int(dt.Seconds())
				}
				if prev.SpeedKmh > HighSpeedKmh && p.SpeedKmh > HighSpeedKmh && p.SpeedKmh <= MaxKmh {
					s.HighSpeedS += int(dt.Seconds())
				}
				if dt <= HarshMaxGap && prev.SpeedKmh <= MaxKmh && p.SpeedKmh <= MaxKmh {
					switch rate := (p.SpeedKmh - prev.SpeedKmh) / dt.Seconds(); {
					case rate <= -HarshBrakeKmhPerS:
						s.HarshBrakes++
					case rate >= HarshAccelKmhPerS:
						s.HarshAccels++
					}
				}
			}
		}
		prev = &pts[i]
	}
	// As viagens que começam no dia (a que vem da véspera conta lá).
	for _, t := range Trips(inside) {
		if !t.Start.Before(from) {
			s.Trips++
		}
	}
	return s
}

// Trips separa as viagens: começa no último ponto parado antes de andar e
// termina no primeiro ponto parado de uma parada de StopAfter (ou no último
// antes de um buraco sem posições). Viagens curtas demais são descartadas.
func Trips(points []Point) []Trip {
	pts := validPoints(points)
	var out []Trip
	var cur *Trip
	var arrival *Point
	finish := func(end Point, ongoing bool) {
		cur.End, cur.EndLat, cur.EndLon, cur.Ongoing = end.At, end.Lat, end.Lon, ongoing
		if cur.DistanceM >= MinTripMeters {
			out = append(out, *cur)
		}
		cur, arrival = nil, nil
	}
	for i := range pts {
		p := pts[i]
		var prev *Point
		if i > 0 {
			prev = &pts[i-1]
		}
		if cur != nil && prev != nil && p.At.Sub(prev.At) > MaxGap {
			end := *prev
			if arrival != nil {
				end = *arrival
			}
			finish(end, false)
		}
		if cur == nil {
			if !moving(p) {
				continue
			}
			start := p
			if prev != nil && p.At.Sub(prev.At) <= MaxGap {
				start = *prev
			}
			cur = &Trip{Start: start.At, StartLat: start.Lat, StartLon: start.Lon, MaxSpeed: p.SpeedKmh}
			if start.At.Before(p.At) {
				cur.DistanceM += step(start, p)
			}
			continue
		}
		cur.DistanceM += step(*prev, p)
		if moving(p) {
			cur.MaxSpeed = math.Max(cur.MaxSpeed, p.SpeedKmh)
			arrival = nil
			continue
		}
		if arrival == nil {
			a := p
			arrival = &a
		}
		if p.At.Sub(arrival.At) >= StopAfter {
			finish(*arrival, false)
		}
	}
	if cur != nil {
		last := pts[len(pts)-1]
		if arrival != nil {
			finish(*arrival, false)
		} else {
			finish(last, true)
		}
	}
	return out
}

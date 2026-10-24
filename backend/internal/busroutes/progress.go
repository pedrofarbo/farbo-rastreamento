package busroutes

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// O progresso de uma viagem, a partir das posições do veículo: o ponto conta
// como passado na primeira posição dentro do raio dele (na ordem; com
// posições espaçadas, um ou dois pontos podem ser pulados — ficam sem
// horário). O atraso é o do último ponto passado (ou da saída, se ainda não
// passou nenhum e já devia).

// Point é uma posição do veículo.
type Point struct {
	At        time.Time `json:"at"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	SpeedKmh  float64   `json:"speedKmh"`
}

// StopProgress é um ponto na viagem.
type StopProgress struct {
	Seq       int        `json:"seq"`
	Name      string     `json:"name"`
	Planned   time.Time  `json:"planned"`
	ArrivedAt *time.Time `json:"arrivedAt"`
	DelayMin  *int       `json:"delayMin"`
	// ETA: a previsão dos que faltam (o previsto mais o atraso de agora).
	ETA *time.Time `json:"eta"`
}

// Os estados da viagem.
const (
	TripScheduled = "scheduled"
	TripRunning   = "running"
	TripFinished  = "finished"
	TripCanceled  = "canceled"
)

// TripProgress é a viagem ao vivo.
type TripProgress struct {
	TripID      uuid.UUID      `json:"tripId"`
	VehicleID   uuid.UUID      `json:"vehicleId"`
	VehicleName string         `json:"vehicleName"`
	DepartsAt   time.Time      `json:"departsAt"`
	Status      string         `json:"status"`
	Stops       []StopProgress `json:"stops"`
	// NextSeq: o próximo ponto (nulo: terminou).
	NextSeq  *int   `json:"nextSeq"`
	DelayMin *int   `json:"delayMin"`
	OffRoute bool   `json:"offRoute"`
	Position *Point `json:"position"`
}

// lookahead: quantos pontos à frente uma posição pode alcançar (o ônibus
// passou rápido por um ponto entre duas posições).
const lookahead = 2

// Progress calcula a viagem com as posições (em ordem) até now.
func Progress(r *Route, t *Trip, points []Point, now time.Time) TripProgress {
	p := TripProgress{TripID: t.ID, VehicleID: t.VehicleID, VehicleName: t.VehicleName, DepartsAt: t.DepartsAt,
		Stops: make([]StopProgress, len(r.Stops))}
	for i, st := range r.Stops {
		p.Stops[i] = StopProgress{Seq: st.Seq, Name: st.Name, Planned: t.DepartsAt.Add(time.Duration(st.OffsetMin) * time.Minute)}
	}
	if t.Canceled {
		p.Status = TripCanceled
		return p
	}
	from, until := Window(r, t)
	next := 0
	var last *Point
	for i := range points {
		pt := points[i]
		if pt.At.Before(from) || pt.At.After(until) || pt.At.After(now) {
			continue
		}
		last = &points[i]
		for k := next; k < len(r.Stops) && k <= next+lookahead; k++ {
			st := r.Stops[k]
			if distance(pt.Latitude, pt.Longitude, st.Latitude, st.Longitude) <= float64(st.RadiusM) {
				at := pt.At
				p.Stops[k].ArrivedAt = &at
				delay := int(math.Round(at.Sub(p.Stops[k].Planned).Minutes()))
				p.Stops[k].DelayMin = &delay
				next = k + 1
				break
			}
		}
	}
	p.Position = last
	// O atraso de agora: o do último ponto passado; sem nenhum, o da saída.
	var delay *int
	for k := len(p.Stops) - 1; k >= 0; k-- {
		if p.Stops[k].DelayMin != nil {
			d := *p.Stops[k].DelayMin
			delay = &d
			break
		}
	}
	if delay == nil && len(p.Stops) > 0 && now.After(p.Stops[0].Planned) {
		d := int(math.Round(now.Sub(p.Stops[0].Planned).Minutes()))
		delay = &d
	}
	p.DelayMin = delay
	switch {
	case next >= len(r.Stops) || now.After(until):
		p.Status = TripFinished
	case next == 0 && now.Before(t.DepartsAt):
		p.Status = TripScheduled
	default:
		p.Status = TripRunning
	}
	if p.Status != TripFinished && next < len(r.Stops) {
		seq := r.Stops[next].Seq
		p.NextSeq = &seq
		shift := 0
		if delay != nil && *delay > 0 {
			shift = *delay
		}
		for k := next; k < len(p.Stops); k++ {
			eta := p.Stops[k].Planned.Add(time.Duration(shift) * time.Minute)
			if eta.Before(now) {
				eta = now
			}
			p.Stops[k].ETA = &eta
		}
	}
	if p.Status == TripRunning && last != nil && len(r.Stops) > 1 {
		p.OffRoute = distanceToPath(last.Latitude, last.Longitude, r.Stops) > float64(r.DeviationM)
	}
	return p
}

// distance em metros (haversine).
func distance(lat1, lon1, lat2, lon2 float64) float64 {
	const rad = math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	h := math.Pow(math.Sin(dLat/2), 2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Pow(math.Sin(dLon/2), 2)
	return 2 * 6371000 * math.Asin(math.Min(1, math.Sqrt(h)))
}

// distanceToPath é a menor distância (m) do ponto ao caminho entre os pontos
// da linha (as retas de um ao outro, numa projeção local).
func distanceToPath(lat, lon float64, stops []Stop) float64 {
	const rad = math.Pi / 180
	kx := 6371000 * math.Cos(lat*rad) * rad
	ky := 6371000 * rad
	best := math.Inf(1)
	for i := 0; i+1 < len(stops); i++ {
		ax, ay := (stops[i].Longitude-lon)*kx, (stops[i].Latitude-lat)*ky
		bx, by := (stops[i+1].Longitude-lon)*kx, (stops[i+1].Latitude-lat)*ky
		dx, dy := bx-ax, by-ay
		t := 0.0
		if l := dx*dx + dy*dy; l > 0 {
			t = math.Max(0, math.Min(1, -(ax*dx+ay*dy)/l))
		}
		px, py := ax+t*dx, ay+t*dy
		best = math.Min(best, math.Hypot(px, py))
	}
	return best
}

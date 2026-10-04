// Package tracking cuida das posições: persistência, histórico e a pipeline de
// ingestão que transforma telemetria em estado, eventos e tempo real.
package tracking

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// Origem do registro. Heartbeat não traz coordenada própria.
const (
	SourceGPS       = "gps"
	SourceHeartbeat = "heartbeat"
	SourceLBS       = "lbs"
)

type Position struct {
	ID       int64     `json:"id"`
	DeviceID uuid.UUID `json:"deviceId"`

	// GPSTimestamp é o instante informado pelo aparelho; ReceivedAt é o
	// instante em que o servidor recebeu. Eles divergem sempre que o
	// rastreador descarrega posições guardadas offline (§35).
	GPSTimestamp time.Time `json:"gpsTimestamp"`
	ReceivedAt   time.Time `json:"receivedAt"`

	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`

	SpeedKmh float64  `json:"speedKmh"`
	Heading  *float64 `json:"heading"`
	Altitude *float64 `json:"altitude"`

	GPSValid   *bool    `json:"gpsValid"`
	Satellites *int     `json:"satellites"`
	HDOP       *float64 `json:"hdop"`

	ACC *bool `json:"acc"`

	BatteryVoltage *float64 `json:"batteryVoltage"`
	BatteryPercent *int     `json:"batteryPercent"`
	GSMLevel       *int     `json:"gsmLevel"`

	RelayOn *bool `json:"relayOn"`

	Protocol string `json:"protocol"`
	Source   string `json:"source"`

	// RawPayload só é devolvido em consultas pontuais: seria peso morto numa
	// listagem de milhares de pontos.
	RawPayload string `json:"rawPayload,omitempty"`
}

const lightColumns = `id, device_id, gps_timestamp, received_at, latitude, longitude, speed_kmh,
	heading, altitude, gps_valid, satellites, hdop, acc, battery_voltage, battery_percent,
	gsm_level, relay_on, COALESCE(protocol, ''), source`

const fullColumns = lightColumns + `, COALESCE(raw_payload, '')`

type Repository struct{ db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

func scanLight(row database.Scanner) (*Position, error) {
	var p Position
	err := row.Scan(&p.ID, &p.DeviceID, &p.GPSTimestamp, &p.ReceivedAt, &p.Latitude, &p.Longitude,
		&p.SpeedKmh, &p.Heading, &p.Altitude, &p.GPSValid, &p.Satellites, &p.HDOP, &p.ACC,
		&p.BatteryVoltage, &p.BatteryPercent, &p.GSMLevel, &p.RelayOn, &p.Protocol, &p.Source)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &p, nil
}

func scanFull(row database.Scanner) (*Position, error) {
	var p Position
	err := row.Scan(&p.ID, &p.DeviceID, &p.GPSTimestamp, &p.ReceivedAt, &p.Latitude, &p.Longitude,
		&p.SpeedKmh, &p.Heading, &p.Altitude, &p.GPSValid, &p.Satellites, &p.HDOP, &p.ACC,
		&p.BatteryVoltage, &p.BatteryPercent, &p.GSMLevel, &p.RelayOn, &p.Protocol, &p.Source,
		&p.RawPayload)
	if err != nil {
		return nil, database.MapError(err)
	}
	return &p, nil
}

func (r *Repository) Insert(ctx context.Context, p *Position) error {
	if p.Source == "" {
		p.Source = SourceGPS
	}
	return database.MapError(r.db.QueryRow(ctx, `
		INSERT INTO positions (device_id, gps_timestamp, latitude, longitude, speed_kmh, heading,
			altitude, gps_valid, satellites, hdop, acc, battery_voltage, battery_percent,
			gsm_level, relay_on, protocol, source, raw_payload)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,NULLIF($18, ''))
		RETURNING id, received_at`,
		p.DeviceID, p.GPSTimestamp, p.Latitude, p.Longitude, p.SpeedKmh, p.Heading,
		p.Altitude, p.GPSValid, p.Satellites, p.HDOP, p.ACC, p.BatteryVoltage,
		p.BatteryPercent, p.GSMLevel, p.RelayOn, p.Protocol, p.Source, p.RawPayload,
	).Scan(&p.ID, &p.ReceivedAt))
}

// Latest devolve a posição mais recente pelo relógio do aparelho.
func (r *Repository) Latest(ctx context.Context, deviceID uuid.UUID) (*Position, error) {
	return scanFull(r.db.QueryRow(ctx, `SELECT `+fullColumns+` FROM positions
		WHERE device_id = $1 ORDER BY gps_timestamp DESC, id DESC LIMIT 1`, deviceID))
}

// LatestWithLocation ignora registros de heartbeat, que repetem a coordenada
// anterior apenas para carregar estado.
func (r *Repository) LatestWithLocation(ctx context.Context, deviceID uuid.UUID) (*Position, error) {
	return scanFull(r.db.QueryRow(ctx, `SELECT `+fullColumns+` FROM positions
		WHERE device_id = $1 AND source = 'gps'
		ORDER BY gps_timestamp DESC, id DESC LIMIT 1`, deviceID))
}

// LatestFor devolve a última posição (pelo relógio do aparelho) de cada
// rastreador pedido, numa só consulta. Cada um é uma busca curta no índice
// (device_id, gps_timestamp): o tempo depende de quantos rastreadores, não
// do tamanho do histórico. Ler a tabela inteira com DISTINCT ON levava 4,8 s
// com 2 milhões de posições (um dia de 5000 rastreadores), a cada lista de
// veículos aberta. Quem não tem posição fica fora do mapa.
func (r *Repository) LatestFor(ctx context.Context, deviceIDs []uuid.UUID) (map[uuid.UUID]*Position, error) {
	if len(deviceIDs) == 0 {
		return map[uuid.UUID]*Position{}, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT p.* FROM unnest($1::uuid[]) AS d(id)
		CROSS JOIN LATERAL (
			SELECT `+lightColumns+` FROM positions
			WHERE device_id = d.id
			ORDER BY gps_timestamp DESC, id DESC LIMIT 1) p`, deviceIDs)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := map[uuid.UUID]*Position{}
	for rows.Next() {
		p, err := scanLight(rows)
		if err != nil {
			return nil, err
		}
		out[p.DeviceID] = p
	}
	return out, rows.Err()
}

// HistoryQuery descreve uma consulta de trajeto.
type HistoryQuery struct {
	DeviceID  uuid.UUID
	From      time.Time
	To        time.Time
	Limit     int
	OnlyValid bool
	// Simplify aplica Douglas-Peucker ao resultado, para desenhar o trajeto
	// com menos pontos sem deformar a rota.
	Simplify        bool
	ToleranceMeters float64
}

// HistoryResult carrega os pontos e como eles foram obtidos, para a interface
// deixar claro que está vendo uma amostra.
type HistoryResult struct {
	Positions []*Position `json:"positions"`
	// Total é quantos pontos existem de fato no período.
	Total int `json:"total"`
	// Returned é quantos vieram nesta resposta.
	Returned int `json:"returned"`
	// Sampled indica que o banco devolveu 1 a cada SampleStep pontos.
	Sampled    bool `json:"sampled"`
	SampleStep int  `json:"sampleStep"`
	// Simplified indica que Douglas-Peucker foi aplicado depois da amostragem.
	Simplified bool `json:"simplified"`
}

// History devolve o trajeto de um período, garantindo que jamais retorne
// milhões de pontos (§12): acima do limite o banco amostra uniformemente.
func (r *Repository) History(ctx context.Context, q HistoryQuery) (*HistoryResult, error) {
	if q.Limit <= 0 {
		q.Limit = 2000
	}

	filter := `device_id = $1 AND gps_timestamp >= $2 AND gps_timestamp <= $3 AND source = 'gps'`
	if q.OnlyValid {
		filter += ` AND gps_valid IS TRUE`
	}

	var total int
	if err := r.db.QueryRow(ctx,
		`SELECT count(*) FROM positions WHERE `+filter, q.DeviceID, q.From, q.To,
	).Scan(&total); err != nil {
		return nil, database.MapError(err)
	}

	result := &HistoryResult{Total: total, Positions: []*Position{}}
	if total == 0 {
		return result, nil
	}

	step := 1
	if total > q.Limit {
		step = int(math.Ceil(float64(total) / float64(q.Limit)))
		result.Sampled = true
	}
	result.SampleStep = step

	// A amostragem mantém sempre o primeiro e o último ponto, para o trajeto
	// começar e terminar onde realmente começou e terminou.
	rows, err := r.db.Query(ctx, `
		SELECT `+lightColumns+` FROM (
			SELECT *,
			       row_number() OVER (ORDER BY gps_timestamp, id) AS rn,
			       count(*) OVER () AS total_rows
			FROM positions WHERE `+filter+`
		) sampled
		WHERE $4 = 1 OR rn % $4 = 1 OR rn = total_rows
		ORDER BY gps_timestamp, id`,
		q.DeviceID, q.From, q.To, step)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	for rows.Next() {
		p, err := scanLight(rows)
		if err != nil {
			return nil, err
		}
		result.Positions = append(result.Positions, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if q.Simplify && len(result.Positions) > 2 {
		tolerance := q.ToleranceMeters
		if tolerance <= 0 {
			tolerance = 5
		}
		before := len(result.Positions)
		result.Positions = Simplify(result.Positions, tolerance)
		result.Simplified = len(result.Positions) < before
	}

	result.Returned = len(result.Positions)
	return result, nil
}

// PageQuery é a paginação por cursor, usada para exportar o histórico bruto.
type PageQuery struct {
	DeviceID uuid.UUID
	From     time.Time
	To       time.Time
	Limit    int
	// AfterID devolve apenas registros posteriores a este id.
	AfterID int64
}

type Page struct {
	Positions  []*Position `json:"positions"`
	NextCursor *int64      `json:"nextCursor"`
}

func (r *Repository) Page(ctx context.Context, q PageQuery) (*Page, error) {
	if q.Limit <= 0 || q.Limit > 1000 {
		q.Limit = 500
	}
	rows, err := r.db.Query(ctx, `
		SELECT `+lightColumns+` FROM positions
		WHERE device_id = $1 AND gps_timestamp >= $2 AND gps_timestamp <= $3 AND id > $4
		ORDER BY id LIMIT $5`,
		q.DeviceID, q.From, q.To, q.AfterID, q.Limit+1)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	page := &Page{Positions: []*Position{}}
	for rows.Next() {
		p, err := scanLight(rows)
		if err != nil {
			return nil, err
		}
		page.Positions = append(page.Positions, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(page.Positions) > q.Limit {
		page.Positions = page.Positions[:q.Limit]
		cursor := page.Positions[len(page.Positions)-1].ID
		page.NextCursor = &cursor
	}
	return page, nil
}

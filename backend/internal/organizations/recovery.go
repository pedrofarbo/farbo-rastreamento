package organizations

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// O painel de recuperação: os modos roubo ligados nos veículos dos
// associados — os em andamento, o histórico e quantos foram recuperados.

// TheftCase é um modo roubo de um veículo de associado.
type TheftCase struct {
	ID              uuid.UUID  `json:"id"`
	VehicleID       uuid.UUID  `json:"vehicleId"`
	VehicleName     string     `json:"vehicleName"`
	Plate           string     `json:"plate"`
	AssociateID     uuid.UUID  `json:"associateId"`
	AssociateName   string     `json:"associateName"`
	ActivatedByName string     `json:"activatedByName"`
	ActivatedAt     time.Time  `json:"activatedAt"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	EndedAt         *time.Time `json:"endedAt"`
	// Outcome: RECOVERED, CANCELLED ou EXPIRED; vazio enquanto está ligado.
	Outcome string `json:"outcome"`
}

// Recoveries é o painel: os números do período, os em andamento e o histórico.
type Recoveries struct {
	Since     time.Time `json:"since"`
	Total     int       `json:"total"`
	Active    int       `json:"active"`
	Recovered int       `json:"recovered"`
	Cancelled int       `json:"cancelled"`
	Expired   int       `json:"expired"`
	// RecoveryRate: recuperados sobre os que terminaram de verdade
	// (recuperados mais os que desligaram no prazo); nulo sem nenhum.
	RecoveryRate *float64 `json:"recoveryRate"`
	// MedianRecoveryMinutes: o tempo do meio entre ligar e recuperar.
	MedianRecoveryMinutes *int        `json:"medianRecoveryMinutes"`
	Cases                 []TheftCase `json:"cases"`
}

// MaxRecoveryCases limita o histórico do painel.
const MaxRecoveryCases = 500

// Recoveries é o painel de recuperação da organização desde since.
func (s *Service) Recoveries(ctx context.Context, orgID uuid.UUID, since time.Time) (*Recoveries, error) {
	rows, err := s.db.Query(ctx, `
		SELECT t.id, v.id, v.name, COALESCE(v.plate, ''), a.id, a.name, COALESCE(b.name, ''), t.activated_at,
			t.expires_at, t.ended_at, COALESCE(t.outcome, '')
		FROM theft_modes t
		JOIN vehicles v ON v.id = t.vehicle_id
		JOIN users a ON a.id = v.owner_id
		LEFT JOIN users b ON b.id = t.activated_by
		WHERE a.organization_id = $1 AND (t.activated_at >= $2 OR t.ended_at IS NULL)
		ORDER BY t.ended_at IS NULL DESC, t.activated_at DESC
		LIMIT $3`, orgID, since, MaxRecoveryCases)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	out := &Recoveries{Since: since, Cases: []TheftCase{}}
	var durations []int
	for rows.Next() {
		var c TheftCase
		if err := rows.Scan(&c.ID, &c.VehicleID, &c.VehicleName, &c.Plate, &c.AssociateID, &c.AssociateName,
			&c.ActivatedByName, &c.ActivatedAt, &c.ExpiresAt, &c.EndedAt, &c.Outcome); err != nil {
			return nil, database.MapError(err)
		}
		out.Cases = append(out.Cases, c)
		out.Total++
		switch c.Outcome {
		case "":
			out.Active++
		case "RECOVERED":
			out.Recovered++
			if c.EndedAt != nil {
				durations = append(durations, int(c.EndedAt.Sub(c.ActivatedAt).Minutes()))
			}
		case "CANCELLED":
			out.Cancelled++
		case "EXPIRED":
			out.Expired++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, database.MapError(err)
	}
	if finished := out.Recovered + out.Expired; finished > 0 {
		rate := float64(int(1000*float64(out.Recovered)/float64(finished)+0.5)) / 10
		out.RecoveryRate = &rate
	}
	if len(durations) > 0 {
		slices.Sort(durations)
		median := durations[len(durations)/2]
		if len(durations)%2 == 0 {
			median = (durations[len(durations)/2-1] + durations[len(durations)/2]) / 2
		}
		out.MedianRecoveryMinutes = &median
	}
	return out, nil
}

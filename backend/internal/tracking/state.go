package tracking

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedrofarbo/farbo-rastreamento/backend/internal/database"
)

// State é a telemetria de estado do dispositivo — o que chega em heartbeat e
// pacotes de status, e que não é posição.
//
// Guardar isso separado de positions é deliberado: um heartbeat não traz
// coordenada, e criar uma posição com a coordenada anterior seria inventar
// dado de GPS que o aparelho não enviou (§34).
type State struct {
	DeviceID uuid.UUID `json:"deviceId"`

	// Overspeed guarda o estado do detector de excesso, para gerar um evento
	// na entrada e outro na saída em vez de milhares seguidos (§21).
	Overspeed bool  `json:"overspeed"`
	ACC       *bool `json:"acc"`
	GPSValid  *bool `json:"gpsValid"`
	RelayOn   *bool `json:"relayOn"`

	BatteryPercent *int     `json:"batteryPercent"`
	BatteryVoltage *float64 `json:"batteryVoltage"`
	GSMLevel       *int     `json:"gsmLevel"`

	InsideFences []uuid.UUID `json:"insideFences"`

	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

// Clone evita que quem lê o cache altere o estado compartilhado.
func (s *State) Clone() *State {
	if s == nil {
		return nil
	}
	copied := *s
	copied.InsideFences = append([]uuid.UUID(nil), s.InsideFences...)
	return &copied
}

const stateColumns = `device_id, overspeed, acc, gps_valid, relay_on, battery_percent,
	battery_voltage, gsm_level, inside_fences, last_heartbeat_at, updated_at`

type StateRepository struct{ db *database.DB }

func NewStateRepository(db *database.DB) *StateRepository { return &StateRepository{db: db} }

func scanState(row database.Scanner) (*State, error) {
	var s State
	err := row.Scan(&s.DeviceID, &s.Overspeed, &s.ACC, &s.GPSValid, &s.RelayOn,
		&s.BatteryPercent, &s.BatteryVoltage, &s.GSMLevel, &s.InsideFences,
		&s.LastHeartbeatAt, &s.UpdatedAt)
	if err != nil {
		return nil, database.MapError(err)
	}
	if s.InsideFences == nil {
		s.InsideFences = []uuid.UUID{}
	}
	return &s, nil
}

func (r *StateRepository) LoadAll(ctx context.Context) (map[uuid.UUID]*State, error) {
	rows, err := r.db.Query(ctx, `SELECT `+stateColumns+` FROM device_states`)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()

	out := map[uuid.UUID]*State{}
	for rows.Next() {
		s, err := scanState(rows)
		if err != nil {
			return nil, err
		}
		out[s.DeviceID] = s
	}
	return out, rows.Err()
}

const saveState = `
		INSERT INTO device_states (` + stateColumns + `)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW())
		ON CONFLICT (device_id) DO UPDATE SET
			overspeed = EXCLUDED.overspeed,
			acc = EXCLUDED.acc,
			gps_valid = EXCLUDED.gps_valid,
			relay_on = EXCLUDED.relay_on,
			battery_percent = EXCLUDED.battery_percent,
			battery_voltage = EXCLUDED.battery_voltage,
			gsm_level = EXCLUDED.gsm_level,
			inside_fences = EXCLUDED.inside_fences,
			last_heartbeat_at = EXCLUDED.last_heartbeat_at,
			updated_at = NOW()`

func saveArgs(s *State) []any {
	if s.InsideFences == nil {
		s.InsideFences = []uuid.UUID{}
	}
	return []any{s.DeviceID, s.Overspeed, s.ACC, s.GPSValid, s.RelayOn, s.BatteryPercent,
		s.BatteryVoltage, s.GSMLevel, s.InsideFences, s.LastHeartbeatAt}
}

func (r *StateRepository) Save(ctx context.Context, s *State) error {
	_, err := r.db.Exec(ctx, saveState, saveArgs(s)...)
	return database.MapError(err)
}

// SaveMany grava vários estados numa ida ao banco e numa transação.
func (r *StateRepository) SaveMany(ctx context.Context, states []*State) error {
	if len(states) == 0 {
		return nil
	}
	return database.MapError(pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		batch := &pgx.Batch{}
		for _, s := range states {
			batch.Queue(saveState, saveArgs(s)...)
		}
		return tx.SendBatch(ctx, batch).Close()
	}))
}

// flushEvery é o ritmo da gravação dos estados no banco.
const flushEvery = time.Second

// StateStore mantém os estados em memória (a ingestão lê a cada pacote) e
// escreve no banco para sobreviver a reinício do servidor.
//
// A escrita vai em lote (Run): cada posição muda o estado até três vezes
// (telemetria, excesso de velocidade, cercas), e gravar cada mudança dava
// três escritas no banco por posição. Todas as leituras são da memória; o
// banco só serve para o próximo boot — numa queda, perde-se no máximo
// flushEvery de estado.
type StateStore struct {
	repo *StateRepository

	mu     sync.RWMutex
	states map[uuid.UUID]*State
	// dirty são os aparelhos com estado ainda não gravado.
	dirty map[uuid.UUID]struct{}
	// running: o Run está gravando em lote. Sem ele (testes), cada mudança
	// é gravada na hora, como antes.
	running atomic.Bool
}

func NewStateStore(repo *StateRepository) *StateStore {
	return &StateStore{repo: repo, states: map[uuid.UUID]*State{}, dirty: map[uuid.UUID]struct{}{}}
}

// Run grava os estados alterados a cada flushEvery até o contexto acabar, e
// uma última vez na saída.
func (s *StateStore) Run(ctx context.Context, log *slog.Logger) {
	s.running.Store(true)
	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			if err := s.Flush(final); err != nil {
				log.Warn("estados dos rastreadores não gravados no encerramento", "err", err)
			}
			cancel()
			return
		case <-ticker.C:
			if err := s.Flush(ctx); err != nil && ctx.Err() == nil {
				log.Warn("falha ao gravar os estados dos rastreadores", "err", err)
			}
		}
	}
}

// Flush grava de uma vez os estados alterados desde a última gravação. Se o
// banco falhar, eles voltam para a próxima.
func (s *StateStore) Flush(ctx context.Context) error {
	s.mu.Lock()
	if len(s.dirty) == 0 {
		s.mu.Unlock()
		return nil
	}
	batch := make([]*State, 0, len(s.dirty))
	for id := range s.dirty {
		batch = append(batch, s.states[id].Clone())
	}
	s.dirty = map[uuid.UUID]struct{}{}
	s.mu.Unlock()

	if err := s.repo.SaveMany(ctx, batch); err != nil {
		s.mu.Lock()
		for _, st := range batch {
			s.dirty[st.DeviceID] = struct{}{}
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *StateStore) Load(ctx context.Context) error {
	loaded, err := s.repo.LoadAll(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.states = loaded
	s.mu.Unlock()
	return nil
}

func (s *StateStore) Get(deviceID uuid.UUID) *State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.states[deviceID].Clone()
}

// Update aplica a mutação sobre o estado atual e persiste o resultado.
// Devolve o estado anterior e o novo, para que a ingestão detecte transições.
func (s *StateStore) Update(ctx context.Context, deviceID uuid.UUID, mutate func(*State)) (before, after *State) {
	s.mu.Lock()
	current, ok := s.states[deviceID]
	if !ok {
		current = &State{DeviceID: deviceID, InsideFences: []uuid.UUID{}}
	}
	before = current.Clone()

	next := current.Clone()
	mutate(next)
	next.DeviceID = deviceID
	next.UpdatedAt = time.Now().UTC()
	s.states[deviceID] = next
	if s.running.Load() {
		s.dirty[deviceID] = struct{}{}
		s.mu.Unlock()
		return before, next
	}
	s.mu.Unlock()

	// Sem o Run: a persistência não pode travar a ingestão; o cache em
	// memória já está correto e o banco serve para o próximo boot.
	go func(snapshot *State) {
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.repo.Save(saveCtx, snapshot)
	}(next.Clone())

	return before, next
}

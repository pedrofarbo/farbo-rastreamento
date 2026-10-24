import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { busRoutesApi } from '@/api/resources';
import { RouteMap } from '@/components/map/RouteMap';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { SelectField, TextField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { TRIP_STATUS, delayLabel, delayTone, nextStopName, routeProblem } from '@/services/busRoutes';
import { formatDateTime, todayISO } from '@/services/format';
import { defaultSlot, hourLabel } from '@/services/installations';
import { useAuth } from '@/stores/AuthContext';
import type { BusRoute, BusRouteInput, TripProgress, VehicleView } from '@/types';

import pageStyles from '../Page.module.css';
import styles from './BusRoutes.module.css';

const routesKey = ['fleet-ops', 'routes'] as const;

const COLORS = ['#22c55e', '#3b82f6', '#f97316', '#a855f7', '#ef4444', '#eab308', '#14b8a6', '#ec4899'];

/**
 * Os itinerários (as linhas de ônibus) da conta: os pontos com o minuto
 * previsto desde a saída, as viagens de cada veículo e o dia ao vivo — o
 * atraso, o próximo ponto e o desvio do caminho. Com o link público, o
 * passageiro vê onde o ônibus está.
 */
export function BusRoutesTab({ vehicles }: { vehicles: VehicleView[] }) {
  const { canOperateAccount } = useAuth();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [editing, setEditing] = useState<BusRoute | 'new' | null>(null);
  const [scheduling, setScheduling] = useState<BusRoute | null>(null);
  const [watching, setWatching] = useState<string | null>(null);
  const routes = useQuery({ queryKey: routesKey, queryFn: busRoutesApi.list });
  const list = routes.data ?? [];
  const remove = useMutation({
    mutationFn: busRoutesApi.remove,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: routesKey });
      setWatching(null);
      notify({ tone: 'success', title: 'Linha apagada' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para apagar', description: err.message }),
  });
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      notify({ tone: 'success', title: 'Link copiado' });
    } catch {
      /* sem permissão: a pessoa copia na mão */
    }
  };
  const watched = list.find((r) => r.id === watching) ?? null;

  return (
    <>
      <Card
        title="Linhas"
        subtitle="Os pontos de cada linha, na ordem, com os minutos previstos desde a saída."
        actions={
          canOperateAccount ? (
            <Button size="small" variant="primary" onClick={() => setEditing('new')}>
              Nova linha
            </Button>
          ) : undefined
        }
        flush
      >
        {routes.isLoading ? (
          <Spinner label="Carregando" />
        ) : list.length === 0 ? (
          <EmptyState
            icon="🚌"
            title="Nenhuma linha"
            description="Cadastre os pontos de uma linha e programe as viagens: o atraso e o desvio aparecem ao vivo."
          />
        ) : (
          <div className={styles.routes}>
            {list.map((r) => (
              <div key={r.id} className={styles.route}>
                <span className={styles.swatch} style={{ background: r.color }} aria-hidden="true" />
                <div className={styles.routeBody}>
                  <span className={styles.name}>
                    {r.name} {!r.active && <Badge tone="neutral">Inativa</Badge>}
                  </span>
                  <span className={styles.muted}>
                    {r.stops.length} pontos · {r.stops.length ? `${r.stops[r.stops.length - 1].offsetMin} min de viagem` : ''}
                    {r.public ? ' · link público ligado' : ''}
                  </span>
                </div>
                <div className={styles.actions}>
                  <Button size="small" variant={watching === r.id ? 'primary' : 'secondary'} onClick={() => setWatching(watching === r.id ? null : r.id)}>
                    {watching === r.id ? 'Fechar o dia' : 'Ver o dia'}
                  </Button>
                  {canOperateAccount && (
                    <>
                      <Button size="small" variant="secondary" onClick={() => setScheduling(r)}>
                        Programar viagens
                      </Button>
                      <Button size="small" variant="ghost" onClick={() => setEditing(r)}>
                        Editar
                      </Button>
                    </>
                  )}
                  {r.publicUrl && (
                    <Button size="small" variant="ghost" onClick={() => void copy(r.publicUrl ?? '')}>
                      Copiar link do passageiro
                    </Button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
      {watched && <RouteDayCard route={watched} />}
      {editing && (
        <RouteEditor
          route={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onDelete={editing !== 'new' ? () => remove.mutate(editing.id) : undefined}
        />
      )}
      {scheduling && <TripsModal route={scheduling} vehicles={vehicles} onClose={() => setScheduling(null)} />}
    </>
  );
}

/** As viagens do dia na linha, ao vivo (atualiza a cada 30 s). */
function RouteDayCard({ route }: { route: BusRoute }) {
  const { canOperateAccount } = useAuth();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [day, setDay] = useState(todayISO());
  const today = day === todayISO();
  const data = useQuery({
    queryKey: [...routesKey, route.id, 'day', day],
    queryFn: () => busRoutesApi.day(route.id, day),
    refetchInterval: today ? 30_000 : false,
  });
  const cancel = useMutation({
    mutationFn: busRoutesApi.cancelTrip,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [...routesKey, route.id, 'day'] });
      notify({ tone: 'success', title: 'Viagem cancelada' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para cancelar', description: err.message }),
  });
  const trips = data.data?.trips ?? [];
  const buses = trips
    .filter((t) => t.status === 'running' && t.position)
    .map((t) => ({ id: t.tripId, label: t.vehicleName, latitude: t.position!.latitude, longitude: t.position!.longitude, offRoute: t.offRoute }));
  return (
    <Card title={`${route.name}: o dia`} subtitle={today ? 'Ao vivo: atualiza sozinho a cada 30 s.' : undefined} flush>
      <div className={styles.toolbar}>
        <label className={styles.date}>
          Dia
          <input className={fieldStyles.input} type="date" value={day} onChange={(e) => e.target.value && setDay(e.target.value)} />
        </label>
      </div>
      <div style={{ padding: '0 var(--space-4) var(--space-3)' }}>
        <div className={styles.map}>
          <RouteMap stops={route.stops} color={route.color} buses={buses} className={styles.leaflet} />
        </div>
      </div>
      {data.isLoading ? (
        <Spinner label="Carregando" />
      ) : trips.length === 0 ? (
        <p className={styles.empty}>Nenhuma viagem programada neste dia.</p>
      ) : (
        <div className={styles.trips}>
          {trips.map((t) => (
            <TripRow
              key={t.tripId}
              trip={t}
              onCancel={canOperateAccount && t.status === 'scheduled' ? () => cancel.mutate(t.tripId) : undefined}
            />
          ))}
        </div>
      )}
    </Card>
  );
}

function TripRow({ trip, onCancel }: { trip: TripProgress; onCancel?: () => void }) {
  const st = TRIP_STATUS[trip.status];
  const next = nextStopName(trip);
  return (
    <div className={styles.trip}>
      <div className={styles.tripHead}>
        <strong>{hourLabel(trip.departsAt)}</strong>
        <span>{trip.vehicleName}</span>
        <Badge tone={st.tone}>{st.label}</Badge>
        {trip.status !== 'canceled' && trip.delayMin !== null && <Badge tone={delayTone(trip.delayMin)}>{delayLabel(trip.delayMin)}</Badge>}
        {trip.offRoute && <Badge tone="danger">Fora do caminho</Badge>}
        {trip.status === 'running' && next && <span className={styles.muted}>indo para {next}</span>}
        {onCancel && (
          <Button size="small" variant="ghost" onClick={onCancel}>
            Cancelar
          </Button>
        )}
      </div>
      {trip.status !== 'canceled' && trip.status !== 'scheduled' && (
        <div className={styles.timeline}>
          {trip.stops.map((s) => (
            <div
              key={s.seq}
              className={`${styles.point} ${trip.nextSeq === s.seq ? styles.pointNext : ''} ${s.arrivedAt ? styles.pointPassed : ''}`}
            >
              <strong>{s.name}</strong>
              <span className={styles.muted}>previsto {hourLabel(s.planned)}</span>
              {s.arrivedAt ? (
                <span>
                  passou {hourLabel(s.arrivedAt)}
                  {s.delayMin !== null && Math.abs(s.delayMin) > 1 ? ` (${s.delayMin > 0 ? '+' : ''}${s.delayMin} min)` : ''}
                </span>
              ) : s.eta ? (
                <span>chega ~{hourLabel(s.eta)}</span>
              ) : (
                <span className={styles.muted}>sem registro</span>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

type StopDraft = BusRouteInput['stops'][number];

function RouteEditor({ route, onClose, onDelete }: { route: BusRoute | null; onClose: () => void; onDelete?: () => void }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [name, setName] = useState(route?.name ?? '');
  const [color, setColor] = useState(route?.color ?? COLORS[0]);
  const [deviation, setDeviation] = useState(String(route?.deviationM ?? 500));
  const [isPublic, setPublic] = useState(route?.public ?? false);
  const [active, setActive] = useState(route?.active ?? true);
  const [stops, setStops] = useState<StopDraft[]>(
    route?.stops.map(({ name, latitude, longitude, radiusM, offsetMin }) => ({ name, latitude, longitude, radiusM, offsetMin })) ?? [],
  );
  const [error, setError] = useState('');
  const [confirmDelete, setConfirmDelete] = useState(false);
  const input: BusRouteInput = { name, color, deviationM: Number(deviation) || 500, public: isPublic, active, stops };
  const save = useMutation({
    mutationFn: () => {
      const problem = routeProblem(input);
      if (problem) throw new Error(problem);
      return busRoutesApi.save(route?.id ?? null, input);
    },
    onSuccess: (r) => {
      void queryClient.invalidateQueries({ queryKey: routesKey });
      notify({ tone: 'success', title: `${r.name} salva`, description: r.publicUrl ? 'O link do passageiro está em Copiar link.' : undefined });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });
  const update = (i: number, patch: Partial<StopDraft>) => setStops((list) => list.map((s, k) => (k === i ? { ...s, ...patch } : s)));
  const move = (i: number, delta: number) =>
    setStops((list) => {
      const j = i + delta;
      if (j < 0 || j >= list.length) return list;
      const next = [...list];
      [next[i], next[j]] = [next[j], next[i]];
      return next;
    });
  const add = (latitude: number, longitude: number) =>
    setStops((list) => [
      ...list,
      {
        name: `Ponto ${list.length + 1}`,
        latitude: Number(latitude.toFixed(6)),
        longitude: Number(longitude.toFixed(6)),
        radiusM: 80,
        offsetMin: list.length ? list[list.length - 1].offsetMin + 5 : 0,
      },
    ]);

  return (
    <Modal
      open
      wide
      title={route ? `Editar: ${route.name}` : 'Nova linha'}
      onClose={onClose}
      footer={
        <>
          {onDelete &&
            (confirmDelete ? (
              <Button variant="danger" onClick={onDelete}>
                Apagar mesmo (com as viagens)
              </Button>
            ) : (
              <Button variant="ghost" onClick={() => setConfirmDelete(true)}>
                Apagar a linha
              </Button>
            ))}
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Salvar
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        {error && <div className={pageStyles.note}>{error}</div>}
        <div className={pageStyles.formRow}>
          <TextField label="Nome da linha" maxLength={80} value={name} onChange={(e) => setName(e.target.value)} />
          <SelectField label="Cor" value={color} onChange={(e) => setColor(e.target.value)}>
            {COLORS.map((c, i) => (
              <option key={c} value={c}>
                {['Verde', 'Azul', 'Laranja', 'Roxo', 'Vermelho', 'Amarelo', 'Turquesa', 'Rosa'][i]}
              </option>
            ))}
          </SelectField>
          <SelectField label="Desvio a partir de" value={deviation} onChange={(e) => setDeviation(e.target.value)}>
            {[200, 300, 500, 800, 1000, 2000].map((m) => (
              <option key={m} value={m}>
                {m < 1000 ? `${m} m` : `${m / 1000} km`}
              </option>
            ))}
          </SelectField>
        </div>
        <div className={styles.actions}>
          <label className={styles.check}>
            <input type="checkbox" checked={isPublic} onChange={(e) => setPublic(e.target.checked)} />
            Link do passageiro (qualquer um com o link vê onde o ônibus está, sem a placa)
          </label>
          <label className={styles.check}>
            <input type="checkbox" checked={active} onChange={(e) => setActive(e.target.checked)} />
            Ativa
          </label>
        </div>
        <p className={styles.muted}>Clique no mapa para pôr os pontos, na ordem da viagem. Depois ajuste o nome e os minutos desde a saída.</p>
        <div className={`${styles.map} ${styles.mapTall}`}>
          <RouteMap stops={stops} color={color} onAdd={add} className={styles.leaflet} />
        </div>
        {stops.length > 0 && (
          <div className={styles.stops}>
            {stops.map((s, i) => (
              <div key={i} className={styles.stop}>
                <span className={styles.seq} style={{ background: color }}>
                  {i + 1}
                </span>
                <TextField label="Ponto" maxLength={80} value={s.name} onChange={(e) => update(i, { name: e.target.value })} />
                <TextField
                  label="Min. da saída"
                  type="number"
                  min={0}
                  max={1440}
                  value={String(s.offsetMin)}
                  onChange={(e) => update(i, { offsetMin: Math.max(0, Number(e.target.value) || 0) })}
                />
                <TextField
                  label="Raio (m)"
                  type="number"
                  min={20}
                  max={1000}
                  value={String(s.radiusM)}
                  onChange={(e) => update(i, { radiusM: Number(e.target.value) || 80 })}
                />
                <div className={styles.actions}>
                  <Button size="small" variant="ghost" aria-label="Subir" disabled={i === 0} onClick={() => move(i, -1)}>
                    ↑
                  </Button>
                  <Button size="small" variant="ghost" aria-label="Descer" disabled={i === stops.length - 1} onClick={() => move(i, 1)}>
                    ↓
                  </Button>
                  <Button size="small" variant="ghost" aria-label="Tirar" onClick={() => setStops((list) => list.filter((_, k) => k !== i))}>
                    ✕
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </Modal>
  );
}

function TripsModal({ route, vehicles, onClose }: { route: BusRoute; vehicles: VehicleView[]; onClose: () => void }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const withDevice = vehicles.filter((v) => v.device);
  const [vehicleId, setVehicleId] = useState(withDevice[0]?.id ?? vehicles[0]?.id ?? '');
  const [departs, setDeparts] = useState(defaultSlot().replace('T09:00', 'T06:00'));
  const [days, setDays] = useState('1');
  const [error, setError] = useState('');
  const save = useMutation({
    mutationFn: () => busRoutesApi.createTrips(route.id, { vehicleId, departsAt: new Date(departs).toISOString(), days: Number(days) || 1 }),
    onSuccess: (trips) => {
      void queryClient.invalidateQueries({ queryKey: [...routesKey, route.id, 'day'] });
      notify({
        tone: 'success',
        title: trips.length === 1 ? 'Viagem programada' : `${trips.length} viagens programadas`,
        description: trips[0] ? `A partir de ${formatDateTime(trips[0].departsAt).slice(0, 17)}.` : undefined,
      });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });
  return (
    <Modal
      open
      title={`Programar viagens: ${route.name}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} disabled={!vehicleId} onClick={() => save.mutate()}>
            Programar
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        {error && <div className={pageStyles.note}>{error}</div>}
        <SelectField label="Veículo" value={vehicleId} onChange={(e) => setVehicleId(e.target.value)}>
          {vehicles.map((v) => (
            <option key={v.id} value={v.id}>
              {v.name}
              {v.plate ? ` · ${v.plate}` : ''}
              {v.device ? '' : ' (sem rastreador)'}
            </option>
          ))}
        </SelectField>
        <div className={pageStyles.formRow}>
          <TextField label="Saída" type="datetime-local" value={departs} onChange={(e) => setDeparts(e.target.value)} />
          <SelectField label="Repetir" value={days} onChange={(e) => setDays(e.target.value)}>
            <option value="1">Só esse dia</option>
            <option value="5">5 dias seguidos</option>
            <option value="7">7 dias seguidos</option>
            <option value="14">14 dias seguidos</option>
            <option value="30">30 dias seguidos</option>
          </SelectField>
        </div>
        <p className={styles.muted}>
          No mesmo horário, todo dia. A viagem vai até o último ponto ({route.stops[route.stops.length - 1]?.offsetMin ?? 0} min depois
          da saída).
        </p>
      </div>
    </Modal>
  );
}

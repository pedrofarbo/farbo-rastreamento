import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { geofencesApi } from '@/api/resources';
import { DEFAULT_RADIUS, fenceProblem, formFromFence, formatRadius, notifyLabel, RADIUS_STEPS, radiusStep, scheduleLabel, shapeLabel } from '@/app/fence';
import type { FenceForm } from '@/app/fence';
import { FenceMap } from '@/app/FenceMap';
import type { FenceFocus } from '@/app/FenceMap';
import { fencesKey } from '@/app/screens/FencesScreen';
import { isSuspendedError, SuspendedNotice } from '@/components/billing/SuspendedNotice';
import { PolygonDrawMap } from '@/components/geofence/PolygonDrawMap';
import { ScheduleFields } from '@/components/geofence/ScheduleFields';
import { TrackerMap } from '@/components/map/TrackerMap';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { TextField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { useVehicles } from '@/hooks/useVehicles';
import { useAuth } from '@/stores/AuthContext';
import type { Geofence, VehicleView } from '@/types';

import pageStyles from '../Page.module.css';
import styles from './CustomerFencesPage.module.css';

/** Com mais cercas ou veículos que isso, as listas ganham a busca. */
const SEARCH_FROM = 8;
/** Veículos por vez na escolha (numa frota, centenas). */
const VEHICLES_PAGE = 100;
const DEFAULT_CENTER: [number, number] = [-23.5505, -46.6333];

/**
 * As cercas do cliente no painel: a lista e o mapa com todas; o editor com
 * círculo (o pino no meio, arrastando o mapa) ou área desenhada (clique a
 * clique), os veículos que ela vigia, os avisos e o horário deles.
 */
export function CustomerFencesPage() {
  const { canOperateAccount } = useAuth();
  const fences = useQuery({ queryKey: fencesKey, queryFn: geofencesApi.list });
  const limit = useQuery({ queryKey: [...fencesKey, 'limit'], queryFn: geofencesApi.limit });
  const vehicles = useVehicles();
  const [editing, setEditing] = useState<Geofence | 'new' | null>(null);
  const [search, setSearch] = useState('');

  const error = fences.error ?? vehicles.error;
  if (isSuspendedError(error)) return <SuspendedNotice message={error?.message} />;

  const list = fences.data ?? [];
  const own = (vehicles.data ?? []).filter((v) => !v.shared);
  const max = limit.data?.limit ?? null;
  const full = max !== null && list.length >= max;
  const term = search.trim().toLowerCase();
  const shown = term ? list.filter((f) => f.name.toLowerCase().includes(term)) : list;

  if (editing) {
    return (
      <FenceEditor
        key={editing === 'new' ? 'nova' : editing.id}
        existing={editing === 'new' ? undefined : editing}
        vehicles={own}
        canEdit={canOperateAccount}
        onDone={() => setEditing(null)}
      />
    );
  }

  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>Cercas</h1>
            <p className={pageStyles.description}>
              Avisos quando um veículo entra ou sai de um lugar: um círculo (casa, garagem) ou uma área desenhada (o pátio,
              um bairro, uma rota), só nos horários que você quiser.
              {max !== null && ` Esta conta tem ${list.length} de ${max} cercas.`}
            </p>
          </div>
          {canOperateAccount && (
            <div className={pageStyles.actions}>
              <Button variant="primary" disabled={full || fences.isLoading} onClick={() => setEditing('new')}>
                Nova cerca
              </Button>
            </div>
          )}
        </header>
        {full && <p className={styles.muted}>A conta chegou ao limite de {max} cercas: apague uma para criar outra.</p>}

        {fences.isLoading || vehicles.isLoading ? (
          <Spinner label="Carregando as cercas" />
        ) : list.length === 0 ? (
          <Card>
            <EmptyState
              icon="📍"
              title="Nenhuma cerca ainda"
              description="Crie uma cerca para saber quando um veículo chega ou sai de um lugar."
              action={
                canOperateAccount ? (
                  <Button variant="primary" onClick={() => setEditing('new')}>
                    Nova cerca
                  </Button>
                ) : undefined
              }
            />
          </Card>
        ) : (
          <div className={styles.split}>
            <div className={styles.list}>
              {list.length > SEARCH_FROM && (
                <input
                  className={fieldStyles.input}
                  type="search"
                  placeholder="Buscar cerca"
                  aria-label="Buscar cerca"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                />
              )}
              {shown.map((fence) => (
                <button key={fence.id} type="button" className={styles.fenceCard} onClick={() => setEditing(fence)}>
                  <span className={styles.fenceTop}>
                    <span className={fence.shape === 'polygon' ? styles.area : styles.ring} aria-hidden="true" />
                    <strong className={styles.fenceName}>{fence.name}</strong>
                    {!fence.active && <span className={styles.off}>desligada</span>}
                  </span>
                  <span className={styles.fenceMeta}>
                    <span>{shapeLabel(fence)}</span>
                    <span>
                      {fence.vehicleIds.length} {fence.vehicleIds.length === 1 ? 'veículo' : 'veículos'}
                    </span>
                    <span>{notifyLabel(fence)}</span>
                    <span>{scheduleLabel(fence)}</span>
                  </span>
                </button>
              ))}
              {shown.length === 0 && <p className={styles.muted}>Nenhuma cerca com esse nome.</p>}
            </div>
            <div className={styles.mapBox}>
              <TrackerMap vehicles={own} geofences={list} showControls={false} frame={framePoints(list)} />
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

/** Os pontos que enquadram as cercas: os vértices das áreas e as bordas dos círculos. */
function framePoints(fences: Geofence[]): [number, number][] {
  return fences.flatMap((f) => {
    if (f.shape === 'polygon' && f.polygon) return f.polygon;
    const dLat = f.radiusMeters / 111_320;
    const dLon = dLat / Math.max(0.1, Math.cos((f.latitude * Math.PI) / 180));
    return [
      [f.latitude - dLat, f.longitude - dLon],
      [f.latitude + dLat, f.longitude + dLon],
    ] as [number, number][];
  });
}

function newForm(vehicles: VehicleView[]): FenceForm {
  const anchor = vehicles.find((v) => v.lastPosition)?.lastPosition;
  return {
    name: '',
    shape: 'circle',
    latitude: anchor?.latitude ?? DEFAULT_CENTER[0],
    longitude: anchor?.longitude ?? DEFAULT_CENTER[1],
    radiusMeters: DEFAULT_RADIUS,
    polygon: null,
    vehicleIds: vehicles.length <= SEARCH_FROM ? vehicles.map((v) => v.id) : [],
    notifyEnter: true,
    notifyExit: true,
    scheduleDays: [],
    scheduleStart: null,
    scheduleEnd: null,
  };
}

function FenceEditor({
  existing,
  vehicles,
  canEdit,
  onDone,
}: {
  existing?: Geofence;
  vehicles: VehicleView[];
  canEdit: boolean;
  onDone: () => void;
}) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [form, setForm] = useState<FenceForm>(() => (existing ? formFromFence(existing) : newForm(vehicles)));
  const [focus] = useState<FenceFocus>(() => ({ lat: form.latitude, lon: form.longitude, key: 0 }));
  const [touched, setTouched] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [vehicleSearch, setVehicleSearch] = useState('');
  const [vehicleLimit, setVehicleLimit] = useState(VEHICLES_PAGE);
  const set = (patch: Partial<FenceForm>) => setForm((current) => ({ ...current, ...patch }));
  const problem = fenceProblem(form);

  const done = (title: string) => {
    queryClient.invalidateQueries({ queryKey: fencesKey });
    notify({ tone: 'success', title });
    onDone();
  };
  const save = useMutation({
    mutationFn: () => {
      const input = { ...form, name: form.name.trim() };
      return existing ? geofencesApi.update(existing.id, input) : geofencesApi.create(input);
    },
    onSuccess: () => done(existing ? 'Cerca salva' : 'Cerca criada'),
    onError: (error: Error) => notify({ tone: 'error', title: 'Não foi possível salvar', description: error.message }),
  });
  const remove = useMutation({
    mutationFn: () => geofencesApi.remove(existing!.id),
    onSuccess: () => done('Cerca apagada'),
    onError: (error: Error) => notify({ tone: 'error', title: 'Não foi possível apagar', description: error.message }),
  });

  const foundVehicles = useMemo(() => {
    const t = vehicleSearch.trim().toLowerCase();
    return t ? vehicles.filter((v) => `${v.name} ${v.plate ?? ''}`.toLowerCase().includes(t)) : vehicles;
  }, [vehicles, vehicleSearch]);
  const toggle = (id: string) =>
    set({ vehicleIds: form.vehicleIds.includes(id) ? form.vehicleIds.filter((v) => v !== id) : [...form.vehicleIds, id] });
  const center: [number, number] = [form.latitude, form.longitude];

  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>{existing ? existing.name : 'Nova cerca'}</h1>
            <p className={pageStyles.description}>
              {form.shape === 'polygon'
                ? 'Clique no mapa para desenhar a borda da área, ponto a ponto.'
                : 'Arraste o mapa: a cerca fica no centro. O tamanho é o raio, ao lado.'}
            </p>
          </div>
          <div className={pageStyles.actions}>
            <Button variant="ghost" onClick={onDone}>
              Voltar às cercas
            </Button>
          </div>
        </header>

        <div className={styles.editor}>
          <div className={styles.editorMap}>
            {canEdit && (
              <div className={styles.shapes} role="radiogroup" aria-label="Formato da cerca">
                <button
                  type="button"
                  role="radio"
                  aria-checked={form.shape === 'circle'}
                  className={form.shape === 'circle' ? styles.shapeOn : styles.shapeOff}
                  onClick={() => set({ shape: 'circle' })}
                >
                  Círculo
                </button>
                <button
                  type="button"
                  role="radio"
                  aria-checked={form.shape === 'polygon'}
                  className={form.shape === 'polygon' ? styles.shapeOn : styles.shapeOff}
                  onClick={() => set({ shape: 'polygon', polygon: form.polygon ?? [] })}
                >
                  Desenhar área
                </button>
              </div>
            )}
            <div className={styles.mapFrame}>
              {form.shape === 'polygon' ? (
                <PolygonDrawMap
                  points={form.polygon ?? []}
                  onChange={(polygon) => canEdit && set({ polygon })}
                  center={center}
                  vehicles={vehicles}
                />
              ) : (
                <FenceMap
                  focus={focus}
                  radius={form.radiusMeters}
                  vehicles={vehicles}
                  onCenter={(latitude, longitude) => set({ latitude, longitude })}
                />
              )}
            </div>
            {form.shape === 'polygon' && canEdit && (
              <div className={styles.drawActions}>
                <Button
                  size="small"
                  variant="ghost"
                  disabled={!form.polygon?.length}
                  onClick={() => set({ polygon: (form.polygon ?? []).slice(0, -1) })}
                >
                  Desfazer o último ponto
                </Button>
                <Button size="small" variant="ghost" disabled={!form.polygon?.length} onClick={() => set({ polygon: [] })}>
                  Começar de novo
                </Button>
              </div>
            )}
          </div>

          <div className={styles.form}>
            <TextField
              label="Nome da cerca"
              value={form.name}
              maxLength={100}
              placeholder="Ex.: Pátio, Cliente Centro"
              disabled={!canEdit}
              error={touched && !form.name.trim() ? 'Dê um nome para a cerca.' : undefined}
              onChange={(e) => set({ name: e.target.value })}
            />

            {form.shape === 'circle' && (
              <label className={styles.field}>
                <span>
                  Tamanho: <strong>{formatRadius(form.radiusMeters)}</strong> de raio
                </span>
                <input
                  type="range"
                  min={0}
                  max={RADIUS_STEPS.length - 1}
                  step={1}
                  value={radiusStep(form.radiusMeters)}
                  disabled={!canEdit}
                  onChange={(e) => set({ radiusMeters: RADIUS_STEPS[Number(e.target.value)] })}
                  aria-valuetext={formatRadius(form.radiusMeters)}
                />
              </label>
            )}

            <fieldset className={styles.group} disabled={!canEdit}>
              <legend>
                Veículos ({form.vehicleIds.length} de {vehicles.length})
              </legend>
              {vehicles.length > SEARCH_FROM && (
                <div className={styles.vehicleTools}>
                  <input
                    className={fieldStyles.input}
                    type="search"
                    placeholder="Buscar por nome ou placa"
                    aria-label="Buscar veículo"
                    value={vehicleSearch}
                    onChange={(e) => {
                      setVehicleSearch(e.target.value);
                      setVehicleLimit(VEHICLES_PAGE);
                    }}
                  />
                  <Button
                    size="small"
                    variant="ghost"
                    onClick={() => set({ vehicleIds: [...new Set([...form.vehicleIds, ...foundVehicles.map((v) => v.id)])] })}
                  >
                    Marcar {vehicleSearch.trim() ? 'os achados' : 'todos'}
                  </Button>
                  <Button size="small" variant="ghost" onClick={() => set({ vehicleIds: [] })}>
                    Desmarcar todos
                  </Button>
                </div>
              )}
              <ul className={styles.vehicles}>
                {foundVehicles.slice(0, vehicleLimit).map((v) => (
                  <li key={v.id}>
                    <label>
                      <input type="checkbox" checked={form.vehicleIds.includes(v.id)} onChange={() => toggle(v.id)} />
                      <span>{v.name}</span>
                      {v.plate && <span className={styles.plate}>{v.plate}</span>}
                    </label>
                  </li>
                ))}
              </ul>
              {foundVehicles.length > vehicleLimit && (
                <Button size="small" variant="ghost" onClick={() => setVehicleLimit((n) => n + VEHICLES_PAGE)}>
                  Mostrar mais {Math.min(VEHICLES_PAGE, foundVehicles.length - vehicleLimit)}
                </Button>
              )}
            </fieldset>

            <fieldset className={styles.group} disabled={!canEdit}>
              <legend>Avisar quando</legend>
              <label className={styles.check}>
                <input type="checkbox" checked={form.notifyEnter} onChange={() => set({ notifyEnter: !form.notifyEnter })} />
                Entrar na cerca
              </label>
              <label className={styles.check}>
                <input type="checkbox" checked={form.notifyExit} onChange={() => set({ notifyExit: !form.notifyExit })} />
                Sair da cerca
              </label>
            </fieldset>

            <fieldset className={styles.group} disabled={!canEdit}>
              <legend>Horário dos avisos</legend>
              <ScheduleFields value={form} onChange={(schedule) => set(schedule)} />
            </fieldset>

            {canEdit ? (
              <div className={styles.formActions}>
                {touched && problem && <p className={styles.error}>{problem}</p>}
                <Button
                  variant="primary"
                  loading={save.isPending}
                  onClick={() => {
                    setTouched(true);
                    if (!problem) save.mutate();
                  }}
                >
                  {existing ? 'Salvar cerca' : 'Criar cerca'}
                </Button>
                {existing && (
                  <Button
                    variant={confirmDelete ? 'danger' : 'ghost'}
                    loading={remove.isPending}
                    onClick={() => (confirmDelete ? remove.mutate() : setConfirmDelete(true))}
                  >
                    {confirmDelete ? 'Clique de novo para apagar' : 'Apagar cerca'}
                  </Button>
                )}
              </div>
            ) : (
              <p className={styles.muted}>Seu acesso a esta conta é para acompanhar: as cercas são do titular e dos gestores.</p>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

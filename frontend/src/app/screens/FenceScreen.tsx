import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';

import { geofencesApi } from '@/api/resources';
import { isSuspendedError, SuspendedNotice } from '@/components/billing/SuspendedNotice';
import { Address } from '@/components/ui/Address';
import { Button } from '@/components/ui/Button';
import { EmptyState } from '@/components/ui/EmptyState';
import { TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { useVehicles } from '@/hooks/useVehicles';
import type { Geofence, VehicleView } from '@/types';

import { DEFAULT_RADIUS, fenceProblem, formatRadius, NAME_SUGGESTIONS, RADIUS_STEPS, radiusStep } from '../fence';
import type { FenceForm } from '../fence';
import { FenceMap } from '../FenceMap';
import type { FenceFocus } from '../FenceMap';
import { haptic } from '../haptics';
import { BackIcon } from '../icons';
import { fencesKey } from './FencesScreen';
import fence from './Fence.module.css';
import styles from './Screen.module.css';

/** Sem veículo com posição: o centro de São Paulo. */
const DEFAULT_CENTER = { lat: -23.5505, lon: -46.6333 };

/** Nova cerca (/cercas/nova, ?veiculo=<id> para começar nele) ou edição. */
export function FenceScreen() {
  const { id } = useParams<{ id: string }>();
  const [params] = useSearchParams();
  const vehicles = useVehicles();
  const fences = useQuery({ queryKey: fencesKey, queryFn: geofencesApi.list });

  const error = vehicles.error ?? fences.error;
  if (isSuspendedError(error)) return <SuspendedNotice message={error?.message} />;
  if (vehicles.isLoading || fences.isLoading) return <Spinner label="Carregando" />;

  const existing = id ? fences.data?.find((item) => item.id === id) : undefined;
  if (id && !existing) {
    return <EmptyState title="Cerca não encontrada" description="Ela pode ter sido apagada." />;
  }
  return (
    <FenceEditor
      key={id ?? 'nova'}
      existing={existing}
      vehicles={vehicles.data ?? []}
      preselect={params.get('veiculo')}
    />
  );
}

function initialForm(existing: Geofence | undefined, vehicles: VehicleView[], preselect: string | null): FenceForm {
  if (existing) {
    return {
      name: existing.name,
      latitude: existing.latitude,
      longitude: existing.longitude,
      radiusMeters: existing.radiusMeters,
      vehicleIds: existing.vehicleIds,
      notifyEnter: existing.notifyEnter,
      notifyExit: existing.notifyExit,
    };
  }
  // Nova: começa onde o veículo escolhido (ou o primeiro com sinal) está.
  const chosen = vehicles.find((vehicle) => vehicle.id === preselect);
  const anchor = (chosen ?? vehicles.find((vehicle) => vehicle.lastPosition))?.lastPosition;
  return {
    name: '',
    latitude: anchor?.latitude ?? DEFAULT_CENTER.lat,
    longitude: anchor?.longitude ?? DEFAULT_CENTER.lon,
    radiusMeters: DEFAULT_RADIUS,
    vehicleIds: chosen ? [chosen.id] : vehicles.map((vehicle) => vehicle.id),
    notifyEnter: true,
    notifyExit: true,
  };
}

function FenceEditor({
  existing,
  vehicles,
  preselect,
}: {
  existing?: Geofence;
  vehicles: VehicleView[];
  preselect: string | null;
}) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [form, setForm] = useState<FenceForm>(() => initialForm(existing, vehicles, preselect));
  const [focus, setFocus] = useState<FenceFocus>(() => ({ lat: form.latitude, lon: form.longitude, key: 0 }));
  const [locating, setLocating] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [touched, setTouched] = useState(false);

  const located = vehicles.filter((vehicle) => vehicle.lastPosition);
  const problem = fenceProblem(form);
  const set = (patch: Partial<FenceForm>) => setForm((current) => ({ ...current, ...patch }));

  // Aberto direto (link ou recarregar), não há tela anterior no app.
  const leave = () => {
    const state = window.history.state as { idx?: number } | null;
    if (state?.idx) navigate(-1);
    else navigate('/cercas', { replace: true });
  };

  const done = (title: string) => {
    queryClient.invalidateQueries({ queryKey: fencesKey });
    haptic();
    notify({ tone: 'success', title });
    leave();
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

  const goTo = (lat: number, lon: number) => setFocus((current) => ({ lat, lon, key: current.key + 1 }));

  const locate = () => {
    if (!('geolocation' in navigator)) return;
    setLocating(true);
    navigator.geolocation.getCurrentPosition(
      (position) => {
        setLocating(false);
        goTo(position.coords.latitude, position.coords.longitude);
      },
      () => {
        setLocating(false);
        notify({
          tone: 'error',
          title: 'Não foi possível saber onde você está',
          description: 'Libere a localização para este app nas configurações do celular, ou arraste o mapa.',
        });
      },
      { enableHighAccuracy: true, timeout: 15_000, maximumAge: 60_000 },
    );
  };

  const toggleVehicle = (vehicleId: string) =>
    set({
      vehicleIds: form.vehicleIds.includes(vehicleId)
        ? form.vehicleIds.filter((item) => item !== vehicleId)
        : [...form.vehicleIds, vehicleId],
    });

  const submit = () => {
    setTouched(true);
    if (!problem) save.mutate();
  };

  const busy = save.isPending || remove.isPending;

  return (
    <div className={styles.screen}>
      <div className={styles.navbar}>
        <button type="button" className={styles.back} onClick={leave} aria-label="Voltar">
          <BackIcon />
        </button>
        <div className={styles.navTitle}>
          <h1>{existing ? existing.name : 'Nova cerca'}</h1>
        </div>
      </div>

      <div className={fence.where}>
        <FenceMap
          focus={focus}
          radius={form.radiusMeters}
          vehicles={vehicles}
          onCenter={(latitude, longitude) => set({ latitude, longitude })}
        />
        <Address lat={form.latitude} lon={form.longitude} className={fence.address} />
        {(located.length > 0 || 'geolocation' in navigator) && (
          <div className={styles.chips} role="group" aria-label="Levar o centro da cerca até">
            {'geolocation' in navigator && (
              <button type="button" className={styles.chip} onClick={locate} disabled={locating}>
                {locating ? 'Localizando…' : 'Onde estou'}
              </button>
            )}
            {located.map((vehicle) => (
              <button
                key={vehicle.id}
                type="button"
                className={styles.chip}
                onClick={() => goTo(vehicle.lastPosition!.latitude, vehicle.lastPosition!.longitude)}
              >
                {vehicle.name}
              </button>
            ))}
          </div>
        )}
      </div>

      <section className={styles.section}>
        <TextField
          label="Nome da cerca"
          value={form.name}
          maxLength={100}
          placeholder="Ex.: Casa"
          enterKeyHint="done"
          error={touched && !form.name.trim() ? 'Dê um nome para a cerca.' : undefined}
          onChange={(event) => set({ name: event.target.value })}
        />
        <div className={fence.suggestions}>
          {NAME_SUGGESTIONS.map((name) => (
            <button
              key={name}
              type="button"
              className={`${styles.chip} ${form.name === name ? styles.chipActive : ''}`}
              onClick={() => set({ name })}
              aria-pressed={form.name === name}
            >
              {name}
            </button>
          ))}
        </div>
      </section>

      <section className={styles.section}>
        <div className={fence.radiusHead}>
          <h2 className={styles.sectionTitle}>Tamanho</h2>
          <span className={fence.radiusValue}>{formatRadius(form.radiusMeters)}</span>
        </div>
        <input
          type="range"
          className={fence.slider}
          min={0}
          max={RADIUS_STEPS.length - 1}
          step={1}
          value={radiusStep(form.radiusMeters)}
          onChange={(event) => set({ radiusMeters: RADIUS_STEPS[Number(event.target.value)] })}
          aria-label="Raio da cerca"
          aria-valuetext={formatRadius(form.radiusMeters)}
        />
        <div className={fence.scale} aria-hidden="true">
          <span>{formatRadius(RADIUS_STEPS[0])}</span>
          <span>{formatRadius(RADIUS_STEPS[RADIUS_STEPS.length - 1])}</span>
        </div>
        <p className={styles.muted}>
          A cerca é o círculo verde. Para casa ou trabalho, 200 m costumam bastar: o GPS varia alguns metros, e uma cerca
          muito justa pode avisar à toa.
        </p>
      </section>

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>Veículos</h2>
        {vehicles.length === 0 ? (
          <p className={styles.muted}>Você ainda não tem veículos.</p>
        ) : (
          <ul className={fence.toggles}>
            {vehicles.map((vehicle) => (
              <li key={vehicle.id}>
                <label className={fence.toggle}>
                  <span className={fence.toggleText}>
                    <span className={fence.toggleLabel}>{vehicle.name}</span>
                    {vehicle.plate && <span className={styles.plate}>{vehicle.plate}</span>}
                  </span>
                  <input
                    type="checkbox"
                    className={fence.switch}
                    checked={form.vehicleIds.includes(vehicle.id)}
                    onChange={() => toggleVehicle(vehicle.id)}
                  />
                </label>
              </li>
            ))}
          </ul>
        )}
        {touched && form.vehicleIds.length === 0 && <p className={styles.error}>Escolha ao menos um veículo.</p>}
      </section>

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>Avisar quando</h2>
        <ul className={fence.toggles}>
          <li>
            <label className={fence.toggle}>
              <span className={fence.toggleText}>
                <span className={fence.toggleLabel}>Entrar na cerca</span>
                <span className={fence.toggleHint}>Ex.: chegou em casa</span>
              </span>
              <input
                type="checkbox"
                className={fence.switch}
                checked={form.notifyEnter}
                onChange={() => set({ notifyEnter: !form.notifyEnter })}
              />
            </label>
          </li>
          <li>
            <label className={fence.toggle}>
              <span className={fence.toggleText}>
                <span className={fence.toggleLabel}>Sair da cerca</span>
                <span className={fence.toggleHint}>Ex.: saiu da garagem</span>
              </span>
              <input
                type="checkbox"
                className={fence.switch}
                checked={form.notifyExit}
                onChange={() => set({ notifyExit: !form.notifyExit })}
              />
            </label>
          </li>
        </ul>
        <p className={styles.muted}>
          O aviso chega por e-mail e, com as notificações ligadas, neste celular. Entradas e saídas ficam no histórico do
          veículo mesmo com o aviso desligado.
        </p>
      </section>

      <div className={fence.actions}>
        {touched && problem && <p className={styles.error}>{problem}</p>}
        <Button variant="primary" onClick={submit} loading={save.isPending} disabled={busy} block>
          {existing ? 'Salvar cerca' : 'Criar cerca'}
        </Button>
        {existing && (
          <Button
            variant={confirmDelete ? 'danger' : 'ghost'}
            onClick={() => (confirmDelete ? remove.mutate() : setConfirmDelete(true))}
            loading={remove.isPending}
            disabled={busy}
            block
          >
            {confirmDelete ? 'Toque de novo para apagar' : 'Apagar cerca'}
          </Button>
        )}
      </div>
    </div>
  );
}

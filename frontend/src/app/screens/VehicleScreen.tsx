import { useCallback, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';

import { geofencesApi, vehiclesApi } from '@/api/resources';
import { isSuspendedError, SuspendedNotice } from '@/components/billing/SuspendedNotice';
import { TrackerMap } from '@/components/map/TrackerMap';
import { Address } from '@/components/ui/Address';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { CommandPanel } from '@/components/vehicle/CommandPanel';
import { TheftPanel } from '@/components/vehicle/TheftPanel';
import { useVehicle } from '@/hooks/useVehicles';
import {
  eventSeverity,
  formatDateTime,
  formatDeviceStatus,
  formatDistance,
  formatEvent,
  formatRelative,
  formatSpeed,
} from '@/services/format';

import { fencesOf, formatRadius, notifyLabel } from '../fence';
import { BackIcon, ChevronIcon, ExpandIcon } from '../icons';
import { isIos } from '../pwa';
import { directionsUrl, summarizeTrip, tripWindow } from '../trip';
import type { TripRange } from '../trip';
import { VehicleMapFullscreen } from '../VehicleMapFullscreen';
import { fencesKey } from './FencesScreen';
import { statusTone } from './MapScreen';
import styles from './Screen.module.css';

const RANGES: { value: TripRange; label: string }[] = [
  { value: 'today', label: 'Hoje' },
  { value: 'yesterday', label: 'Ontem' },
  { value: 'last24h', label: 'Últimas 24 h' },
];

function yesNo(value: boolean | null | undefined, yes: string, no: string): string {
  return value === null || value === undefined ? '—' : value ? yes : no;
}

/** Um veículo: onde está, como está, comandos, trajeto e eventos. */
export function VehicleScreen() {
  const { id = '' } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { notify } = useToast();
  const query = useVehicle(id);
  const [range, setRange] = useState<TripRange | null>(null);
  const location = useLocation();
  const [params] = useSearchParams();
  // Tela cheia no endereço (?mapa=tela-cheia): o "voltar" do celular fecha
  // o mapa em vez de sair do veículo.
  const fullscreen = params.get('mapa') === 'tela-cheia';
  const openFullscreen = () => navigate({ search: '?mapa=tela-cheia' }, { state: { fullscreenMap: true } });
  const closeFullscreen = useCallback(() => {
    if ((location.state as { fullscreenMap?: boolean } | null)?.fullscreenMap) navigate(-1);
    else navigate({ search: '' }, { replace: true });
  }, [location.state, navigate]);

  // Veículo de outra pessoa, compartilhado com quem está vendo: só a posição
  // ao vivo (o trajeto, os eventos e as cercas continuam só do dono).
  const shared = query.data?.shared ?? null;
  const owned = Boolean(query.data && !query.data.shared);

  const period = range ? tripWindow(range) : null;
  const trip = useQuery({
    queryKey: ['app-trip', id, range],
    queryFn: () => vehiclesApi.positions(id, { from: period!.from, to: period!.to }),
    enabled: Boolean(range && id && owned),
  });
  const events = useQuery({ queryKey: ['app-events', id], queryFn: () => vehiclesApi.events(id, { limit: 15 }), enabled: Boolean(id && owned) });
  const fences = useQuery({ queryKey: fencesKey, queryFn: geofencesApi.list });
  const mine = useMemo(() => fencesOf(fences.data ?? [], id), [fences.data, id]);

  const track = trip.data?.positions ?? [];
  const summary = useMemo(() => summarizeTrip(track), [track]);

  if (isSuspendedError(query.error)) return <SuspendedNotice message={query.error?.message} />;
  if (query.isLoading) return <Spinner label="Carregando veículo" />;
  const vehicle = query.data;
  if (!vehicle) {
    return <EmptyState title="Veículo não encontrado" description="Ele pode ter sido removido da sua conta." />;
  }

  const position = vehicle.lastPosition;
  const state = vehicle.state;
  const acc = state?.acc ?? position?.acc ?? null;
  const voltage = state?.batteryVoltage ?? position?.batteryVoltage ?? null;
  const battery = state?.batteryPercent ?? position?.batteryPercent ?? null;
  const gsm = state?.gsmLevel ?? position?.gsmLevel ?? null;

  // Aberto direto no veículo (toque na notificação), não há tela anterior no
  // app: voltar leva ao mapa em vez de sair do app.
  const goBack = () => {
    const state = window.history.state as { idx?: number } | null;
    if (state?.idx) navigate(-1);
    else navigate('/mapa', { replace: true });
  };

  const share = async () => {
    if (!position) return;
    const url = directionsUrl(position.latitude, position.longitude, isIos());
    try {
      if (navigator.share) {
        await navigator.share({ title: vehicle.name, text: `Localização de ${vehicle.name}`, url });
      } else {
        await navigator.clipboard.writeText(url);
        notify({ tone: 'success', title: 'Link da localização copiado' });
      }
    } catch {
      /* o cliente cancelou o compartilhamento */
    }
  };

  return (
    <div className={styles.screen}>
      <div className={styles.navbar}>
        <button type="button" className={styles.back} onClick={goBack} aria-label="Voltar">
          <BackIcon />
        </button>
        <div className={styles.navTitle}>
          <h1>{vehicle.name}</h1>
          {vehicle.plate && <span className={styles.plate}>{vehicle.plate}</span>}
        </div>
        <Badge tone={statusTone(vehicle)} dot>
          {vehicle.device ? formatDeviceStatus(vehicle.device.status) : 'Sem rastreador'}
        </Badge>
      </div>

      {shared && (
        <p className={styles.sharedNote}>
          Compartilhado por <strong>{shared.ownerName}</strong>: você acompanha a posição ao vivo
          {shared.canBlock ? ' e pode bloquear o motor numa emergência' : ''}.
        </p>
      )}

      <div className={styles.vehicleMap}>
        <TrackerMap
          vehicles={[vehicle]}
          selectedId={vehicle.id}
          track={range ? track : undefined}
          geofences={mine}
          showControls={false}
        />
        <button type="button" className={styles.mapExpand} onClick={openFullscreen} aria-label="Ver o mapa em tela cheia">
          <ExpandIcon />
        </button>
      </div>
      {fullscreen && (
        <VehicleMapFullscreen
          vehicle={vehicle}
          geofences={mine}
          range={range}
          onRange={setRange}
          track={track}
          trackLoading={trip.isLoading}
          trip={summary}
          onClose={closeFullscreen}
        />
      )}

      {position ? (
        <>
          <Address lat={position.latitude} lon={position.longitude} className={styles.muted} />
          <div className={styles.tiles}>
            <div className={styles.tile}>
              <span className={styles.tileLabel}>Ignição</span>
              <span className={`${styles.tileValue} ${acc ? styles.good : ''}`}>{yesNo(acc, 'Ligada', 'Desligada')}</span>
            </div>
            <div className={styles.tile}>
              <span className={styles.tileLabel}>Velocidade</span>
              <span className={styles.tileValue}>{formatSpeed(position.speedKmh)}</span>
            </div>
            <div className={styles.tile}>
              <span className={styles.tileLabel}>Motor</span>
              <span className={`${styles.tileValue} ${state?.relayOn ? styles.bad : ''}`}>
                {yesNo(state?.relayOn, 'Bloqueado', 'Liberado')}
              </span>
            </div>
            <div className={styles.tile}>
              <span className={styles.tileLabel}>Bateria</span>
              <span className={styles.tileValue}>
                {voltage !== null ? `${voltage.toFixed(1)} V` : battery !== null ? `${battery}%` : '—'}
              </span>
            </div>
            <div className={styles.tile}>
              <span className={styles.tileLabel}>Sinal</span>
              <span className={styles.tileValue}>{gsm !== null ? `${gsm}/4` : '—'}</span>
            </div>
            <div className={styles.tile}>
              <span className={styles.tileLabel}>Atualizado</span>
              <span className={styles.tileValue}>{formatRelative(position.gpsTimestamp)}</span>
            </div>
          </div>
          <div className={styles.row}>
            <Button variant="secondary" onClick={() => openExternal(directionsUrl(position.latitude, position.longitude, isIos()))}>
              Como chegar
            </Button>
            {!shared && (
              <Button variant="secondary" onClick={share}>
                Compartilhar
              </Button>
            )}
          </div>
        </>
      ) : (
        <p className={styles.muted}>
          {vehicle.device ? 'Aguardando o primeiro sinal do rastreador.' : 'Este veículo ainda não tem rastreador instalado.'}
        </p>
      )}

      {/* Modo roubo: aberto pelo alerta (?roubo=1), já pergunta se é para ligar. */}
      <TheftPanel
        vehicle={vehicle}
        prompt={params.get('roubo') === '1'}
        reportUrl={owned ? `/relatorio-roubo/${vehicle.id}` : undefined}
      />

      {vehicle.device && (
        <section className={styles.section}>
          <h2 className={styles.sectionTitle}>Comandos</h2>
          <CommandPanel vehicle={vehicle} />
        </section>
      )}

      {/* Só do dono: as cercas, o trajeto e os eventos. */}
      {!shared && (
        <>
          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>Cercas</h2>
            {mine.length === 0 ? (
              <p className={styles.muted}>Receba um aviso quando {vehicle.name} chegar ou sair de casa, do trabalho ou da escola.</p>
            ) : (
              <ul className={styles.list}>
                {mine.map((fence) => (
                  <li key={fence.id}>
                    <Link to={`/cercas/${fence.id}`} className={styles.listLink}>
                      <span>
                        <strong>{fence.name}</strong>
                        <span className={styles.muted}>
                          {' '}
                          · {formatRadius(fence.radiusMeters)} · {notifyLabel(fence).toLowerCase()}
                        </span>
                      </span>
                      <span className={styles.chevron}>
                        <ChevronIcon />
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
            <Button variant="secondary" onClick={() => navigate(`/cercas/nova?veiculo=${vehicle.id}`)}>
              Criar cerca aqui
            </Button>
          </section>

          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>Trajeto</h2>
            <div className={styles.chips} role="group" aria-label="Período do trajeto">
              {RANGES.map((option) => (
                <button
                  key={option.value}
                  type="button"
                  className={`${styles.chip} ${range === option.value ? styles.chipActive : ''}`}
                  onClick={() => setRange(range === option.value ? null : option.value)}
                  aria-pressed={range === option.value}
                >
                  {option.label}
                </button>
              ))}
            </div>
            {range === null && <p className={styles.muted}>Escolha um período para ver o caminho no mapa.</p>}
            {range !== null && trip.isLoading && <Spinner label="Buscando trajeto" />}
            {range !== null && trip.data && (
              track.length < 2 ? (
                <p className={styles.muted}>Sem deslocamento registrado nesse período.</p>
              ) : (
                <div className={styles.tiles}>
                  <div className={styles.tile}>
                    <span className={styles.tileLabel}>Distância</span>
                    <span className={styles.tileValue}>{formatDistance(summary.distanceMeters)}</span>
                  </div>
                  <div className={styles.tile}>
                    <span className={styles.tileLabel}>Máxima</span>
                    <span className={styles.tileValue}>{formatSpeed(summary.maxSpeedKmh)}</span>
                  </div>
                  <div className={styles.tile}>
                    <span className={styles.tileLabel}>Pontos</span>
                    <span className={styles.tileValue}>{trip.data.total}</span>
                  </div>
                </div>
              )
            )}
          </section>

          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>Últimos eventos</h2>
            {events.isLoading ? (
              <Spinner label="Carregando eventos" />
            ) : (events.data ?? []).length === 0 ? (
              <p className={styles.muted}>Nenhum evento recente.</p>
            ) : (
              <ul className={styles.list}>
                {(events.data ?? []).map((event) => (
                  <li key={event.id} className={styles.listItem}>
                    <span>
                      <Badge tone={eventSeverity(event.type)}>{formatEvent(event.type)}</Badge>
                      {typeof event.metadata?.geofenceName === 'string' && event.metadata.geofenceName && (
                        <span className={styles.muted}> {event.metadata.geofenceName}</span>
                      )}
                    </span>
                    <span className={styles.muted}>{formatDateTime(event.timestamp)}</span>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </>
      )}
    </div>
  );
}

/** Abre o app de mapas (fora do app, sem perder o app aberto). */
function openExternal(url: string) {
  window.open(url, '_blank', 'noopener,noreferrer');
}

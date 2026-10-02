import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

import { TrackerMap } from '@/components/map/TrackerMap';
import { Address } from '@/components/ui/Address';
import { Badge } from '@/components/ui/Badge';
import { formatDeviceStatus, formatDistance, formatRelative, formatSpeed } from '@/services/format';
import type { Geofence, Position, VehicleView } from '@/types';

import { haptic } from './haptics';
import { CloseIcon, LocateIcon } from './icons';
import { statusTone } from './screens/MapScreen';
import type { TripRange } from './trip';
import styles from './VehicleMapFullscreen.module.css';

const RANGES: { value: TripRange | null; label: string }[] = [
  { value: null, label: 'Agora' },
  { value: 'today', label: 'Hoje' },
  { value: 'yesterday', label: 'Ontem' },
  { value: 'last24h', label: 'Últimas 24 h' },
];

interface VehicleMapFullscreenProps {
  vehicle: VehicleView;
  geofences: Geofence[];
  range: TripRange | null;
  onRange: (range: TripRange | null) => void;
  track: Position[];
  trackLoading: boolean;
  trip: { distanceMeters: number; maxSpeedKmh: number };
  onClose: () => void;
}

/**
 * O mapa do veículo na tela inteira, por cima das barras do app. Segue o
 * veículo ao vivo até o cliente arrastar o mapa; a mira volta a seguir. O
 * trajeto (hoje, ontem, 24 h) também pode ser visto aqui.
 */
export function VehicleMapFullscreen({
  vehicle,
  geofences,
  range,
  onRange,
  track,
  trackLoading,
  trip,
  onClose,
}: VehicleMapFullscreenProps) {
  const [following, setFollowing] = useState(true);
  const [recenter, setRecenter] = useState(0);
  const closeRef = useRef<HTMLButtonElement>(null);
  const position = vehicle.lastPosition;
  const acc = vehicle.state?.acc ?? position?.acc ?? null;
  const showingTrip = range !== null;

  // Esc fecha (teclado/computador); o foco vai para o fechar e volta para quem
  // abriu.
  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    closeRef.current?.focus({ preventScroll: true });
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('keydown', onKey);
      opener?.focus?.({ preventScroll: true });
    };
  }, [onClose]);

  const center = () => {
    haptic();
    setFollowing(true);
    setRecenter((key) => key + 1);
  };

  return createPortal(
    <div className={styles.overlay} role="dialog" aria-modal="true" aria-label={`Mapa de ${vehicle.name} em tela cheia`}>
      <div className={styles.map}>
        <TrackerMap
          vehicles={[vehicle]}
          selectedId={vehicle.id}
          track={showingTrip ? track : undefined}
          geofences={geofences}
          showControls={false}
          following={following}
          onFollowingChange={setFollowing}
          recenterKey={recenter}
        />
      </div>

      <div className={styles.top}>
        <button ref={closeRef} type="button" className={styles.round} onClick={onClose} aria-label="Sair da tela cheia">
          <CloseIcon />
        </button>
        <div className={styles.title}>
          <span className={styles.name}>{vehicle.name}</span>
          <Badge tone={statusTone(vehicle)} dot>
            {vehicle.device ? formatDeviceStatus(vehicle.device.status) : 'Sem rastreador'}
          </Badge>
        </div>
      </div>

      <div className={styles.dock}>
        <button
          type="button"
          className={`${styles.round} ${styles.locate} ${following && !showingTrip ? styles.locateOn : ''}`}
          onClick={center}
          aria-label={showingTrip ? 'Mostrar o trajeto inteiro' : 'Seguir o veículo'}
          aria-pressed={showingTrip ? undefined : following}
        >
          <LocateIcon active={following && !showingTrip} />
        </button>

        <div className={styles.bottom}>
          <div className={styles.chips} role="group" aria-label="O que mostrar no mapa">
            {RANGES.map((option) => (
              <button
                key={option.label}
                type="button"
                className={`${styles.chip} ${range === option.value ? styles.chipActive : ''}`}
                onClick={() => {
                  onRange(option.value);
                  setFollowing(true);
                }}
                aria-pressed={range === option.value}
              >
                {option.label}
              </button>
            ))}
          </div>
          <div className={styles.info} aria-live="polite">
            {showingTrip ? (
              trackLoading ? (
                <span className={styles.muted}>Buscando trajeto…</span>
              ) : track.length < 2 ? (
                <span className={styles.muted}>Sem deslocamento registrado nesse período.</span>
              ) : (
                <span className={styles.stats}>
                  <span>{formatDistance(trip.distanceMeters)}</span>
                  <span>máx. {formatSpeed(trip.maxSpeedKmh)}</span>
                </span>
              )
            ) : position ? (
              <>
                <span className={styles.stats}>
                  <span>{formatSpeed(position.speedKmh)}</span>
                  <span className={acc ? styles.good : undefined}>
                    Ignição {acc === null ? '—' : acc ? 'ligada' : 'desligada'}
                  </span>
                  <span>{formatRelative(position.gpsTimestamp)}</span>
                </span>
                <Address lat={position.latitude} lon={position.longitude} className={styles.address} />
              </>
            ) : (
              <span className={styles.muted}>Aguardando o primeiro sinal do rastreador.</span>
            )}
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
}

import { useCallback, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { CSSProperties } from 'react';
import { useNavigate } from 'react-router-dom';

import { geofencesApi } from '@/api/resources';
import { isSuspendedError, SuspendedNotice } from '@/components/billing/SuspendedNotice';
import { TrackerMap } from '@/components/map/TrackerMap';
import { Badge } from '@/components/ui/Badge';
import type { BadgeTone } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { useVehicles } from '@/hooks/useVehicles';
import { formatDeviceStatus, formatRelative, formatSpeed } from '@/services/format';
import type { VehicleView } from '@/types';

import { BottomSheet } from '../BottomSheet';
import { ChevronIcon } from '../icons';
import { fencesKey } from './FencesScreen';
import styles from './Screen.module.css';

export function statusTone(vehicle: VehicleView): BadgeTone {
  switch (vehicle.device?.status) {
    case 'ONLINE':
      return 'success';
    case 'STALE':
      return 'warning';
    default:
      return 'neutral';
  }
}

/** Primeiro os que estão comunicando, depois por nome. */
function sortVehicles(list: VehicleView[]): VehicleView[] {
  const rank = (v: VehicleView) => (v.device?.status === 'ONLINE' ? 0 : v.device ? 1 : 2);
  return [...list].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
}

/** Tela inicial: o mapa na tela toda e a lista de veículos deslizando por cima. */
export function MapScreen() {
  const navigate = useNavigate();
  const vehicles = useVehicles();
  const fences = useQuery({ queryKey: fencesKey, queryFn: geofencesApi.list });
  const [selected, setSelected] = useState<string | null>(null);
  const [mapInset, setMapInset] = useState(0);
  const growTimer = useRef<number>();
  const list = useMemo(() => sortVehicles(vehicles.data ?? []), [vehicles.data]);

  // O mapa termina onde a lista para (até o meio), para o veículo ficar no
  // centro da parte visível. Ao abrir a lista, o mapa encolhe só depois da
  // animação (senão aparece um vão atrás dela); ao recolher, cresce na hora.
  const onSettle = useCallback((visible: number, half: number) => {
    const next = Math.max(0, Math.min(visible, half) - 22);
    window.clearTimeout(growTimer.current);
    setMapInset((current) => {
      if (next > current) {
        growTimer.current = window.setTimeout(() => setMapInset(next), 340);
        return current;
      }
      return next;
    });
  }, []);

  if (isSuspendedError(vehicles.error)) return <SuspendedNotice message={vehicles.error?.message} />;
  if (vehicles.isLoading) {
    return (
      <div className={styles.centered}>
        <Spinner label="Carregando veículos" />
      </div>
    );
  }

  const online = list.filter((v) => v.device?.status === 'ONLINE').length;

  return (
    <div className={styles.mapScreen} style={{ '--map-inset': `${mapInset}px` } as CSSProperties}>
      <div className={styles.map}>
        <TrackerMap
          vehicles={list}
          selectedId={selected}
          onSelect={setSelected}
          geofences={fences.data}
          showControls={false}
        />
      </div>
      <BottomSheet
        label="Seus veículos"
        title="Seus veículos"
        aside={<span className={styles.muted}>{online} de {list.length} online</span>}
        onSettle={onSettle}
      >
        {list.length === 0 && (
          <div className={styles.section}>
            <p className={styles.muted}>Você ainda não tem veículos com rastreador.</p>
            <Button variant="primary" onClick={() => navigate('/meus-veiculos')}>
              Novo veículo
            </Button>
          </div>
        )}
        {list.map((vehicle) => {
          const position = vehicle.lastPosition;
          const acc = vehicle.state?.acc ?? position?.acc ?? null;
          return (
            <button
              key={vehicle.id}
              type="button"
              className={`${styles.vehicleCard} ${vehicle.id === selected ? styles.vehicleCardSelected : ''}`}
              onClick={() => navigate(`/veiculos/${vehicle.id}`)}
            >
              <span className={styles.vehicleTop}>
                <span className={styles.vehicleName}>{vehicle.name}</span>
                <Badge tone={statusTone(vehicle)} dot>
                  {vehicle.device ? formatDeviceStatus(vehicle.device.status) : 'Sem rastreador'}
                </Badge>
                <span className={styles.chevron}>
                  <ChevronIcon />
                </span>
              </span>
              <span className={styles.vehicleMeta}>
                {vehicle.plate && <span className={styles.plate}>{vehicle.plate}</span>}
                {vehicle.device ? (
                  <>
                    <span className={acc ? styles.good : undefined}>
                      Ignição {acc === null ? '—' : acc ? 'ligada' : 'desligada'}
                    </span>
                    {position && <span>{formatSpeed(position.speedKmh)}</span>}
                    <span>{position ? formatRelative(position.gpsTimestamp) : 'aguardando o primeiro sinal'}</span>
                  </>
                ) : (
                  <span>Acompanhe a entrega em Veículos</span>
                )}
              </span>
            </button>
          );
        })}
      </BottomSheet>
    </div>
  );
}

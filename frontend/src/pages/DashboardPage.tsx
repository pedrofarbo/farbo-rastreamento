import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { geofencesApi } from '@/api/resources';
import { Address } from '@/components/ui/Address';
import { SuspendedNotice, isSuspendedError } from '@/components/billing/SuspendedNotice';
import { FLEET_SIZE } from '@/components/map/fleet';
import { TrackerMap } from '@/components/map/TrackerMap';
import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { TelemetryBar } from '@/components/vehicle/TelemetryBar';
import { VehicleList } from '@/components/vehicle/VehicleList';
import { useVehicles } from '@/hooks/useVehicles';

import styles from './DashboardPage.module.css';

export function DashboardPage() {
  const { data: vehicles = [], isLoading, error } = useVehicles();
  const { data: geofences = [] } = useQuery({
    queryKey: ['geofences'],
    queryFn: geofencesApi.list,
    staleTime: 5 * 60_000,
  });

  const [selectedId, setSelectedId] = useState<string | null>(null);

  // Seleciona o primeiro veículo assim que a lista chega, para o painel não
  // abrir vazio. Numa frota, não: o mapa abre mostrando todos.
  useEffect(() => {
    if (!selectedId && vehicles.length > 0 && vehicles.length <= FLEET_SIZE) {
      setSelectedId(vehicles[0].id);
    }
  }, [vehicles, selectedId]);

  const selected = vehicles.find((vehicle) => vehicle.id === selectedId) ?? null;
  const online = vehicles.filter((vehicle) => vehicle.device?.status === 'ONLINE').length;

  if (isLoading) {
    return <Spinner label="Carregando veículos" />;
  }
  if (error) {
    if (isSuspendedError(error)) return <SuspendedNotice message={error.message} />;
    return <Spinner label="Falha ao carregar. Tentando novamente…" />;
  }

  return (
    <div className={styles.page}>
      <aside className={styles.sidebar}>
        <div className={styles.sidebarHeader}>
          <span className={styles.sidebarTitle}>Veículos</span>
          <span className={styles.counts}>
            {online} de {vehicles.length} online
          </span>
        </div>
        <VehicleList vehicles={vehicles} selectedId={selectedId} onSelect={setSelectedId} />
      </aside>

      <section className={styles.main}>
        <div className={styles.mapArea}>
          <TrackerMap
            vehicles={vehicles}
            selectedId={selectedId}
            onSelect={setSelectedId}
            geofences={geofences}
          />
        </div>

        {selected && (
          <>
            <div className={styles.detailBar}>
              <div>
                <div className={styles.detailName}>{selected.name}</div>
                <div className={styles.detailMeta}>
                  {selected.lastPosition ? (
                    <Address
                      lat={selected.lastPosition.latitude}
                      lon={selected.lastPosition.longitude}
                    />
                  ) : (
                    'sem posição conhecida'
                  )}
                  {selected.device ? ` · ${selected.device.protocol || 'protocolo pendente'}` : ''}
                </div>
              </div>

              <Link to={`/veiculos/${selected.id}`}>
                <Button variant="primary" size="small">
                  Abrir veículo
                </Button>
              </Link>
            </div>

            <TelemetryBar
              position={selected.lastPosition}
              state={selected.state}
              lastSeenAt={selected.device?.lastSeenAt}
            />
          </>
        )}
      </section>
    </div>
  );
}

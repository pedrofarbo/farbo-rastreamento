import { useCallback, useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom';

import { geofencesApi } from '@/api/resources';
import { ExpandIcon } from '@/app/icons';
import { Address } from '@/components/ui/Address';
import { SuspendedNotice, isSuspendedError } from '@/components/billing/SuspendedNotice';
import { FLEET_SIZE } from '@/components/map/fleet';
import { FleetMapFullscreen, enterBrowserFullscreen } from '@/components/map/FleetMapFullscreen';
import { TrackerMap } from '@/components/map/TrackerMap';
import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { TelemetryBar } from '@/components/vehicle/TelemetryBar';
import { VehicleList } from '@/components/vehicle/VehicleList';
import { VehicleSwitcher } from '@/components/vehicle/VehicleSwitcher';
import fieldStyles from '@/components/ui/Field.module.css';
import { useFleet } from '@/hooks/useFleet';
import { useVehicles } from '@/hooks/useVehicles';
import { NO_GROUP, inGroup, sortVehicles } from '@/services/vehicleFilters';

import styles from './DashboardPage.module.css';

export function DashboardPage() {
  const { data: all = [], isLoading, error } = useVehicles();
  // O grupo escolhido filtra a lista e o mapa.
  const { groups } = useFleet();
  const [group, setGroup] = useState('');
  const vehicles = useMemo(() => (group ? all.filter((v) => inGroup(v, group)) : all), [all, group]);
  const { data: geofences = [] } = useQuery({
    queryKey: ['geofences'],
    queryFn: geofencesApi.list,
    staleTime: 5 * 60_000,
  });

  const [selectedId, setSelectedId] = useState<string | null>(null);

  // Tela cheia no endereço (?mapa=tela-cheia): o "voltar" do celular fecha.
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const location = useLocation();
  const fullscreen = params.get('mapa') === 'tela-cheia';
  const openFullscreen = () => {
    enterBrowserFullscreen();
    navigate({ search: '?mapa=tela-cheia' }, { state: { fullscreenMap: true } });
  };
  const closeFullscreen = useCallback(() => {
    if ((location.state as { fullscreenMap?: boolean } | null)?.fullscreenMap) navigate(-1);
    else navigate({ search: '' }, { replace: true });
  }, [location.state, navigate]);

  // Seleciona o primeiro veículo assim que a lista chega, para o painel não
  // abrir vazio. Numa frota, não: o mapa abre mostrando todos.
  useEffect(() => {
    if (!selectedId && vehicles.length > 0 && vehicles.length <= FLEET_SIZE) {
      setSelectedId(vehicles[0].id);
    }
  }, [vehicles, selectedId]);

  const selected = vehicles.find((vehicle) => vehicle.id === selectedId) ?? null;
  // O anterior e o próximo seguem a ordem da lista ao lado.
  const ordered = useMemo(() => sortVehicles(vehicles), [vehicles]);
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
        {groups.length > 0 && (
          <div className={styles.groupFilter}>
            <select
              className={fieldStyles.select}
              aria-label="Grupo"
              value={group}
              onChange={(e) => {
                setGroup(e.target.value);
                setSelectedId(null);
              }}
            >
              <option value="">Todos os grupos</option>
              {groups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name} ({g.vehicles})
                </option>
              ))}
              <option value={NO_GROUP}>Sem grupo</option>
            </select>
          </div>
        )}
        <VehicleList vehicles={vehicles} selectedId={selectedId} onSelect={setSelectedId} />
      </aside>

      <section className={styles.main}>
        <div className={styles.mapArea}>
          {/* Na tela cheia, o mapa de baixo sai (a frota não é desenhada duas vezes). */}
          {!fullscreen && (
            <TrackerMap
              vehicles={vehicles}
              selectedId={selectedId}
              onSelect={setSelectedId}
              geofences={geofences}
            />
          )}
          <button
            type="button"
            className={styles.mapExpand}
            onClick={openFullscreen}
            aria-label="Ver o mapa em tela cheia"
            title="Tela cheia"
          >
            <ExpandIcon />
          </button>
        </div>

        {selected && (
          <>
            <div className={styles.detailBar}>
              <div className={styles.detailText}>
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

              <VehicleSwitcher vehicles={ordered} currentId={selected.id} onSelect={setSelectedId} search={false} />
              <Link to={`/veiculos/${selected.id}`} className={styles.detailAction}>
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

      {fullscreen && (
        <FleetMapFullscreen
          vehicles={ordered}
          selectedId={selectedId}
          onSelect={setSelectedId}
          geofences={geofences}
          title={(group === NO_GROUP ? 'Sem grupo' : groups.find((g) => g.id === group)?.name) || 'Frota'}
          onClose={closeFullscreen}
        />
      )}
    </div>
  );
}

import { useMemo, useState } from 'react';

import { Badge } from '@/components/ui/Badge';
import { Link } from 'react-router-dom';

import { EmptyState } from '@/components/ui/EmptyState';
import { useAuth } from '@/stores/AuthContext';
import { formatDeviceStatus, formatRelative, formatSpeed } from '@/services/format';
import type { DeviceStatus, VehicleView } from '@/types';

import styles from './VehicleList.module.css';

interface VehicleListProps {
  vehicles: VehicleView[];
  selectedId?: string | null;
  onSelect: (vehicleId: string) => void;
}

function statusTone(status: DeviceStatus | undefined | null) {
  switch (status) {
    case 'ONLINE':
      return 'success' as const;
    case 'STALE':
      return 'warning' as const;
    case 'OFFLINE':
      return 'neutral' as const;
    default:
      return 'neutral' as const;
  }
}

export function VehicleList({ vehicles, selectedId, onSelect }: VehicleListProps) {
  const { isCustomer } = useAuth();
  const [search, setSearch] = useState('');

  const filtered = useMemo(() => {
    const term = search.trim().toLowerCase();
    const matching = term
      ? vehicles.filter(
          (vehicle) =>
            vehicle.name.toLowerCase().includes(term) ||
            vehicle.plate?.toLowerCase().includes(term) ||
            vehicle.device?.imei.includes(term),
        )
      : vehicles;

    // Online primeiro: é o que o operador precisa ver sem rolar.
    const rank: Record<string, number> = { ONLINE: 0, STALE: 1, OFFLINE: 2 };
    return [...matching].sort((a, b) => {
      const byStatus =
        (rank[a.device?.status ?? 'OFFLINE'] ?? 3) - (rank[b.device?.status ?? 'OFFLINE'] ?? 3);
      return byStatus !== 0 ? byStatus : a.name.localeCompare(b.name);
    });
  }, [vehicles, search]);

  return (
    <div className={styles.list}>
      <div className={styles.search}>
        <input
          className={styles.searchInput}
          type="search"
          placeholder="Buscar por nome, placa ou IMEI"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          aria-label="Buscar veículo"
        />
      </div>

      <div className={styles.items}>
        {filtered.length === 0 ? (
          <EmptyState
            icon="🚗"
            title={vehicles.length === 0 ? 'Nenhum veículo cadastrado' : 'Nada encontrado'}
            description={
              vehicles.length === 0
                ? isCustomer
                  ? (
                      <>
                        Cadastre seu veículo em <Link to="/meus-veiculos">Meus veículos</Link>; ele
                        aparece aqui assim que o rastreador for instalado.
                      </>
                    )
                  : 'Cadastre um rastreador e vincule-o a um veículo para começar.'
                : 'Ajuste a busca para ver outros veículos.'
            }
          />
        ) : (
          filtered.map((vehicle) => {
            const position = vehicle.lastPosition;
            const status = vehicle.device?.status;
            const blocked = vehicle.state?.relayOn === true;

            return (
              <button
                key={vehicle.id}
                type="button"
                className={`${styles.item} ${vehicle.id === selectedId ? styles.selected : ''}`}
                onClick={() => onSelect(vehicle.id)}
                aria-current={vehicle.id === selectedId}
              >
                <div className={styles.itemHeader}>
                  <span className={styles.name}>{vehicle.name}</span>
                  <Badge tone={statusTone(status)} dot pulse={status === 'ONLINE'}>
                    {formatDeviceStatus(status)}
                  </Badge>
                </div>

                {vehicle.plate && <div className={styles.plate}>{vehicle.plate}</div>}
                {vehicle.shared && <div className={styles.shared}>Compartilhado por {vehicle.shared.ownerName}</div>}

                <div className={styles.metrics}>
                  <span className={`${styles.metric} ${styles.speed}`}>
                    {position ? formatSpeed(position.speedKmh) : '— km/h'}
                  </span>
                  <span className={styles.metric}>
                    {vehicle.state?.acc === null || vehicle.state?.acc === undefined
                      ? 'ACC —'
                      : vehicle.state.acc
                        ? 'ACC ligada'
                        : 'ACC desligada'}
                  </span>
                  {position?.gsmLevel !== null && position?.gsmLevel !== undefined && (
                    <span className={styles.metric}>4G {position.gsmLevel}/4</span>
                  )}
                </div>

                <div className={styles.footerRow}>
                  <span className={styles.lastSeen}>
                    {vehicle.device
                      ? `Comunicou ${formatRelative(vehicle.device.lastSeenAt)}`
                      : 'Sem rastreador vinculado'}
                  </span>
                  {blocked && <span className={styles.blocked}>Motor bloqueado</span>}
                </div>
              </button>
            );
          })
        )}
      </div>
    </div>
  );
}

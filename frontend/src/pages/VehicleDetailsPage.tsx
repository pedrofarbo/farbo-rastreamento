import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useParams, useSearchParams } from 'react-router-dom';

import { Address } from '@/components/ui/Address';

import { vehiclesApi } from '@/api/resources';
import { SuspendedNotice, isSuspendedError } from '@/components/billing/SuspendedNotice';
import { TrackerMap } from '@/components/map/TrackerMap';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { CommandHistory } from '@/components/vehicle/CommandHistory';
import { CommandPanel } from '@/components/vehicle/CommandPanel';
import { EventList } from '@/components/vehicle/EventList';
import { Playback } from '@/components/vehicle/Playback';
import { TelemetryBar } from '@/components/vehicle/TelemetryBar';
import { TheftPanel } from '@/components/vehicle/TheftPanel';
import { useVehicle } from '@/hooks/useVehicles';
import { useAuth } from '@/stores/AuthContext';
import {
  formatCoordinates,
  formatDateTime,
  formatDeviceStatus,
  formatHeading,
  hoursAgo,
  nowISO,
} from '@/services/format';
import type { Position } from '@/types';

import styles from './VehicleDetailsPage.module.css';

const RANGES = [
  { label: '1 h', hours: 1 },
  { label: '6 h', hours: 6 },
  { label: '12 h', hours: 12 },
  { label: '24 h', hours: 24 },
  { label: '7 d', hours: 24 * 7 },
  { label: '14 d', hours: 24 * 14 },
  { label: '30 d', hours: 24 * 30 },
] as const;

export function VehicleDetailsPage() {
  const { id } = useParams<{ id: string }>();
  const { data: vehicle, isLoading, error } = useVehicle(id);
  const { isCustomer } = useAuth();
  const backTo = isCustomer ? '/meus-veiculos' : '/dashboard';
  // Aberto pelo alerta de bateria desconectada ou de movimento sem ignição.
  const [params] = useSearchParams();

  const [rangeHours, setRangeHours] = useState<number | null>(null);
  const [frame, setFrame] = useState<Position | null>(null);
  // Veículo de outra pessoa, compartilhado com quem está vendo: só a posição
  // ao vivo (histórico, eventos e comandos continuam só do dono).
  const shared = vehicle?.shared ?? null;
  const owned = Boolean(vehicle && !shared);

  // O período só é recalculado quando o usuário troca a faixa; sem isso cada
  // render geraria um "from" novo e refaria a consulta.
  const range = useMemo(
    () => (rangeHours === null ? null : { from: hoursAgo(rangeHours), to: nowISO() }),
    [rangeHours],
  );

  const history = useQuery({
    queryKey: ['history', id, rangeHours],
    queryFn: () => vehiclesApi.positions(id as string, { from: range!.from, to: range!.to }),
    enabled: Boolean(id && range && owned),
  });

  const events = useQuery({
    queryKey: ['events', id],
    queryFn: () => vehiclesApi.events(id as string, { limit: 100 }),
    enabled: Boolean(id && owned),
    refetchInterval: 60_000,
  });

  const commands = useQuery({
    queryKey: ['commands', id],
    queryFn: () => vehiclesApi.commands(id as string),
    enabled: Boolean(id && owned),
  });

  if (isLoading) return <Spinner label="Carregando veículo" />;
  if (isSuspendedError(error)) return <SuspendedNotice message={(error as Error).message} />;
  if (!vehicle) {
    return (
      <EmptyState
        title="Veículo não encontrado"
        description={<Link to={backTo}>Voltar</Link>}
      />
    );
  }

  const track = history.data?.positions ?? [];
  const device = vehicle.device;

  return (
    <div className={styles.page}>
      <div className={styles.mapColumn}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>{vehicle.name}</h1>
            <div className={styles.subtitle}>
              {[vehicle.plate, vehicle.brand, vehicle.model, vehicle.year]
                .filter(Boolean)
                .join(' · ') || 'sem dados do veículo'}
            </div>
          </div>

          <div style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
            <Badge
              tone={
                device?.status === 'ONLINE'
                  ? 'success'
                  : device?.status === 'STALE'
                    ? 'warning'
                    : 'neutral'
              }
              dot
              pulse={device?.status === 'ONLINE'}
            >
              {formatDeviceStatus(device?.status)}
            </Badge>
            <Link to={backTo}>
              <Button size="small" variant="ghost">
                Voltar
              </Button>
            </Link>
          </div>
        </header>

        {shared ? (
          <p className={styles.sharedNote}>
            Compartilhado por <strong>{shared.ownerName}</strong>: você acompanha a posição ao vivo
            {shared.canBlock ? ' e pode bloquear o motor numa emergência' : ''}.
          </p>
        ) : (
          <div className={styles.rangeBar}>
            <span className={styles.rangeLabel}>Histórico:</span>
            <button
              type="button"
              className={`${styles.rangeButton} ${rangeHours === null ? styles.rangeActive : ''}`}
              onClick={() => {
                setRangeHours(null);
                setFrame(null);
              }}
            >
              Ao vivo
            </button>
            {/* Só os períodos que cabem no histórico guardado do veículo. */}
            {RANGES.filter((option) => option.hours <= (vehicle.historyDays || 30) * 24).map((option) => (
              <button
                key={option.hours}
                type="button"
                className={`${styles.rangeButton} ${rangeHours === option.hours ? styles.rangeActive : ''}`}
                onClick={() => setRangeHours(option.hours)}
              >
                {option.label}
              </button>
            ))}

            {rangeHours === null && vehicle.historyDays > 0 && (
              <span className={styles.sampleNote}>Histórico guardado por {vehicle.historyDays} dias</span>
            )}
            {history.data && rangeHours !== null && (
              <span className={styles.sampleNote}>
                {history.data.returned} de {history.data.total} pontos
                {history.data.sampled ? ` (1 a cada ${history.data.sampleStep})` : ''}
                {history.data.simplified ? ' · traçado simplificado' : ''}
              </span>
            )}
          </div>
        )}

        <div className={styles.mapArea}>
          <TrackerMap
            vehicles={[vehicle]}
            selectedId={vehicle.id}
            track={rangeHours === null ? undefined : track}
            highlight={frame}
          />
        </div>

        {rangeHours !== null && track.length > 0 && (
          <Playback positions={track} onFrame={setFrame} />
        )}

        <TelemetryBar
          position={vehicle.lastPosition}
          state={vehicle.state}
          lastSeenAt={device?.lastSeenAt}
        />
      </div>

      <aside className={styles.sidebar}>
        <TheftPanel
          vehicle={vehicle}
          prompt={params.get('roubo') === '1'}
          reportUrl={owned ? `/relatorio-roubo/${vehicle.id}` : undefined}
        />

        <Card title="Comandos">
          <CommandPanel vehicle={vehicle} />
        </Card>

        <Card title="Posição atual">
          {vehicle.lastPosition ? (
            <div className={styles.infoGrid}>
              <span className={styles.addressLine}>
                <Address
                  lat={vehicle.lastPosition.latitude}
                  lon={vehicle.lastPosition.longitude}
                />
              </span>
              <Info
                label="Coordenadas"
                value={formatCoordinates(
                  vehicle.lastPosition.latitude,
                  vehicle.lastPosition.longitude,
                )}
                mono
              />
              <Info label="Direção" value={formatHeading(vehicle.lastPosition.heading)} />
              <Info
                label="Data do GPS"
                value={formatDateTime(vehicle.lastPosition.gpsTimestamp)}
              />
              <Info
                label="Recebido em"
                value={formatDateTime(vehicle.lastPosition.receivedAt)}
              />
              <Info
                label="Satélites"
                value={vehicle.lastPosition.satellites?.toString() ?? '—'}
              />
              <Info
                label="Altitude"
                value={
                  vehicle.lastPosition.altitude !== null
                    ? `${vehicle.lastPosition.altitude.toFixed(0)} m`
                    : '—'
                }
              />
            </div>
          ) : (
            <EmptyState icon="📍" title="Sem posição registrada" />
          )}
        </Card>

        {device && !shared && (
          <Card title="Rastreador" subtitle={device.model || device.manufacturer || undefined}>
            <div className={styles.infoGrid}>
              <Info label="IMEI" value={device.imei} mono />
              <Info label="Protocolo" value={device.protocol || 'ainda não detectado'} mono />
              <Info label="Firmware" value={device.firmware || '—'} />
              {!isCustomer && <Info label="Linha" value={device.phoneNumber || '—'} />}
              {!isCustomer && <Info label="ICCID do chip" value={device.iccid || '—'} mono />}
              <Info label="Última comunicação" value={formatDateTime(device.lastSeenAt)} />
              <Info label="Conexão" value={vehicle.connected ? 'aberta' : 'fechada'} />
            </div>
          </Card>
        )}

        {!shared && (
          <>
            <Card title="Eventos" flush>
              {events.isLoading ? (
                <Spinner inline />
              ) : (
                <EventList events={events.data ?? []} />
              )}
            </Card>

            <Card title="Histórico de comandos" flush>
              {commands.isLoading ? (
                <Spinner inline />
              ) : (
                <CommandHistory commands={commands.data ?? []} />
              )}
            </Card>
          </>
        )}
      </aside>
    </div>
  );
}

function Info({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className={styles.infoItem}>
      <span className={styles.infoLabel}>{label}</span>
      <span className={`${styles.infoValue} ${mono ? styles.mono : ''}`}>{value}</span>
    </div>
  );
}

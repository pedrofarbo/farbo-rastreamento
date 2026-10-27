import { useQuery } from '@tanstack/react-query';

import { eventsApi, vehiclesApi } from '@/api/resources';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { eventSeverity, formatDateTime, formatEvent, formatSpeed } from '@/services/format';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

import styles from './Page.module.css';

const TONE_COLORS: Record<string, string> = {
  danger: 'var(--danger)',
  warning: 'var(--warning)',
  success: 'var(--success)',
  neutral: 'var(--neutral)',
};

export function EventsPage() {
  const events = useQuery({
    queryKey: ['events', 'recent'],
    queryFn: () => eventsApi.recent(200),
    refetchInterval: 30_000,
  });

  const paged = usePagedList(events.data ?? [], 'events');

  const vehicles = useQuery({ queryKey: ['vehicles'], queryFn: vehiclesApi.list });

  const nameByDevice = new Map(
    (vehicles.data ?? [])
      .filter((vehicle) => vehicle.deviceId)
      .map((vehicle) => [vehicle.deviceId as string, vehicle.name]),
  );

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Eventos</h1>
            <p className={styles.description}>
              Tudo o que a frota registrou: ignição, excesso de velocidade, cercas, alarmes do
              aparelho e o ciclo dos comandos de motor.
            </p>
          </div>
        </header>

        <Card flush>
          {events.isLoading ? (
            <Spinner label="Carregando eventos" />
          ) : (events.data ?? []).length === 0 ? (
            <EmptyState icon="🗒" title="Nenhum evento registrado ainda" />
          ) : (
            <>
              <div className={styles.tableWrap}>
                <table className={styles.table}>
                  <thead>
                    <tr>
                      <th>Evento</th>
                      <th>Veículo</th>
                      <th>Velocidade</th>
                      <th>Local</th>
                      <th>Quando</th>
                    </tr>
                  </thead>
                  <tbody>
                    {paged.items.map((event) => (
                      <tr key={event.id}>
                        <td>
                          <span
                            style={{
                              display: 'inline-block',
                              width: 8,
                              height: 8,
                              borderRadius: '50%',
                              marginRight: 8,
                              background: TONE_COLORS[eventSeverity(event.type)],
                            }}
                            aria-hidden="true"
                          />
                          {formatEvent(event.type)}
                        </td>
                        <td>{nameByDevice.get(event.deviceId) ?? '—'}</td>
                        <td>{event.speedKmh !== null ? formatSpeed(event.speedKmh) : '—'}</td>
                        <td className={styles.mono}>
                          {event.latitude !== null && event.longitude !== null
                            ? `${event.latitude.toFixed(5)}, ${event.longitude.toFixed(5)}`
                            : '—'}
                        </td>
                        <td>{formatDateTime(event.timestamp)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <Pager {...paged.pager} />
            </>
          )}
        </Card>
      </div>
    </div>
  );
}

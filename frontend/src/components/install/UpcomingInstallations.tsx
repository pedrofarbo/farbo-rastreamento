import { useQuery } from '@tanstack/react-query';

import { meApi } from '@/api/resources';
import { KIND_LABEL, dayKey, dayLabel, hourLabel, isOpen } from '@/services/installations';

import styles from './UpcomingInstallations.module.css';

/**
 * A instalação ou a desinstalação marcada (o cliente vê quando o instalador
 * vai e quem é): dos veículos da conta, ou de um só. Sem nada em aberto,
 * nada aparece.
 */
export function UpcomingInstallations({ vehicleId }: { vehicleId?: string }) {
  const list = useQuery({ queryKey: ['me', 'installations'], queryFn: meApi.installations, staleTime: 60_000 });
  const open = (list.data ?? []).filter((a) => isOpen(a) && (!vehicleId || a.vehicleId === vehicleId));
  if (open.length === 0) return null;
  return (
    <section className={styles.box} aria-label="Agendamentos do instalador">
      {open.map((a) => (
        <div key={a.id} className={styles.item}>
          <span className={styles.icon} aria-hidden="true">
            🔧
          </span>
          <div className={styles.text}>
            <strong>
              {a.status === 'arrived' ? 'O instalador chegou' : `${KIND_LABEL[a.kind]} agendada`}
              {vehicleId ? '' : `: ${a.vehicleName}`}
            </strong>
            <span>
              {dayLabel(dayKey(a.scheduledFor))} às {hourLabel(a.scheduledFor)}
              {a.installerName ? ` · ${a.installerName}` : ''}
            </span>
            {a.address && <span className={styles.muted}>{a.address}</span>}
          </div>
        </div>
      ))}
    </section>
  );
}

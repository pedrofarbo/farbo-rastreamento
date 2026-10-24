import type { ReactNode } from 'react';

import { AuthImage } from '@/components/media/AuthImage';
import { Badge } from '@/components/ui/Badge';
import { KIND_LABEL, dayLabel, groupByDay, hourLabel, slotsOf, statusOf } from '@/services/installations';
import type { InstallationAppointment, InstallationSlot } from '@/types';

import styles from './Appointments.module.css';

/**
 * Os agendamentos de instalação e de desinstalação por dia (a central, a
 * associação): o horário, o tipo, o veículo, o cliente, o instalador, a
 * situação e, depois de concluído, o IMEI e as fotos.
 */
export function AppointmentList({
  list,
  actions,
  photoUrl,
  showCustomer = true,
  empty = 'Nenhuma instalação agendada no período.',
}: {
  list: InstallationAppointment[];
  actions?: (a: InstallationAppointment) => ReactNode;
  /** As fotos (com o login); sem isto, não mostra. */
  photoUrl?: (id: string, slot: InstallationSlot) => string;
  showCustomer?: boolean;
  empty?: string;
}) {
  if (list.length === 0) return <p className={styles.empty}>{empty}</p>;
  return (
    <div className={styles.days}>
      {groupByDay(list).map(({ day, items }) => (
        <section key={day} aria-label={dayLabel(day)}>
          <h3 className={styles.day}>{dayLabel(day)}</h3>
          {items.map((a) => {
            const st = statusOf(a);
            return (
              <article key={a.id} className={styles.item}>
                <span className={styles.hour}>{hourLabel(a.scheduledFor)}</span>
                <div className={styles.body}>
                  <span className={styles.name}>
                    {a.vehicleName}
                    {a.plate && <span className={styles.plate}>{a.plate}</span>}
                  </span>
                  {showCustomer && (
                    <span className={styles.muted}>
                      {a.customerName}
                      {a.customerPhone ? ` · ${a.customerPhone}` : ''}
                      {a.organizationName ? ` · ${a.organizationName}` : ''}
                    </span>
                  )}
                  {a.address && <span className={styles.muted}>📍 {a.address}</span>}
                  <span className={styles.muted}>
                    {a.installerName ? `Instalador: ${a.installerName}` : 'Sem instalador definido'} · {a.durationMin} min
                  </span>
                  {a.notes && <span className={styles.muted}>📝 {a.notes}</span>}
                  <span className={styles.badges}>
                    <Badge tone={a.kind === 'uninstall' ? 'warning' : 'accent'}>{KIND_LABEL[a.kind]}</Badge>
                    <Badge tone={st.tone}>{st.label}</Badge>
                  </span>
                  {a.status === 'done' ? (
                    <span className={styles.muted}>
                      {a.kind === 'uninstall' ? 'Retirado o' : 'Instalado o'} IMEI {a.imei || '—'}
                      {a.installerNote ? ` · ${a.installerNote}` : ''}
                    </span>
                  ) : (
                    a.kind === 'uninstall' &&
                    a.imei && <span className={styles.muted}>Rastreador a retirar: IMEI {a.imei}</span>
                  )}
                  {photoUrl && a.photos.length > 0 && (
                    <div className={styles.photos}>
                      {slotsOf(a.kind).filter((s) => a.photos.includes(s.slot)).map((s) => (
                        <AuthImage key={s.slot} src={photoUrl(a.id, s.slot)} alt={s.label} className={styles.thumb} />
                      ))}
                    </div>
                  )}
                </div>
                {actions && <div className={styles.actions}>{actions(a)}</div>}
              </article>
            );
          })}
        </section>
      ))}
    </div>
  );
}

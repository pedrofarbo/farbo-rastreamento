import { useCallback, useEffect, useState } from 'react';
import type { FormEvent } from 'react';

import { ApiError } from '@/api/client';
import { publicApi } from '@/api/resources';
import type { InstallationAppointment, InstallationKind, PartnerQueue, PartnerVehicle } from '@/types';

import styles from '../Partner.module.css';
import base from '../PreLaunch.module.css';

/** O que está sendo agendado ou remarcado (um por vez). */
type Editing =
  | { mode: 'new'; kind: InstallationKind; vehicle: PartnerVehicle }
  | { mode: 'reschedule'; appointment: InstallationAppointment };

const KIND_LABEL: Record<InstallationKind, string> = { install: 'Instalação', uninstall: 'Retirada do rastreador' };

const WHEN = new Intl.DateTimeFormat('pt-BR', {
  weekday: 'short', day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit', timeZone: 'America/Sao_Paulo',
});

/** "qua., 05/11 às 14:00": o dia e a hora do agendamento, no horário de Brasília. */
export function whenLabel(value: string): string {
  const parts = Object.fromEntries(WHEN.formatToParts(new Date(value)).map((p) => [p.type, p.value]));
  return `${parts.weekday}, ${parts.day}/${parts.month} às ${parts.hour}:${parts.minute}`;
}

/** "2026-11-05T14:00" (o campo de dia e hora) a partir da data. */
function localInput(value: string | Date): string {
  const date = new Date(value);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** O telefone em link para ligar (só os dígitos, com o 55). */
function phoneHref(phone: string): string {
  const digits = phone.replace(/\D/g, '');
  return `tel:+${digits.startsWith('55') ? digits : `55${digits}`}`;
}

/**
 * As instalações dos clientes do representante (com a gestão ligada): os
 * agendamentos, com o link do instalador (o teste do sinal, as fotos e a
 * conclusão), e os veículos esperando a instalação e a retirada do rastreador
 * do comodato. O cliente recebe o aviso no app; a central vê tudo na agenda.
 */
export function InstallationsSection({ token }: { token: string }) {
  const [queue, setQueue] = useState<PartnerQueue | null>(null);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<Editing | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(
    () =>
      publicApi
        .partnerInstallations(token)
        .then((q) => {
          setQueue(q);
          setError('');
        })
        .catch(() => setError('Não deu para carregar as instalações agora. Tente de novo em instantes.')),
    [token],
  );
  useEffect(() => {
    void load();
  }, [load]);

  const act = async (run: () => Promise<unknown>) => {
    setBusy(true);
    setError('');
    try {
      await run();
      setEditing(null);
      await load();
    } catch (err) {
      setError(err instanceof ApiError || err instanceof Error ? err.message : 'Não deu agora. Tente de novo.');
    } finally {
      setBusy(false);
    }
  };

  const open = queue?.appointments.filter((a) => a.status === 'scheduled' || a.status === 'arrived') ?? [];
  const done = queue?.appointments.filter((a) => a.status === 'done') ?? [];

  const form = (key: string) =>
    editing &&
    ((editing.mode === 'new' && `${editing.kind}-${editing.vehicle.vehicleId}` === key) ||
      (editing.mode === 'reschedule' && editing.appointment.id === key)) ? (
      <ScheduleForm
        initial={editing.mode === 'reschedule' ? editing.appointment : undefined}
        busy={busy}
        onCancel={() => setEditing(null)}
        onSubmit={(scheduledFor, notes) =>
          void act(() =>
            editing.mode === 'new'
              ? publicApi.partnerSchedule(token, { kind: editing.kind, vehicleId: editing.vehicle.vehicleId, scheduledFor, notes })
              : publicApi.partnerReschedule(token, editing.appointment.id, { scheduledFor, notes }),
          )
        }
      />
    ) : null;

  const waiting = (vehicles: PartnerVehicle[], kind: InstallationKind, title: string, empty: string) => (
    <div className={styles.installGroup}>
      <h3 className={styles.subheading}>{title}</h3>
      {vehicles.length === 0 ? (
        <p className={styles.qrHint}>{empty}</p>
      ) : (
        <ul className={styles.installList}>
          {vehicles.map((v) => (
            <li key={v.vehicleId} className={styles.installItem}>
              <Vehicle vehicle={v} />
              {kind === 'uninstall' && v.imei && <span className={styles.muted}>Rastreador {v.imei}</span>}
              {form(`${kind}-${v.vehicleId}`) ?? (
                <div className={styles.installActions}>
                  <button
                    type="button"
                    className={`${styles.installButton} ${styles.installPrimary}`}
                    onClick={() => setEditing({ mode: 'new', kind, vehicle: v })}
                  >
                    {kind === 'install' ? 'Agendar a instalação' : 'Agendar a retirada'}
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );

  return (
    <section className={styles.qrCard} aria-label="Instalações dos seus clientes">
      <h2 className={styles.heading}>Instalações dos seus clientes</h2>
      <p className={styles.qrHint}>
        Você agenda e faz a instalação e a retirada do rastreador. No dia, abra o link do instalador: ele testa o sinal,
        guarda as fotos e conclui. O cliente recebe os avisos no app.
      </p>
      {error && (
        <p className={base.error} role="alert">
          {error}
        </p>
      )}
      {!queue ? (
        !error && (
          <p className={styles.qrHint} role="status">
            Carregando…
          </p>
        )
      ) : (
        <>
          <div className={styles.installGroup}>
            <h3 className={styles.subheading}>Agendadas</h3>
            {open.length === 0 ? (
              <p className={styles.qrHint}>Nada agendado agora.</p>
            ) : (
              <ul className={styles.installList}>
                {open.map((a) => (
                  <li key={a.id} className={styles.installItem}>
                    <span className={styles.installWhen}>
                      {KIND_LABEL[a.kind]} · {whenLabel(a.scheduledFor)}
                    </span>
                    <Vehicle
                      vehicle={{
                        vehicleId: a.vehicleId, vehicleName: a.vehicleName, plate: a.plate, vehicleDetails: a.vehicleDetails,
                        customerName: a.customerName, customerPhone: a.customerPhone, address: a.address, imei: a.imei, since: a.createdAt,
                        blocking: a.blocking && a.kind === 'install',
                      }}
                    />
                    {a.notes && <span className={styles.muted}>Obs.: {a.notes}</span>}
                    {form(a.id) ?? (
                      <div className={styles.installActions}>
                        {a.url && (
                          <a
                            className={`${styles.installButton} ${styles.installPrimary}`}
                            href={a.url}
                            target="_blank"
                            rel="noopener noreferrer"
                          >
                            Abrir o link do instalador
                          </a>
                        )}
                        {a.status === 'scheduled' && (
                          <button
                            type="button"
                            className={styles.installButton}
                            onClick={() => setEditing({ mode: 'reschedule', appointment: a })}
                          >
                            Remarcar
                          </button>
                        )}
                        <button
                          type="button"
                          className={styles.installButton}
                          disabled={busy}
                          onClick={() => void act(() => publicApi.partnerCancel(token, a.id))}
                        >
                          Cancelar
                        </button>
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </div>
          {waiting(queue.awaitingInstall, 'install', 'Esperando a instalação', 'Nenhum cliente esperando a instalação.')}
          {waiting(
            queue.awaitingUninstall,
            'uninstall',
            'Esperando a retirada (comodato encerrado)',
            'Nenhum rastreador para retirar.',
          )}
          {done.length > 0 && (
            <div className={styles.installGroup}>
              <h3 className={styles.subheading}>Concluídas (30 dias)</h3>
              <ul className={styles.installList}>
                {done.map((a) => (
                  <li key={a.id} className={styles.installItem}>
                    <span className={styles.muted}>
                      {KIND_LABEL[a.kind]} · {whenLabel(a.completedAt ?? a.scheduledFor)}
                    </span>
                    <strong>
                      {a.vehicleName} · {a.customerName}
                    </strong>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </section>
  );
}

/** O veículo e o cliente: o nome, o telefone (para ligar) e o endereço. */
function Vehicle({ vehicle: v }: { vehicle: PartnerVehicle }) {
  return (
    <>
      <strong>
        {v.vehicleName}
        {v.plate && ` · ${v.plate}`}
      </strong>
      {v.vehicleDetails && <span className={styles.muted}>{v.vehicleDetails}</span>}
      {v.blocking && (
        <span>
          <strong>Com bloqueio:</strong> instalar também o relé que bloqueia o motor pelo app.
        </span>
      )}
      <span>
        {v.customerName}
        {v.customerPhone && (
          <>
            {' · '}
            <a href={phoneHref(v.customerPhone)}>{v.customerPhone}</a>
          </>
        )}
      </span>
      <span className={styles.muted}>{v.address || 'sem endereço: peça à Farbo para cadastrar'}</span>
    </>
  );
}

/** O dia e a hora e a observação de um agendamento. */
function ScheduleForm({
  initial,
  busy,
  onCancel,
  onSubmit,
}: {
  initial?: InstallationAppointment;
  busy: boolean;
  onCancel: () => void;
  onSubmit: (scheduledFor: string, notes: string) => void;
}) {
  const [when, setWhen] = useState(initial ? localInput(initial.scheduledFor) : '');
  const [notes, setNotes] = useState(initial?.notes ?? '');
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!when) return;
    onSubmit(new Date(when).toISOString(), notes.trim());
  };
  return (
    <form className={styles.installForm} onSubmit={submit}>
      <label className={base.field}>
        <span>Dia e hora</span>
        <input type="datetime-local" value={when} onChange={(e) => setWhen(e.target.value)} required />
      </label>
      <label className={base.field}>
        <span>Observação (opcional)</span>
        <input type="text" maxLength={300} value={notes} onChange={(e) => setNotes(e.target.value)} placeholder="Ex.: portão azul" />
      </label>
      <div className={styles.installActions}>
        <button type="submit" className={`${styles.installButton} ${styles.installPrimary}`} disabled={!when || busy}>
          {busy ? 'Salvando…' : initial ? 'Salvar o novo horário' : 'Agendar'}
        </button>
        <button type="button" className={styles.installButton} onClick={onCancel}>
          Voltar
        </button>
      </div>
    </form>
  );
}

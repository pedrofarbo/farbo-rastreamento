import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { installationsApi, installersApi } from '@/api/resources';
import { AppointmentList } from '@/components/install/AppointmentList';
import styles from '@/components/install/Appointments.module.css';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { Field, SelectField, TextField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { whatsappLink } from '@/config/contact';
import { formatDateOnly, formatDateTime, todayISO } from '@/services/format';
import { KIND_LABEL, dayKey, defaultSlot, installerMessage, isOpen, localInput } from '@/services/installations';
import type { InstallationAppointment, InstallationKind, Installer } from '@/types';

import pageStyles from '../Page.module.css';

const key = ['installations'] as const;

/** O veículo a agendar (da fila de instalação ou da de desinstalação). */
interface Target {
  kind: InstallationKind;
  vehicleId: string;
  vehicleName: string;
  address: string;
}

/** A data (AAAA-MM-DD) somada de dias. */
function addDays(day: string, n: number): string {
  const [y, m, d] = day.split('-').map(Number);
  return dayKey(new Date(y, m - 1, d + n));
}

/** O link do instalador no WhatsApp: com o número dele (da lista de prestadores) ou para escolher a conversa. */
function installerWhatsapp(a: InstallationAppointment, installers: Installer[]): string {
  const text = installerMessage(a);
  const phone = installers.find((i) => i.id === a.installerId)?.whatsapp ?? '';
  return phone ? whatsappLink(phone, text) : `https://wa.me/?text=${encodeURIComponent(text)}`;
}

/**
 * A agenda de instalação e de desinstalação (central): os veículos à espera
 * do rastreador e os que a associação mandou retirar (ou de associado
 * desligado), o agendamento com o instalador (o link do passo a passo vai
 * pelo WhatsApp) e a semana, com quem chegou, quem concluiu e as fotos.
 */
export function InstallationsAgendaPage() {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [from, setFrom] = useState(todayISO());
  const to = addDays(from, 6);
  const [scheduling, setScheduling] = useState<{ target: Target | null; appointment: InstallationAppointment | null } | null>(null);
  const agenda = useQuery({ queryKey: [...key, from], queryFn: () => installationsApi.list(from, to), refetchInterval: 60_000 });
  const awaiting = useQuery({ queryKey: [...key, 'awaiting'], queryFn: installationsApi.awaiting });
  const removals = useQuery({ queryKey: [...key, 'awaiting-uninstall'], queryFn: installationsApi.awaitingUninstall });
  const installers = useQuery({ queryKey: ['installers'], queryFn: installersApi.list });
  const close = useMutation({
    mutationFn: ({ id, status }: { id: string; status: 'canceled' | 'no_show' }) => installationsApi.close(id, status),
    onSuccess: (a) => {
      void queryClient.invalidateQueries({ queryKey: key });
      notify({ tone: 'success', title: a.status === 'canceled' ? `${KIND_LABEL[a.kind]} cancelada` : 'Marcada como cliente ausente' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para alterar', description: err.message }),
  });
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      notify({ tone: 'success', title: 'Link copiado' });
    } catch {
      /* sem permissão: a pessoa copia na mão */
    }
  };
  const waiting = awaiting.data ?? [];
  const pending = waiting.filter((w) => !w.scheduledFor);
  const toRemove = removals.data ?? [];
  const removalsPending = toRemove.filter((w) => !w.scheduledFor);

  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>Agenda de instalação e desinstalação</h1>
            <p className={pageStyles.description}>
              Marque o dia com o cliente e mande o link ao instalador. Na instalação, ele confere o IMEI, testa o sinal ao
              vivo, tira as fotos e conclui — o rastreador fica ligado ao veículo na hora. Na desinstalação, tira a foto do
              rastreador retirado e conclui — o veículo fica sem rastreador e a taxa entra na fatura da associação. O
              cliente recebe o aviso no app.
            </p>
          </div>
        </header>

        <Card
          title="Aguardando instalação"
          subtitle={
            pending.length > 0
              ? `${pending.length} veículo(s) sem data marcada; primeiro os que a Farbo instala (comodato, instalação contratada, associados).`
              : 'Os veículos sem rastreador.'
          }
          flush
        >
          {awaiting.isLoading ? (
            <Spinner label="Carregando" />
          ) : waiting.length === 0 ? (
            <p className={styles.empty}>Nenhum veículo esperando a instalação.</p>
          ) : (
            <div className={pageStyles.tableWrap}>
              <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
                <thead>
                  <tr>
                    <th>Veículo</th>
                    <th>Cliente</th>
                    <th>Endereço</th>
                    <th>Desde</th>
                    <th aria-label="Ações" />
                  </tr>
                </thead>
                <tbody>
                  {waiting.map((w) => (
                    <tr key={w.vehicleId}>
                      <td>
                        <strong>{w.vehicleName}</strong>
                        {w.plate && <span className={styles.plate}>{w.plate}</span>}
                        <div>
                          {w.farboInstall ? (
                            <Badge tone="accent">Instalação Farbo</Badge>
                          ) : (
                            <span className={styles.muted}>cliente instala por conta própria</span>
                          )}
                        </div>
                      </td>
                      <td data-label="Cliente">
                        {w.customerName}
                        <div className={styles.muted}>
                          {[w.customerPhone, w.organizationName].filter(Boolean).join(' · ')}
                        </div>
                      </td>
                      <td data-label="Endereço">{w.address || <span className={styles.muted}>sem endereço</span>}</td>
                      <td data-label="Desde">{formatDateOnly(w.createdAt.slice(0, 10))}</td>
                      <td>
                        {w.scheduledFor ? (
                          <Badge tone="accent">Agendada {formatDateTime(w.scheduledFor).slice(0, 17)}</Badge>
                        ) : (
                          <Button
                            size="small"
                            variant="primary"
                            onClick={() =>
                              setScheduling({
                                target: { kind: 'install', vehicleId: w.vehicleId, vehicleName: w.vehicleName, address: w.address },
                                appointment: null,
                              })
                            }
                          >
                            Agendar
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>

        <Card
          title="Aguardando desinstalação"
          subtitle={
            removalsPending.length > 0
              ? `${removalsPending.length} veículo(s) sem data marcada.`
              : 'Os veículos que a associação mandou retirar e os de associado desligado.'
          }
          flush
        >
          {removals.isLoading ? (
            <Spinner label="Carregando" />
          ) : toRemove.length === 0 ? (
            <p className={styles.empty}>Nenhum rastreador para retirar.</p>
          ) : (
            <div className={pageStyles.tableWrap}>
              <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
                <thead>
                  <tr>
                    <th>Veículo</th>
                    <th>Associado</th>
                    <th>Motivo</th>
                    <th>Endereço</th>
                    <th aria-label="Ações" />
                  </tr>
                </thead>
                <tbody>
                  {toRemove.map((w) => (
                    <tr key={w.vehicleId}>
                      <td>
                        <strong>{w.vehicleName}</strong>
                        {w.plate && <span className={styles.plate}>{w.plate}</span>}
                        <div className={styles.muted}>IMEI {w.imei}</div>
                      </td>
                      <td data-label="Associado">
                        {w.customerName}
                        <div className={styles.muted}>{[w.customerPhone, w.organizationName].filter(Boolean).join(' · ')}</div>
                      </td>
                      <td data-label="Motivo">
                        {w.reason === 'inactive' ? (
                          <Badge tone="neutral">Associado desligado</Badge>
                        ) : (
                          <>
                            <Badge tone="warning">Pedido da associação</Badge>
                            <div className={styles.muted}>
                              {w.requestedAt ? formatDateTime(w.requestedAt).slice(0, 10) : ''}
                              {w.note ? ` · ${w.note}` : ''}
                            </div>
                          </>
                        )}
                      </td>
                      <td data-label="Endereço">{w.address || <span className={styles.muted}>sem endereço</span>}</td>
                      <td>
                        {w.scheduledFor ? (
                          <Badge tone="accent">Agendada {formatDateTime(w.scheduledFor).slice(0, 17)}</Badge>
                        ) : (
                          <Button
                            size="small"
                            variant="primary"
                            onClick={() =>
                              setScheduling({
                                target: { kind: 'uninstall', vehicleId: w.vehicleId, vehicleName: w.vehicleName, address: w.address },
                                appointment: null,
                              })
                            }
                          >
                            Agendar retirada
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>

        <Card
          title="Agenda"
          actions={
            <div className={styles.toolbar}>
              <Button size="small" variant="ghost" aria-label="Semana anterior" onClick={() => setFrom(addDays(from, -7))}>
                ‹
              </Button>
              <span className={styles.period}>
                {formatDateOnly(from).slice(0, 5)} a {formatDateOnly(to).slice(0, 5)}
              </span>
              <Button size="small" variant="ghost" aria-label="Próxima semana" onClick={() => setFrom(addDays(from, 7))}>
                ›
              </Button>
              {from !== todayISO() && (
                <Button size="small" variant="secondary" onClick={() => setFrom(todayISO())}>
                  Hoje
                </Button>
              )}
            </div>
          }
          flush
        >
          {agenda.isLoading ? (
            <Spinner label="Carregando" />
          ) : (
            <AppointmentList
              list={agenda.data ?? []}
              photoUrl={installationsApi.photoUrl}
              actions={(a) =>
                isOpen(a) ? (
                  <>
                    <a
                      className={styles.waLink}
                      href={installerWhatsapp(a, installers.data ?? [])}
                      target="_blank"
                      rel="noreferrer noopener"
                    >
                      WhatsApp ao instalador
                    </a>
                    {a.url && (
                      <Button size="small" variant="ghost" onClick={() => void copy(a.url ?? '')}>
                        Copiar link
                      </Button>
                    )}
                    <Button size="small" variant="secondary" onClick={() => setScheduling({ target: null, appointment: a })}>
                      Remarcar
                    </Button>
                    <Button size="small" variant="ghost" onClick={() => close.mutate({ id: a.id, status: 'no_show' })}>
                      Ausente
                    </Button>
                    <Button size="small" variant="ghost" onClick={() => close.mutate({ id: a.id, status: 'canceled' })}>
                      Cancelar
                    </Button>
                  </>
                ) : null
              }
            />
          )}
        </Card>
      </div>
      {scheduling && (
        <ScheduleModal
          target={scheduling.target}
          appointment={scheduling.appointment}
          installers={(installers.data ?? []).filter((i) => i.active)}
          onClose={() => setScheduling(null)}
        />
      )}
    </div>
  );
}

function ScheduleModal({
  target,
  appointment,
  installers,
  onClose,
}: {
  target: Target | null;
  appointment: InstallationAppointment | null;
  installers: Installer[];
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [installerId, setInstallerId] = useState(appointment?.installerId ?? '');
  const [installerName, setInstallerName] = useState(appointment && !appointment.installerId ? appointment.installerName : '');
  const [when, setWhen] = useState(appointment ? localInput(appointment.scheduledFor) : defaultSlot());
  const [duration, setDuration] = useState(String(appointment?.durationMin ?? 60));
  const [address, setAddress] = useState(appointment?.address ?? target?.address ?? '');
  const [notes, setNotes] = useState(appointment?.notes ?? '');
  const [error, setError] = useState('');
  const [saved, setSaved] = useState<InstallationAppointment | null>(null);
  const vehicleName = appointment?.vehicleName ?? target?.vehicleName ?? '';
  const kind: InstallationKind = appointment?.kind ?? target?.kind ?? 'install';
  const save = useMutation({
    mutationFn: () => {
      const picked = installers.find((i) => i.id === installerId);
      const input = {
        kind,
        vehicleId: appointment?.vehicleId ?? target?.vehicleId ?? '',
        installerId: picked ? picked.id : null,
        installerName: picked ? picked.name : installerName.trim(),
        scheduledFor: new Date(when).toISOString(),
        durationMin: Number(duration) || 60,
        address,
        notes,
      };
      return appointment ? installationsApi.reschedule(appointment.id, input) : installationsApi.create(input);
    },
    onSuccess: (a) => {
      void queryClient.invalidateQueries({ queryKey: key });
      notify({
        tone: 'success',
        title: `${KIND_LABEL[kind]} ${appointment ? 'remarcada' : 'agendada'}`,
        description: 'O cliente recebe o aviso no app.',
      });
      setSaved(a);
    },
    onError: (err: Error) => setError(err.message),
  });

  if (saved) {
    return (
      <Modal
        open
        title="Mande o link ao instalador"
        onClose={onClose}
        footer={
          <Button variant="primary" onClick={onClose}>
            Pronto
          </Button>
        }
      >
        <div className={pageStyles.form}>
          <p className={pageStyles.description}>
            {saved.vehicleName}, {formatDateTime(saved.scheduledFor).slice(0, 17)}. Pelo link, o instalador segue o passo a
            passo no celular, sem login. O link vale até 1 dia depois de concluído.
          </p>
          <div className={styles.link}>
            <span className={styles.url}>{saved.url}</span>
            <div className={styles.actions} style={{ justifyContent: 'flex-start' }}>
              <a className={styles.waLink} href={installerWhatsapp(saved, installers)} target="_blank" rel="noreferrer noopener">
                Enviar pelo WhatsApp
              </a>
              <Button
                size="small"
                variant="ghost"
                onClick={() => void navigator.clipboard?.writeText(saved.url ?? '').then(() => notify({ tone: 'success', title: 'Link copiado' }))}
              >
                Copiar
              </Button>
            </div>
          </div>
        </div>
      </Modal>
    );
  }

  return (
    <Modal
      open
      title={`${appointment ? 'Remarcar' : 'Agendar'} ${KIND_LABEL[kind].toLowerCase()}: ${vehicleName}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            {appointment ? 'Remarcar' : 'Agendar'}
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        {error && <div className={pageStyles.note}>{error}</div>}
        <SelectField label="Instalador" value={installerId} onChange={(e) => setInstallerId(e.target.value)}>
          <option value="">Outro (digitar o nome)</option>
          {installers.map((i) => (
            <option key={i.id} value={i.id}>
              {i.name}
              {i.city ? ` · ${i.city}` : ''}
            </option>
          ))}
        </SelectField>
        {!installerId && (
          <TextField label="Nome do instalador" maxLength={120} value={installerName} onChange={(e) => setInstallerName(e.target.value)} />
        )}
        <div className={pageStyles.formRow}>
          <TextField label="Dia e hora" type="datetime-local" value={when} onChange={(e) => setWhen(e.target.value)} />
          <SelectField label="Duração" value={duration} onChange={(e) => setDuration(e.target.value)}>
            {[30, 60, 90, 120, 180].map((m) => (
              <option key={m} value={m}>
                {m < 60 ? `${m} min` : `${m / 60} h`.replace('.5', ',5')}
              </option>
            ))}
          </SelectField>
        </div>
        <TextField label="Endereço" maxLength={300} value={address} onChange={(e) => setAddress(e.target.value)} />
        <Field label="Observações para o instalador">
          {(id) => (
            <textarea
              id={id}
              className={fieldStyles.textarea}
              maxLength={500}
              value={notes}
              placeholder="Portão, vaga, quem recebe…"
              onChange={(e) => setNotes(e.target.value)}
            />
          )}
        </Field>
      </div>
    </Modal>
  );
}

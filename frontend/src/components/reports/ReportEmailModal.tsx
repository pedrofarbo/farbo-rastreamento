import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { reportSchedulesApi } from '@/api/resources';
import type { ReportScheduleInput } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { SelectField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatWhen } from '@/services/reports';
import type { ReportFrequency, ReportSchedule, VehicleGroup } from '@/types';

import pageStyles from '@/pages/Page.module.css';
import styles from './ReportEmailModal.module.css';

export const reportSchedulesKey = ['me', 'report-schedules'] as const;

export const FREQUENCIES: Record<ReportFrequency, { label: string; when: string; first: string }> = {
  weekly: { label: 'Toda semana', when: 'às segundas, às 7h, com a semana anterior', first: 'a semana que passou' },
  monthly: { label: 'Todo mês', when: 'no dia 1º, às 7h, com o mês anterior', first: 'o mês que passou' },
};

/** Os e-mails a mais, digitados separados por vírgula, ponto e vírgula ou linha. */
export function parseEmails(text: string): string[] {
  return text
    .split(/[\s,;]+/)
    .map((e) => e.trim().toLowerCase())
    .filter(Boolean)
    .filter((e, i, all) => all.indexOf(e) === i);
}

/** O e-mail que parece errado, ou null. */
export function invalidEmail(emails: string[]): string | null {
  return emails.find((e) => !/^[^\s@<>",;]+@[^\s@<>",;]+\.[^\s@<>",;]+$/.test(e)) ?? null;
}

/** Uma linha do agendamento: "Toda semana · Toda a frota". */
export function scheduleLabel(s: Pick<ReportSchedule, 'frequency' | 'groupId'>, groups: VehicleGroup[]): string {
  const group = s.groupId ? (groups.find((g) => g.id === s.groupId)?.name ?? 'grupo') : 'Toda a frota';
  return `${FREQUENCIES[s.frequency].label} · ${group}`;
}

/**
 * O relatório da frota por e-mail: quem está logado agenda o resumo da
 * semana ou do mês, da frota toda ou de um grupo, para si e (titular e
 * Gestor) para mais alguns e-mails. A planilha completa vai anexa.
 */
export function ReportEmailModal({
  open,
  onClose,
  groups,
  restricted,
}: {
  open: boolean;
  onClose: () => void;
  /** Os grupos que a pessoa vê. */
  groups: VehicleGroup[];
  /** Restrita a grupos: escolhe um deles (não a frota toda). */
  restricted: boolean;
}) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const list = useQuery({ queryKey: reportSchedulesKey, queryFn: reportSchedulesApi.list, enabled: open });
  const [frequency, setFrequency] = useState<ReportFrequency>('weekly');
  const [group, setGroup] = useState('');
  const [extras, setExtras] = useState('');
  const [error, setError] = useState('');
  const refresh = () => queryClient.invalidateQueries({ queryKey: reportSchedulesKey });

  const create = useMutation({
    mutationFn: (input: ReportScheduleInput) => reportSchedulesApi.create(input),
    onSuccess: (created) => {
      refresh();
      setExtras('');
      setError('');
      notify({
        tone: 'success',
        title: 'Relatório agendado',
        description: `O primeiro chega em alguns minutos, com ${FREQUENCIES[created.frequency].first}.`,
      });
    },
    onError: (err: Error) => setError(err.message),
  });
  const remove = useMutation({
    mutationFn: (id: string) => reportSchedulesApi.remove(id),
    onSuccess: () => {
      refresh();
      notify({ tone: 'success', title: 'Relatório parado' });
    },
    onError: (err: Error) => setError(err.message),
  });

  const data = list.data;
  const schedules = data?.schedules ?? [];
  const full = data ? schedules.length >= data.maxSchedules : false;
  const chosenGroup = restricted ? group || groups[0]?.id || '' : group;

  const submit = () => {
    const emails = parseEmails(extras);
    const bad = invalidEmail(emails);
    if (bad) return setError(`E-mail inválido: ${bad}`);
    if (data && emails.length > data.maxExtraRecipients) {
      return setError(`Até ${data.maxExtraRecipients} e-mails a mais por relatório.`);
    }
    if (restricted && !chosenGroup) return setError('Escolha um dos seus grupos.');
    setError('');
    create.mutate({ frequency, groupId: chosenGroup || null, extraRecipients: emails });
  };

  return (
    <Modal
      open={open}
      title="Receber por e-mail"
      onClose={onClose}
      footer={
        <Button variant="ghost" onClick={onClose}>
          Fechar
        </Button>
      }
    >
      <div className={pageStyles.form}>
        <p className={pageStyles.description}>
          O resumo da frota chega no seu e-mail: os totais e os veículos que mais rodaram, com a planilha completa (e a
          dos excessos de velocidade) anexa.
        </p>

        {list.isLoading ? (
          <Spinner label="Carregando" />
        ) : schedules.length > 0 ? (
          <ul className={styles.list}>
            {schedules.map((s) => (
              <li key={s.id} className={styles.item}>
                <div className={styles.what}>
                  <strong>{scheduleLabel(s, groups)}</strong>
                  <span className={styles.muted}>
                    {FREQUENCIES[s.frequency].when}
                    {s.extraRecipients.length > 0 && ` · também para ${s.extraRecipients.join(', ')}`}
                  </span>
                  {s.lastSentAt && <span className={styles.muted}>Último envio: {formatWhen(s.lastSentAt)}</span>}
                </div>
                <Button size="small" variant="ghost" loading={remove.isPending && remove.variables === s.id} onClick={() => remove.mutate(s.id)}>
                  Parar
                </Button>
              </li>
            ))}
          </ul>
        ) : null}

        {error && (
          <div className={pageStyles.note} role="alert">
            {error}
          </div>
        )}

        {full ? (
          <p className={styles.muted}>Você já tem {data?.maxSchedules} relatórios agendados: pare um para agendar outro.</p>
        ) : (
          <section className={styles.add}>
            <h3 className={styles.heading}>Agendar um relatório</h3>
            <div className={styles.choices} role="radiogroup" aria-label="Frequência">
              {(Object.keys(FREQUENCIES) as ReportFrequency[]).map((f) => (
                <label key={f} className={`${styles.choice} ${frequency === f ? styles.choiceOn : ''}`}>
                  <input type="radio" name="report-frequency" checked={frequency === f} onChange={() => setFrequency(f)} />
                  <span>
                    <strong>{FREQUENCIES[f].label}</strong>
                    <span className={styles.muted}>{FREQUENCIES[f].when}</span>
                  </span>
                </label>
              ))}
            </div>
            {(groups.length > 0 || restricted) && (
              <SelectField label="Veículos" value={chosenGroup} onChange={(e) => setGroup(e.target.value)}>
                {!restricted && <option value="">Toda a frota</option>}
                {groups.map((g) => (
                  <option key={g.id} value={g.id}>
                    Grupo {g.name}
                  </option>
                ))}
              </SelectField>
            )}
            {data?.canAddRecipients && (
              <label className={styles.field}>
                <span>Também mandar para (opcional)</span>
                <textarea
                  className={styles.textarea}
                  rows={2}
                  value={extras}
                  placeholder="gerente@empresa.com.br, financeiro@empresa.com.br"
                  onChange={(e) => setExtras(e.target.value)}
                />
                <span className={styles.muted}>Até {data.maxExtraRecipients} e-mails, separados por vírgula. Eles recebem uma cópia, sem acesso ao painel.</span>
              </label>
            )}
            <div>
              <Button variant="primary" loading={create.isPending} onClick={submit}>
                Agendar
              </Button>
            </div>
            <p className={styles.muted}>O primeiro chega em alguns minutos, com {FREQUENCIES[frequency].first}.</p>
          </section>
        )}
      </div>
    </Modal>
  );
}

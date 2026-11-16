import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { orgApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateTime, formatRelative } from '@/services/format';
import { whatsappLink } from '@/services/occurrences';
import { useAuth } from '@/stores/AuthContext';
import type { TrackerHealthVehicle, TrackerIssue } from '@/types';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

import pageStyles from '../Page.module.css';
import styles from './OrgOccurrences.module.css';

export const trackerHealthKey = ['org', 'tracker-health'] as const;

/** Como a tela escreve cada problema, e o tom. */
export const TRACKER_ISSUES: Record<TrackerIssue, { label: string; tone: 'danger' | 'warning' | 'neutral' | 'accent'; hint: string }> = {
  removed: {
    label: 'Corte de energia e sem sinal',
    tone: 'danger',
    hint: 'Ficou sem a energia do veículo e parou de mandar sinal: pode ter sido desligado ou retirado.',
  },
  silent_long: { label: 'Sem sinal há mais de 3 dias', tone: 'danger', hint: 'Veículo parado há muito tempo, bateria desligada ou o rastreador com problema.' },
  silent: { label: 'Sem sinal há mais de 1 dia', tone: 'warning', hint: 'Pode ser o veículo parado em garagem fechada ou a bateria desligada.' },
  never: { label: 'Nunca mandou sinal', tone: 'warning', hint: 'Instalado, mas o rastreador ainda não conectou.' },
  power_cut: { label: 'Corte de energia na semana', tone: 'neutral', hint: 'A bateria do veículo foi desconectada e o sinal voltou.' },
};

/** A mensagem do WhatsApp ao associado sobre o rastreador. */
export function trackerMessage(v: Pick<TrackerHealthVehicle, 'associateName' | 'vehicleName' | 'plate' | 'issue' | 'lastSeenAt'>, orgName: string): string {
  const first = v.associateName.trim().split(/\s+/)[0] ?? '';
  const vehicle = v.plate ? `${v.vehicleName} (${v.plate})` : v.vehicleName;
  const since = v.lastSeenAt ? ` desde ${formatDateTime(v.lastSeenAt)}` : '';
  const what =
    v.issue === 'removed'
      ? `ficou sem a energia do veículo e parou de mandar sinal${since}`
      : v.issue === 'never'
        ? 'ainda não mandou sinal'
        : `está sem sinal${since}`;
  return (
    `Olá${first ? `, ${first}` : ''}! Aqui é da ${orgName}. O rastreador do seu ${vehicle} ${what}. ` +
    'Pode ligar o veículo e deixá-lo num lugar aberto por alguns minutos? Se continuar sem sinal, me avise para agendarmos uma visita.'
  );
}

/**
 * A saúde dos rastreadores: quantos estão mandando sinal e, do mais grave,
 * os que pararam, os que nunca conectaram e o corte de energia. Avisar o
 * associado no app (ou pelo WhatsApp); a rotina avisa sozinha uma vez por
 * dia, no máximo a cada 3 dias por veículo.
 */
export function OrgHealthPage() {
  const { canOperateOrg, isOrgBroker, user } = useAuth();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const health = useQuery({ queryKey: trackerHealthKey, queryFn: orgApi.trackerHealth, refetchInterval: 60_000 });
  const orgName = user?.organization?.name ?? 'associação';
  const notifyOne = useMutation({
    mutationFn: (vehicleId: string) => orgApi.notifyTrackerHealth(vehicleId),
    onSuccess: ({ delivered }) => {
      void queryClient.invalidateQueries({ queryKey: trackerHealthKey });
      notify(
        delivered > 0
          ? { tone: 'success', title: 'Associado avisado no app' }
          : { tone: 'warning', title: 'O aviso não chegou', description: 'O associado não tem as notificações do app ligadas: mande pelo WhatsApp.' },
      );
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para avisar', description: err.message }),
  });
  const auto = useMutation({
    mutationFn: (on: boolean) => orgApi.setTrackerHealthAutoNotice(on),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: trackerHealthKey }),
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para mudar', description: err.message }),
  });

  const h = health.data;
  const rows = h?.vehicles ?? [];
  const paged = usePagedList(rows, 'tracker-health');
  const tiles: { label: string; value: number; tone?: 'danger' | 'warning' }[] = h
    ? [
        { label: 'Instalados', value: h.installed },
        { label: 'Mandando sinal agora', value: h.online },
        { label: 'Corte de energia e sem sinal', value: h.removed, tone: h.removed ? 'danger' : undefined },
        { label: 'Sem sinal há mais de 3 dias', value: h.silentLong, tone: h.silentLong ? 'danger' : undefined },
        { label: 'Sem sinal há mais de 1 dia', value: h.silent, tone: h.silent ? 'warning' : undefined },
        { label: 'Nunca mandaram sinal', value: h.never, tone: h.never ? 'warning' : undefined },
        { label: 'Corte de energia na semana', value: h.powerCuts },
      ]
    : [];
  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>Saúde dos rastreadores</h1>
            <p className={pageStyles.description}>
              Os rastreadores que pararam de mandar sinal e o corte da bateria do veículo, do mais grave: é o que precisa estar em dia
              antes do dia do roubo. Avise o associado no app ou pelo WhatsApp.
            </p>
          </div>
          {h && canOperateOrg && !isOrgBroker && (
            <label className={styles.toggle}>
              <input type="checkbox" checked={h.autoNotice} disabled={auto.isPending} onChange={(e) => auto.mutate(e.target.checked)} />
              <span>
                Avisar o associado sozinho
                <small>Uma vez por dia, das 9h às 20h, no máximo a cada 3 dias por veículo.</small>
              </span>
            </label>
          )}
        </header>

        {health.isLoading ? (
          <Spinner label="Carregando" />
        ) : !h ? (
          <EmptyState title="Não deu para carregar" />
        ) : (
          <>
            <div className={styles.tiles}>
              {tiles.map((t) => (
                <div key={t.label} className={`${styles.tile} ${t.tone === 'danger' ? styles.tileDanger : t.tone === 'warning' ? styles.tileWarning : ''}`}>
                  <span className={styles.tileValue}>{t.value}</span>
                  <span className={styles.tileLabel}>{t.label}</span>
                </div>
              ))}
            </div>
            <Card flush>
              {rows.length === 0 ? (
                <EmptyState
                  icon="✅"
                  title={h.installed ? 'Todos os rastreadores em dia' : 'Nenhum rastreador instalado'}
                  description={h.installed ? 'Nenhum sem sinal e nenhum corte de energia na última semana.' : undefined}
                />
              ) : (
                <>
                  <div className={pageStyles.tableWrap}>
                    <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
                      <thead>
                        <tr>
                          <th>Veículo</th>
                          <th>Associado</th>
                          <th>Problema</th>
                          <th>Último sinal</th>
                          <th>Último aviso</th>
                          <th />
                        </tr>
                      </thead>
                      <tbody>
                        {paged.items.map((v) => {
                          const issue = TRACKER_ISSUES[v.issue];
                          return (
                            <tr key={v.vehicleId}>
                              <td>
                                <Link to={`/veiculos/${v.vehicleId}`} className={styles.rowLink}>
                                  {v.vehicleName}
                                </Link>
                                {v.plate && <span className={styles.plate}>{v.plate}</span>}
                              </td>
                              <td data-label="Associado">
                                <Link to={`/associados/${v.associateId}`}>{v.associateName}</Link>
                              </td>
                              <td data-label="Problema">
                                <Badge tone={issue.tone}>{issue.label}</Badge>
                                <span className={styles.issueHint}>
                                  {issue.hint}
                                  {v.lastPowerCutAt ? ` Corte em ${formatDateTime(v.lastPowerCutAt)}.` : ''}
                                </span>
                              </td>
                              <td data-label="Último sinal" title={v.lastSeenAt ? formatDateTime(v.lastSeenAt) : undefined}>
                                {v.lastSeenAt ? formatRelative(v.lastSeenAt) : 'nunca'}
                              </td>
                              <td data-label="Último aviso">{v.notifiedAt ? formatDateTime(v.notifiedAt) : '—'}</td>
                              <td>
                                <div className={styles.actions}>
                                  {canOperateOrg && v.issue !== 'power_cut' && (
                                    <Button
                                      size="small"
                                      variant="secondary"
                                      title={v.pushEnabled ? undefined : 'O associado não tem as notificações do app ligadas'}
                                      loading={notifyOne.isPending && notifyOne.variables === v.vehicleId}
                                      onClick={() => notifyOne.mutate(v.vehicleId)}
                                    >
                                      Avisar no app
                                    </Button>
                                  )}
                                  {v.phone && (
                                    <a className={styles.whatsapp} href={whatsappLink(v.phone, trackerMessage(v, orgName))} target="_blank" rel="noreferrer noopener">
                                      WhatsApp
                                    </a>
                                  )}
                                </div>
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                  <Pager {...paged.pager} />
                </>
              )}
            </Card>
          </>
        )}
      </div>
    </div>
  );
}

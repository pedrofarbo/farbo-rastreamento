import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { priceAdjustmentsApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import pageStyles from '@/pages/Page.module.css';
import { formatDateOnly, formatMoney } from '@/services/format';
import type { PriceAdjustment } from '@/types';

import styles from './Billing.module.css';

const STATUS: Record<PriceAdjustment['status'], { label: string; tone: 'warning' | 'success' | 'neutral' }> = {
  NOTIFIED: { label: 'Agendado', tone: 'warning' },
  APPLIED: { label: 'Aplicado', tone: 'success' },
  CANCELED: { label: 'Cancelado', tone: 'neutral' },
  NO_CHANGE: { label: 'Sem reajuste', tone: 'neutral' },
};

const priceAdjustmentsKey = ['price-adjustments'] as const;

/**
 * O reajuste anual pelo IPCA (Clientes): o próximo, o do ano com as
 * mensalidades reajustadas e o veto, e os anteriores.
 */
export function PriceAdjustmentCard() {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const overview = useQuery({ queryKey: priceAdjustmentsKey, queryFn: priceAdjustmentsApi.overview });
  const cancel = useMutation({
    mutationFn: (year: number) => priceAdjustmentsApi.cancel(year),
    onSuccess: (_, year) => {
      void queryClient.invalidateQueries({ queryKey: priceAdjustmentsKey });
      void queryClient.invalidateQueries({ queryKey: ['customer'] });
      notify({ tone: 'success', title: `Reajuste de ${year} cancelado`, description: 'Os clientes avisados recebem o cancelamento por e-mail.' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível cancelar', description: err.message }),
  });

  const data = overview.data;
  const scheduled = data?.adjustments.find((a) => a.status === 'NOTIFIED');
  const history = (data?.adjustments ?? []).filter((a) => a !== scheduled);

  return (
    <Card
      title="Reajuste anual (IPCA)"
      subtitle="Todo agosto, pelo IPCA acumulado nos 12 meses até maio. Os clientes são avisados por e-mail em 1º de julho."
    >
      {overview.isLoading ? (
        <Spinner label="Carregando o reajuste" />
      ) : !data ? (
        <p className={styles.muted}>Não deu para carregar o reajuste.</p>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
          {!data.enabled && (
            <div className={`${styles.banner} ${styles.bannerWarning}`}>
              <span>O reajuste automático está desligado no servidor (PRICE_ADJUSTMENT_ENABLED).</span>
            </div>
          )}

          {scheduled ? (
            <>
              <p style={{ margin: 0 }}>
                <Badge tone={STATUS.NOTIFIED.tone}>{STATUS.NOTIFIED.label}</Badge>{' '}
                <strong>
                  {scheduled.year}: IPCA {scheduled.rate}
                </strong>{' '}
                <span className={styles.muted}>({scheduled.period})</span> · {scheduled.subscriptions}{' '}
                {scheduled.subscriptions === 1 ? 'mensalidade' : 'mensalidades'} · vale para as faturas que vencem a partir
                de {formatDateOnly(scheduled.effectiveFrom)} · receita +{formatMoney(scheduled.monthlyDiffCents)}/mês
              </p>
              {scheduled.canCancel && (
                <div className={pageStyles.actions} style={{ alignItems: 'center' }}>
                  <Button
                    size="small"
                    variant="danger"
                    loading={cancel.isPending}
                    onClick={() => {
                      if (
                        window.confirm(
                          `Cancelar o reajuste de ${scheduled.year}? As mensalidades ficam como estão e os clientes avisados recebem o cancelamento por e-mail. Não dá para refazer este ano.`,
                        )
                      ) {
                        cancel.mutate(scheduled.year);
                      }
                    }}
                  >
                    Cancelar o reajuste de {scheduled.year}
                  </Button>
                  <span className={styles.muted}>
                    Dá para cancelar até {formatDateOnly(scheduled.cancelUntil)}, antes de as faturas com o preço novo
                    saírem.
                  </span>
                </div>
              )}
              {scheduled.items.length > 0 && (
                <div className={pageStyles.tableWrap}>
                  <table className={pageStyles.table}>
                    <thead>
                      <tr>
                        <th>Cliente</th>
                        <th>Mensalidade</th>
                        <th>De → para</th>
                        <th>Aviso</th>
                      </tr>
                    </thead>
                    <tbody>
                      {scheduled.items.map((item) => (
                        <tr key={item.subscriptionId}>
                          <td>
                            <Link to={`/clientes/${item.customerId}`}>{item.customerName}</Link>
                          </td>
                          <td>
                            {item.vehicle || '—'}
                            <div className={styles.muted}>{item.plan}</div>
                          </td>
                          <td className={styles.amount}>
                            {formatMoney(item.oldPriceCents)} → {formatMoney(item.newPriceCents)}
                          </td>
                          <td>{item.notified ? <Badge tone="success">Enviado</Badge> : <Badge tone="warning">Pendente</Badge>}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </>
          ) : (
            data.upcoming && (
              <p style={{ margin: 0 }}>
                <strong>Próximo: agosto de {data.upcoming.year}.</strong>{' '}
                <span className={styles.muted}>
                  Em {formatDateOnly(data.upcoming.noticeDate)}, o sistema busca o IPCA de {data.upcoming.period} no
                  Banco Central, avisa os clientes por e-mail e manda o resumo para os admins. Entram as assinaturas com 12
                  meses ou mais, fora da promoção e com o contrato atual aceito; índice zero ou negativo mantém o preço.
                </span>
              </p>
            )
          )}

          {history.length > 0 && (
            <ul style={{ margin: 0, paddingLeft: 'var(--space-5)' }}>
              {history.map((a) => (
                <li key={a.year} className={styles.muted}>
                  {a.year}: IPCA {a.rate} ({a.period}) · <Badge tone={STATUS[a.status].tone}>{STATUS[a.status].label}</Badge>
                  {a.status === 'APPLIED' && ` · ${a.subscriptions} mensalidades desde ${formatDateOnly(a.effectiveFrom)}`}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </Card>
  );
}

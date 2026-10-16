import type { UseQueryResult } from '@tanstack/react-query';

import billing from '@/components/billing/Billing.module.css';
import { Card } from '@/components/ui/Card';
import { Spinner } from '@/components/ui/Spinner';
import { formatMoney } from '@/services/format';
import { monthLabel } from '@/services/referral';
import type { MRR, MRRForecast } from '@/types';

import styles from '../Page.module.css';

/** +R$ 10,00 / −R$ 10,00, e — no zero. */
export function signedMoney(cents: number): string {
  if (cents === 0) return '—';
  return `${cents > 0 ? '+' : '−'}${formatMoney(Math.abs(cents))}`;
}

/** A previsão do mês que vem, num quadro: o total e a divisão. */
export function ForecastTile({ forecast }: { forecast: MRRForecast | null | undefined }) {
  return (
    <div className={billing.tile}>
      <span className={billing.tileLabel}>Previsão {forecast ? monthLabel(forecast.month) : 'do mês que vem'}</span>
      <span className={billing.tileValue}>{forecast ? formatMoney(forecast.totalCents) : '—'}</span>
      <span className={billing.tileHint}>
        {forecast
          ? `${formatMoney(forecast.monthlyCents)} em ${forecast.monthlies} ${forecast.monthlies === 1 ? 'mensalidade' : 'mensalidades'}` +
            (forecast.installmentsCents > 0 ? ` + ${formatMoney(forecast.installmentsCents)} parcelas` : '')
          : 'o que vai ser cobrado'}
      </span>
    </div>
  );
}

/**
 * O MRR mês a mês: no fim de cada mês (o atual, hoje), o que entrou com as
 * assinaturas novas, o que saiu com as encerradas e o resto da variação (fim
 * de promoção, reajuste); depois, as parcelas do rastreador do mês e o total.
 */
export function MrrCard({ mrr }: { mrr: UseQueryResult<MRR> }) {
  return (
    <Card
      flush
      title="MRR mês a mês"
      subtitle="Mensalidades: a soma das assinaturas ativas no fim do mês, pelo preço do mês (o da promoção enquanto vale), sem as grátis. Parcelas: as do rastreador que vencem no mês (no atual, também as que ainda vão ser cobradas). Total: as duas."
    >
      {mrr.isLoading ? (
        <Spinner label="Calculando o MRR" />
      ) : !mrr.data ? (
        <p className={billing.muted} style={{ padding: 'var(--space-4)' }}>
          Não deu para calcular o MRR.
        </p>
      ) : (
        <div className={styles.tableWrap}>
          <table className={`${styles.table} ${styles.stackTable}`}>
            <thead>
              <tr>
                <th>Mês</th>
                <th>Mensalidades</th>
                <th>Novas</th>
                <th>Encerradas</th>
                <th>Outras mudanças</th>
                <th>Variação</th>
                <th>Parcelas do rastreador</th>
                <th>Total</th>
              </tr>
            </thead>
            <tbody>
              {mrr.data.months.map((m, i) => (
                <tr key={m.month}>
                  <td data-label="Mês">
                    <strong>{monthLabel(m.month)}</strong>
                    {i === 0 && <div className={billing.muted}>até hoje</div>}
                  </td>
                  <td data-label="Mensalidades">
                    <strong>{formatMoney(m.mrrCents)}</strong>
                    <div className={billing.muted}>
                      {m.subscriptions} {m.subscriptions === 1 ? 'assinatura' : 'assinaturas'}
                    </div>
                  </td>
                  <td data-label="Novas">
                    {signedMoney(m.newCents)}
                    {m.newCount > 0 && (
                      <div className={billing.muted}>
                        {m.newCount} {m.newCount === 1 ? 'nova' : 'novas'}
                      </div>
                    )}
                  </td>
                  <td data-label="Encerradas">
                    {signedMoney(-m.churnCents)}
                    {m.churnCount > 0 && (
                      <div className={billing.muted}>
                        {m.churnCount} {m.churnCount === 1 ? 'encerrada' : 'encerradas'}
                      </div>
                    )}
                  </td>
                  <td data-label="Outras mudanças" title="Fim de promoção ou reajuste">
                    {signedMoney(m.otherCents)}
                  </td>
                  <td data-label="Variação">
                    <strong>{signedMoney(m.changeCents)}</strong>
                  </td>
                  <td data-label="Parcelas do rastreador">
                    {m.installmentsCents > 0 ? formatMoney(m.installmentsCents) : '—'}
                    {m.installmentsCents > 0 && (
                      <div className={billing.muted}>{formatMoney(m.installmentsPaidCents)} pagas</div>
                    )}
                  </td>
                  <td data-label="Total">
                    <strong>{formatMoney(m.totalCents)}</strong>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

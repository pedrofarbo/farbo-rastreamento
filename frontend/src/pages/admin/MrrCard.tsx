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

/** O MRR em partes: "R$ 100,00 mensalidades + R$ 60,00 associações + R$ 12,00 parcelas" (só o que tem valor). */
export function mrrParts(p: { monthlyCents: number; organizationsCents: number; installmentsCents: number }): string {
  const parts = [`${formatMoney(p.monthlyCents)} mensalidades`];
  if (p.organizationsCents > 0) parts.push(`${formatMoney(p.organizationsCents)} associações`);
  if (p.installmentsCents > 0) parts.push(`${formatMoney(p.installmentsCents)} parcelas`);
  return parts.join(' + ');
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
            (forecast.organizationsCents > 0 ? ` + ${formatMoney(forecast.organizationsCents)} associações` : '') +
            (forecast.installmentsCents > 0 ? ` + ${formatMoney(forecast.installmentsCents)} parcelas` : '')
          : 'o que vai ser cobrado'}
      </span>
    </div>
  );
}

/**
 * O MRR mês a mês, pelo que é cobrado: as mensalidades das faturas que vencem
 * em cada mês (no atual, também as que ainda vão sair), o que entrou com as
 * assinaturas novas, o que saiu com as encerradas e o resto da variação (fim
 * de promoção, reajuste, mês sem cobrança); depois, a mensalidade das
 * associações, as parcelas do rastreador do mês e o total.
 */
export function MrrCard({ mrr }: { mrr: UseQueryResult<MRR> }) {
  return (
    <Card
      flush
      title="MRR mês a mês"
      subtitle="Pelo que é cobrado: as mensalidades, a mensalidade dos veículos das associações e as parcelas do rastreador das faturas que vencem em cada mês (no atual, também as que ainda vão sair), sem as grátis nem as taxas avulsas. A assinatura entra no mês da 1ª mensalidade. Total: as três."
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
                <th>Associações</th>
                <th>Parcelas do rastreador</th>
                <th>Total</th>
              </tr>
            </thead>
            <tbody>
              {mrr.data.months.map((m, i) => (
                <tr key={m.month}>
                  <td data-label="Mês">
                    <strong>{monthLabel(m.month)}</strong>
                    {i === 0 && <div className={billing.muted}>este mês</div>}
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
                  <td data-label="Associações">
                    {m.organizationsCents > 0 ? formatMoney(m.organizationsCents) : '—'}
                    {m.organizationVehicles > 0 && (
                      <div className={billing.muted}>
                        {m.organizationVehicles} {m.organizationVehicles === 1 ? 'veículo' : 'veículos'}
                        {m.organizationsChangeCents !== 0 && ` · ${signedMoney(m.organizationsChangeCents)}`}
                      </div>
                    )}
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

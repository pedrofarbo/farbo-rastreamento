/**
 * A comissão do representante no semestral e no anual: a regra própria
 * (valor fixo por período pago ou percentual do valor pago, de uma vez) ou,
 * sem ela, a do mensal pelos meses do período.
 */
import { formatMoney, formatPercentBps } from '@/services/format';
import type { PeriodCommission } from '@/types';

/** "R$ 30,00 por semestre pago", "10% do ano pago" ou "6 meses de comissão" (a do mensal). */
export function periodCommissionLabel(p: PeriodCommission | null | undefined, months: 6 | 12): string {
  const period = months === 12 ? 'ano' : 'semestre';
  if (p?.type === 'fixed') return `${formatMoney(p.cents)} por ${period} pago`;
  if (p?.type === 'percent') return `${formatPercentBps(p.bps)} do ${period} pago`;
  return `${months} meses de comissão`;
}

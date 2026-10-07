import { todayISO } from './format';

/** O mesmo dia do mês anterior ("2027-08-01" → "2027-07-01"; 31/03 → 28 ou 29/02). */
function oneMonthBefore(iso: string): string {
  const [y, m, d] = iso.split('-').map(Number);
  const year = m === 1 ? y - 1 : y;
  const month = m === 1 ? 12 : m - 1;
  const lastDay = new Date(Date.UTC(year, month, 0)).getUTCDate();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${year}-${pad(month)}-${pad(Math.min(d, lastDay))}`;
}

/**
 * O reajuste agendado aparece para o cliente? Só a partir de um mês antes da
 * data em que vale (o dia do e-mail), até o preço novo virar o da assinatura.
 */
export function adjustmentNoticeVisible(
  sub: { nextPriceCents?: number | null; nextPriceFrom?: string | null },
  today: string = todayISO(),
): boolean {
  if (!sub.nextPriceCents || !sub.nextPriceFrom) return false;
  return today >= oneMonthBefore(sub.nextPriceFrom);
}

import type { Invoice } from '@/types';

/** Faturas em aberto que dá para pagar juntas, num Pix só. */
export interface InvoiceGroup {
  /** A chave do Pix delas na janela de pagamento ("grupo:<ids>"). */
  key: string;
  ids: string[];
  dueDate: string | null;
  amountCents: number;
  invoices: Invoice[];
}

const GROUP_PREFIX = 'grupo:';

function group(invoices: Invoice[], dueDate: string | null): InvoiceGroup {
  const ids = invoices.map((i) => i.id);
  return {
    key: GROUP_PREFIX + ids.join(','),
    ids,
    dueDate,
    amountCents: invoices.reduce((sum, i) => sum + i.amountCents, 0),
    invoices,
  };
}

/**
 * A fatura do mês: as faturas em aberto que vencem no mesmo dia (uma por
 * veículo, numa frota), quando são duas ou mais, do vencimento mais antigo
 * para o mais novo.
 */
export function monthlyGroups(invoices: Invoice[]): InvoiceGroup[] {
  const byDate = new Map<string, Invoice[]>();
  for (const inv of invoices) {
    if (inv.status !== 'OPEN' || inv.amountCents <= 0) continue;
    byDate.set(inv.dueDate, [...(byDate.get(inv.dueDate) ?? []), inv]);
  }
  return [...byDate.entries()]
    .filter(([, list]) => list.length > 1)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([date, list]) => group(list, date));
}

/** As vencidas, de mais de um vencimento (para pagar tudo de uma vez); null se não há o que juntar. */
export function overdueGroup(invoices: Invoice[]): InvoiceGroup | null {
  const late = invoices.filter((i) => i.status === 'OPEN' && i.overdue && i.amountCents > 0);
  if (late.length < 2 || new Set(late.map((i) => i.dueDate)).size < 2) return null;
  return group(late, null);
}

/** As faturas da chave de um grupo (null se a chave é de uma fatura só). */
export function groupIds(key: string): string[] | null {
  return key.startsWith(GROUP_PREFIX) ? key.slice(GROUP_PREFIX.length).split(',') : null;
}

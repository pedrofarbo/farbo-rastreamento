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

/** Um grupo para pagar num Pix só (a chave leva as linhas; com uma só, é ela). */
export function groupOf(invoices: Invoice[], dueDate: string | null): InvoiceGroup {
  const ids = invoices.map((i) => i.id);
  return {
    key: ids.length === 1 ? ids[0] : GROUP_PREFIX + ids.join(','),
    ids,
    dueDate,
    amountCents: invoices.reduce((sum, i) => sum + i.amountCents, 0),
    invoices,
  };
}

/**
 * A fatura do mês: tudo o que a conta paga vence no mesmo dia (a mensalidade
 * de cada veículo, o equipamento, o frete, o proporcional); as linhas em
 * aberto de cada vencimento são uma fatura só, da mais antiga para a mais nova.
 */
export function monthlyGroups(invoices: Invoice[]): InvoiceGroup[] {
  const byDate = new Map<string, Invoice[]>();
  for (const inv of invoices) {
    if (inv.status !== 'OPEN' || inv.amountCents <= 0) continue;
    const list = byDate.get(inv.dueDate);
    if (list) list.push(inv);
    else byDate.set(inv.dueDate, [inv]);
  }
  return [...byDate.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([date, list]) => groupOf(list, date));
}

/** As vencidas, de mais de um vencimento (para pagar tudo de uma vez); null se não há o que juntar. */
export function overdueGroup(invoices: Invoice[]): InvoiceGroup | null {
  const late = invoices.filter((i) => i.status === 'OPEN' && i.overdue && i.amountCents > 0);
  if (late.length < 2 || new Set(late.map((i) => i.dueDate)).size < 2) return null;
  return groupOf(late, null);
}

/**
 * O pagamento do pedido da pessoa física, no ato: o equipamento (com o frete),
 * a instalação (no comodato) e a 1ª mensalidade, num pagamento só (null se
 * não há nada em aberto).
 */
export function orderPayment(
  invoices: (Invoice | null)[],
): (Pick<Invoice, 'id' | 'description'> & { amountCents: number }) | null {
  const open = invoices.filter((i): i is Invoice => i !== null && i.status === 'OPEN' && i.amountCents > 0);
  if (open.length === 0) return null;
  if (open.length === 1) return open[0];
  const group = groupOf(open, open[0].dueDate);
  return { id: group.key, description: 'Pedido: rastreador e 1ª mensalidade', amountCents: group.amountCents };
}

/** As faturas da chave de um grupo (null se a chave é de uma fatura só). */
export function groupIds(key: string): string[] | null {
  return key.startsWith(GROUP_PREFIX) ? key.slice(GROUP_PREFIX.length).split(',') : null;
}

/**
 * Quantas faturas fechadas o servidor manda por página do histórico (o
 * mesmo ClosedInvoicesPage do backend): com menos que isso, não há mais
 * antigas. O último vencimento vem sempre inteiro.
 */
export const CLOSED_PAGE = 120;

/** Um vencimento com tantas linhas assim vira uma fatura só no histórico (a fatura do mês). */
export const HISTORY_GROUP_MIN = 2;

/** As faturas de um vencimento, numa linha do histórico. */
export interface HistoryDay {
  kind: 'day';
  dueDate: string;
  invoices: Invoice[];
  amountCents: number;
  /** As em aberto (para pagar num Pix só) e quantas de cada situação. */
  open: Invoice[];
  overdue: number;
  paid: number;
  canceled: number;
}

export type HistoryRow = { kind: 'invoice'; invoice: Invoice } | HistoryDay;

/**
 * O histórico, do vencimento mais novo para o mais antigo: com poucas
 * faturas no dia, cada uma na sua linha (como sempre); numa frota, o dia
 * vira uma linha com o total e a situação, e as faturas abrem sob demanda.
 */
export function historyRows(invoices: Invoice[]): HistoryRow[] {
  const sorted = [...invoices].sort((a, b) => b.dueDate.localeCompare(a.dueDate));
  const byDate = new Map<string, Invoice[]>();
  for (const inv of sorted) {
    const list = byDate.get(inv.dueDate);
    if (list) list.push(inv);
    else byDate.set(inv.dueDate, [inv]);
  }
  const rows: HistoryRow[] = [];
  for (const [dueDate, list] of byDate) {
    if (list.length < HISTORY_GROUP_MIN) {
      for (const invoice of list) rows.push({ kind: 'invoice', invoice });
      continue;
    }
    const open = list.filter((i) => i.status === 'OPEN');
    rows.push({
      kind: 'day',
      dueDate,
      invoices: list,
      amountCents: list.reduce((sum, i) => sum + i.amountCents, 0),
      open,
      overdue: open.filter((i) => i.overdue).length,
      paid: list.filter((i) => i.status === 'PAID').length,
      canceled: list.filter((i) => i.status === 'CANCELED').length,
    });
  }
  return rows;
}

/** A situação da fatura do mês (as linhas de um vencimento), numa palavra ou duas. */
export function dayStatus(day: HistoryDay): { tone: 'success' | 'warning' | 'danger' | 'neutral'; label: string } {
  if (day.open.length > 0) {
    const late = day.overdue > 0;
    const label = late ? 'Vencida' : 'Em aberto';
    return { tone: late ? 'danger' : 'warning', label: day.paid > 0 ? `${label} · paga em parte` : label };
  }
  if (day.paid > 0) return { tone: 'success', label: 'Paga' };
  return { tone: 'neutral', label: 'Cancelada' };
}

/** As faturas fechadas mais antigas que já estão na tela (o "antes" da próxima página). */
export function oldestClosedDue(invoices: Invoice[]): string | null {
  let oldest: string | null = null;
  for (const inv of invoices) {
    if (inv.status !== 'OPEN' && (oldest === null || inv.dueDate < oldest)) oldest = inv.dueDate;
  }
  return oldest;
}

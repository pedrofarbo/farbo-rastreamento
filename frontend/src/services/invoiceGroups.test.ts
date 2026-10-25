import { describe, expect, it } from 'vitest';

import type { Invoice } from '@/types';

import {
  HISTORY_GROUP_MIN,
  dayStatus,
  groupIds,
  historyRows,
  monthlyGroups,
  oldestClosedDue,
  orderPayment,
  overdueGroup,
} from './invoiceGroups';
import type { HistoryDay } from './invoiceGroups';

const inv = (id: string, dueDate: string, amountCents: number, over: Partial<Invoice> = {}): Invoice =>
  ({ id, dueDate, amountCents, status: 'OPEN', overdue: false, description: id, ...over }) as Invoice;

describe('faturas para pagar juntas', () => {
  it('a fatura do mês: as linhas em aberto de cada vencimento (uma só também é a fatura)', () => {
    const groups = monthlyGroups([
      inv('van', '2026-11-13', 4990),
      inv('carro', '2026-11-13', 4990),
      inv('frete', '2026-11-20', 2240),
      inv('moto', '2026-10-13', 3990),
      inv('outra', '2026-10-13', 3990),
      inv('paga', '2026-11-13', 4990, { status: 'PAID' }),
    ]);
    expect(groups.map((g) => [g.dueDate, g.ids, g.amountCents])).toEqual([
      ['2026-10-13', ['moto', 'outra'], 7980],
      ['2026-11-13', ['van', 'carro'], 9980],
      ['2026-11-20', ['frete'], 2240],
    ]);
    expect(groupIds(groups[0].key)).toEqual(['moto', 'outra']);
    // Uma linha só: a chave é a própria fatura.
    expect(groups[2].key).toBe('frete');
    expect(groupIds('fatura-1')).toBeNull();
  });

  it('as vencidas: só quando são de mais de um vencimento', () => {
    expect(overdueGroup([inv('a', '2026-09-13', 100, { overdue: true }), inv('b', '2026-09-13', 100, { overdue: true })])).toBeNull();
    const late = overdueGroup([
      inv('set', '2026-09-13', 4990, { overdue: true }),
      inv('out', '2026-10-13', 4990, { overdue: true }),
      inv('nov', '2026-11-13', 4990),
    ]);
    expect(late?.ids).toEqual(['set', 'out']);
    expect(late?.amountCents).toBe(9980);
  });

  it('o pedido da pessoa física: o rastreador, a instalação e a 1ª mensalidade num pagamento só', () => {
    const due = '2026-10-13';
    const paid = orderPayment([inv('rastreador', due, 15000), inv('instalacao', due, 9900), inv('mensalidade', due, 6990)]);
    expect(paid?.amountCents).toBe(31890);
    expect(groupIds(paid?.id ?? '')).toEqual(['rastreador', 'instalacao', 'mensalidade']);
    // Uma só: ela mesma; nada em aberto (ou de valor zero): nada a pagar.
    expect(orderPayment([null, inv('mensalidade', due, 6990), null])?.id).toBe('mensalidade');
    expect(orderPayment([null, inv('paga', due, 6990, { status: 'PAID' }), inv('zero', due, 0)])).toBeNull();
  });
});

describe('o histórico de faturas', () => {
  const fleet = (dueDate: string, n: number, over: Partial<Invoice> = {}) =>
    Array.from({ length: n }, (_, i) => inv(`${dueDate}-${i}`, dueDate, 4990, over));

  it('a linha sozinha no dia fica como está; com mais de uma, o dia é a fatura do mês, numa linha só', () => {
    const rows = historyRows([
      ...fleet('2026-10-13', 3, { status: 'PAID' }),
      ...fleet('2026-11-13', 1000),
      inv('frete', '2026-11-20', 2240),
    ]);
    expect(rows.map((r) => (r.kind === 'invoice' ? r.invoice.id : `${r.dueDate}×${r.invoices.length}`))).toEqual([
      'frete',
      '2026-11-13×1000',
      '2026-10-13×3',
    ]);
    const day = rows[1] as HistoryDay;
    expect(day.amountCents).toBe(1000 * 4990);
    expect(day.open).toHaveLength(1000);
  });

  it('junta as páginas: um vencimento que veio em duas partes vira uma linha só, no lugar certo', () => {
    const firstPage = [inv('novembro', '2026-11-13', 4990, { status: 'PAID' }), inv('agosto', '2026-08-13', 4990, { overdue: true })];
    const older = [...fleet('2026-09-13', 6, { status: 'PAID' }), ...fleet('2026-08-13', HISTORY_GROUP_MIN - 1, { status: 'PAID' })];
    const rows = historyRows([...firstPage, ...older]);
    expect(rows.map((r) => (r.kind === 'invoice' ? r.invoice.dueDate : `${r.dueDate}×${r.invoices.length}`))).toEqual([
      '2026-11-13',
      '2026-09-13×6',
      `2026-08-13×${HISTORY_GROUP_MIN}`,
    ]);
    expect(oldestClosedDue([...firstPage, ...older])).toBe('2026-08-13');
    expect(oldestClosedDue([inv('a', '2026-01-13', 100)])).toBeNull();
  });

  it('a situação da fatura do mês numa palavra', () => {
    const status = (list: Invoice[]) => dayStatus(historyRows(list)[0] as HistoryDay);
    expect(status(fleet('2026-10-13', 5, { status: 'PAID' }))).toEqual({ tone: 'success', label: 'Paga' });
    expect(status([...fleet('2026-10-13', 4, { status: 'PAID' }), inv('c', '2026-10-13', 1, { status: 'CANCELED' })])).toEqual({
      tone: 'success',
      label: 'Paga',
    });
    expect(status(fleet('2026-11-13', 5))).toEqual({ tone: 'warning', label: 'Em aberto' });
    expect(status([...fleet('2026-09-13', 2, { overdue: true }), ...fleet('2026-09-13', 3, { status: 'PAID' })])).toEqual({
      tone: 'danger',
      label: 'Vencida · paga em parte',
    });
    expect(status(fleet('2026-09-13', 5, { status: 'CANCELED' }))).toEqual({ tone: 'neutral', label: 'Cancelada' });
  });
});

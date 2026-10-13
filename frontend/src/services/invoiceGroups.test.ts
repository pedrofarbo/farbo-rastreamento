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
  it('a fatura do mês: as em aberto do mesmo vencimento, quando são duas ou mais', () => {
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
    ]);
    expect(groupIds(groups[0].key)).toEqual(['moto', 'outra']);
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
});

describe('o pagamento logo depois do pedido', () => {
  it('o equipamento e a 1ª mensalidade num pagamento só; só uma, ela; nenhuma em aberto, nada', () => {
    const both = orderPayment([inv('equipamento', '2026-10-13', 12000), inv('mensalidade', '2026-10-13', 3990)]);
    expect(both).toEqual({ id: 'grupo:equipamento,mensalidade', description: 'Pedido: equipamento e 1ª mensalidade', amountCents: 15990 });
    expect(groupIds(both!.id)).toEqual(['equipamento', 'mensalidade']);
    // Parcelado sem frete: só a 1ª mensalidade.
    expect(orderPayment([null, inv('mensalidade', '2026-10-13', 3990)])?.id).toBe('mensalidade');
    expect(orderPayment([null, inv('mensalidade', '2026-10-13', 0, { status: 'CANCELED' })])).toBeNull();
    expect(orderPayment([null, null])).toBeNull();
  });
});

describe('o histórico de faturas', () => {
  const fleet = (dueDate: string, n: number, over: Partial<Invoice> = {}) =>
    Array.from({ length: n }, (_, i) => inv(`${dueDate}-${i}`, dueDate, 4990, over));

  it('com poucas no dia, cada uma na sua linha; numa frota, o dia numa linha só', () => {
    const rows = historyRows([
      ...fleet('2026-10-13', 3, { status: 'PAID' }),
      ...fleet('2026-11-13', 1000),
      inv('frete', '2026-11-20', 2240),
    ]);
    expect(rows.map((r) => (r.kind === 'invoice' ? r.invoice.id : `${r.dueDate}×${r.invoices.length}`))).toEqual([
      'frete',
      '2026-11-13×1000',
      '2026-10-13-0',
      '2026-10-13-1',
      '2026-10-13-2',
    ]);
    const day = rows[1] as HistoryDay;
    expect(day.amountCents).toBe(1000 * 4990);
    expect(day.open).toHaveLength(1000);
  });

  it('junta as páginas: um vencimento que veio em duas partes vira uma linha só, no lugar certo', () => {
    const firstPage = [...fleet('2026-11-13', 4, { status: 'PAID' }), inv('agosto', '2026-08-13', 4990, { overdue: true })];
    const older = [...fleet('2026-09-13', 6, { status: 'PAID' }), ...fleet('2026-08-13', HISTORY_GROUP_MIN - 1, { status: 'PAID' })];
    const rows = historyRows([...firstPage, ...older]);
    expect(rows.map((r) => (r.kind === 'invoice' ? r.invoice.dueDate : `${r.dueDate}×${r.invoices.length}`))).toEqual([
      '2026-11-13',
      '2026-11-13',
      '2026-11-13',
      '2026-11-13',
      '2026-09-13×6',
      `2026-08-13×${HISTORY_GROUP_MIN}`,
    ]);
    expect(oldestClosedDue([...firstPage, ...older])).toBe('2026-08-13');
    expect(oldestClosedDue([inv('a', '2026-01-13', 100)])).toBeNull();
  });

  it('a situação do dia numa frase', () => {
    const status = (list: Invoice[]) => dayStatus(historyRows(list)[0] as HistoryDay);
    expect(status(fleet('2026-10-13', 5, { status: 'PAID' }))).toEqual({ tone: 'success', label: 'Pagas' });
    expect(status([...fleet('2026-10-13', 4, { status: 'PAID' }), inv('c', '2026-10-13', 1, { status: 'CANCELED' })])).toEqual({
      tone: 'success',
      label: 'Pagas · 1 cancelada',
    });
    expect(status(fleet('2026-11-13', 5))).toEqual({ tone: 'warning', label: '5 em aberto' });
    expect(status([...fleet('2026-09-13', 2, { overdue: true }), ...fleet('2026-09-13', 3, { status: 'PAID' })])).toEqual({
      tone: 'danger',
      label: '2 vencidas · 3 pagas',
    });
    expect(status(fleet('2026-09-13', 5, { status: 'CANCELED' }))).toEqual({ tone: 'neutral', label: 'Canceladas' });
  });
});

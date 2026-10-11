import { describe, expect, it } from 'vitest';

import type { Invoice } from '@/types';

import { groupIds, monthlyGroups, overdueGroup } from './invoiceGroups';

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

import { describe, expect, it } from 'vitest';

import type { Subscription } from '@/types';

import { accountDueDay, activeDueDays } from './subscriptions';

const sub = (dueDay: number, createdAt: string, status: Subscription['status'] = 'ACTIVE') =>
  ({ id: `${dueDay}-${createdAt}`, dueDay, createdAt, status }) as Subscription;

describe('o vencimento da conta', () => {
  it('é o dia das assinaturas ativas; sem nenhuma, nada', () => {
    expect(accountDueDay([sub(10, '2026-01-01'), sub(10, '2026-02-01')])).toBe(10);
    expect(accountDueDay([sub(10, '2026-01-01', 'CANCELED')])).toBeNull();
    expect(accountDueDay([])).toBeNull();
  });

  it('numa conta com dias diferentes: o mais comum; no empate, o da mais antiga (como o servidor)', () => {
    expect(accountDueDay([sub(25, '2026-01-01'), sub(10, '2026-02-01'), sub(10, '2026-03-01')])).toBe(10);
    expect(accountDueDay([sub(25, '2026-03-01'), sub(10, '2026-02-01')])).toBe(10);
    expect(accountDueDay([sub(25, '2026-01-01'), sub(10, '2026-02-01'), sub(5, '2026-01-01', 'CANCELED')])).toBe(25);
    expect(activeDueDays([sub(25, 'a'), sub(10, 'b'), sub(10, 'c'), sub(5, 'd', 'CANCELED')])).toEqual([10, 25]);
  });
});

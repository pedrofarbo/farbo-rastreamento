import { describe, expect, it } from 'vitest';

import { expenseKindLabel, monthStart, rentalKm, rentalStatus } from './fleetOps';

describe('gestão da frota', () => {
  it('escreve os tipos e a situação da locação', () => {
    expect(expenseKindLabel('fuel')).toBe('Combustível');
    const now = new Date('2026-10-24T12:00:00Z');
    expect(rentalStatus({ returnedAt: null, overdue: true, startsAt: '2026-10-20T10:00:00Z' }, now).label).toBe('Atrasada');
    expect(rentalStatus({ returnedAt: null, overdue: false, startsAt: '2026-10-25T10:00:00Z' }, now).label).toBe('Agendada');
    expect(rentalStatus({ returnedAt: '2026-10-21T10:00:00Z', overdue: false, startsAt: '2026-10-20T10:00:00Z' }, now).label).toBe('Devolvido');
    expect(rentalStatus({ returnedAt: null, overdue: false, startsAt: '2026-10-20T10:00:00Z' }, now).label).toBe('Em andamento');
  });

  it('calcula o mês e os km da locação', () => {
    expect(monthStart(new Date(2026, 9, 24))).toBe('2026-10-01');
    expect(rentalKm({ kmStart: 10500, kmEnd: 10820 })).toBe(320);
    expect(rentalKm({ kmStart: null, kmEnd: 10820 })).toBeNull();
  });
});

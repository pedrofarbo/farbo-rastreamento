import { describe, expect, it } from 'vitest';

import { periodCommissionLabel } from './commission';

const text = (s: string) => s.replace(/ /g, ' ');

describe('a comissão do semestral e do anual', () => {
  it('a regra própria: valor fixo por período pago ou percentual do valor pago; sem ela, os meses do período', () => {
    expect(text(periodCommissionLabel({ type: 'fixed', cents: 3000, bps: 0 }, 6))).toBe('R$ 30,00 por semestre pago');
    expect(periodCommissionLabel({ type: 'percent', cents: 0, bps: 1000 }, 12)).toBe('10% do ano pago');
    expect(periodCommissionLabel({ type: '', cents: 0, bps: 0 }, 6)).toBe('6 meses de comissão');
    expect(periodCommissionLabel(null, 12)).toBe('12 meses de comissão');
  });
});

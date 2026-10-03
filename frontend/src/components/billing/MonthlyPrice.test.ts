import { describe, expect, it } from 'vitest';

import { promoActive, promoMonthlyFor } from './MonthlyPrice';

describe('promoActive', () => {
  const promo = { priceCents: 6990, promoPriceCents: 2990, promoUntil: '2027-10-10' };

  it('vale enquanto a próxima fatura vence antes do fim da promoção', () => {
    expect(promoActive({ ...promo, nextDueDate: '2026-10-10' })).toBe(true);
    expect(promoActive({ ...promo, nextDueDate: '2027-09-10' })).toBe(true);
  });

  it('acaba na fatura que vence no fim da promoção', () => {
    expect(promoActive({ ...promo, nextDueDate: '2027-10-10' })).toBe(false);
  });

  it('sem promoção, nunca', () => {
    expect(promoActive({ priceCents: 6990, promoPriceCents: null, promoUntil: null, nextDueDate: '2026-10-10' })).toBe(false);
  });
});

describe('promoMonthlyFor', () => {
  const offer = {
    equipmentCents: 12000,
    monthlyCents: 3490,
    insanosMonthlyCents: 2790,
    insanosPlanName: 'Especial Insanos MC',
    months: 12,
  };

  it('no plano do Insanos MC, a mensalidade deles (sem diferença de maiúsculas e espaços)', () => {
    expect(promoMonthlyFor(offer, 'Especial Insanos MC')).toBe(2790);
    expect(promoMonthlyFor(offer, '  especial insanos mc ')).toBe(2790);
  });

  it('nos outros planos, a mensalidade da promoção', () => {
    expect(promoMonthlyFor(offer, 'Plano Mensal')).toBe(3490);
    expect(promoMonthlyFor(offer, 'Frota')).toBe(3490);
    expect(promoMonthlyFor({ ...offer, insanosPlanName: '' }, '')).toBe(3490);
  });
});

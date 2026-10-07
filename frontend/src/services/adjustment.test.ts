import { describe, expect, it } from 'vitest';

import { adjustmentNoticeVisible } from './adjustment';

describe('aviso do reajuste na tela', () => {
  const sub = { nextPriceCents: 7333, nextPriceFrom: '2027-08-01' };

  it('só a partir de um mês antes de valer (o dia do e-mail)', () => {
    expect(adjustmentNoticeVisible(sub, '2026-10-07')).toBe(false);
    expect(adjustmentNoticeVisible(sub, '2027-06-30')).toBe(false);
    expect(adjustmentNoticeVisible(sub, '2027-07-01')).toBe(true);
    expect(adjustmentNoticeVisible(sub, '2027-08-01')).toBe(true);
  });

  it('aviso atrasado e fim de mês', () => {
    // Reajuste em 19/08 (o IPCA atrasou): a tela mostra desde 19/07.
    expect(adjustmentNoticeVisible({ nextPriceCents: 1, nextPriceFrom: '2030-08-19' }, '2030-07-18')).toBe(false);
    expect(adjustmentNoticeVisible({ nextPriceCents: 1, nextPriceFrom: '2030-08-19' }, '2030-07-19')).toBe(true);
    expect(adjustmentNoticeVisible({ nextPriceCents: 1, nextPriceFrom: '2031-03-31' }, '2031-02-27')).toBe(false);
    expect(adjustmentNoticeVisible({ nextPriceCents: 1, nextPriceFrom: '2031-03-31' }, '2031-02-28')).toBe(true);
    expect(adjustmentNoticeVisible({ nextPriceCents: 1, nextPriceFrom: '2031-01-15' }, '2030-12-15')).toBe(true);
  });

  it('sem reajuste agendado, nada', () => {
    expect(adjustmentNoticeVisible({ nextPriceCents: null, nextPriceFrom: null }, '2027-07-15')).toBe(false);
    expect(adjustmentNoticeVisible({}, '2027-07-15')).toBe(false);
  });
});

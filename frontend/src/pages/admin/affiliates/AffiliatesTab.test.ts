import { describe, expect, it } from 'vitest';

import { codeFor } from './AffiliateModal';
import { inDays, previousMonth } from './AffiliatesTab';

describe('aba Afiliados', () => {
  it('o mês do fechamento e o vencimento', () => {
    expect(previousMonth(new Date(2026, 9, 5))).toBe('2026-09');
    expect(previousMonth(new Date(2027, 0, 2))).toBe('2026-12');
    expect(inDays(new Date(2026, 9, 29), 5)).toBe('2026-11-03');
  });

  it('o código do link sai do digitado, do @ ou do nome', () => {
    expect(codeFor({ code: '', handle: '@Fulano.Moto', name: 'Fulano' })).toBe('fulano-moto');
    expect(codeFor({ code: '', handle: '', name: 'Ação Rápida' })).toBe('acao-rapida');
    expect(codeFor({ code: 'Promo Fulano', handle: '@fulano', name: 'Fulano' })).toBe('promo-fulano');
  });
});

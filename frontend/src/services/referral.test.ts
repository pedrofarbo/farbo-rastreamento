// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';

import { currentReferral, partnerUrl, referralCode, referralUrl, saveReferral } from './referral';

afterEach(() => localStorage.clear());

describe('link de indicação', () => {
  it('o código como o servidor faz', () => {
    expect(referralCode('@Fulano.Moto')).toBe('fulano-moto');
    expect(referralCode('  Ação Rápida  ')).toBe('acao-rapida');
    expect(referralCode('abc-'.repeat(20)).length).toBeLessThanOrEqual(40);
    expect(referralUrl('fulano-moto')).toBe('https://farborastreadores.com.br/indicacao/fulano-moto');
    expect(partnerUrl('tok')).toBe('https://farborastreadores.com.br/parceiro/tok');
  });

  it('guarda por 60 dias; o mais recente vale', () => {
    const day = 24 * 60 * 60 * 1000;
    expect(currentReferral()).toBe('');
    saveReferral('Joao-Moto', 0);
    expect(currentReferral(59 * day)).toBe('joao-moto');
    saveReferral('maria', 10 * day);
    expect(currentReferral(69 * day)).toBe('maria');
    expect(currentReferral(71 * day)).toBe('');
    // Expirado, sai.
    expect(localStorage.getItem('farbo.ref')).toBeNull();
    saveReferral('!', 0);
    expect(currentReferral(0)).toBe('');
    localStorage.setItem('farbo.ref', '{quebrado');
    expect(currentReferral()).toBe('');
  });
});

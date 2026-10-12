import { describe, expect, it } from 'vitest';

import { formatKm, intervalLabel, parseWholeNumber, remainingLabel } from './maintenance';

describe('manutenção', () => {
  it('escreve o intervalo e quanto falta', () => {
    expect(formatKm(48312.4)).toBe('48.312 km');
    expect(intervalLabel({ everyKm: 10000, everyMonths: 6 })).toBe('A cada 10.000 km ou 6 meses');
    expect(intervalLabel({ everyKm: null, everyMonths: 1 })).toBe('A cada 1 mês');
    expect(remainingLabel({ remainingKm: 900, remainingDays: 8, status: 'soon' })).toBe('faltam 900 km · faltam 8 dias');
    expect(remainingLabel({ remainingKm: -320, remainingDays: null, status: 'due' })).toBe('passou 320 km');
    expect(remainingLabel({ remainingKm: null, remainingDays: -1, status: 'due' })).toBe('venceu há 1 dia');
    expect(remainingLabel({ remainingKm: null, remainingDays: 0, status: 'due' })).toBe('vence hoje');
    expect(remainingLabel({ remainingKm: null, remainingDays: null, status: 'unknown' })).toContain('informe o hodômetro');
  });

  it('lê os números como a pessoa digita', () => {
    expect(parseWholeNumber('10.000')).toBe(10000);
    expect(parseWholeNumber(' 48 312 ')).toBe(48312);
    expect(parseWholeNumber('')).toBeNull();
    expect(parseWholeNumber('dez')).toBeNaN();
  });
});

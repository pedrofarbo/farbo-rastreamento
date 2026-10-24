import { describe, expect, it } from 'vitest';

import { brDate, formatHours, formatKm, intervalLabel, parseWholeNumber, remainingLabel, renewLabel } from './maintenance';

describe('manutenção', () => {
  it('escreve o intervalo e quanto falta', () => {
    expect(formatKm(48312.4)).toBe('48.312 km');
    expect(formatHours(1234.4)).toBe('1.234 h');
    expect(intervalLabel({ everyKm: 10000, everyMonths: 6, everyHours: null })).toBe('A cada 10.000 km ou 6 meses');
    expect(intervalLabel({ everyKm: null, everyMonths: 1, everyHours: null })).toBe('A cada 1 mês');
    expect(intervalLabel({ everyKm: 10000, everyMonths: 6, everyHours: 250 })).toBe('A cada 10.000 km, 250 h de motor ou 6 meses');
    expect(intervalLabel({ everyKm: null, everyMonths: null, everyHours: 250 })).toBe('A cada 250 h de motor');
    const left = (remainingKm: number | null, remainingDays: number | null, status: 'ok' | 'soon' | 'due' | 'unknown', remainingHours: number | null = null) =>
      remainingLabel({ remainingKm, remainingHours, remainingDays, status });
    expect(left(900, 8, 'soon')).toBe('faltam 900 km · faltam 8 dias');
    expect(left(-320, null, 'due')).toBe('passou 320 km');
    expect(left(null, -1, 'due')).toBe('venceu há 1 dia');
    expect(left(null, 0, 'due')).toBe('vence hoje');
    expect(left(null, null, 'unknown')).toContain('informe o hodômetro');
    expect(left(null, 293, 'due', -15.4)).toBe('passou 15 h de motor · faltam 293 dias');
    expect(left(null, null, 'soon', 40)).toBe('faltam 40 h de motor');
  });

  it('escreve a renovação e o vencimento dos documentos', () => {
    expect(brDate('2026-11-05')).toBe('05/11/2026');
    expect(renewLabel(12)).toBe('Renova todo ano');
    expect(renewLabel(60)).toBe('Renova a cada 5 anos');
    expect(renewLabel(6)).toBe('Renova a cada 6 meses');
    expect(intervalLabel({ kind: 'document', everyKm: null, everyMonths: 12, everyHours: null })).toBe('Renova todo ano');
    const doc = (remainingDays: number, status: 'ok' | 'soon' | 'due') =>
      remainingLabel({ kind: 'document', nextDate: '2026-11-05', remainingKm: null, remainingHours: null, remainingDays, status });
    expect(doc(24, 'soon')).toBe('vence em 05/11/2026 (faltam 24 dias)');
    expect(doc(0, 'due')).toBe('vence hoje');
    expect(doc(-3, 'due')).toBe('venceu há 3 dias');
  });

  it('lê os números como a pessoa digita', () => {
    expect(parseWholeNumber('10.000')).toBe(10000);
    expect(parseWholeNumber(' 48 312 ')).toBe(48312);
    expect(parseWholeNumber('')).toBeNull();
    expect(parseWholeNumber('dez')).toBeNaN();
  });
});

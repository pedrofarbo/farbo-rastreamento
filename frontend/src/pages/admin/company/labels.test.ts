import { describe, expect, it } from 'vitest';

import { addDays, daysBetween, entrySituation, installmentLabel, monthLabel, splitInstallments, todayISO } from './labels';
import { formatDocument } from './RegistryTab';
import { margin, money } from './ResultTab';

describe('datas', () => {
  it('mês, hoje em Brasília, diferença e soma de dias', () => {
    expect(monthLabel('2026-10')).toBe('out/2026');
    expect(monthLabel('2027-01')).toBe('jan/2027');
    // 01h UTC do dia 16 ainda é dia 15 em Brasília.
    expect(todayISO(new Date('2026-10-16T01:00:00Z'))).toBe('2026-10-15');
    expect(todayISO(new Date('2026-10-16T04:00:00Z'))).toBe('2026-10-16');
    expect(daysBetween('2026-10-15', '2026-10-18')).toBe(3);
    expect(daysBetween('2026-10-15', '2026-10-05')).toBe(-10);
    expect(addDays('2026-10-30', 3)).toBe('2026-11-02');
  });
});

describe('situação da conta', () => {
  const today = '2026-10-15';
  const at = (dueDate: string, status: 'OPEN' | 'PAID' | 'CANCELED' = 'OPEN', kind: 'PAYABLE' | 'RECEIVABLE' = 'PAYABLE') =>
    entrySituation({ dueDate, status, kind }, today);

  it('vencida, hoje, em breve, depois, paga e cancelada', () => {
    expect(at('2026-10-14')).toEqual({ label: 'Vencida há 1 dia', tone: 'danger' });
    expect(at('2026-10-05').label).toBe('Vencida há 10 dias');
    expect(at('2026-10-15')).toEqual({ label: 'Vence hoje', tone: 'warning' });
    expect(at('2026-10-18')).toEqual({ label: 'Vence em 3 dias', tone: 'warning' });
    expect(at('2026-10-25')).toEqual({ label: 'Em 10 dias', tone: 'neutral' });
    expect(at('2026-10-01', 'PAID').label).toBe('Paga');
    expect(at('2026-10-01', 'PAID', 'RECEIVABLE').label).toBe('Recebida');
    expect(at('2026-10-01', 'CANCELED').tone).toBe('neutral');
  });

  it('parcelas', () => {
    expect(installmentLabel({ installment: 2, installments: 10 })).toBe('2/10');
    expect(installmentLabel({ installment: null, installments: null })).toBe('');
    expect(splitInstallments(100000, 3)).toEqual([33334, 33333, 33333]);
    expect(splitInstallments(500, 1)).toEqual([500]);
  });
});

describe('formatos', () => {
  it('documento e margem', () => {
    expect(formatDocument('02558157000162')).toBe('02.558.157/0001-62');
    expect(formatDocument('12345678909')).toBe('123.456.789-09');
    expect(formatDocument('123')).toBe('123');
    expect(margin(2500, 10000)).toBe('25%');
    expect(margin(-1701, 4349)).toBe('-39,1%');
    expect(margin(100, 0)).toBe('—');
    expect(money(-0).replace(/\u00a0/g, ' ')).toBe('R$ 0,00');
    expect(money(-1500).replace(/\u00a0/g, ' ')).toBe('-R$ 15,00');
  });
});

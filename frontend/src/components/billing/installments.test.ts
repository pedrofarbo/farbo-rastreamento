import { describe, expect, it } from 'vitest';

import { installmentChoices, installmentProgress, installmentsLabel, splitInstallments } from './installments';

const money = (s: string | null) => (s ?? '').replace(/\u00a0/g, ' ');

describe('rastreador parcelado', () => {
  it('divide como o servidor: a 1ª leva os centavos que sobram', () => {
    expect(splitInstallments(15000, 10)).toEqual(Array(10).fill(1500));
    expect(splitInstallments(12000, 7)).toEqual([1716, 1714, 1714, 1714, 1714, 1714, 1714]);
    expect(splitInstallments(999, 1)).toEqual([999]);
  });

  it('as opções: à vista e de 2 até o teto', () => {
    const choices = installmentChoices(15000, 10);
    expect(choices).toHaveLength(10);
    expect(money(choices[0].label)).toBe('À vista: R$ 150,00');
    expect(money(choices[9].label)).toBe('10x de R$ 15,00 sem juros');
    expect(money(installmentsLabel(12000, 7))).toBe('7x sem juros: 1ª de R$ 17,16 e 6 de R$ 17,14');
    // Sem valor (cortesia) ou só à vista no catálogo: nada a escolher.
    expect(installmentChoices(0, 10)).toEqual([]);
    expect(installmentChoices(15000, 1)).toHaveLength(1);
  });

  it('o andamento na assinatura', () => {
    const base = {
      installments: 10, commitmentUntil: '2027-06-10', equipmentCents: 15000, installmentCents: 1500,
      installmentsPaid: 3, installmentsDueCents: 10500,
    };
    expect(money(installmentProgress(base))).toBe('Rastreador em 10x de R$ 15,00: 3 de 10 pagas, faltam R$ 105,00 · ativa até 10/06/2027');
    expect(money(installmentProgress({ ...base, installmentsPaid: 10, installmentsDueCents: 0 }))).toBe(
      'Rastreador em 10x de R$ 15,00: 10 de 10 pagas (quitado)',
    );
    expect(money(installmentProgress({ ...base, installmentsPaid: 1, installmentsDueCents: 13500 }))).toContain('1 de 10 paga,');
    expect(installmentProgress({ ...base, installments: 0 })).toBeNull();
  });
});

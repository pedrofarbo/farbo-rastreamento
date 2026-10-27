import { describe, expect, it } from 'vitest';

import { iccidProblem, parseChipBatch, templateChipCsv } from './chipImport';

const ALGAR = { carrier: 'Algar', apn: 'ss.algar.br', apnUser: 'algar', apnPassword: 'algar', notes: 'Lote 12' };

describe('a planilha do lote de chips', () => {
  it('um ICCID por linha: o resto vem do "para todos"; repetidos e inválidos não entram', () => {
    const batch = parseChipBatch(
      ['89553202100093795330', '8955 3202 1000 9379 5348 SP', '', '89553202100093795330', '89553202100093795349', '895532021000937953', '89553202100093795355'].join('\n'),
      ['89553202100093795355'],
      ALGAR,
    );
    expect(batch.rows.map((r) => [r.lineNumber, r.status, r.message])).toEqual([
      [1, 'valid', ''],
      [2, 'valid', ''],
      [4, 'duplicate', 'ICCID repetido nesta lista'],
      [5, 'error', 'O último dígito do ICCID não confere: confira na etiqueta'],
      [6, 'error', 'ICCID tem 19 ou 20 dígitos (este tem 18)'],
      [7, 'duplicate', 'Já está no estoque'],
    ]);
    expect(batch.validCount).toBe(2);
    expect(batch.duplicateCount).toBe(2);
    expect(batch.errorCount).toBe(2);
    expect(batch.rows[1].input).toEqual({ iccid: '89553202100093795348', phoneNumber: '', ...ALGAR });
  });

  it('com cabeçalho (em qualquer ordem): o que a linha traz vale mais que o "para todos"', () => {
    const batch = parseChipBatch(
      'Linha;ICCID;Operadora;Observações\n(34) 99999-0001;89553202100093795330;Vivo;\n;89553202100093795348;;Lote 13',
      [],
      ALGAR,
    );
    expect(batch.rows.map((r) => r.lineNumber)).toEqual([2, 3]);
    expect(batch.rows[0].input).toEqual(expect.objectContaining({ iccid: '89553202100093795330', carrier: 'Vivo', apn: 'ss.algar.br', notes: 'Lote 12' }));
    expect(batch.rows[0].input.phoneNumber).not.toBe('');
    expect(batch.rows[1].input).toEqual(expect.objectContaining({ carrier: 'Algar', notes: 'Lote 13' }));
  });

  it('o APN com vírgula ou # é recusado (vai no comando do rastreador)', () => {
    const batch = parseChipBatch('89553202100093795330', [], { ...ALGAR, apn: 'a,b' });
    expect(batch.rows[0].status).toBe('error');
    expect(batch.rows[0].message).toContain('vírgula');
  });

  it('o modelo de planilha passa na própria leitura', () => {
    const batch = parseChipBatch(templateChipCsv());
    expect(batch.validCount).toBe(2);
    expect(iccidProblem('89553202100093795330SP')).toEqual({ iccid: '89553202100093795330', problem: '' });
    expect(iccidProblem('99553202100093795330').problem).toBe('ICCID começa com 89');
  });
});

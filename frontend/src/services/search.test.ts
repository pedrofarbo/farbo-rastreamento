import { describe, expect, it } from 'vitest';

import { compactSearch, isExactCode, matchesSearch } from './search';

describe('busca', () => {
  it('ignora acento, maiúscula, pontuação e espaço', () => {
    expect(compactSearch('São Paulo')).toBe('saopaulo');
    expect(compactSearch('(34) 99999-0000')).toBe('34999990000');
    expect(compactSearch('ABC-1D23')).toBe('abc1d23');
  });

  it('acha pelo pedaço, do jeito que for digitado', () => {
    const fields = ['869247061900077', '89553202100093795330', '(34) 99999-0000', 'TK910', 'Honda CG 160', 'ABC1D23', 'Lia Souza'];
    expect(matchesSearch('', fields)).toBe(true);
    expect(matchesSearch('0619', fields)).toBe(true);
    expect(matchesSearch('8955 3202', fields)).toBe(true);
    expect(matchesSearch('34 99999-0000', fields)).toBe(true);
    expect(matchesSearch('abc-1d', fields)).toBe(true);
    expect(matchesSearch('lia souza', fields)).toBe(true);
    expect(matchesSearch('honda cg', fields)).toBe(true);
    // O texto inteiro: o "1" de "chip 1" não casa com os números do ICCID.
    expect(matchesSearch('chip 1', ['Chip 10', '89553202100093700001'])).toBe(true);
    expect(matchesSearch('chip 1', ['Chip 2', '89553202100093700001'])).toBe(false);
    // A placa não casa com os números do IMEI.
    expect(matchesSearch('XYZ1D23', fields)).toBe(false);
    expect(matchesSearch('souzá', fields)).toBe(true);
  });

  it('o código inteiro escolhe; um pedaço não', () => {
    expect(isExactCode('869247061900077', ['869247061900077', ''])).toBe(true);
    expect(isExactCode(' 8692 4706 1900 077 ', ['869247061900077'])).toBe(true);
    expect(isExactCode('86924706190', ['869247061900077'])).toBe(false);
    expect(isExactCode('', [''])).toBe(false);
  });
});

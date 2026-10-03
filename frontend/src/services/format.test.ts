import { describe, expect, it } from 'vitest';

import { formatPhoneInput, isPhoneComplete } from './format';

describe('formatPhoneInput', () => {
  it('formata enquanto digita', () => {
    expect(formatPhoneInput('1')).toBe('(1');
    expect(formatPhoneInput('11')).toBe('(11');
    expect(formatPhoneInput('119')).toBe('(11) 9');
    expect(formatPhoneInput('1198888')).toBe('(11) 9888-8');
    expect(formatPhoneInput('1133334444')).toBe('(11) 3333-4444'); // fixo
    expect(formatPhoneInput('11988887777')).toBe('(11) 98888-7777'); // celular
  });

  it('aceita colado com +55, espaços e traços, e para em 11 dígitos', () => {
    expect(formatPhoneInput('+55 11 98888-7777')).toBe('(11) 98888-7777');
    expect(formatPhoneInput('11 98888 7777 123')).toBe('(11) 98888-7777');
    expect(formatPhoneInput('')).toBe('');
  });

  it('apagar o hífen apaga o dígito antes dele', () => {
    // "(11) 9888-8" → apaga o "-" → "(11) 98888" (mesmos dígitos, mais curto)
    expect(formatPhoneInput('(11) 98888', '(11) 9888-8')).toBe('(11) 9888');
  });
});

describe('isPhoneComplete', () => {
  it('quer o DDD e o número', () => {
    expect(isPhoneComplete('(11) 98888-7777')).toBe(true);
    expect(isPhoneComplete('(11) 3333-4444')).toBe(true);
    expect(isPhoneComplete('98888-7777')).toBe(false);
    expect(isPhoneComplete('')).toBe(false);
  });
});

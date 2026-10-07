import { describe, expect, it } from 'vitest';

import { formatTaxId, taxIdDigits, validTaxId } from './taxid';

describe('CPF e CNPJ', () => {
  it('a máscara acompanha a digitação', () => {
    expect(formatTaxId('529')).toBe('529');
    expect(formatTaxId('5299822')).toBe('529.982.2');
    expect(formatTaxId('52998224725')).toBe('529.982.247-25');
    expect(formatTaxId('49757084000100')).toBe('49.757.084/0001-00');
    expect(formatTaxId('abc529.982.247-25xyz')).toBe('529.982.247-25');
    expect(taxIdDigits('529.982.247-25')).toBe('52998224725');
  });

  it('os dígitos verificadores', () => {
    expect(validTaxId('529.982.247-25')).toBe(true);
    expect(validTaxId('390.533.447-05')).toBe(true);
    expect(validTaxId('49.757.084/0001-00')).toBe(true);
    expect(validTaxId('529.982.247-24')).toBe(false);
    expect(validTaxId('111.111.111-11')).toBe(false);
    expect(validTaxId('49.757.084/0001-01')).toBe(false);
    expect(validTaxId('123')).toBe(false);
  });
});

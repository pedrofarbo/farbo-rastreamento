import { describe, expect, it } from 'vitest';

import { imeiProblem, luhnValid } from './imei';

describe('a conferência do IMEI', () => {
  it('o caso real: um zero a mais no cadastro (16 dígitos); o IMEI de verdade passa', () => {
    expect(imeiProblem('8658930088121639')).toContain('tem 16 dígitos');
    expect(imeiProblem('865893088121639')).toBe('');
    expect(imeiProblem(' 865893088121639 ')).toBe('');
  });

  it('15 dígitos com o verificador errado; o que não tem cara de IMEI não é com ela', () => {
    expect(imeiProblem('865893008812163')).toContain('o último dígito');
    expect(imeiProblem('1234567890')).toBe('');
    expect(imeiProblem('')).toBe('');
    expect(imeiProblem('86589308812163a')).toBe('');
    expect(luhnValid('865893088121639')).toBe(true);
    expect(luhnValid('865893088121638')).toBe(false);
  });
});

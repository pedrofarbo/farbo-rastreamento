import { describe, expect, it } from 'vitest';

import { formatBoletoLine, gatewayName } from './PixCheckout';

describe('linha digitável', () => {
  it('sai em blocos, como no boleto impresso', () => {
    // A do sandbox da AbacatePay (out/2026): banco 001, fator 8789, R$ 50,00.
    expect(formatBoletoLine('00190000090275928800021932978170187890000005000')).toBe(
      '00190.00009 02759.288000 21932.978170 1 87890000005000',
    );
  });

  it('o que não tem 47 dígitos passa como veio', () => {
    expect(formatBoletoLine('123')).toBe('123');
    expect(formatBoletoLine('')).toBe('');
  });
});

describe('o gateway do pagamento', () => {
  it('Pagar.me ou AbacatePay, como na ficha do cliente', () => {
    expect(gatewayName({ provider: 'pagarme' })).toBe('Pagar.me');
    expect(gatewayName({ provider: 'abacatepay' })).toBe('AbacatePay');
  });
});

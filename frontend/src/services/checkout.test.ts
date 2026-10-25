import { describe, expect, it } from 'vitest';

import { OFFER } from '@/test/checkoutOffer';
import type { CheckoutOffer } from '@/types';

import { checkoutTotals, payDestination, vehicleName } from './checkout';

const offer: CheckoutOffer = OFFER;

describe('a conta da contratação pelo site', () => {
  it('comprado à vista, por conta própria: a 1ª mensalidade, o rastreador e o frete', () => {
    const t = checkoutTotals(offer, { insanos: false, equipmentMode: 'sale', installments: 1, farboInstall: false }, 2290);
    expect(t).toMatchObject({ monthlyCents: 3490, equipmentNowCents: 12000, installNowCents: 0, totalNowCents: 3490 + 12000 + 2290 });
  });

  it('comprado em 10x com a instalação da Farbo: a 1ª parcela na 1ª mensalidade', () => {
    const t = checkoutTotals(offer, { insanos: false, equipmentMode: 'sale', installments: 10, farboInstall: true });
    expect(t.parcels).toHaveLength(10);
    expect(t.firstMonthlyCents).toBe(3490 + 1200);
    expect(t.equipmentNowCents).toBe(0);
    expect(t.installNowCents).toBe(15000);
    expect(t.totalNowCents).toBe(3490 + 1200 + 15000);
  });

  it('Insanos em comodato: a mensalidade dele com a garantia de troca e a instalação do comodato', () => {
    const t = checkoutTotals(offer, { insanos: true, equipmentMode: 'loan', installments: 5, farboInstall: true });
    expect(t).toMatchObject({ monthlyCents: 2790 + 300, guaranteeCents: 300, parcels: [], equipmentNowCents: 0, installNowCents: 15000 });
    expect(t.totalNowCents).toBe(3090 + 15000);
  });

  it('o nome do veículo', () => {
    expect(vehicleName('CAR', ' Fiat ', 'Uno')).toBe('Fiat Uno');
    expect(vehicleName('MOTORCYCLE', '', '')).toBe('Minha moto');
  });
});

describe('o Pix depois da contratação', () => {
  it('a página de pagamento deste site abre sem recarregar, com a marca de cliente novo', () => {
    expect(payDestination('https://farborastreadores.com.br/pagar/abc?todas=1', 'https://farborastreadores.com.br')).toEqual({
      path: '/pagar/abc?todas=1&novo=1',
    });
  });

  it('outro endereço vai inteiro', () => {
    expect(payDestination('https://pagamento.exemplo.com/x?y=1', 'https://farborastreadores.com.br')).toEqual({
      url: 'https://pagamento.exemplo.com/x?y=1&novo=1',
    });
  });
});

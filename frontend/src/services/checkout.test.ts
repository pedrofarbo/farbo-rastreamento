import { describe, expect, it } from 'vitest';

import { OFFER, offerWith } from '@/test/checkoutOffer';
import type { CheckoutOffer } from '@/types';

import { checkoutTotals, payDestination, vehicleName } from './checkout';

const offer: CheckoutOffer = OFFER;

describe('a conta da contratação pelo site', () => {
  it('o rastreador incluso, por conta própria: a 1ª mensalidade e o frete (a parte dele já no preço do site)', () => {
    const t = checkoutTotals(offer, { equipmentMode: 'sale', farboInstall: false }, 2290);
    expect(t).toMatchObject({ monthlyCents: 3490, firstMonthlyCents: 3490, trackerMonths: 0, installNowCents: 0 });
    expect(t.totalNowCents).toBe(3490 + 2290);
  });

  it('com a instalação da Farbo: a instalação, hoje', () => {
    const t = checkoutTotals(offer, { equipmentMode: 'sale', farboInstall: true });
    expect(t.installNowCents).toBe(15000);
    expect(t.totalNowCents).toBe(3490 + 15000);
  });

  it('comodato: a mensalidade com a garantia de troca e a instalação do comodato, sem a parte do rastreador', () => {
    const table = offerWith({ tracker: { cents: 1200, months: 10, added: true } });
    const t = checkoutTotals(table, { equipmentMode: 'loan', farboInstall: true });
    expect(t).toMatchObject({ monthlyCents: 3490 + 300, guaranteeCents: 300, firstMonthlyCents: 3490 + 300, installNowCents: 15000 });
    expect(t.trackerMonths).toBe(0);
    expect(t.totalNowCents).toBe(3790 + 15000);
  });

  it('pelo link de um afiliado com tabela: a mensalidade do tipo do veículo, + a parte do rastreador nas primeiras', () => {
    const table = offerWith({
      planByKind: { motorcycleCents: 3990, carCents: 4990, truckCents: 5990 },
      tracker: { cents: 1200, months: 10, added: true },
    });
    const sale = { equipmentMode: 'sale', farboInstall: false } as const;
    expect(checkoutTotals(table, sale, 0, 'MOTORCYCLE')).toMatchObject({
      monthlyCents: 3990,
      firstMonthlyCents: 3990 + 1200,
      trackerMonths: 10,
      totalNowCents: 3990 + 1200,
    });
    expect(checkoutTotals(table, sale, 0, 'CAR').monthlyCents).toBe(4990);
    expect(checkoutTotals(table, sale, 0, 'TRUCK').firstMonthlyCents).toBe(5990 + 1200);
    expect(checkoutTotals(table, { ...sale, equipmentMode: 'loan' }, 0, 'TRUCK').firstMonthlyCents).toBe(5990 + 300);
  });

  it('pelo link de um representante: a instalação é paga direto a ele, fora do Pix', () => {
    const rep = offerWith({ installDirect: true, install: { enabled: true, priceCents: 12000 } });
    const sale = checkoutTotals(rep, { equipmentMode: 'sale', farboInstall: true });
    expect(sale).toMatchObject({ installNowCents: 0, totalNowCents: 3490 });
    const loan = checkoutTotals(rep, { equipmentMode: 'loan', farboInstall: false });
    expect(loan).toMatchObject({ installNowCents: 0, totalNowCents: 3490 + 300 });
  });

  it('o nome do veículo', () => {
    expect(vehicleName('CAR', ' Fiat ', 'Uno')).toBe('Fiat Uno');
    expect(vehicleName('MOTORCYCLE', '', '')).toBe('Minha moto');
    expect(vehicleName('TRUCK', '', '')).toBe('Meu caminhão');
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

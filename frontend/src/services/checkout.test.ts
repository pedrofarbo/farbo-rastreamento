import { describe, expect, it } from 'vitest';

import { OFFER, offerWith } from '@/test/checkoutOffer';
import type { CheckoutOffer } from '@/types';

import { blockingOffered, checkoutTotals, chosenInstall, payDestination, vehicleName } from './checkout';

const offer: CheckoutOffer = OFFER;

describe('a conta da contratação pelo site', () => {
  it('o semestral e o anual: o valor do período, com o rastreador incluso (nada somado); no comodato, com a garantia', () => {
    const periods = offerWith({
      tracker: { cents: 1200, months: 10, added: true },
      periods: [
        { months: 6, planName: 'Plano Semestral', cents: 20000, loanCents: 21800 },
        { months: 12, planName: 'Plano Anual', cents: 40000, loanCents: 43600 },
      ],
    });
    const t = checkoutTotals(periods, { equipmentMode: 'sale', farboInstall: true, periodMonths: 6 }, 2290);
    expect(t).toMatchObject({ monthlyCents: 20000, firstMonthlyCents: 20000, trackerMonths: 0, periodMonths: 6, installNowCents: 15000 });
    expect(t.totalNowCents).toBe(20000 + 15000 + 2290);
    expect(checkoutTotals(periods, { equipmentMode: 'sale', farboInstall: false, periodMonths: 12 }).totalNowCents).toBe(40000);
    // No comodato: o valor do período com a garantia de troca de cada mês, e a instalação do comodato.
    expect(checkoutTotals(periods, { equipmentMode: 'loan', farboInstall: false, periodMonths: 6 })).toMatchObject({
      periodMonths: 6, monthlyCents: 21800, guaranteeCents: 1800, installNowCents: 15000, totalNowCents: 21800 + 15000,
    });
    // Sem o período no link: o mensal.
    expect(checkoutTotals(offer, { equipmentMode: 'sale', farboInstall: false, periodMonths: 6 }).periodMonths).toBe(1);
  });

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

  it('a instalação com bloqueio, quando o link oferece: o preço dela no lugar da sem bloqueio', () => {
    const farbo = offerWith({
      install: { enabled: true, priceCents: 10000, blockingCents: 18000 },
      loan: { enabled: true, surchargeCents: 300, installFeeCents: 10000, uninstallFeeCents: 8500 },
    });
    expect(blockingOffered(farbo)).toBe(true);
    expect(blockingOffered(offerWith({ install: { enabled: true, priceCents: 10000 } }))).toBe(false);
    const sale = { equipmentMode: 'sale' as const, farboInstall: true };
    expect(chosenInstall(farbo, sale)).toEqual({ cents: 10000, blocking: false });
    expect(chosenInstall(farbo, { ...sale, installBlocking: true })).toEqual({ cents: 18000, blocking: true });
    expect(checkoutTotals(farbo, { ...sale, installBlocking: true })).toMatchObject({ installNowCents: 18000, totalNowCents: 3490 + 18000 });
    // Por conta própria, a escolha não vale.
    expect(chosenInstall(farbo, { ...sale, farboInstall: false, installBlocking: true })).toEqual({ cents: 0, blocking: false });
    // No comodato, a taxa da instalação é a com bloqueio.
    const loan = { equipmentMode: 'loan' as const, farboInstall: false, installBlocking: true };
    expect(checkoutTotals(farbo, loan)).toMatchObject({ installNowCents: 18000 });
    // Pelo link do representante, fora do Pix do mesmo jeito.
    const rep = { ...farbo, installDirect: true };
    expect(checkoutTotals(rep, { ...sale, installBlocking: true })).toMatchObject({ installNowCents: 0, totalNowCents: 3490 });
    expect(chosenInstall(rep, { ...sale, installBlocking: true })).toEqual({ cents: 18000, blocking: true });
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

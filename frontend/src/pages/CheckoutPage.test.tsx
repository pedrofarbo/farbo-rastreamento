// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import { saveReferral } from '@/services/referral';
import { offerWith } from '@/test/checkoutOffer';
import type { CheckoutInput, CheckoutOffer, ShippingQuoteView } from '@/types';

let offer: CheckoutOffer;
/** O link do afiliado com que cada oferta foi pedida. */
const offerRefs: (string | undefined)[] = [];
/** O link com que cada cotação do frete foi pedida, e a cotação devolvida. */
const shippingRefs: (string | undefined)[] = [];
let shippingView: ShippingQuoteView | null = null;
let placeResult: () => Promise<unknown>;
const placed: CheckoutInput[] = [];
/** Os cupons conferidos; o BEMVINDO vale (10% nas 3 primeiras), o VINTE dá R$ 20 na 1ª, os outros não. */
const couponCodes: string[] = [];
const logins: string[] = [];
const quote: ShippingQuoteView = {
  enabled: true, zipCode: '13010000', problem: '', arrange: false,
  quotes: [{ serviceId: 1, company: 'Correios', service: 'PAC', priceCents: 2290, deliveryDays: 7 }],
} as ShippingQuoteView;

vi.mock('@/api/resources', () => ({
  checkoutApi: {
    offer: async (ref?: string) => {
      offerRefs.push(ref);
      return offer;
    },
    shipping: async (_: unknown, ref?: string) => {
      shippingRefs.push(ref);
      return shippingView ?? quote;
    },
    place: (input: CheckoutInput) => {
      placed.push(input);
      return placeResult();
    },
    coupon: async (code: string) => {
      couponCodes.push(code);
      if (code === 'BEMVINDO') return { code, description: '', percent: 10, charges: 3 };
      if (code === 'VINTE') return { code, description: '', amountCents: 2000, charges: 1 };
      throw new ApiError(400, 'cupom não encontrado: confira o código');
    },
  },
}));
vi.mock('@/services/zipcode', () => ({ lookupZipCode: async () => ({ status: 'not-found' }) }));
vi.mock('@/stores/AuthContext', () => ({
  useAuth: () => ({
    user: null,
    completeLogin: (_: unknown, email: string) => {
      logins.push(email);
      return {};
    },
  }),
}));

import { CheckoutPage } from './CheckoutPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');

function PayStub() {
  const location = useLocation();
  return <p>Pix {location.pathname + location.search}</p>;
}

async function render(path = '/contratar') {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="/contratar" element={<CheckoutPage />} />
            <Route path="/pagar/:token" element={<PayStub />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

function type(el: HTMLInputElement | HTMLSelectElement, value: string) {
  const proto = el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(el, value);
  act(() => {
    el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  });
}

/** O campo pelo rótulo (o TextField liga o rótulo ao campo). */
function field(label: string): HTMLInputElement {
  const el = Array.from(document.querySelectorAll('label')).find((l) => l.textContent === label);
  return document.getElementById(el!.htmlFor) as HTMLInputElement;
}

const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) =>
    (b.textContent ?? '').replace(/\u00a0/g, ' ').trim().startsWith(label),
  ) as HTMLButtonElement;
const radio = (title: string) =>
  Array.from(document.querySelectorAll('label')).find((l) => l.textContent?.startsWith(title))!.querySelector('input')!;

async function click(el: HTMLElement) {
  await act(async () => el.click());
  await flush();
}

async function fillUntilPayment() {
  await click(button('Continuar'));
  type(field('Marca'), 'Honda');
  type(field('Modelo'), 'CG 160');
  type(field('Placa'), 'abc1d23');
  await click(button('Continuar'));
  type(field('Nome completo'), 'Lia Souza');
  type(field('CPF'), '52998224725');
  type(field('Celular (WhatsApp)'), '19988887777');
  type(field('E-mail'), 'lia@exemplo.com.br');
  type(field('Crie uma senha'), 'senha-forte');
  type(field('CEP'), '13010000');
  type(field('Rua'), 'Rua das Flores');
  type(field('Número'), '10');
  type(field('Bairro'), 'Centro');
  type(field('Cidade'), 'Campinas');
  type(field('UF') as unknown as HTMLSelectElement, 'SP');
  await click(button('Continuar'));
}

beforeEach(() => {
  offer = offerWith();
  placeResult = async () => ({
    payUrl: 'https://farborastreadores.com.br/pagar/tok123?todas=1',
    tokens: { accessToken: 'a', refreshToken: 'r', expiresAt: '', user: { email: 'lia@exemplo.com.br' } },
  });
  placed.length = 0;
  couponCodes.length = 0;
  logins.length = 0;
  offerRefs.length = 0;
  shippingRefs.length = 0;
  shippingView = null;
});
afterEach(() => {
  document.body.innerHTML = '';
});

describe('a contratação pelo site', () => {
  it('comodato: a garantia na mensalidade, a instalação e o frete no Pix; a conta entra e o Pix abre', async () => {
    // O link antigo do plano do Insanos abre o plano de sempre: só a central põe alguém nele.
    await render('/contratar?plano=insanos');
    expect(text()).not.toContain('Insanos');
    expect(radio('Plano Mensal').checked).toBe(true);
    await click(radio('Comodato (emprestado)'));
    // 34,90 + 3,00 de garantia; hoje, a 1ª mensalidade e a instalação (o frete depois).
    expect(text()).toContain('MensalidadeR$ 37,90/mês');
    expect(text()).toContain('Hoje, no PixR$ 187,90 + frete');
    expect(text()).not.toContain('A instalação');

    await fillUntilPayment();
    // A única entrega já vem marcada; o resumo com o frete.
    expect(text()).toContain('Correios PAC');
    expect(text()).toContain('Instalação pelos instaladores da FarboR$ 150,00');
    expect(text()).toContain('FreteR$ 22,90');
    expect(text()).toContain('Hoje, no PixR$ 210,80');
    expect(text()).toContain('R$ 3,00 dela é a garantia de troca do rastreador');
    expect(text()).toContain('a desinstalação custa R$ 85,00');
    expect(button('Pagar R$ 210,80 com Pix').disabled).toBe(true); // falta o aceite
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar R$ 210,80 com Pix'));

    expect(placed[0]).not.toHaveProperty('insanos');
    expect(placed[0]).not.toHaveProperty('installments');
    expect(placed[0]).toMatchObject({
      equipmentMode: 'loan', farboInstall: false,
      vehicle: { kind: 'MOTORCYCLE', name: 'Honda CG 160', brand: 'Honda', model: 'CG 160', plate: 'ABC1D23' },
      name: 'Lia Souza', email: 'lia@exemplo.com.br', document: '52998224725', password: 'senha-forte',
      address: expect.objectContaining({ zipCode: '13010000', city: 'Campinas', state: 'SP' }),
      shippingServiceId: 1, contractVersion: '7', accept: true, website: '',
    });
    expect(logins).toEqual(['lia@exemplo.com.br']);
    expect(text()).toContain('Pix /pagar/tok123?todas=1&novo=1');
  });

  it('o rastreador incluso com a instalação da Farbo: sem preço do rastreador nem parcelas', async () => {
    await render();
    expect(radio('Rastreador incluso').checked).toBe(true);
    expect(text()).toContain('Ele fica com você, e você só paga o frete.');
    expect(text()).toContain('Se encerrar antes da 10ª mensalidade, a parte do rastreador que falta (R$ 12,00 por mensalidade) vence de uma vez.');
    expect(text()).not.toMatch(/Comprar|à vista|parcel/);
    expect(document.querySelector('select')).toBeNull();
    await click(radio('Com os instaladores da Farbo'));
    // 34,90 + 150,00 da instalação.
    expect(text()).toContain('Hoje, no PixR$ 184,90 + frete');
    await fillUntilPayment();
    expect(text()).toContain('Rastreador (fica com você)incluso');
    expect(text()).toContain('O rastreador incluso fica com você. Se a assinatura for encerrada antes da 10ª mensalidade');
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(placed[0]).toMatchObject({ equipmentMode: 'sale', farboInstall: true });
    expect(placed[0]).not.toHaveProperty('installments');
  });

  it('o semestral e o anual pelo link: o valor do período no Pix, o rastreador no valor e o pedido com os meses', async () => {
    offer = offerWith({
      periods: [
        { months: 6, planName: 'Plano Semestral', cents: 20000, loanCents: 21800 },
        { months: 12, planName: 'Plano Anual', cents: 40000, loanCents: 43600 },
      ],
    });
    await render();
    expect(radio('Mensal').checked).toBe(true);
    expect(text()).toContain('SemestralR$ 200,00/semestreR$ 33,33 por mês, pagos de uma vez a cada 6 meses');
    expect(text()).toContain('AnualR$ 400,00/ano');
    await click(radio('Semestral'));
    expect(text()).toContain('Plano semestralR$ 200,00/semestre');
    expect(text()).toContain('Hoje, no PixR$ 200,00 + frete');
    expect(text()).toContain('Rastreador inclusono valor do plano');
    expect(text()).not.toContain('Se encerrar antes da 10ª mensalidade');
    // No comodato, o semestral continua: o valor com a garantia de troca de cada mês.
    await click(radio('Comodato'));
    expect(radio('Semestral').checked).toBe(true);
    expect(text()).toContain('SemestralR$ 218,00/semestre');
    expect(text()).toContain('com a garantia de troca do rastreador emprestado');
    expect(text()).toContain('Comodato (emprestado)+R$ 18,00 no semestre');
    await click(radio('Rastreador incluso'));
    expect(radio('Semestral').checked).toBe(true);
    await click(radio('Anual'));
    await fillUntilPayment();
    expect(text()).toContain('1º ano · Plano AnualR$ 400,00');
    expect(text()).toContain('Depois, R$ 400,00 a cada 12 meses, no dia 10');
    expect(text()).toContain('não há devolução');
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(placed[0]).toMatchObject({ equipmentMode: 'sale', periodMonths: 12 });
  });

  it('o semestral no comodato: a garantia de cada mês no valor, a instalação no Pix e a desinstalação no fim', async () => {
    offer = offerWith({ periods: [{ months: 6, planName: 'Plano Semestral', cents: 20000, loanCents: 21800 }] });
    await render();
    await click(radio('Comodato'));
    await click(radio('Semestral'));
    expect(text()).toContain('Hoje, no PixR$ 368,00 + frete');
    await fillUntilPayment();
    expect(text()).toContain('1º semestre · Plano SemestralR$ 218,00');
    expect(text()).toContain('Depois, R$ 218,00 a cada 6 meses, no dia 10');
    expect(text()).toContain('O rastreador é emprestado; R$ 18,00 do valor é a garantia de troca dele');
    expect(text()).toContain('no fim, a desinstalação custa R$ 85,00');
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(placed[0]).toMatchObject({ equipmentMode: 'loan', periodMonths: 6, farboInstall: false });
  });

  it('sem o semestral e o anual no link: só o mensal, e o pedido sem os meses', async () => {
    await render();
    expect(text()).not.toContain('Como pagar');
    await fillUntilPayment();
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(placed[0]).not.toHaveProperty('periodMonths');
  });

  it('pelo link de um afiliado com tabela: o tipo do veículo dá a mensalidade', async () => {
    saveReferral('lia');
    // Pela tabela, a parte do rastreador incluso soma nas 10 primeiras.
    offer = offerWith({
      planByKind: { motorcycleCents: 3990, carCents: 4990, truckCents: 5990 },
      tracker: { cents: 1200, months: 10, added: true },
    });
    await render();
    expect(offerRefs).toContain('lia');
    expect(text()).toContain('Plano Mensal · MotoR$ 39,90/mês+ R$ 12,00/mês nas 10 primeiras mensalidades, com o rastreador incluso.');
    expect(text()).toContain('Plano Mensal · CarroR$ 49,90/mês');
    expect(text()).toContain('Rastreador incluso+R$ 12,00/mês nas 10 primeiras');
    expect(text()).toContain('Mensalidade, nas 10 primeirasR$ 51,90/mêsDepoisR$ 39,90/mês');
    await click(radio('Plano Mensal · Caminhão'));
    expect(text()).toContain('Mensalidade, nas 10 primeirasR$ 71,90/mêsDepoisR$ 59,90/mês');
    // Na etapa do veículo, o tipo é o do plano: sem trocar por moto ou carro ali.
    await click(button('Continuar'));
    expect(text()).toContain('Caminhão, o tipo do plano escolhido.');
    expect(Array.from(document.querySelectorAll('input[name="kind"]'))).toHaveLength(0);
    // "Mudar o plano" volta à etapa do plano.
    await click(button('Mudar o plano'));
    expect(radio('Plano Mensal · Caminhão').checked).toBe(true);
    await fillUntilPayment();
    expect(text()).toContain('1ª mensalidade · Plano Mensal · CaminhãoR$ 71,90');
    expect(text()).toContain('Depois, mais 9 mensalidades de R$ 71,90 e, a partir da 11ª, R$ 59,90/mês, no dia 10');
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(placed[0]).toMatchObject({ vehicle: expect.objectContaining({ kind: 'TRUCK' }), ref: 'lia' });
    localStorage.clear();
  });

  it('pelo link de um representante: a instalação e a desinstalação com ele, pagas direto, fora do Pix', async () => {
    offer = offerWith({
      installDirect: true,
      install: { enabled: true, priceCents: 12000 },
      loan: { enabled: true, surchargeCents: 300, installFeeCents: 12000, uninstallFeeCents: 8000 },
    });
    await render();
    expect(text()).toContain('Comodato (emprestado)+R$ 3,00/mês');
    expect(text()).toContain('Instalação pelo representante por R$ 120,00, paga direto a ele');
    expect(text()).toContain('desinstalação de R$ 80,00 ao encerrar, também direto a ele');
    await click(radio('Com o representante'));
    expect(text()).toContain('Pago direto a ele na instalação, fora do Pix.');
    // Hoje, só a 1ª mensalidade (o rastreador vem incluso).
    expect(text()).toContain('Hoje, no PixR$ 34,90 + frete');
    await fillUntilPayment();
    expect(text()).not.toContain('Instalação pelos instaladores da Farbo');
    expect(text()).toContain('A instalação (R$ 120,00) é paga direto ao representante, no dia, fora do Pix.');
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(placed[0]).toMatchObject({ equipmentMode: 'sale', farboInstall: true });
  });

  it('pelo link do representante com a instalação com bloqueio: o cliente escolhe, e o pedido vai com a escolha', async () => {
    offer = offerWith({
      installDirect: true,
      equipmentModes: 'sale',
      loan: null,
      install: { enabled: true, priceCents: 10000, blockingCents: 18000 },
    });
    await render();
    expect(text()).not.toContain('Com o representanteR$');
    expect(text()).toContain('Com o representante, sem bloqueioR$ 100,00');
    expect(text()).toContain('Com o representante, com bloqueioR$ 180,00');
    await click(radio('Com o representante, com bloqueio'));
    expect(text()).toContain('Também o relé que bloqueia o motor pelo app');
    // Pago direto a ele: hoje, só a 1ª mensalidade.
    expect(text()).toContain('Hoje, no PixR$ 34,90 + frete');
    await fillUntilPayment();
    expect(text()).toContain('A instalação com bloqueio (R$ 180,00) é paga direto ao representante');
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(placed[0]).toMatchObject({ equipmentMode: 'sale', farboInstall: true, installBlocking: true });
  });

  it('a instalação sem bloqueio, ou por conta própria depois de marcar a com bloqueio: o pedido vai sem ela', async () => {
    offer = offerWith({ installDirect: true, install: { enabled: true, priceCents: 10000, blockingCents: 18000 } });
    await render();
    await click(radio('Com o representante, com bloqueio'));
    await click(radio('Por conta própria'));
    await fillUntilPayment();
    expect(text()).not.toContain('paga direto ao representante');
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(placed[0]).toMatchObject({ farboInstall: false });
    expect(placed[0]).not.toHaveProperty('installBlocking');
  });

  it('pelo link do representante que entrega em mãos: sem frete', async () => {
    saveReferral('rui');
    offer = offerWith({ handDelivery: 'Rui Representante' });
    shippingView = { enabled: true, zipCode: '13010000', problem: '', arrange: true, quotes: [], handBy: 'Rui Representante' };
    await render();
    expect(text()).toContain('Hoje, no PixR$ 34,90');
    expect(text()).not.toContain('+ frete');
    await fillUntilPayment();
    expect(shippingRefs).toContain('rui');
    expect(text()).toContain('Em mãos com o representante Rui Representante');
    expect(text()).toContain('sem frete');
    expect(text()).not.toContain('Frete');
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar R$ 34,90 com Pix'));
    expect(placed[0]).toMatchObject({ arrangeDelivery: true, ref: 'rui' });
    localStorage.clear();
  });

  it('pelo link só de comodato: sem o incluso, o comodato já marcado', async () => {
    offer = offerWith({ equipmentModes: 'loan' });
    await render();
    expect(text()).not.toContain('Rastreador incluso');
    expect(radio('Comodato (emprestado)').checked).toBe(true);
    expect(text()).toContain('MensalidadeR$ 37,90/mês');
    await fillUntilPayment();
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(placed[0]).toMatchObject({ equipmentMode: 'loan', farboInstall: false });
  });

  it('comodato sem garantia de troca (a do afiliado em zero): sem taxa mensal', async () => {
    offer = offerWith({ loan: { enabled: true, surchargeCents: 0, installFeeCents: 15000, uninstallFeeCents: 8500 } });
    await render();
    expect(text()).toContain('Comodato (emprestado)sem taxa mensal');
    expect(text()).not.toContain('garantia de troca do rastreador na mensalidade');
    await click(radio('Comodato (emprestado)'));
    expect(text()).toContain('MensalidadeR$ 34,90/mês');
  });

  it('do QR Code de um evento: vai com o evento e sem o link de indicação guardado no aparelho', async () => {
    saveReferral('equipe-farbo');
    await render('/contratar?evento=encontro-sp');
    await fillUntilPayment();
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(placed[0]).toMatchObject({ event: 'encontro-sp' });
    expect(placed[0].ref).toBeUndefined();
    expect(offerRefs).toEqual(['']);
    localStorage.clear();
  });

  it('confere o CPF e recusa o CNPJ antes de seguir', async () => {
    await render();
    await click(button('Continuar'));
    type(field('Marca'), 'Fiat');
    await click(button('Continuar'));
    type(field('Nome completo'), 'Empresa');
    type(field('CPF'), '11222333000181');
    await click(button('Continuar'));
    expect(text()).toContain('A contratação pelo site é para pessoa física');
    type(field('CPF'), '52998224726');
    await click(button('Continuar'));
    expect(text()).toContain('Confira o CPF.');
  });

  it('e-mail com conta: leva ao login', async () => {
    placeResult = async () => {
      throw new ApiError(409, 'este e-mail já tem conta', { code: 'EMAIL_TAKEN' });
    };
    await render();
    await fillUntilPayment();
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(text()).toContain('Este e-mail já tem conta.');
    expect(document.querySelector('a[href="/login"]')).not.toBeNull();
    expect(logins).toEqual([]);
  });

  it('contratação ainda esperando o pagamento: o link do pedido já feito', async () => {
    placeResult = async () => {
      throw new ApiError(409, 'você já contratou com este e-mail e o pagamento vale até 16:39 de 04/11: pague o pedido que já fez', {
        code: 'CHECKOUT_UNPAID', payUrl: 'https://painel.farbo.test/pagar/abc?todas=1',
      });
    };
    await render();
    await fillUntilPayment();
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(text()).toContain('Você já contratou com este e-mail e o pagamento vale até 16:39 de 04/11');
    expect(document.querySelector('a[href="https://painel.farbo.test/pagar/abc?todas=1"]')?.textContent).toBe('Pagar o pedido já feito');
    expect(logins).toEqual([]);
  });

  it('sem cupom valendo (ou pelo link de um representante): o campo do cupom não aparece', async () => {
    await render('/contratar?cupom=BEMVINDO');
    await fillUntilPayment();
    expect(text()).not.toContain('Cupom de desconto');
    expect(couponCodes).toEqual([]);
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar R$ 57,80 com Pix'));
    expect(placed[0]).not.toHaveProperty('coupon');
  });

  it('o cupom: o código errado diz o motivo; o certo abate a 1ª mensalidade no Pix e vai no pedido; o Enter não paga', async () => {
    offer = offerWith({ coupons: true });
    await render();
    await fillUntilPayment();
    // 34,90 + 22,90 de frete.
    expect(button('Pagar R$ 57,80 com Pix')).toBeTruthy();
    type(field('Cupom de desconto'), 'errado');
    await click(button('Aplicar'));
    expect(couponCodes).toEqual(['ERRADO']);
    expect(text()).toContain('Cupom não encontrado: confira o código.');
    type(field('Cupom de desconto'), 'bemvindo');
    expect(text()).not.toContain('Cupom não encontrado');
    // O Enter no campo aplica o cupom, sem enviar o pedido.
    await act(async () => {
      field('Cupom de desconto').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    });
    await flush();
    expect(placed).toEqual([]);
    expect(text()).toContain('Cupom BEMVINDO: 10% de desconto nas 3 primeiras cobranças.');
    // 10% de 34,90 = 3,49: a 1ª mensalidade por 31,41.
    expect(text()).toContain('1ª mensalidade · Plano MensalR$ 34,90');
    expect(text()).toContain('Cupom BEMVINDO−R$ 3,49');
    expect(text()).toContain('Hoje, no PixR$ 54,31');
    expect(text()).toContain('Depois, mais 2 mensalidades de R$ 31,41, com o cupom, e, a partir da 4ª, R$ 34,90/mês, no dia 10.');
    // Tirar e pôr de novo.
    await click(button('Remover'));
    expect(button('Pagar R$ 57,80 com Pix')).toBeTruthy();
    expect(field('Cupom de desconto').value).toBe('');
    type(field('Cupom de desconto'), 'BEMVINDO');
    await click(button('Aplicar'));
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar R$ 54,31 com Pix'));
    expect(placed[0]).toMatchObject({ coupon: 'BEMVINDO', equipmentMode: 'sale' });
  });

  it('o cupom do link (?cupom=) já vem aplicado, desde o plano; no anual, o valor fixo sai do período', async () => {
    offer = offerWith({ coupons: true, periods: [{ months: 12, planName: 'Plano Anual', cents: 40000 }] });
    await render('/contratar?cupom=VINTE');
    expect(couponCodes).toEqual(['VINTE']);
    expect(text()).toContain('Cupom de desconto−R$ 20,00');
    expect(text()).toContain('Hoje, no PixR$ 14,90 + frete');
    await click(radio('Anual'));
    expect(text()).toContain('Hoje, no PixR$ 380,00 + frete');
    await fillUntilPayment();
    expect(text()).toContain('Cupom VINTE: R$ 20,00 de desconto na 1ª cobrança.');
    expect(text()).toContain('1º ano · Plano AnualR$ 400,00');
    expect(text()).toContain('Hoje, no PixR$ 402,90');
    expect(text()).toContain('Depois, R$ 400,00 a cada 12 meses');
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar R$ 402,90 com Pix'));
    expect(placed[0]).toMatchObject({ coupon: 'VINTE', periodMonths: 12 });
  });

  it('fechada na central: chama para deixar o contato', async () => {
    offer = { ...offer, enabled: false };
    await render();
    expect(text()).toContain('A contratação pelo site está fechada agora');
    expect(document.querySelector('a[href="/#contratar"]')).not.toBeNull();
  });
});

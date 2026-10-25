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
let placeResult: () => Promise<unknown>;
const placed: CheckoutInput[] = [];
const logins: string[] = [];
const quote: ShippingQuoteView = {
  enabled: true, zipCode: '13010000', problem: '', arrange: false,
  quotes: [{ serviceId: 1, company: 'Correios', service: 'PAC', priceCents: 2290, deliveryDays: 7 }],
} as ShippingQuoteView;

vi.mock('@/api/resources', () => ({
  checkoutApi: {
    offer: async () => offer,
    shipping: async () => quote,
    place: (input: CheckoutInput) => {
      placed.push(input);
      return placeResult();
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
  logins.length = 0;
});
afterEach(() => {
  document.body.innerHTML = '';
});

describe('a contratação pelo site', () => {
  it('Insanos em comodato: a garantia na mensalidade, a instalação e o frete no Pix; a conta entra e o Pix abre', async () => {
    await render('/contratar?plano=insanos');
    expect(radio('Sou integrante do Insanos MC').checked).toBe(true);
    await click(radio('Comodato (emprestado)'));
    // 27,90 + 3,00 de garantia; hoje, a 1ª mensalidade e a instalação (o frete depois).
    expect(text()).toContain('MensalidadeR$ 30,90/mês');
    expect(text()).toContain('Hoje, no PixR$ 180,90 + frete');
    expect(text()).not.toContain('A instalação');

    await fillUntilPayment();
    // A única entrega já vem marcada; o resumo com o frete.
    expect(text()).toContain('Correios PAC');
    expect(text()).toContain('Instalação pelos instaladores da FarboR$ 150,00');
    expect(text()).toContain('FreteR$ 22,90');
    expect(text()).toContain('Hoje, no PixR$ 203,80');
    expect(text()).toContain('R$ 3,00 dela é a garantia de troca do rastreador');
    expect(text()).toContain('a desinstalação custa R$ 85,00');
    expect(button('Pagar R$ 203,80 com Pix').disabled).toBe(true); // falta o aceite
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar R$ 203,80 com Pix'));

    expect(placed[0]).toMatchObject({
      insanos: true, equipmentMode: 'loan', installments: 1, farboInstall: false,
      vehicle: { kind: 'MOTORCYCLE', name: 'Honda CG 160', brand: 'Honda', model: 'CG 160', plate: 'ABC1D23' },
      name: 'Lia Souza', email: 'lia@exemplo.com.br', document: '52998224725', password: 'senha-forte',
      address: expect.objectContaining({ zipCode: '13010000', city: 'Campinas', state: 'SP' }),
      shippingServiceId: 1, contractVersion: '7', accept: true, website: '',
    });
    expect(logins).toEqual(['lia@exemplo.com.br']);
    expect(text()).toContain('Pix /pagar/tok123?todas=1&novo=1');
  });

  it('comprado em 10x com a instalação da Farbo', async () => {
    await render();
    type(document.querySelector('select') as unknown as HTMLSelectElement, '10');
    await click(radio('Com os instaladores da Farbo'));
    // 34,90 + 12,00 da 1ª parcela + 150,00 da instalação.
    expect(text()).toContain('Hoje, no PixR$ 196,90 + frete');
    await fillUntilPayment();
    expect(text()).toContain('Rastreador, 1ª de 10 parcelas');
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(placed[0]).toMatchObject({ insanos: false, equipmentMode: 'sale', installments: 10, farboInstall: true });
  });

  it('do QR Code de um evento: vai com o evento e sem o link de indicação guardado no aparelho', async () => {
    saveReferral('equipe-farbo');
    await render('/contratar?evento=encontro-sp');
    await fillUntilPayment();
    await click(document.querySelector('input[type="checkbox"]') as HTMLInputElement);
    await click(button('Pagar'));
    expect(placed[0]).toMatchObject({ event: 'encontro-sp' });
    expect(placed[0].ref).toBeUndefined();
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

  it('fechada na central: chama para deixar o contato', async () => {
    offer = { ...offer, enabled: false };
    await render();
    expect(text()).toContain('A contratação pelo site está fechada agora');
    expect(document.querySelector('a[href="/#contratar"]')).not.toBeNull();
  });
});

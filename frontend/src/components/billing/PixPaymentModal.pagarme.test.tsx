// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { PixApi } from '@/api/resources';
import { ToastProvider } from '@/components/ui/Toast';
import type { CardPayInput, CardView, PixCharge } from '@/types';

// O iframe da AbacatePay não pode abrir: na Pagar.me os campos são daqui.
vi.mock('@/services/abacatepayCard', async (original) => ({
  ...(await original<typeof import('@/services/abacatepayCard')>()),
  loadAbacatePay: async () => {
    throw new Error('o formulário da AbacatePay não devia abrir');
  },
}));

import { PixPaymentModal } from './PixPaymentModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/\u00a0/g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.replace(/\u00a0/g, ' ') === label) as HTMLButtonElement;
const radio = (label: string) =>
  Array.from(document.querySelectorAll('[role="radio"]')).find((b) => b.textContent?.startsWith(label)) as HTMLButtonElement;
const field = (label: string) => document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!;
const type = async (input: HTMLInputElement, value: string) =>
  act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });

const payer = { name: 'Ana Pagar', email: 'ana@frota.test', taxId: '52998224725', cellphone: '11999990000' };
const charge = (over: Partial<PixCharge>): PixCharge =>
  ({ id: 'c1', invoiceId: 'i1', provider: 'pagarme', method: 'PIX', amountCents: 4990, status: 'PENDING', brCode: '000201',
    qrCodeImage: 'data:,', devMode: false, expiresAt: null, invoiceStatus: 'OPEN', boletoLine: '', boletoUrl: '', cardBrand: '',
    cardLast4: '', ...over }) as PixCharge;

let cardView: CardView;
let cardPays: CardPayInput[] = [];
const tokenCalls: { url: string; body: unknown }[] = [];
let tokenStatus = 200;
const api: PixApi = {
  invoicePix: async () => charge({}),
  invoiceBoleto: async () => charge({ id: 'b1', method: 'BOLETO' }),
  invoiceCard: async (_id, input) => {
    cardPays.push(input);
    return charge({ id: 'k1', method: 'CARD', status: 'PAID', invoiceStatus: 'PAID', cardBrand: 'Visa', cardLast4: '0010' });
  },
  card: async () => cardView,
  charge: async (id) => charge({ id }),
  simulateCharge: async () => charge({ status: 'PAID' }),
};

beforeEach(() => {
  vi.stubGlobal('fetch', async (url: string, init: RequestInit) => {
    tokenCalls.push({ url, body: JSON.parse(String(init.body)) });
    return new Response(JSON.stringify(tokenStatus === 200 ? { id: 'token_abc', card: { brand: 'Visa', last_four_digits: '0010' } } : {}), {
      status: tokenStatus,
    });
  });
});

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ToastProvider>
          <PixPaymentModal
            invoice={{ id: 'i1', description: 'Plano Frota — novembro/2026', amountCents: 4990 }}
            api={api}
            audience="customer"
            onClose={() => undefined}
            onPaid={() => undefined}
          />
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  cardPays = [];
  tokenCalls.length = 0;
  tokenStatus = 200;
  vi.unstubAllGlobals();
});

describe('cartão pela Pagar.me', () => {
  it('os campos aqui, o token direto na Pagar.me e só o token vai ao servidor', async () => {
    cardView = { enabled: true, provider: 'pagarme', publicKey: 'pk_test_x', saved: null, payer };
    await render();
    await act(async () => radio('Cartão').click());
    await flush();
    expect(text()).toContain('vão direto para a Pagar.me');
    expect(button('Pagar R$ 49,90').disabled).toBe(true);

    await type(field('Número do cartão'), '4000000000000011');
    await type(field('Nome impresso no cartão'), 'Ana Pagar');
    await type(field('Validade (MM/AA)'), '1230');
    await type(field('CVV'), '123');
    expect(field('Número do cartão').value).toBe('4000 0000 0000 0011');
    expect(field('Validade (MM/AA)').value).toBe('12/30');
    // Número que não passa no dígito verificador: nem vai à Pagar.me.
    await act(async () => button('Pagar R$ 49,90').click());
    await flush();
    expect(text()).toContain('Confira o número do cartão');
    expect(tokenCalls).toHaveLength(0);

    await type(field('Número do cartão'), '4000000000000010');
    await act(async () => button('Pagar R$ 49,90').click());
    await flush();
    await flush();
    expect(tokenCalls).toHaveLength(1);
    expect(tokenCalls[0].url).toBe('https://api.pagar.me/core/v5/tokens?appId=pk_test_x');
    expect(tokenCalls[0].body).toEqual({
      type: 'card',
      card: { number: '4000000000000010', holder_name: 'ANA PAGAR', exp_month: 12, exp_year: 2030, cvv: '123' },
    });
    expect(cardPays).toEqual([{ token: 'token_abc', save: false, autoDebit: false }]);
    expect(text()).toContain('Pagamento recebido');
  });

  it('a Pagar.me recusa os dados: avisa e não cobra', async () => {
    cardView = { enabled: true, provider: 'pagarme', publicKey: 'pk_test_x', saved: null, payer };
    tokenStatus = 422;
    await render();
    await act(async () => radio('Cartão').click());
    await flush();
    await type(field('Número do cartão'), '4000000000000010');
    await type(field('Nome impresso no cartão'), 'Ana Pagar');
    await type(field('Validade (MM/AA)'), '1230');
    await type(field('CVV'), '123');
    await act(async () => button('Pagar R$ 49,90').click());
    await flush();
    expect(text()).toContain('A Pagar.me recusou os dados do cartão');
    expect(cardPays).toHaveLength(0);
  });

  it('sem endereço no cadastro: explica antes de abrir os campos', async () => {
    cardView = { enabled: true, provider: 'pagarme', addressMissing: true, publicKey: 'pk_test_x', saved: null, payer };
    await render();
    await act(async () => radio('Cartão').click());
    await flush();
    expect(text()).toContain('o cadastro da conta precisa do endereço');
    expect(document.querySelector('input[aria-label="Número do cartão"]')).toBeNull();
  });
});

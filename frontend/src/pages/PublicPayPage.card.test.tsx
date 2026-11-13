// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { CardPayInput, CardView, PixCharge, PublicInvoice } from '@/types';

const cardPays: [string, CardPayInput][] = [];
let cardView: CardView;
const charge = (over: Partial<PixCharge> = {}): PixCharge =>
  ({
    id: 'c1', invoiceId: 'i1', method: 'PIX', amountCents: 6990, status: 'PENDING', brCode: '000201', qrCodeImage: 'data:,',
    devMode: false, expiresAt: null, paidAt: null, invoiceStatus: 'OPEN', ...over,
  }) as unknown as PixCharge;

vi.mock('@/api/resources', () => ({
  GROUP_SUFFIX: ':todas',
  payLinkApi: {
    invoice: async (): Promise<PublicInvoice> => ({
      firstName: 'Lia', description: 'Plano Mensal — outubro/2026', amountCents: 6990, dueDate: '2026-10-10', status: 'OPEN',
      overdue: false, daysOverdue: 0, paidAt: null, onlinePayment: true, paymentUrl: '', pixCode: '',
    }),
    pix: () => ({
      invoicePix: async () => charge(),
      invoiceBoleto: async () => charge({ id: 'b1', method: 'BOLETO' }),
      card: async () => cardView,
      invoiceCard: async (key: string, input: CardPayInput) => {
        cardPays.push([key, input]);
        return charge({ id: 'k1', method: 'CARD', status: 'PAID', invoiceStatus: 'PAID' });
      },
      cardKey: 'fatura:tok123',
      charge: async (id: string) => charge({ id }),
      simulateCharge: async () => charge({ status: 'PAID' }),
    }),
  },
}));
// A Pagar.me de mentira: o token do cartão.
vi.mock('@/services/pagarmeCard', async (original) => ({
  ...(await original<typeof import('@/services/pagarmeCard')>()),
  tokenizePagarmeCard: async () => ({ token: 'token_pm_1' }),
}));

import { PublicPayPage } from './PublicPayPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const radio = (label: string) =>
  Array.from(document.querySelectorAll('[role="radio"]')).find((b) => b.textContent?.startsWith(label)) as HTMLButtonElement;
const input = (label: string) => document.querySelector(`input[aria-label="${label}"]`) as HTMLInputElement;
function type(el: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value);
  act(() => {
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ToastProvider>
          <MemoryRouter initialEntries={['/pagar/tok123']}>
            <Routes>
              <Route path="/pagar/:token" element={<PublicPayPage />} />
            </Routes>
          </MemoryRouter>
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
  await flush();
}

const READY: CardView = { enabled: true, provider: 'pagarme', publicKey: 'pk_test_x', saved: null, payer: null, payerReady: true, publicLink: true };

afterEach(() => {
  document.body.innerHTML = '';
  cardPays.length = 0;
});

describe('o cartão no link de pagamento (sem login)', () => {
  it('os campos do cartão, sem salvar nem débito automático; paga só com o token', async () => {
    cardView = READY;
    await render();
    await act(async () => radio('Cartão').click());
    await flush();
    expect(text()).not.toContain('Débito automático');
    type(input('Número do cartão'), '4111111111111111');
    type(input('Nome impresso no cartão'), 'LIA MARTINS');
    type(input('Validade (MM/AA)'), '1230');
    type(input('CVV'), '123');
    const pay = Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.replace(/ /g, ' ') === 'Pagar R$ 69,90')!;
    await act(async () => pay.click());
    await flush();
    expect(cardPays).toEqual([['tok123', { token: 'token_pm_1' }]]);
  });

  it('sem os dados que o cartão pede no cadastro: explica, sem o formulário', async () => {
    cardView = { ...READY, payerReady: false };
    await render();
    await act(async () => radio('Cartão').click());
    await flush();
    expect(text()).toContain('Para pagar com cartão por este link, o cadastro da conta precisa do CPF ou CNPJ');
    expect(input('Número do cartão')).toBeNull();
  });
});

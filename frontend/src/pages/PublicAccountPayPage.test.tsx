// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import { ToastProvider } from '@/components/ui/Toast';
import type { PixCharge, PublicAccountPay } from '@/types';

let view: () => Promise<PublicAccountPay>;
let chargeStatus = 'PENDING';
const calls: string[] = [];
const charge = (status: string): PixCharge =>
  ({
    id: 'c1', invoiceId: 'i1', amountCents: 21970, status, brCode: '00020101-todas', qrCodeImage: 'data:image/png;base64,AAAA',
    devMode: false, expiresAt: null, paidAt: null, invoiceStatus: status === 'PAID' ? 'PAID' : 'OPEN',
  }) as unknown as PixCharge;

vi.mock('@/api/resources', () => ({
  accountPayApi: {
    view: (token: string) => {
      calls.push('conta ' + token);
      return view();
    },
    pix: (token: string) => ({
      invoicePix: async () => {
        calls.push('pix ' + token);
        return charge('PENDING');
      },
      invoiceBoleto: async () => charge('PENDING'),
      charge: async () => charge(chargeStatus),
      simulateCharge: async () => charge('PAID'),
    }),
  },
}));

import { PublicAccountPayPage } from './PublicAccountPayPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');

const OPEN: PublicAccountPay = {
  firstName: 'Lia',
  amountCents: 21970,
  onlinePayment: true,
  items: [
    { description: 'Plano Mensal — outubro/2026', amountCents: 6990, dueDate: '2026-10-10', overdue: true, daysOverdue: 34 },
    { description: 'Plano Mensal — novembro/2026', amountCents: 6990, dueDate: '2026-11-10', overdue: true, daysOverdue: 3 },
    { description: 'Instalação do rastreador', amountCents: 7990, dueDate: '2026-11-20', overdue: false, daysOverdue: 0 },
  ],
};

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ToastProvider>
          <MemoryRouter initialEntries={['/pagar/conta/conta123']}>
            <Routes>
              <Route path="/pagar/conta/:token" element={<PublicAccountPayPage />} />
            </Routes>
          </MemoryRouter>
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
  chargeStatus = 'PENDING';
});

describe('link de todas as faturas em aberto', () => {
  it('as faturas de vários vencimentos, o total e um Pix só; pago, agradece', async () => {
    view = async () => OPEN;
    await render();
    expect(text()).toContain('Olá, Lia!');
    expect(text()).toContain('3 faturas em aberto · R$ 219,70');
    expect(text()).toContain('Plano Mensal — outubro/2026 · vencida há 34 dias');
    expect(text()).toContain('Instalação do rastreador · vence em 20/11/2026');
    expect(text()).toContain('3 faturas num pagamento só');
    expect(calls).toEqual(['conta conta123', 'pix conta123']);
    expect((document.querySelector('input[aria-label="Pix copia e cola"]') as HTMLInputElement).value).toBe('00020101-todas');
    expect(document.querySelector('meta[name="robots"]')?.getAttribute('content')).toBe('noindex, nofollow');
    chargeStatus = 'PAID';
    view = async () => ({ ...OPEN, amountCents: 0, items: [] });
    await act(async () => new Promise((resolve) => setTimeout(resolve, 4200)));
    await flush();
    expect(text()).toContain('Pagamento recebido. Obrigado!');
  }, 10_000);

  it('tudo pago: avisa, sem Pix; o link errado diz que não existe', async () => {
    view = async () => ({ ...OPEN, amountCents: 0, items: [] });
    await render();
    expect(text()).toContain('Está tudo pago');
    expect(text()).toContain('Lia, não há nenhuma fatura em aberto');
    expect(calls).toEqual(['conta conta123']);
    document.body.innerHTML = '';
    view = async () => {
      throw new ApiError(404, 'link de pagamento não encontrado');
    };
    await render();
    expect(text()).toContain('Link de pagamento não encontrado');
  });
});

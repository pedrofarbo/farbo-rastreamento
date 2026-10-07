// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import { ToastProvider } from '@/components/ui/Toast';
import type { PixCharge, PublicInvoice } from '@/types';

let invoice: () => Promise<PublicInvoice>;
let chargeStatus = 'PENDING';
const calls: string[] = [];
const charge = (status: string): PixCharge =>
  ({
    id: 'c1', invoiceId: 'i1', amountCents: 6990, status, brCode: '00020101-copia-e-cola',
    qrCodeImage: 'data:image/png;base64,AAAA', devMode: false, expiresAt: null, paidAt: null,
    invoiceStatus: status === 'PAID' ? 'PAID' : 'OPEN',
  }) as unknown as PixCharge;

vi.mock('@/api/resources', () => ({
  payLinkApi: {
    invoice: (token: string) => {
      calls.push('fatura ' + token);
      return invoice();
    },
    pix: (token: string) => ({
      invoicePix: async () => {
        calls.push('pix ' + token);
        return charge('PENDING');
      },
      charge: async () => charge(chargeStatus),
      simulateCharge: async () => charge('PAID'),
    }),
  },
}));

import { PublicPayPage } from './PublicPayPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/\u00a0/g, ' ');
const open = (over: Partial<PublicInvoice> = {}): PublicInvoice => ({
  firstName: 'Lia', description: 'Plano Mensal — outubro/2026', amountCents: 6990, dueDate: '2026-10-10',
  status: 'OPEN', overdue: false, daysOverdue: 0, paidAt: null, onlinePayment: true, paymentUrl: '', pixCode: '',
  ...over,
});

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

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
  chargeStatus = 'PENDING';
});

describe('link de pagamento da fatura', () => {
  it('abre o Pix sem login e mostra quando cai', async () => {
    invoice = async () => open();
    await render();
    expect(text()).toContain('Olá, Lia!');
    expect(text()).toContain('Plano Mensal — outubro/2026');
    expect(text()).toContain('R$ 69,90');
    expect(text()).toContain('vence em 10/10/2026');
    expect(calls).toEqual(['fatura tok123', 'pix tok123']);
    expect((document.querySelector('input[aria-label="Pix copia e cola"]') as HTMLInputElement).value).toBe('00020101-copia-e-cola');
    expect(document.querySelector('meta[name="robots"]')?.getAttribute('content')).toBe('noindex, nofollow');
    // O pagamento cai: a página recarrega a fatura e agradece.
    chargeStatus = 'PAID';
    invoice = async () => open({ status: 'PAID', paidAt: '2026-10-10T13:00:00Z' });
    await act(async () => new Promise((resolve) => setTimeout(resolve, 4200)));
    await flush();
    expect(text()).toContain('Pagamento recebido. Obrigado!');
  }, 10_000);

  it('vencida: mostra há quantos dias', async () => {
    invoice = async () => open({ overdue: true, daysOverdue: 3 });
    await render();
    expect(text()).toContain('vencida há 3 dias');
  });

  it('já paga ou cancelada: não gera Pix', async () => {
    invoice = async () => open({ status: 'PAID', paidAt: '2026-10-09T13:00:00Z' });
    await render();
    expect(text()).toContain('Esta fatura já está paga');
    expect(calls).toEqual(['fatura tok123']);
    document.body.innerHTML = '';
    invoice = async () => open({ status: 'CANCELED' });
    await render();
    expect(text()).toContain('Esta fatura foi cancelada');
  });

  it('link inexistente', async () => {
    invoice = async () => {
      throw new ApiError(404, 'link de pagamento não encontrado');
    };
    await render();
    expect(text()).toContain('Link de pagamento não encontrado');
  });
});

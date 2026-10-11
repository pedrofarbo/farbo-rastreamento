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
  GROUP_SUFFIX: ':todas',
  payLinkApi: {
    invoice: (token: string) => {
      calls.push('fatura ' + token);
      return invoice();
    },
    pix: () => ({
      invoicePix: async (key: string) => {
        calls.push('pix ' + key);
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

  it('com outras faturas no mesmo vencimento (a frota): paga todas num Pix só, ou só esta', async () => {
    invoice = async () =>
      open({
        description: 'Plano Frota — outubro/2026 (Van 01)',
        amountCents: 4990,
        group: {
          amountCents: 13970,
          items: [
            { description: 'Plano Frota — outubro/2026 (Van 01)', amountCents: 4990 },
            { description: 'Plano Frota — outubro/2026 (Carro 02)', amountCents: 4990 },
            { description: 'Plano Frota — outubro/2026 (Moto 03)', amountCents: 3990 },
          ],
        },
      });
    await render();
    expect(text()).toContain('3 faturas vencem neste dia · R$ 139,70');
    expect(text()).toContain('Plano Frota — outubro/2026 (Moto 03)R$ 39,90');
    expect(text()).toContain('3 faturas num Pix só');
    expect(calls).toEqual(['fatura tok123', 'pix tok123:todas']);
    const onlyThis = Array.from(document.querySelectorAll('button')).find((b) => b.textContent === 'Só esta (R$ 49,90)') as HTMLButtonElement;
    await act(async () => onlyThis.click());
    await flush();
    expect(onlyThis.getAttribute('aria-checked')).toBe('true');
    expect(calls).toEqual(['fatura tok123', 'pix tok123:todas', 'pix tok123']);
  });

  it('link inexistente', async () => {
    invoice = async () => {
      throw new ApiError(404, 'link de pagamento não encontrado');
    };
    await render();
    expect(text()).toContain('Link de pagamento não encontrado');
  });
});

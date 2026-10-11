// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { Invoice, PixCharge } from '@/types';

const calls: unknown[] = [];
let invoices: Invoice[] = [];
const charge = { id: 'c1', invoiceId: 'van', amountCents: 9980, status: 'PENDING', brCode: '000201', qrCodeImage: 'data:image/png;base64,AA',
  devMode: false, expiresAt: null, paidAt: null, invoiceStatus: 'OPEN' } as unknown as PixCharge;
vi.mock('@/api/resources', () => ({
  meApi: {
    account: async () => ({ onlinePayment: true, overdueInvoices: 0, suspended: false, openAmountCents: 0, openInvoices: 0,
      nextInvoice: null, activeSubscriptions: 2, vehicles: 2, suspendAfterDays: 10 }),
    invoices: async () => invoices,
    subscriptions: async () => [],
    invoicePix: async (id: string) => {
      calls.push(['uma', id]);
      return charge;
    },
    invoicesPix: async (ids: string[]) => {
      calls.push(['várias', ids]);
      return charge;
    },
    charge: async () => charge,
    simulateCharge: async () => charge,
  },
}));

import { InvoicesPage } from './InvoicesPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.replace(/\u00a0/g, ' ') === label) as HTMLButtonElement;
const inv = (id: string, dueDate: string, amountCents: number, over: Partial<Invoice> = {}): Invoice =>
  ({ id, customerId: 'c', subscriptionId: 's', description: `Plano Frota — ${id}`, dueDate, amountCents, status: 'OPEN', overdue: false,
    daysOverdue: 0, paidAt: null, paidVia: '', paidChargeId: null, paymentUrl: '', pixCode: '', createdAt: '', updatedAt: '', ...over }) as Invoice;

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <ToastProvider>
          <MemoryRouter>
            <InvoicesPage />
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
});

describe('faturas do cliente', () => {
  it('a fatura do mês da frota sai num Pix só; as vencidas de meses diferentes, também', async () => {
    invoices = [
      inv('van', '2026-11-13', 4990),
      inv('carro', '2026-11-13', 4990),
      inv('frete', '2026-11-20', 2240),
      inv('setembro', '2026-09-13', 4990, { overdue: true, daysOverdue: 20 }),
      inv('outubro', '2026-10-13', 4990, { overdue: true, daysOverdue: 2 }),
    ];
    await render();
    expect(text()).toContain('Fatura de 13/11/2026');
    expect(text()).toContain('2 faturas no mesmo vencimento');
    expect(text()).toContain('2 faturas vencidas · R$ 99,80.');
    await act(async () => button('Pagar R$ 99,80 com Pix').click());
    await flush();
    expect(calls).toEqual([['várias', ['van', 'carro']]]);
    expect(text()).toContain('2 faturas · vencimento 13/11/2026');
  });

  it('com uma fatura por vencimento, nada muda: o Pix de cada uma', async () => {
    invoices = [inv('van', '2026-11-13', 4990), inv('frete', '2026-11-20', 2240)];
    await render();
    expect(text()).not.toContain('Fatura de ');
    const buttons = Array.from(document.querySelectorAll('button')).filter((b) => b.textContent === 'Pagar com Pix');
    expect(buttons).toHaveLength(2);
    await act(async () => buttons[0].click());
    await flush();
    expect(calls).toEqual([['uma', 'van']]);
  });
});

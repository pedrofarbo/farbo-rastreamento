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
let olderPages: Invoice[][] = [];
const charge = { id: 'c1', invoiceId: 'van', amountCents: 9980, status: 'PENDING', brCode: '000201', qrCodeImage: 'data:image/png;base64,AA',
  devMode: false, expiresAt: null, paidAt: null, invoiceStatus: 'OPEN' } as unknown as PixCharge;
vi.mock('@/api/resources', () => ({
  meApi: {
    account: async () => ({ onlinePayment: true, overdueInvoices: 0, suspended: false, openAmountCents: 0, openInvoices: 0,
      nextInvoice: null, activeSubscriptions: 2, vehicles: 2, suspendAfterDays: 10 }),
    invoices: async () => invoices,
    olderInvoices: async (before: string) => {
      calls.push(['antigas', before]);
      return olderPages.shift() ?? [];
    },
    subscriptions: async () => [],
    invoicePix: async (id: string) => {
      calls.push(['uma', id]);
      return charge;
    },
    invoicesPix: async (ids: string[]) => {
      calls.push(['várias', ids]);
      return charge;
    },
    invoiceBoleto: async (id: string) => {
      calls.push(['boleto de uma', id]);
      return { ...charge, id: 'b1', method: 'BOLETO' };
    },
    invoicesBoleto: async (ids: string[]) => {
      calls.push(['boleto de várias', ids]);
      return { ...charge, id: 'b1', method: 'BOLETO' };
    },
    charge: async () => charge,
    simulateCharge: async () => charge,
    card: async () => ({ enabled: false, publicKey: '', saved: null, payer: null }),
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
  olderPages = [];
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
    expect(text()).toContain('A fatura do mês: todos os veículos e o que foi contratado, num pagamento só.');
    expect(text()).toContain('2 faturas vencidas · R$ 99,80.');
    await act(async () => button('Pagar R$ 99,80').click());
    await flush();
    expect(calls).toEqual([['várias', ['van', 'carro']]]);
    // O boleto do mesmo grupo, um só para as duas.
    await act(async () => (document.querySelector('[role="radio"][aria-checked="false"]') as HTMLButtonElement).click());
    await flush();
    expect(calls).toEqual([['várias', ['van', 'carro']], ['boleto de várias', ['van', 'carro']]]);
  });

  it('com uma linha só no vencimento, ela é a fatura do mês: o Pix dela', async () => {
    invoices = [inv('van', '2026-11-13', 4990), inv('frete', '2026-11-20', 2240)];
    await render();
    expect(text()).toContain('Fatura de 13/11/2026');
    expect(text()).toContain('Fatura de 20/11/2026');
    await act(async () => button('Pagar R$ 49,90').click());
    await flush();
    expect(calls).toEqual([['uma', 'van']]);
  });

  it('frota de 1000: o mês resumido, o histórico por vencimento e as mais antigas sob demanda', async () => {
    const fleet = (dueDate: string, n: number, over: Partial<Invoice> = {}) =>
      Array.from({ length: n }, (_, i) => inv(`${dueDate} Van ${String(i + 1).padStart(4, '0')}`, dueDate, 4990, over));
    invoices = [...fleet('2026-11-13', 1000), ...fleet('2026-10-13', 1000, { status: 'PAID', paidVia: 'PIX' })];
    olderPages = [fleet('2026-09-13', 1000, { status: 'PAID' })];
    await render();
    // O cartão do mês: 5 linhas, o resto sob demanda.
    expect(text()).toContain('A fatura do mês: todos os veículos');
    expect(text()).toContain('2026-11-13 Van 0005');
    expect(text()).not.toContain('2026-11-13 Van 0006');
    expect(text()).toContain('e mais 995 faturas · ver todas as 1000');
    // O histórico: uma linha por vencimento, em vez de 2000.
    expect(document.querySelectorAll('tbody tr')).toHaveLength(2);
    expect(text()).toContain('Em aberto');
    expect(text()).toContain('Paga');
    // Abrir outubro mostra 50 e "mostrar mais".
    await act(async () => Array.from(document.querySelectorAll('button')).filter((b) => b.textContent === 'Ver as 1000 linhas')[1].click());
    expect(document.querySelectorAll('tbody tr')).toHaveLength(2 + 50 + 1);
    expect(text()).toContain('Mostrar mais 50 (faltam 950)');
    // As mais antigas: setembro, antes de outubro.
    await act(async () => button('Ver faturas mais antigas').click());
    await flush();
    expect(calls).toEqual([['antigas', '2026-10-13']]);
    expect(text()).toContain('13/09/2026');
    // Veio uma página cheia: ainda pode haver mais.
    expect(button('Ver faturas mais antigas')).toBeTruthy();
    // Pagar o mês pela linha do histórico: um Pix só, das 1000.
    const payMonth = Array.from(document.querySelectorAll('tbody button')).find((b) =>
      b.textContent?.replace(/\u00a0/g, ' ').startsWith('Pagar R$ 49.900,00'),
    ) as HTMLButtonElement;
    await act(async () => payMonth.click());
    await flush();
    expect((calls[1] as [string, string[]])[1]).toHaveLength(1000);
  });
});

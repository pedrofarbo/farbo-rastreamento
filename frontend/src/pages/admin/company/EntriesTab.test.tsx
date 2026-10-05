// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { FinanceEntry } from '@/types';

import { totals } from './EntriesTab';

function entry(over: Partial<FinanceEntry>): FinanceEntry {
  return {
    id: 'e1', kind: 'PAYABLE', description: 'Contador', categoryId: 'c1', categoryName: 'Contabilidade', group: 'OPERATING',
    supplierId: null, supplierName: '', amountCents: 30000, dueDate: '2026-10-18', status: 'OPEN', paidOn: null, paidCents: null,
    overdue: false, paymentMethod: '', paymentCode: '', notes: '', recurrenceId: null, installment: null, installments: null,
    stockMovementId: null, attachments: [], createdAt: '', updatedAt: '', ...over,
  };
}

const LIST = [
  entry({}),
  entry({ id: 'e2', description: 'Internet', amountCents: 12000, dueDate: '2026-10-05', overdue: true, supplierName: 'Vivo' }),
  entry({ id: 'e3', description: 'Notebook', amountCents: 33334, dueDate: '2026-10-31', installment: 1, installments: 3 }),
];

const asked: unknown[] = [];
const paid: unknown[] = [];
vi.mock('@/api/resources', () => ({
  financeApi: {
    entries: async (f: unknown) => {
      asked.push(f);
      return LIST;
    },
    categories: async () => [{ id: 'c1', name: 'Contabilidade', kind: 'EXPENSE', group: 'OPERATING', active: true }],
    suppliers: async () => [],
    pay: async (id: string, input: unknown) => {
      paid.push([id, input]);
      return LIST[0];
    },
  },
}));

vi.mock('./labels', async (original) => ({ ...(await original<typeof import('./labels')>()), todayISO: () => '2026-10-15' }));

import { EntriesTab } from './EntriesTab';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

afterEach(() => {
  document.body.innerHTML = '';
  asked.length = 0;
  paid.length = 0;
});

describe('totals', () => {
  it('soma o valor, o pago nas pagas e o vencido', () => {
    const t = totals([...LIST, entry({ id: 'p', status: 'PAID', amountCents: 1000, paidCents: 1100 })]);
    expect(t).toEqual({ count: 4, cents: 30000 + 12000 + 33334 + 1100, overdueCents: 12000 });
  });
});

describe('EntriesTab', () => {
  it('lista as contas com a situação, filtra e dá baixa', async () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <MemoryRouter>
            <ToastProvider>
              <EntriesTab kind="PAYABLE" />
            </ToastProvider>
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    );
    await flush();
    // O real formatado usa espaço não separável depois do R$.
    const text = () => (document.body.textContent ?? '').replace(/\u00a0/g, ' ');
    for (const part of ['3 lançamentos', 'R$ 753,34', 'Vencido R$ 120,00', 'Vencida há 10 dias', 'Vence em 3 dias', 'Notebook · 1/3', 'Contabilidade · Vivo']) {
      expect(text()).toContain(part);
    }
    expect(asked[0]).toMatchObject({ kind: 'PAYABLE', status: 'open' });

    const button = (label: string) =>
      Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;
    await act(async () => button('Pagas').click());
    await flush();
    expect(asked.at(-1)).toMatchObject({ status: 'paid' });

    // Baixa da primeira: hoje, pelo valor, sem forma.
    await act(async () => button('Pagar').click());
    await flush();
    expect(text()).toContain('Pagar: Contador');
    await act(async () => button('Confirmar').click());
    await flush();
    expect(paid[0]).toEqual(['e1', { paidOn: '2026-10-15', paidCents: 30000, method: '' }]);
  });
});

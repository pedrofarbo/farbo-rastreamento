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
    stockMovementId: null, attachments: [], pix: null, createdAt: '', updatedAt: '', ...over,
  };
}

const LIST = [
  entry({}),
  entry({ id: 'e2', description: 'Internet', amountCents: 12000, dueDate: '2026-10-05', overdue: true, supplierName: 'Vivo' }),
  entry({ id: 'e3', description: 'Notebook', amountCents: 33334, dueDate: '2026-10-31', installment: 1, installments: 3 }),
];

const asked: unknown[] = [];
const paid: unknown[] = [];
let pixEnabled = false;
let list = LIST;
vi.mock('@/api/resources', () => ({
  financeApi: {
    entries: async (f: unknown) => {
      asked.push(f);
      return list;
    },
    pixInfo: async () => ({ enabled: pixEnabled, devMode: true, availableCents: 500000, balanceError: '' }),
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
  pixEnabled = false;
  list = LIST;
});

async function renderTab() {
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
  return host;
}

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

  it('com a AbacatePay: pagar por Pix, o comprovante e o Pix a conferir', async () => {
    pixEnabled = true;
    const pix = (status: 'COMPLETE' | 'UNKNOWN' | 'FAILED', error = '') => ({
      id: `t-${status}`, entryId: 'x', providerId: 'tran_1', status, amountCents: 1000, feeCents: 80, key: 'a@b.c',
      sentCents: 1080, deliveredCents: status === 'COMPLETE' ? 1000 : 0,
      keyType: 'EMAIL' as const, receiptUrl: 'https://app.abacatepay.com/receipt/tran_1', devMode: false, error,
      createdAt: '', completedAt: null,
    });
    list = [
      entry({ id: 'a', description: 'Aberta' }),
      entry({ id: 'p', description: 'Paga por Pix', status: 'PAID', paidCents: 1000, paidOn: '2026-10-15', paymentMethod: 'PIX', pix: pix('COMPLETE') }),
      entry({ id: 'u', description: 'Sem resposta', pix: pix('UNKNOWN') }),
      entry({ id: 'f', description: 'Recusada', pix: pix('FAILED', 'Saldo insuficiente') }),
    ];
    const host = await renderTab();
    const row = (text: string) => Array.from(host.querySelectorAll('tr')).find((tr) => tr.textContent?.includes(text))!;
    const buttons = (tr: HTMLElement) => Array.from(tr.querySelectorAll('button')).map((b) => b.textContent);
    expect(buttons(row('Aberta'))).toEqual(['Pagar por Pix', 'Pagar', 'Editar', 'Anexos', 'Cancelar', 'Excluir']);
    // Paga por Pix: o comprovante; não se reabre nem se exclui.
    expect(buttons(row('Paga por Pix'))).toEqual(['Anexos']);
    expect(row('Paga por Pix').querySelector('a')?.getAttribute('href')).toBe('https://app.abacatepay.com/receipt/tran_1');
    // Sem resposta: só conferir.
    expect(buttons(row('Sem resposta'))).toEqual(['Conferir Pix', 'Anexos']);
    expect(row('Sem resposta').textContent).toContain('Pix a conferir');
    // Recusada: o motivo, e dá para tentar de novo.
    expect(row('Recusada').textContent).toContain('Pix não saiu: Saldo insuficiente');
    expect(buttons(row('Recusada'))).toContain('Pagar por Pix');
  });
});

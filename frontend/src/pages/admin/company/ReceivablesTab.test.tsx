// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { CustomerPayments } from '@/types';

const OCTOBER: CustomerPayments = {
  month: '2026-10',
  count: 3,
  cents: 3990 + 5990 + 6990,
  byMethod: [
    { method: 'MANUAL', count: 1, cents: 6990 },
    { method: 'CARD', count: 1, cents: 5990 },
    { method: 'PIX', count: 1, cents: 3990 },
  ],
  payments: [
    {
      invoiceId: 'i1', customerId: 'c-bruno', customerName: 'Bruno Frota', description: 'Plano Frota — outubro/2026',
      amountCents: 5990, dueDate: '2026-10-13', paidAt: '2026-10-14T02:30:00Z', method: 'CARD',
    },
    {
      invoiceId: 'i2', customerId: 'c-carla', customerName: 'Carla Dias', description: 'Plano Mensal — outubro/2026',
      amountCents: 3990, dueDate: '2026-10-13', paidAt: '2026-10-12T15:00:00Z', method: 'PIX',
    },
    {
      invoiceId: 'i3', customerId: 'c-bruno', customerName: 'Bruno Frota', description: 'Antiga',
      amountCents: 6990, dueDate: '2026-10-05', paidAt: '2026-10-05T12:00:00Z', method: 'MANUAL',
    },
  ],
  truncated: false,
};

const asked: unknown[] = [];
let answer: CustomerPayments = OCTOBER;
vi.mock('@/api/resources', () => ({
  financeApi: {
    customerPayments: async (f: unknown) => {
      asked.push(f);
      return answer;
    },
    entries: async () => [],
    categories: async () => [],
    suppliers: async () => [],
  },
}));

vi.mock('./labels', async (original) => ({ ...(await original<typeof import('./labels')>()), todayISO: () => '2026-10-15' }));

import { ReceivablesTab } from './ReceivablesTab';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/\u00a0/g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement | undefined;
const select = (label: string) => document.querySelector(`select[aria-label="${label}"]`) as HTMLSelectElement;

afterEach(() => {
  document.body.innerHTML = '';
  asked.length = 0;
  answer = OCTOBER;
});

async function renderTab(url = '/empresa?aba=receber') {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter initialEntries={[url]}>
          <ToastProvider>
            <ReceivablesTab />
          </ToastProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

describe('receitas: os pagamentos dos clientes', () => {
  it('abre nos pagamentos do mês corrente, com o total por forma e o cliente na ficha', async () => {
    await renderTab();
    expect(asked[0]).toEqual({ month: '2026-10', q: '', method: '' });
    expect(text()).toContain('3 pagamentos');
    expect(text()).toContain('Total R$ 169,70');
    expect(text()).toContain('Baixa manual R$ 69,90');
    expect(text()).toContain('Cartão R$ 59,90');
    const rows = Array.from(document.querySelectorAll('tbody tr'));
    expect(rows).toHaveLength(3);
    expect(rows[0].textContent).toContain('Bruno Frota');
    expect(rows[0].textContent).toContain('Plano Frota — outubro/2026');
    expect(rows[0].textContent).toContain('vencimento 13/10/2026');
    expect(rows[0].textContent).toContain('Cartão');
    expect(rows[0].querySelector('a')?.getAttribute('href')).toBe('/clientes/c-bruno');
    // O seletor oferece o mês corrente e os anteriores.
    const months = Array.from(select('Mês').options).map((o) => o.value);
    expect(months.slice(0, 3)).toEqual(['2026-10', '2026-09', '2026-08']);
    expect(months).toHaveLength(24);
  });

  it('troca o mês e filtra pela forma; sem nada, diz o mês', async () => {
    await renderTab();
    answer = { ...OCTOBER, month: '2026-09', count: 0, cents: 0, byMethod: [], payments: [] };
    await act(async () => {
      select('Mês').value = '2026-09';
      select('Mês').dispatchEvent(new Event('change', { bubbles: true }));
    });
    await flush();
    expect(asked.at(-1)).toEqual({ month: '2026-09', q: '', method: '' });
    expect(text()).toContain('Nenhum pagamento de cliente em set/2026.');
    await act(async () => {
      select('Forma de pagamento').value = 'PIX';
      select('Forma de pagamento').dispatchEvent(new Event('change', { bubbles: true }));
    });
    await flush();
    expect(asked.at(-1)).toEqual({ month: '2026-09', q: '', method: 'PIX' });
    expect(text()).toContain('Nenhum pagamento com esses filtros.');
  });

  it('com mais do que a lista traz, avisa que o total conta todos', async () => {
    answer = { ...OCTOBER, count: 700, truncated: true };
    await renderTab();
    expect(text()).toContain('Mostrando os 3 mais recentes de 700');
  });

  it('as outras receitas continuam ao lado', async () => {
    await renderTab();
    await act(async () => button('Outras receitas')!.click());
    await flush();
    expect(text()).toContain('Aqui ficam as outras receitas, lançadas à mão');
    expect(button('Nova receita')).toBeTruthy();
    // E pelo link direto.
    document.body.innerHTML = '';
    await renderTab('/empresa?aba=receber&ver=avulsas');
    expect(button('Nova receita')).toBeTruthy();
    expect(button('Outras receitas')?.getAttribute('aria-pressed')).toBe('true');
  });
});

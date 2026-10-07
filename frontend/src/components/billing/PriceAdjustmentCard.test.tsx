// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { PriceAdjustment, PriceAdjustmentOverview } from '@/types';

let overview: PriceAdjustmentOverview;
const canceled: number[] = [];
vi.mock('@/api/resources', () => ({
  priceAdjustmentsApi: {
    overview: async () => overview,
    cancel: async (year: number) => {
      canceled.push(year);
    },
  },
}));

import { PriceAdjustmentCard } from './PriceAdjustmentCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.startsWith(label)) as HTMLButtonElement;

const year2027: PriceAdjustment = {
  year: 2027, period: 'junho de 2026 a maio de 2027', rate: '4,91%', rateMillionths: 49070, effectiveFrom: '2027-08-01',
  status: 'NOTIFIED', subscriptions: 2, notifiedAt: '2027-07-01T13:00:00Z', canceledAt: null, cancelUntil: '2027-07-21',
  canCancel: true, monthlyDiffCents: 588,
  items: [
    { subscriptionId: 's1', customerId: 'c1', customerName: 'Ana Lima', vehicle: 'Moto da Ana', plan: 'Plano Mensal',
      oldPriceCents: 6990, newPriceCents: 7333, notified: true },
    { subscriptionId: 's2', customerId: 'c1', customerName: 'Ana Lima', vehicle: 'Carro da Ana', plan: 'Plano Mensal',
      oldPriceCents: 4990, newPriceCents: 5235, notified: false },
  ],
};

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <ToastProvider>
            <PriceAdjustmentCard />
          </ToastProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  canceled.length = 0;
  vi.restoreAllMocks();
});

describe('reajuste anual no painel', () => {
  it('o agendado: índice, mensalidades, aviso e o veto (com confirmação)', async () => {
    overview = {
      enabled: true, upcoming: { year: 2028, noticeDate: '2028-07-01', startDate: '2028-08-01', period: 'x' },
      adjustments: [year2027, { ...year2027, year: 2026, status: 'NO_CHANGE', rate: '-0,31%', items: [], canCancel: false }],
    };
    await render();
    expect(text()).toContain('2027: IPCA 4,91%');
    expect(text()).toContain('a partir de 01/08/2027');
    expect(text()).toContain('receita +R$ 5,88/mês');
    expect(text()).toContain('R$ 69,90 → R$ 73,33');
    expect(text()).toContain('Pendente');
    expect(text()).toContain('até 21/07/2027');
    expect(text()).toContain('2026: IPCA -0,31%');
    expect(text()).toContain('Sem reajuste');
    // Desistiu na confirmação: nada.
    vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true);
    await act(async () => button('Cancelar o reajuste de 2027').click());
    expect(canceled).toEqual([]);
    await act(async () => button('Cancelar o reajuste de 2027').click());
    await flush();
    expect(canceled).toEqual([2027]);
  });

  it('sem reajuste agendado: o próximo; desligado, avisa', async () => {
    overview = {
      enabled: false, upcoming: { year: 2027, noticeDate: '2027-07-01', startDate: '2027-08-01', period: 'junho de 2026 a maio de 2027' },
      adjustments: [],
    };
    await render();
    expect(text()).toContain('Próximo: agosto de 2027.');
    expect(text()).toContain('Em 01/07/2027');
    expect(text()).toContain('junho de 2026 a maio de 2027');
    expect(text()).toContain('desligado no servidor');
    expect(button('Cancelar o reajuste')).toBeFalsy();
  });
});

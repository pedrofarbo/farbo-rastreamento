// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { CashFlow } from '@/types';

// O caso real de outubro/2026: o mesmo resumo do extrato da AbacatePay.
const flow: CashFlow = {
  settings: { openingBalanceCents: 0, openingDate: '2026-10-05' },
  today: '2026-10-07',
  balanceCents: 320,
  months: [{
    month: '2026-10', invoicesCents: 500, otherInCents: 0, inCents: 500, outCents: 180, feesCents: 160, netCents: 320,
    endBalanceCents: 320,
  }],
  projections: [],
};
vi.mock('@/api/resources', () => ({ financeApi: { cashflow: async () => flow } }));

import { CashFlowTab } from './CashFlowTab';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

afterEach(() => {
  document.body.innerHTML = '';
});

describe('o caixa mês a mês', () => {
  it('separa as taxas das saídas, como o extrato da AbacatePay', async () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient()}>
          <ToastProvider>
            <CashFlowTab />
          </ToastProvider>
        </QueryClientProvider>,
      ),
    );
    await flush();
    const cell = (label: string) =>
      (host.querySelector(`td[data-label="${label}"]`)?.textContent ?? '').replace(/ /g, ' ');
    expect(cell('Faturas')).toBe('R$ 5,00');
    expect(cell('Saídas')).toBe('R$ 0,20');
    expect(cell('Taxas')).toBe('R$ 1,60');
    expect(cell('Resultado')).toBe('R$ 3,20');
    expect(cell('Saldo no fim')).toBe('R$ 3,20');
  });
});

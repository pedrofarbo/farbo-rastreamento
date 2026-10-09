// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { AccountPlan } from '@/types';

const calls: unknown[] = [];
vi.mock('@/api/resources', () => ({
  customersApi: {
    setPlan: async (id: string, input: unknown) => {
      calls.push(['definir', id, input]);
      return { ...(input as object), updatedAt: '2026-10-09T12:00:00Z' };
    },
    clearPlan: async (id: string) => {
      calls.push(['padrão', id]);
    },
  },
}));

import { CustomerPlanCard } from './CustomerPlanCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement | undefined;

async function render(plan: AccountPlan | null) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <ToastProvider>
          <CustomerPlanCard customerId="c1" plan={plan} />
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('o plano do cliente na ficha', () => {
  it('sem plano: o padrão; a central define o do Insanos MC', async () => {
    await render(null);
    expect(text()).toContain('Padrão: o plano da assinatura ativa do cliente');
    expect(button('Voltar ao padrão')).toBeUndefined();
    await act(async () => button('Definir o plano')!.click());
    const select = Array.from(document.querySelectorAll('select')).find((s) => s.closest('div')?.textContent?.includes('Plano')) as HTMLSelectElement;
    await act(async () => {
      select.value = 'insanos';
      select.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await act(async () => button('Salvar')!.click());
    await flush();
    expect(calls).toEqual([['definir', 'c1', { planName: 'Especial Insanos MC', priceCents: 3990, dueDay: 10 }]]);
  });

  it('com plano: mostra, troca e volta ao padrão', async () => {
    await render({ planName: 'Especial Insanos MC', priceCents: 3990, dueDay: 15, updatedAt: '2026-10-09T12:00:00Z' });
    expect(text()).toContain('Especial Insanos MC: R$ 39,90/mês, vencimento todo dia 15.');
    await act(async () => button('Trocar o plano')!.click());
    const select = Array.from(document.querySelectorAll('select')).find((s) => s.closest('div')?.textContent?.includes('Plano')) as HTMLSelectElement;
    expect(select.value).toBe('insanos');
    await act(async () => button('Cancelar')!.click());
    await act(async () => button('Voltar ao padrão')!.click());
    await flush();
    expect(calls).toEqual([['padrão', 'c1']]);
  });
});

// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { PromoStatus } from '@/types';

const offer = { equipmentCents: 12000, monthlyCents: 3490, insanosMonthlyCents: 2790, insanosPlanName: 'Especial Insanos MC', months: 12 };
const outside: PromoStatus = {
  eligible: false, reason: 'o e-mail da conta não está na lista de lançamento', code: 'NOT_ON_LIST', offer,
  onList: false, grantedAt: null, claimed: false,
};
const granted: PromoStatus = { ...outside, eligible: true, reason: '', code: '', grantedAt: '2026-10-09T12:00:00Z' };
let current: PromoStatus;
const calls: string[] = [];
vi.mock('@/api/resources', () => ({
  customersApi: {
    launchPromo: async () => current,
    grantLaunchPromo: async (id: string) => {
      calls.push(`liberar:${id}`);
      return granted;
    },
    revokeLaunchPromo: async (id: string) => {
      calls.push(`retirar:${id}`);
      return outside;
    },
  },
}));

import { LaunchPromoCard, promoSummary } from './LaunchPromoCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement | undefined;

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('a promoção de pré-lançamento na ficha do cliente', () => {
  it('o resumo de cada situação', () => {
    expect(promoSummary({ ...outside, eligible: true, reason: '', code: '', onList: true })).toBe(
      'Tem direito: o e-mail está na lista de lançamento.',
    );
    expect(promoSummary(granted)).toContain('Liberada pela central em 09/10/2026');
    expect(promoSummary(outside)).toContain('Fora da lista de lançamento');
    expect(promoSummary({ ...granted, claimed: true, eligible: false, code: 'CLAIMED' })).toContain('Já usada');
    expect(promoSummary({ ...outside, code: 'ENDED', reason: 'a promoção de pré-lançamento foi encerrada' })).toBe(
      'Sem direito: a promoção de pré-lançamento foi encerrada.',
    );
  });

  it('libera para quem está fora da lista e retira depois', async () => {
    current = outside;
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient()}>
          <ToastProvider>
            <LaunchPromoCard customerId="c1" />
          </ToastProvider>
        </QueryClientProvider>,
      ),
    );
    await flush();
    expect(text()).toContain('rastreador por R$ 120,00 e mensalidade de R$ 34,90 nos 12 primeiros meses');
    expect(button('Retirar a liberação')).toBeUndefined();
    await act(async () => button('Liberar a promoção')!.click());
    await flush();
    expect(calls).toEqual(['liberar:c1']);
    expect(text()).toContain('Liberada pela central em 09/10/2026');
    await act(async () => button('Retirar a liberação')!.click());
    await flush();
    expect(calls).toEqual(['liberar:c1', 'retirar:c1']);
    expect(button('Liberar a promoção')).toBeDefined();
  });
});

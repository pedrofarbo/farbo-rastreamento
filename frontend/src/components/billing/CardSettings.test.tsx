// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { CardView, SavedCard } from '@/types';

const calls: unknown[] = [];
const saved = (over: Partial<SavedCard> = {}): SavedCard => ({
  brand: 'VISA', last4: '4242', autoDebit: false, consentAt: null, failedAt: null, lastError: '', createdAt: '', ...over,
});
let view: CardView;
vi.mock('@/api/resources', () => ({
  meApi: {
    card: async () => view,
    setAutoDebit: async (enabled: boolean) => {
      calls.push(['débito', enabled]);
      view = { ...view, saved: view.saved && { ...view.saved, autoDebit: enabled } };
      return view;
    },
    removeCard: async () => {
      calls.push(['apagar']);
    },
  },
}));

import { CardSettings } from './CardSettings';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => document.body.textContent ?? '';
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <ToastProvider>
          <CardSettings />
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('o cartão da conta, em Faturas', () => {
  it('liga o débito só com a autorização, desliga e apaga', async () => {
    view = { enabled: true, publicKey: 'pk', payer: null, saved: saved() };
    await render();
    expect(text()).toContain('VISA final 4242');
    await act(async () => button('Ligar o débito automático').click());
    expect(text()).toContain('Autorizo a Farbo a cobrar no cartão VISA final 4242');
    expect(calls).toEqual([]);
    await act(async () => button('Autorizo').click());
    await flush();
    expect(calls).toEqual([['débito', true]]);
    expect(text()).toContain('débito automático ligado');
    await act(async () => button('Desligar o débito').click());
    await flush();
    await act(async () => button('Apagar o cartão').click());
    await flush();
    expect(calls).toEqual([['débito', true], ['débito', false], ['apagar']]);
    expect(text()).toContain('Pague uma fatura com cartão e marque o débito automático');
  });

  it('avisa a recusa; sem o cartão ligado, some', async () => {
    view = { enabled: true, publicKey: 'pk', payer: null, saved: saved({ autoDebit: true, failedAt: '2026-10-13T10:00:00Z' }) };
    await render();
    expect(text()).toContain('O banco recusou a cobrança de 13/10/2026');
    document.body.innerHTML = '';
    view = { enabled: false, publicKey: '', payer: null, saved: null };
    await render();
    expect(text()).toBe('');
  });
});

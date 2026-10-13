// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { GatewayBalance } from '@/types';

const BALANCE: GatewayBalance = {
  enabled: true, devMode: false, availableCents: 123456, pendingCents: 15990, blockedCents: 0,
  checkedAt: '2026-10-13T17:20:00Z', error: '',
};
let answer: GatewayBalance = BALANCE;
let asked = 0;
vi.mock('@/api/resources', () => ({
  financeApi: {
    balance: async () => {
      asked++;
      return answer;
    },
  },
}));

import { GatewayBalanceCard } from './GatewayBalanceCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement | undefined;

afterEach(() => {
  document.body.innerHTML = '';
  answer = BALANCE;
  asked = 0;
});

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <GatewayBalanceCard />
      </QueryClientProvider>,
    ),
  );
  await flush();
}

describe('o saldo na AbacatePay no topo da empresa', () => {
  it('o disponível em destaque, o a liberar ao lado (o bloqueado zerado some) e a hora da consulta', async () => {
    await render();
    expect(text()).toContain('Saldo na AbacatePay');
    expect(text()).toContain('R$ 1.234,56');
    expect(text()).toContain('disponível · R$ 159,90 a liberar');
    expect(text()).not.toContain('bloqueado');
    expect(text()).toMatch(/atualizado às \d{2}:\d{2}/);
    expect(text()).not.toContain('testes');
  });

  it('atualiza no botão, com o valor novo', async () => {
    await render();
    answer = { ...BALANCE, availableCents: 100000, pendingCents: 0, blockedCents: 5000 };
    await act(async () => button('Atualizar')!.click());
    await flush();
    expect(asked).toBe(2);
    expect(text()).toContain('R$ 1.000,00');
    expect(text()).toContain('disponível · R$ 50,00 bloqueado');
  });

  it('a falha vira aviso; a chave de testes, um selo', async () => {
    answer = { ...BALANCE, devMode: true, availableCents: null, pendingCents: null, blockedCents: null, error: 'chave sem permissão' };
    await render();
    expect(text()).toContain('Não deu para consultar: chave sem permissão');
    expect(text()).toContain('testes');
    expect(button('Atualizar')).toBeTruthy();
  });

  it('sem a AbacatePay configurada, não aparece', async () => {
    answer = { ...BALANCE, enabled: false, availableCents: null, pendingCents: null, blockedCents: null };
    await render();
    expect(document.body.textContent).toBe('');
  });
});

// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { EngineHistoryItem } from '@/types';

const SAFARI =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6.1 Mobile/15E148 Safari/604.1';
const item = (o: Partial<EngineHistoryItem>): EngineHistoryItem => ({
  id: 'x', command: 'ENGINE_CUT', status: 'ACKNOWLEDGED', requestedAt: '2026-10-17T16:44:30Z', acknowledgedAt: null, error: '',
  who: 'Você', whoKind: 'you', userAgent: SAFARI, confirmedWith: 'password', ...o,
});
let list: EngineHistoryItem[] = [];
vi.mock('@/api/resources', () => ({ vehiclesApi: { engineHistory: async () => list } }));

import { EngineHistory } from './EngineHistory';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

afterEach(() => {
  document.body.innerHTML = '';
});

async function rows(): Promise<string[]> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <EngineHistory vehicleId="v1" />
      </QueryClientProvider>,
    ),
  );
  await flush();
  return Array.from(host.querySelectorAll('li')).map((li) => (li.textContent ?? '').replace(/ /g, ' '));
}

describe('rastreio de bloqueio e desbloqueio', () => {
  it('quem, de qual aparelho, como confirmou e o que o rastreador respondeu', async () => {
    list = [
      item({ id: '1' }),
      item({ id: '2', command: 'ENGINE_RESUME', status: 'TIMEOUT', who: 'Bia', whoKind: 'account', confirmedWith: 'biometric' }),
      item({ id: '3', who: 'Caio', whoKind: 'shared' }),
      item({ id: '4', command: 'ENGINE_RESUME', who: 'Central Farbo', whoKind: 'central', userAgent: '', confirmedWith: '' }),
      item({ id: '5', status: 'REJECTED', error: 'veículo em movimento', userAgent: '' }),
    ];
    const [mine, team, shared, central, rejected] = await rows();
    expect(mine).toContain('🔒 Bloqueio');
    expect(mine).toContain('confirmado pelo rastreador');
    expect(mine).toContain('Você');
    expect(mine).toContain('iPhone · Safari (iOS 26.6) · confirmado com a senha');
    expect(team).toContain('🔓 Desbloqueio');
    expect(team).toContain('sem resposta do rastreador');
    expect(team).toContain('Bia');
    expect(team).toContain('confirmado com a biometria');
    expect(shared).toContain('Caio (acesso compartilhado)');
    expect(central).toContain('Central Farbo');
    expect(central).not.toContain('aparelho');
    expect(rejected).toContain('recusado');
    expect(rejected).toContain('veículo em movimento');
    expect(rejected).toContain('aparelho não registrado');
  });

  it('sem nenhum', async () => {
    list = [];
    await rows();
    expect(document.body.textContent).toContain('Nenhum bloqueio ou desbloqueio ainda.');
  });
});

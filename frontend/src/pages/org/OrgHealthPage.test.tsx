// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { TrackerHealth } from '@/types';

const health: TrackerHealth = {
  installed: 6, online: 2, silent: 1, silentLong: 1, never: 1, removed: 1, powerCuts: 2, autoNotice: true,
  vehicles: [
    { vehicleId: 'v4', vehicleName: 'Carro 4', plate: 'EST4A24', associateId: 'a1', associateName: 'Bia Souza', phone: '(34) 9-8888-1234',
      imei: '1', status: 'OFFLINE', lastSeenAt: '2026-11-15T11:00:00Z', lastPowerCutAt: '2026-11-15T10:00:00Z', powerCuts: 1, issue: 'removed',
      notifiedAt: null, pushEnabled: true },
    { vehicleId: 'v5', vehicleName: 'Carro 5', plate: '', associateId: 'a1', associateName: 'Bia Souza', phone: '',
      imei: '2', status: 'ONLINE', lastSeenAt: '2026-11-15T14:59:00Z', lastPowerCutAt: '2026-11-14T10:00:00Z', powerCuts: 1, issue: 'power_cut',
      notifiedAt: null, pushEnabled: false },
  ],
};

const calls = vi.hoisted(() => [] as unknown[][]);
vi.mock('@/api/resources', () => ({
  orgApi: {
    trackerHealth: async () => health,
    notifyTrackerHealth: async (id: string) => {
      calls.push(['notify', id]);
      return { delivered: 1 };
    },
    setTrackerHealthAutoNotice: async (on: boolean) => {
      calls.push(['auto', on]);
    },
  },
}));
vi.mock('@/stores/AuthContext', () => ({
  useAuth: () => ({ canOperateOrg: true, isOrgBroker: false, user: { organization: { name: 'Estrada Boa' } } }),
}));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => undefined }) }));

import { OrgHealthPage, trackerMessage } from './OrgHealthPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('a saúde dos rastreadores', () => {
  it('a mensagem do WhatsApp: o primeiro nome, o veículo e o que aconteceu', () => {
    const text = trackerMessage(health.vehicles[0], 'Estrada Boa');
    expect(text).toMatch(/^Olá, Bia! Aqui é da Estrada Boa\. O rastreador do seu Carro 4 \(EST4A24\) ficou sem a energia do veículo/);
    expect(text).not.toContain('Souza');
    expect(trackerMessage({ ...health.vehicles[0], issue: 'never' }, 'X')).toContain('ainda não mandou sinal');
  });

  it('mostra os números, o problema e avisa no app (o corte que voltou não tem aviso)', async () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={client}>
          <MemoryRouter>
            <OrgHealthPage />
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    );
    await vi.waitFor(() => expect(text()).toContain('Mandando sinal agora'));
    expect(text()).toContain('Corte de energia e sem sinal');
    expect(text()).toContain('pode ter sido desligado ou retirado');
    const buttons = Array.from(document.querySelectorAll('button')).filter((b) => b.textContent === 'Avisar no app');
    expect(buttons).toHaveLength(1);
    expect(document.querySelectorAll('a[href^="https://wa.me/5534988881234"]')).toHaveLength(1);
    await act(async () => buttons[0].click());
    expect(calls).toContainEqual(['notify', 'v4']);
    const toggle = document.querySelector('input[type="checkbox"]') as HTMLInputElement;
    expect(toggle.checked).toBe(true);
    await act(async () => toggle.click());
    expect(calls).toContainEqual(['auto', false]);
  });
});

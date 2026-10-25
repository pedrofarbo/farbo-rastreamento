// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { DeviceCommand, VehicleView } from '@/types';

const sent: unknown[] = [];
let admin = true;
let realtime: ((message: { type: string; data: unknown }) => void) | null = null;
const command = (over: Partial<DeviceCommand> = {}): DeviceCommand =>
  ({
    id: 'k1', deviceId: 'd1', command: 'CUSTOM', payload: '', status: 'SENT', correlationKey: 1, requestedBy: 'u1',
    requestedAt: '2026-10-25T18:30:00Z', sentAt: '2026-10-25T18:30:00Z', acknowledgedAt: null, timeoutAt: null,
    response: '', error: '', createdAt: '2026-10-25T18:30:00Z', ...over,
  }) as DeviceCommand;

vi.mock('@/api/resources', () => ({
  commandsApi: {
    generic: async (vehicleId: string, body: unknown) => {
      sent.push({ vehicleId, body });
      return command();
    },
  },
}));
vi.mock('@/stores/AuthContext', () => ({ useAuth: () => ({ canManage: admin }) }));
vi.mock('@/hooks/useRealtime', () => ({
  useRealtimeEvent: (_types: string[], handler: (message: { type: string; data: unknown }) => void) => {
    realtime = handler;
  },
}));

import { CustomCommand } from './CustomCommand';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const vehicle = { id: 'v1', device: { id: 'd1' } } as unknown as VehicleView;

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <CustomCommand vehicle={vehicle} />
      </QueryClientProvider>,
    ),
  );
  return host;
}

afterEach(() => {
  document.body.innerHTML = '';
  sent.length = 0;
  admin = true;
  realtime = null;
});

describe('comando livre', () => {
  it('o administrador manda o texto como está e vê a resposta do rastreador', async () => {
    const host = await render();
    const input = host.querySelector('input') as HTMLInputElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, ' SZCS#GT06SEL=1#GT06IEXVOL=2# ');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await act(async () => (host.querySelector('button[type="submit"]') as HTMLButtonElement).click());
    expect(sent).toEqual([{ vehicleId: 'v1', body: { command: 'CUSTOM', raw: 'SZCS#GT06SEL=1#GT06IEXVOL=2#' } }]);
    expect(host.textContent).toContain('esperando a resposta do rastreador');

    await act(async () => realtime?.({ type: 'command.acknowledged', data: command({ status: 'ACKNOWLEDGED', response: 'OK' }) }));
    expect(host.textContent).toContain('Resposta do rastreador: OK');
  });

  it('quem não é administrador não vê o campo', async () => {
    admin = false;
    const host = await render();
    expect(host.querySelector('input')).toBeNull();
  });
});

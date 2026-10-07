// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { VehicleView } from '@/types';

const sent: string[] = [];
vi.mock('@/api/resources', () => ({
  commandsApi: {
    engineCutCheck: async () => ({ allowed: true, code: 'OK' }),
    engineResume: async () => {
      sent.push('resume');
      return {};
    },
    engineCut: async () => {
      sent.push('cut');
      return {};
    },
  },
}));
vi.mock('@/stores/AuthContext', () => ({
  useAuth: () => ({ canSendCommands: true, isCustomer: true, user: { id: 'u-caio' } }),
}));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => {} }) }));
vi.mock('@/hooks/useRealtime', () => ({ useRealtimeEvent: () => {} }));

import { CommandPanel } from './CommandPanel';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function vehicle(over: Partial<VehicleView> & { relayOn?: boolean } = {}): VehicleView {
  const { relayOn = false, ...rest } = over;
  return {
    id: 'v1', name: 'Carro da Ana', deviceId: 'd1',
    device: { status: 'ONLINE' }, state: { relayOn },
    ...rest,
  } as unknown as VehicleView;
}

function render(v: VehicleView) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  act(() =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <CommandPanel vehicle={v} />
      </QueryClientProvider>,
    ),
  );
  return host;
}

const buttons = (host: ParentNode) => Array.from(host.querySelectorAll('button')).map((b) => b.textContent?.trim());

afterEach(() => {
  document.body.innerHTML = '';
  sent.length = 0;
});

describe('CommandPanel com veículo compartilhado', () => {
  it('com o bloqueio liberado: pedir posição e bloquear — nada de desbloquear nem status', () => {
    const host = render(vehicle({ shared: { shareId: 's1', ownerName: 'Ana', canBlock: true } }));
    expect(buttons(host)).toEqual(['Solicitar posição', 'Bloquear motor']);
  });

  it('bloqueado: o desbloqueio é com o dono', () => {
    const host = render(vehicle({ relayOn: true, shared: { shareId: 's1', ownerName: 'Ana', canBlock: true } }));
    expect(buttons(host)).toEqual(['Solicitar posição']);
    expect(host.textContent).toContain('Desbloquear é só com Ana.');
    expect(host.textContent).not.toContain('central');
  });

  it('sem o bloqueio liberado: só acompanha', () => {
    const host = render(vehicle({ shared: { shareId: 's1', ownerName: 'Ana', canBlock: false } }));
    expect(buttons(host)).toEqual(['Solicitar posição']);
    expect(host.textContent).toContain('não foi liberado por Ana');
  });
});

describe('CommandPanel do dono', () => {
  it('liberar o motor abre a confirmação antes de enviar', () => {
    const host = render(vehicle({ relayOn: true }));
    expect(buttons(host)).toEqual(['Solicitar posição', 'Solicitar status', 'Liberar motor']);
    act(() => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'Liberar motor') as HTMLButtonElement).click());
    expect(document.body.textContent).toContain('Liberar motor?');
    expect(sent).toEqual([]);
  });
});

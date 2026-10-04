// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { VehicleShare, VehicleView } from '@/types';

const share = (over: Partial<VehicleShare>): VehicleShare => ({
  id: 's1', vehicleId: 'v1', vehicleName: 'Carro', vehiclePlate: 'ABC1D23', ownerName: 'Ana', guestId: 'g1',
  guestName: 'Caio', guestEmail: 'caio@exemplo.com', canBlock: false, createdAt: '', updatedAt: '', ...over,
});

// Funções simples em vez de vi.fn (ver LeadModal.test).
const calls: unknown[][] = [];
vi.mock('@/api/resources', () => ({
  sharesApi: {
    list: async () => [share({}), share({ id: 's2', vehicleId: 'outro', guestName: 'Zeca' })],
    create: async (input: { name: string; email: string }, token: string) => {
      calls.push(['create', input, token]);
      return share({ id: 's3', guestName: input.name, guestEmail: input.email });
    },
    setCanBlock: async (id: string, canBlock: boolean, token?: string) => {
      calls.push(['setCanBlock', id, canBlock, token]);
      return share({ canBlock });
    },
    remove: async (id: string) => {
      calls.push(['remove', id]);
    },
  },
}));
vi.mock('@/services/stepUp', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/services/stepUp')>()),
  passwordGrant: async (purpose: string, password: string) => {
    calls.push(['grant', purpose, password]);
    return { token: 'comprovante', method: 'password', expiresAt: '' };
  },
}));
vi.mock('@/stores/AuthContext', () => ({ useAuth: () => ({ user: { id: 'u-ana' } }) }));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => {} }) }));

import { SharesModal, shareDraftProblem } from './SharesModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const VEHICLE = { id: 'v1', name: 'Carro da Ana' } as VehicleView;

const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={client}>
        <SharesModal vehicle={VEHICLE} onClose={() => {}} />
      </QueryClientProvider>,
    ),
  );
  await flush();
}

function type(el: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value);
  act(() => {
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

const button = (text: string) =>
  Array.from(document.body.querySelectorAll('button')).find((b) => b.textContent?.trim() === text) as HTMLButtonElement;
const input = (name: string) => document.body.querySelector(`input[name="${name}"]`) as HTMLInputElement;

async function confirmWithPassword() {
  await flush(); // sem biometria neste aparelho: vai direto para a senha
  type(input('identity-password'), 'minha-senha');
  await act(async () => {
    (document.body.querySelector('form') as HTMLFormElement).requestSubmit();
  });
  await flush();
}

beforeEach(() => {
  calls.length = 0;
});
afterEach(() => {
  document.body.innerHTML = '';
});

describe('shareDraftProblem', () => {
  it('pede nome e e-mail válido', () => {
    expect(shareDraftProblem({ name: '', email: 'a@b.com', canBlock: false })).toMatch(/nome/);
    expect(shareDraftProblem({ name: 'Bia', email: 'bia', canBlock: false })).toMatch(/e-mail/);
    expect(shareDraftProblem({ name: 'Bia', email: 'bia@exemplo.com', canBlock: true })).toBeNull();
  });
});

describe('SharesModal', () => {
  it('mostra só quem acompanha este veículo', async () => {
    await render();
    const text = document.body.textContent ?? '';
    expect(text).toContain('Caio');
    expect(text).toContain('Só acompanha');
    expect(text).not.toContain('Zeca');
  });

  it('dar acesso pede a confirmação de quem é dono e manda o comprovante', async () => {
    await render();
    expect(button('Dar acesso').disabled).toBe(true);
    type(input('share-name'), 'Bia Lima');
    type(input('share-email'), 'bia@exemplo.com');
    act(() => input('share-can-block').click());
    act(() => button('Dar acesso').click());
    expect(calls).toEqual([]); // nada sai antes da confirmação

    await confirmWithPassword();
    expect(calls).toEqual([
      ['grant', 'vehicle_share', 'minha-senha'],
      ['create', { vehicleId: 'v1', name: 'Bia Lima', email: 'bia@exemplo.com', canBlock: true }, 'comprovante'],
    ]);
  });

  it('liberar o bloqueio pede a confirmação; remover pede um segundo toque', async () => {
    await render();
    act(() => button('Liberar o bloqueio').click());
    await confirmWithPassword();
    expect(calls).toEqual([
      ['grant', 'vehicle_share', 'minha-senha'],
      ['setCanBlock', 's1', true, 'comprovante'],
    ]);

    calls.length = 0;
    act(() => button('Remover').click());
    expect(calls).toEqual([]);
    await act(async () => button('Confirmar remoção').click());
    await flush();
    expect(calls).toEqual([['remove', 's1']]);
  });
});

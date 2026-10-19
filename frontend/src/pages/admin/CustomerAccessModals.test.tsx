// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';

const calls: unknown[] = [];
vi.mock('@/api/resources', () => ({
  customersApi: {
    sessions: async () => ({ sessions: [] }),
    signOut: async () => ({ signedOut: 0 }),
    setPassword: async (id: string, password: string, token: string) => {
      calls.push([id, password, token]);
    },
  },
}));
// A confirmação de verdade (senha ou biometria) tem os próprios testes.
vi.mock('@/components/auth/IdentityCheck', () => ({
  IdentityCheck: ({ action, onGrant }: { action: string; onGrant: (token: string) => void }) => (
    <button type="button" onClick={() => onGrant('comprovante-1')}>
      {action}, confirmar
    </button>
  ),
}));

import { CustomerPasswordModal, randomPassword } from './CustomerAccessModals';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string | RegExp) =>
  Array.from(document.querySelectorAll('button')).find((b) =>
    typeof label === 'string' ? b.textContent === label : label.test(b.textContent ?? ''),
  ) as HTMLButtonElement;

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('a senha nova pela central', () => {
  it('gera senhas de 12 sem as letras que se confundem', () => {
    for (let i = 0; i < 50; i++) {
      const p = randomPassword();
      expect(p).toHaveLength(12);
      expect(p).not.toMatch(/[0O1lI]/);
    }
  });

  it('gera, confirma com a identidade de quem faz e mostra a senha para copiar', async () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient()}>
          <ToastProvider>
            <CustomerPasswordModal customerId="c1" name="Jair" open onClose={() => undefined} />
          </ToastProvider>
        </QueryClientProvider>,
      ),
    );
    // Curta não continua; gerada, sim.
    expect(button('Continuar').disabled).toBe(true);
    await act(async () => button('Gerar senha').click());
    const input = document.querySelector('input') as HTMLInputElement;
    expect(input.value).toHaveLength(12);
    expect(button('Continuar').disabled).toBe(false);

    await act(async () => button('Continuar').click());
    await act(async () => button(/confirmar$/).click());
    await flush();
    expect(calls).toEqual([['c1', input.value, 'comprovante-1']]);
    expect(text()).toContain('A senha de Jair agora é:');
    expect(text()).toContain(input.value || '');
    expect(text()).toContain('As sessões abertas dele foram desconectadas');
  });
});

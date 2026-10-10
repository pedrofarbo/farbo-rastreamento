// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { AccountMember } from '@/types';

const calls: unknown[] = [];
let members: AccountMember[] = [];
vi.mock('@/api/resources', () => ({
  teamApi: {
    list: async () => members,
    add: async (input: { name: string; email: string; role: string }, token: string) => {
      calls.push(['add', input, token]);
      return { userId: 'u9', active: true, createdAt: '', ...input };
    },
    setRole: async (userId: string, role: string) => {
      calls.push(['role', userId, role]);
      return { ...members.find((m) => m.userId === userId), role };
    },
    remove: async (userId: string) => {
      calls.push(['remove', userId]);
    },
  },
}));
// A confirmação de verdade pede a senha ou a biometria: aqui, um botão.
vi.mock('@/components/auth/IdentityCheck', () => ({
  IdentityCheck: ({ action, onGrant }: { action: string; onGrant: (t: string) => void }) => (
    <button type="button" onClick={() => onGrant('comprovante')}>
      Confirmar: {action}
    </button>
  ),
}));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => undefined }) }));

import { TeamManager } from './TeamManager';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string | RegExp) =>
  Array.from(document.querySelectorAll('button')).find((b) =>
    typeof label === 'string' ? b.textContent === label : label.test(b.textContent ?? ''),
  ) as HTMLButtonElement;
const field = (label: string) => {
  const l = Array.from(document.querySelectorAll('label')).find((x) => x.textContent?.startsWith(label));
  return document.getElementById(l?.htmlFor ?? '') as HTMLInputElement;
};
function type(el: HTMLInputElement | HTMLSelectElement, value: string) {
  const proto = el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, 'value')?.set?.call(el, value);
  el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
}

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <TeamManager />
      </QueryClientProvider>,
    ),
  );
  await flush();
  return host;
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('equipe da conta', () => {
  it('mostra quem é da equipe, troca o papel e tira (com a confirmação)', async () => {
    members = [
      { userId: 'u1', name: 'Gi Gestora', email: 'gi@frota.test', role: 'manager', active: true, createdAt: '' },
      { userId: 'u2', name: 'Vi Visualizador', email: 'vi@frota.test', role: 'viewer', active: true, createdAt: '' },
    ];
    await render();
    expect(text()).toContain('Gi Gestoragi@frota.testGestor');
    expect(text()).toContain('Vi Visualizadorvi@frota.testVisualizador');
    // O que cada papel faz, para o titular escolher.
    expect(text()).toContain('Só acompanha: mapa ao vivo, histórico, eventos e cercas');

    const select = document.querySelector('select[aria-label="Papel de Vi Visualizador"]') as HTMLSelectElement;
    await act(async () => type(select, 'manager'));
    await flush();
    expect(calls).toEqual([['role', 'u2', 'manager']]);

    calls.length = 0;
    const removeButtons = Array.from(document.querySelectorAll('button')).filter((b) => b.textContent === 'Tirar da equipe');
    await act(async () => removeButtons[0].click());
    expect(text()).toContain('Tirar Gi Gestora da equipe? O acesso acaba na hora.');
    await act(async () => button('Tirar').click());
    await flush();
    expect(calls).toEqual([['remove', 'u1']]);
  });

  it('põe alguém na equipe só depois da confirmação do titular', async () => {
    members = [];
    await render();
    expect(text()).toContain('Ninguém além de você acessa esta conta.');
    await act(async () => button('Pôr na equipe').click());
    expect(text()).toContain('Informe o nome da pessoa.');
    await act(async () => type(field('Nome'), 'Gi Gestora'));
    await act(async () => type(field('E-mail'), 'gi@'));
    await act(async () => button('Pôr na equipe').click());
    expect(text()).toContain('Informe um e-mail válido.');
    await act(async () => type(field('E-mail'), 'gi@frota.test'));
    const manager = Array.from(document.querySelectorAll('input[type=radio]'))[0] as HTMLInputElement;
    await act(async () => manager.click());
    await act(async () => button('Pôr na equipe').click());
    expect(calls).toEqual([]);
    await act(async () => button(/^Confirmar: Para dar a Gi Gestora acesso aos veículos da sua conta como Gestor/).click());
    await flush();
    expect(calls).toEqual([['add', { name: 'Gi Gestora', email: 'gi@frota.test', role: 'manager' }, 'comprovante']]);
  });
});

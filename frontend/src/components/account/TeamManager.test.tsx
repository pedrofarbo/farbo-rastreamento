// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { AccountMember } from '@/types';

const calls: unknown[] = [];
let members: AccountMember[] = [];
let fleetGroups: { id: string; name: string; vehicles: number }[] = [];
vi.mock('@/api/resources', () => ({
  fleetApi: {
    get: async () => ({ groups: fleetGroups, labels: { label1: '', label2: '' }, maxGroups: 100, maxCustomLength: 100, maxLabelLength: 40 }),
  },
  teamApi: {
    list: async () => members,
    limit: async () => ({ limit: 40, used: members.length }),
    add: async (input: { name: string; email: string; role: string; groups: string[] }, token: string) => {
      calls.push(['add', input, token]);
      return { userId: 'u9', active: true, createdAt: '', restricted: input.groups.length > 0, ...input };
    },
    setRole: async (userId: string, role: string) => {
      calls.push(['role', userId, role]);
      return { ...members.find((m) => m.userId === userId), role };
    },
    setGroups: async (userId: string, groups: string[]) => {
      calls.push(['groups', userId, groups]);
      return { ...members.find((m) => m.userId === userId), restricted: groups.length > 0, groups };
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
  fleetGroups = [];
});

describe('equipe da conta', () => {
  it('mostra quem é da equipe, troca o papel e tira (com a confirmação)', async () => {
    members = [
      { userId: 'u1', name: 'Gi Gestora', email: 'gi@frota.test', role: 'manager', active: true, createdAt: '', restricted: false, groups: [] },
      { userId: 'u2', name: 'Vi Visualizador', email: 'vi@frota.test', role: 'viewer', active: true, createdAt: '', restricted: false, groups: [] },
    ];
    await render();
    expect(text()).toContain('Gi Gestoragi@frota.testTodos os veículosGestor');
    expect(text()).toContain('Vi Visualizadorvi@frota.testTodos os veículosVisualizador');
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
    expect(calls).toEqual([['add', { name: 'Gi Gestora', email: 'gi@frota.test', role: 'manager', groups: [] }, 'comprovante']]);
  });

  it('restringe a pessoa a alguns grupos da frota, ao pôr e depois', async () => {
    fleetGroups = [
      { id: 'g1', name: 'Norte', vehicles: 12 },
      { id: 'g2', name: 'Sul', vehicles: 8 },
    ];
    members = [
      { userId: 'u1', name: 'Su Supervisora', email: 'su@frota.test', role: 'manager', active: true, createdAt: '', restricted: true, groups: ['g1'] },
      { userId: 'u2', name: 'Ana', email: 'ana@frota.test', role: 'viewer', active: true, createdAt: '', restricted: true, groups: [] },
    ];
    await render();
    expect(text()).toContain('su@frota.testGrupo: Norte');
    expect(text()).toContain('Nenhum veículo: os grupos liberados foram apagados');

    // Pôr alguém só com o Sul: sem grupo marcado, não segue.
    await act(async () => type(field('Nome'), 'Lu'));
    await act(async () => type(field('E-mail'), 'lu@frota.test'));
    const onlySome = Array.from(document.querySelectorAll('input[name="team-access"]'))[1] as HTMLInputElement;
    await act(async () => onlySome.click());
    await act(async () => button('Pôr na equipe').click());
    expect(text()).toContain('Escolha ao menos um grupo');
    const sul = Array.from(document.querySelectorAll('label')).find((l) => l.textContent?.startsWith('Sul'))!.querySelector('input')!;
    await act(async () => sul.click());
    await act(async () => button('Pôr na equipe').click());
    await act(async () => button(/^Confirmar: Para dar a Lu acesso aos veículos dos grupos escolhidos/).click());
    await flush();
    expect(calls).toEqual([['add', { name: 'Lu', email: 'lu@frota.test', role: 'viewer', groups: ['g2'] }, 'comprovante']]);

    // A Su passa a ver todos os veículos.
    calls.length = 0;
    const veiculos = Array.from(document.querySelectorAll('button')).filter((b) => b.textContent === 'Veículos');
    await act(async () => veiculos[0].click());
    const all = Array.from(document.querySelectorAll('input[name="access-u1"]'))[0] as HTMLInputElement;
    await act(async () => all.click());
    await act(async () => button('Salvar').click());
    await flush();
    expect(calls).toEqual([['groups', 'u1', []]]);
  });
});

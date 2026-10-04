// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { User } from '@/types';

const ME: User = {
  id: 'u-ana', email: 'ana@farbo.test', name: 'Ana', role: 'admin', phone: '', document: '', active: true,
  createdAt: '2026-09-01T12:00:00Z',
};
const OTTO: User = { ...ME, id: 'u-otto', email: 'otto@farbo.test', name: 'Otto', role: 'operator' };

// Funções simples em vez de vi.fn (ver LeadModal.test).
const calls: { op: string; id?: string; body?: unknown }[] = [];
vi.mock('@/api/resources', () => ({
  usersApi: {
    list: async () => [ME, OTTO],
    create: async (body: { email: string; name: string; role: User['role'] }) => {
      calls.push({ op: 'create', body });
      return { ...ME, id: 'u-novo', ...body };
    },
    update: async (id: string, body: { name: string; role: User['role']; active: boolean }) => {
      calls.push({ op: 'update', id, body });
      return { ...OTTO, ...body };
    },
    invite: async (id: string) => {
      calls.push({ op: 'invite', id });
      return { message: 'ok' };
    },
  },
}));
vi.mock('@/stores/AuthContext', () => ({ useAuth: () => ({ user: ME }) }));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => {} }) }));

import { createInputFrom, EMPTY_USER, updateInputFrom, UsersPage } from './UsersPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={client}>
        <UsersPage />
      </QueryClientProvider>,
    ),
  );
  await flush();
  return host;
}

const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

/** Preenche como o usuário: o setter nativo, para o React ver a mudança. */
function type(el: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value);
  act(() => {
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

const button = (text: string, root: ParentNode = document.body) =>
  Array.from(root.querySelectorAll('button')).find((b) => b.textContent?.trim() === text) as HTMLButtonElement;
const field = (name: string) => document.body.querySelector(`[role="dialog"] input[name="${name}"], input[name="${name}"]`) as HTMLInputElement;
const role = (value: string) => document.body.querySelector(`input[name="user-role"][value="${value}"]`) as HTMLInputElement;

beforeEach(() => {
  calls.length = 0;
});
afterEach(() => {
  document.body.innerHTML = '';
});

describe('formulário de usuário', () => {
  it('o cadastro exige nome, e-mail, perfil e, se for definir agora, a senha', () => {
    const base = { ...EMPTY_USER, name: 'Bia', email: 'bia@farbo.test' };
    expect(createInputFrom(base)).toBe('Escolha o perfil de acesso.');
    expect(createInputFrom({ ...base, role: 'viewer' })).toEqual({
      name: 'Bia', email: 'bia@farbo.test', role: 'viewer', password: '',
    });
    expect(createInputFrom({ ...base, role: 'viewer', access: 'password', password: 'curta' })).toMatch(/10 caracteres/);
    expect(createInputFrom({ ...base, role: 'admin', access: 'password', password: 'senha-forte-1' })).toMatchObject({
      password: 'senha-forte-1',
    });
    expect(createInputFrom({ ...base, email: 'sem-arroba', role: 'viewer' })).toBe('Informe um e-mail válido.');
    expect(updateInputFrom({ ...base, name: ' ', role: 'viewer' })).toBe('Informe o nome.');
  });
});

describe('UsersPage', () => {
  it('lista a equipe com o perfil, marca "você" e explica os perfis', async () => {
    const host = await render();
    const rows = Array.from(host.querySelectorAll('tbody tr'));
    expect(rows).toHaveLength(2);
    expect(rows[0].textContent).toContain('você');
    expect(rows[0].textContent).toContain('Administrador');
    expect(rows[1].textContent).toContain('Operador');
    expect(rows[1].textContent).not.toContain('você');
    for (const label of ['Administrador', 'Operador', 'Visualização']) {
      expect(host.querySelector('dl')?.textContent).toContain(label);
    }
  });

  it('cadastra com o perfil escolhido (nenhum vem marcado) e manda o convite', async () => {
    const host = await render();
    act(() => button('Novo usuário', host).click());

    type(field('name'), 'Bia Lima');
    type(field('email'), 'bia@farbo.test');
    expect(button('Cadastrar').disabled).toBe(true); // falta o perfil
    expect(['admin', 'operator', 'viewer'].some((r) => role(r).checked)).toBe(false);

    act(() => role('operator').click());
    expect(button('Cadastrar').disabled).toBe(false);
    act(() => button('Cadastrar').click());
    await flush();

    expect(calls).toEqual([
      { op: 'create', body: { name: 'Bia Lima', email: 'bia@farbo.test', role: 'operator', password: '' } },
    ]);
  });

  it('muda o perfil de outra pessoa; o próprio perfil e a situação ficam travados', async () => {
    const host = await render();
    const [meRow, ottoRow] = Array.from(host.querySelectorAll('tbody tr'));

    act(() => button('Editar', meRow).click());
    expect(role('admin').disabled).toBe(true);
    expect((document.body.querySelector('#user-active') as HTMLInputElement).disabled).toBe(true);
    expect(document.body.textContent).toContain('Você não pode mudar o próprio perfil');
    act(() => button('Cancelar').click());

    act(() => button('Editar', ottoRow).click());
    expect(role('operator').checked).toBe(true);
    act(() => role('admin').click());
    act(() => button('Salvar').click());
    await flush();
    expect(calls).toEqual([{ op: 'update', id: 'u-otto', body: { name: 'Otto', role: 'admin', active: true } }]);
  });

  it('reenvia o convite', async () => {
    const host = await render();
    const ottoRow = host.querySelectorAll('tbody tr')[1];
    act(() => button('Reenviar convite', ottoRow).click());
    await flush();
    expect(calls).toEqual([{ op: 'invite', id: 'u-otto' }]);
  });
});

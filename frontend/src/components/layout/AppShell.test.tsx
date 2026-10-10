// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

// O usuário de cada teste (a central, por padrão).
const staff = {
  user: { id: 'u1', name: 'Pedro Farbo', email: 'pedro@farbo.test', role: 'admin' },
  canManage: true,
  canOperate: true,
  isCustomer: false,
  membership: null,
  isAccountOwner: false,
  canOperateAccount: false,
};
let auth: Record<string, unknown> = staff;
vi.mock('@/stores/AuthContext', () => ({ useAuth: () => ({ logout: vi.fn(), ...auth }) }));
vi.mock('@/hooks/useRealtime', () => ({ useRealtime: () => ({ connected: true }) }));
vi.mock('@/hooks/useTheme', () => ({ useTheme: () => ({ theme: 'dark', toggle: vi.fn() }) }));
vi.mock('@/api/resources', () => ({
  financeApi: { alerts: async () => ({ overdue: 1, dueToday: 1 }) },
  leadsApi: { stats: async () => ({ new: 0 }) },
  meApi: { account: async () => ({ overdueInvoices: 0 }) },
  whatsappApi: { status: async () => ({ attention: 0 }) },
}));

import { AppShell } from './AppShell';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/dashboard']}>
          <Routes>
            <Route element={<AppShell />}>
              <Route path="/dashboard" element={<p>painel</p>} />
              <Route path="/clientes" element={<p>clientes</p>} />
            </Route>
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  );
  await flush();
  return host;
}

afterEach(() => {
  document.body.innerHTML = '';
  auth = staff;
});

const customer = (over: Record<string, unknown>) => ({
  user: { id: 'c1', name: 'Gi Gestora', email: 'gi@frota.test', role: 'customer' },
  canManage: false,
  canOperate: false,
  isCustomer: true,
  membership: null,
  isAccountOwner: true,
  canOperateAccount: true,
  ...over,
});
const menuLabels = (host: HTMLElement) =>
  Array.from(host.querySelectorAll('#menu-principal a')).map((a) => a.textContent?.trim());

describe('menu do painel', () => {
  it('no celular, abre como barra lateral no ☰ e fecha no Esc e ao trocar de página', async () => {
    const host = await render();
    const menu = host.querySelector('button[aria-controls="menu-principal"]') as HTMLButtonElement;
    const nav = host.querySelector('#menu-principal') as HTMLElement;
    // Duas contas da empresa pedem atenção: o ☰ avisa.
    expect(menu.getAttribute('aria-label')).toBe('Abrir o menu (2 aviso(s))');
    expect(menu.getAttribute('aria-expanded')).toBe('false');
    expect(nav.className).not.toMatch(/navOpen/);

    await act(async () => menu.click());
    expect(menu.getAttribute('aria-expanded')).toBe('true');
    expect(nav.className).toMatch(/navOpen/);
    // Quem está logado, os itens e, no pé, a conta, o tema e o Sair.
    expect(nav.textContent).toContain('Pedro Farbo');
    const labels = Array.from(nav.querySelectorAll('a, button')).map((el) => el.textContent?.trim());
    expect(labels).toEqual(
      expect.arrayContaining(['Painel', 'Clientes', 'Empresa2', 'Usuários', 'Segurança da conta', '☾Tema claro', 'Sair']),
    );
    expect(document.activeElement?.getAttribute('aria-label')).toBe('Fechar o menu');

    await act(async () => window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })));
    expect(menu.getAttribute('aria-expanded')).toBe('false');
    expect(document.activeElement).toBe(menu);

    // Abrir uma página fecha a barra.
    await act(async () => menu.click());
    const clients = Array.from(nav.querySelectorAll('a')).find((a) => a.textContent?.trim() === 'Clientes') as HTMLAnchorElement;
    await act(async () => clients.click());
    expect(host.textContent).toContain('clientes');
    expect(menu.getAttribute('aria-expanded')).toBe('false');
  });

  it('conta empresa: o titular vê a Equipe; quem é da equipe vê de quem é a conta, e o Visualizador não vê faturas', async () => {
    auth = customer({});
    let host = await render();
    expect(menuLabels(host)).toEqual(expect.arrayContaining(['Mapa', 'Faturas', 'Equipe']));
    document.body.innerHTML = '';

    auth = customer({
      membership: { accountId: 'o1', ownerName: 'Frota Ltda', role: 'viewer' },
      isAccountOwner: false,
      canOperateAccount: false,
    });
    host = await render();
    expect(menuLabels(host)).not.toContain('Faturas');
    expect(menuLabels(host)).not.toContain('Equipe');
    expect(host.querySelector('#menu-principal')?.textContent).toContain('Conta de Frota Ltda · Visualizador');
    document.body.innerHTML = '';

    auth = customer({
      membership: { accountId: 'o1', ownerName: 'Frota Ltda', role: 'manager' },
      isAccountOwner: false,
      canOperateAccount: true,
    });
    host = await render();
    expect(menuLabels(host)).toContain('Faturas');
    expect(menuLabels(host)).not.toContain('Equipe');
    expect(host.querySelector('#menu-principal')?.textContent).toContain('Conta de Frota Ltda · Gestor');
  });
});

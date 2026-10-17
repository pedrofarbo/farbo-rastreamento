// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { NotificationInbox } from '@/types';

const read: unknown[] = [];
let inbox: NotificationInbox;
vi.mock('@/api/resources', () => ({
  meApi: {
    notifications: async () => inbox,
    readNotifications: async (id?: number) => {
      read.push(id ?? 'todas');
    },
  },
}));

import { NotificationsScreen, appPath, byDay, dayLabel } from './NotificationsScreen';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string | RegExp) =>
  Array.from(document.querySelectorAll('button')).find((b) =>
    typeof label === 'string' ? b.textContent?.includes(label) : label.test(b.getAttribute('aria-label') ?? ''),
  ) as HTMLButtonElement;

function Where() {
  const location = useLocation();
  return <span data-testid="onde">{location.pathname + location.search}</span>;
}

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ToastProvider>
          <MemoryRouter initialEntries={['/alertas']}>
            <Routes>
              <Route path="/alertas" element={<NotificationsScreen />} />
              <Route path="*" element={<Where />} />
            </Routes>
          </MemoryRouter>
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
  return host;
}

afterEach(() => {
  document.body.innerHTML = '';
  read.length = 0;
});

describe('área de notificações', () => {
  it('o caminho no app e o dia', () => {
    expect(appPath('/app/veiculos/v1')).toBe('/veiculos/v1');
    expect(appPath('/app/faturas?pagar=i1')).toBe('/faturas?pagar=i1');
    expect(appPath('/app/')).toBe('/');
    expect(appPath('https://outro.site/x')).toBeNull();
    const now = new Date(2026, 9, 16, 10, 0);
    expect(dayLabel(new Date(2026, 9, 16, 0, 5).toISOString(), now)).toBe('Hoje');
    expect(dayLabel(new Date(2026, 9, 15, 23, 59).toISOString(), now)).toBe('Ontem');
    expect(dayLabel(new Date(2026, 9, 13, 8, 0).toISOString(), now)).toMatch(/13/);
    const groups = byDay(
      [
        { id: 3, title: 'a', body: '', url: '', severity: 'info', readAt: null, createdAt: new Date(2026, 9, 16, 9).toISOString() },
        { id: 2, title: 'b', body: '', url: '', severity: 'info', readAt: null, createdAt: new Date(2026, 9, 16, 8).toISOString() },
        { id: 1, title: 'c', body: '', url: '', severity: 'info', readAt: null, createdAt: new Date(2026, 9, 15, 8).toISOString() },
      ],
      now,
    );
    expect(groups.map((g) => [g.day, g.items.length])).toEqual([
      ['Hoje', 2],
      ['Ontem', 1],
    ]);
  });

  it('lista por dia, abre o que avisa marcando como lida e marca todas', async () => {
    const now = new Date();
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1, 18, 30);
    inbox = {
      unread: 2,
      items: [
        { id: 3, title: 'SOS acionado', body: 'Moto da Ana', url: '/app/veiculos/v1', severity: 'critical', readAt: null, createdAt: now.toISOString() },
        { id: 2, title: 'Fatura vence amanhã', body: 'R$ 39,90', url: '/app/faturas?pagar=i1', severity: 'warning', readAt: null, createdAt: now.toISOString() },
        { id: 1, title: 'Ignição ligada', body: 'Moto da Ana', url: '/app/veiculos/v1', severity: 'info', readAt: yesterday.toISOString(), createdAt: yesterday.toISOString() },
      ],
    };
    await render();
    expect(text()).toContain('Hoje');
    expect(text()).toContain('Ontem');
    expect(button(/^Não lida: SOS acionado$/)).toBeTruthy();
    expect(button(/^Ignição ligada$/)).toBeTruthy();

    // Marcar todas: a chamada sem id.
    await act(async () => button('Marcar todas como lidas').click());
    await flush();
    expect(read).toEqual(['todas']);
    expect(text()).not.toContain('Marcar todas como lidas');

    // Abrir a fatura leva à tela dela.
    await act(async () => button(/Fatura vence amanhã/).click());
    await flush();
    expect(document.querySelector('[data-testid="onde"]')?.textContent).toBe('/faturas?pagar=i1');
  });

  it('abrir uma não lida marca só ela', async () => {
    inbox = {
      unread: 1,
      items: [{ id: 7, title: 'Saída de cerca', body: 'Casa', url: '/app/veiculos/v2', severity: 'warning', readAt: null, createdAt: new Date().toISOString() }],
    };
    await render();
    await act(async () => button(/Saída de cerca/).click());
    await flush();
    expect(read).toEqual([7]);
    expect(document.querySelector('[data-testid="onde"]')?.textContent).toBe('/veiculos/v2');
  });

  it('sem notificações', async () => {
    inbox = { unread: 0, items: [] };
    await render();
    expect(text()).toContain('Nenhuma notificação ainda');
    expect(text()).toContain('Configurar alertas');
  });
});

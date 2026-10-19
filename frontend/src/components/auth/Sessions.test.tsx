// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { AuthSession } from '@/types';

const IPHONE_SAFARI =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6.1 Mobile/15E148 Safari/604.1';
const IPHONE_APP =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.5.2 Mobile/15E148 Safari/604.1';
let sessions: AuthSession[] = [];
let signedOut = 0;
vi.mock('@/api/resources', () => ({
  authApi: {
    sessions: async () => ({ sessions }),
    signOutOthers: async () => {
      signedOut++;
      const n = sessions.filter((s) => !s.current).length;
      sessions = sessions.filter((s) => s.current);
      return { accessToken: 'novo', expiresAt: '', signedOut: n };
    },
  },
}));

import { MySessions } from './Sessions';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');

afterEach(() => {
  document.body.innerHTML = '';
  signedOut = 0;
});

describe('aparelhos conectados', () => {
  it('mostra este aparelho e sai dos outros', async () => {
    const now = new Date().toISOString();
    sessions = [
      { id: 's1', userAgent: IPHONE_APP, lastSeenAt: now, expiresAt: now, current: true },
      { id: 's2', userAgent: IPHONE_SAFARI, lastSeenAt: now, expiresAt: now, current: false },
    ];
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <ToastProvider>
            <MySessions />
          </ToastProvider>
        </QueryClientProvider>,
      ),
    );
    await flush();
    expect(text()).toContain('iPhone · Safari (iOS 26.5)este aparelho');
    expect(text()).toContain('iPhone · Safari (iOS 26.6)');
    const button = Array.from(host.querySelectorAll('button')).find((b) => b.textContent?.includes('Sair dos outros aparelhos'))!;
    expect(button.textContent).toContain('(1)');
    await act(async () => button.click());
    await flush();
    await flush();
    expect(signedOut).toBe(1);
    expect(text()).toContain('1 aparelho desconectado');
    expect(text()).toContain('Só este aparelho está com a conta aberta.');
    expect(text()).not.toContain('iOS 26.6');
  });
});

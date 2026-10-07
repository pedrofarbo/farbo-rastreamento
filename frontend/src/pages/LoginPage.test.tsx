// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { tokens } from '@/api/client';
import { ToastProvider } from '@/components/ui/Toast';
import type { LoginResponse, User } from '@/types';

const user: User = {
  id: 'u1', email: 'ana@farbo.test', name: 'Ana', role: 'admin', phone: '', document: '', active: true, createdAt: '',
};
const logins: unknown[][] = [];
const verifies: unknown[][] = [];
let loginReply: () => LoginResponse;
vi.mock('@/api/resources', () => ({
  authApi: {
    login: async (...args: unknown[]) => {
      logins.push(args);
      return loginReply();
    },
    me: async () => user,
    logout: async () => undefined,
  },
  twoFactorApi: {
    verify: async (...args: unknown[]) => {
      verifies.push(args);
      return { accessToken: 'acc', refreshToken: 'ref', expiresAt: '', user, deviceToken: 'aparelho-1' };
    },
  },
}));

import { AuthProvider } from '@/stores/AuthContext';

import { LoginPage } from './LoginPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => document.body.textContent ?? '';
const input = (label: string) => {
  const l = Array.from(document.querySelectorAll('label')).find((el) => el.textContent?.startsWith(label));
  return (l?.htmlFor ? document.getElementById(l.htmlFor) : l?.querySelector('input')) as HTMLInputElement;
};
function type(el: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(el, value);
  el.dispatchEvent(new Event('input', { bubbles: true }));
}

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <MemoryRouter initialEntries={['/login']}>
        <ToastProvider>
          <AuthProvider>
            <Routes>
              <Route path="/login" element={<LoginPage />} />
              <Route path="/dashboard" element={<p>painel aberto</p>} />
            </Routes>
          </AuthProvider>
        </ToastProvider>
      </MemoryRouter>,
    ),
  );
  await flush();
}

async function signIn() {
  await act(async () => type(input('E-mail'), 'Ana@Farbo.test'));
  await act(async () => type(input('Senha'), 'senha-secreta-1'));
  await act(async () => (document.querySelector('button[type="submit"]') as HTMLButtonElement).click());
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  tokens.clear();
  localStorage.clear();
  logins.length = 0;
  verifies.length = 0;
});

describe('login do painel com a verificação em duas etapas', () => {
  it('senha, depois o código; o aparelho confiável vai no login seguinte', async () => {
    loginReply = () => ({
      twoFactor: { challenge: 'ch1', kind: 'verify', method: 'totp', emailHint: 'a***@farbo.test', expiresAt: '' },
    });
    await render();
    await signIn();
    expect(logins[0]).toEqual(['Ana@Farbo.test', 'senha-secreta-1', '']);
    expect(text()).toContain('Confirme que é você');
    await act(async () => (document.querySelector('input[type="checkbox"]') as HTMLInputElement).click());
    await act(async () => type(document.querySelector('input[autocomplete="one-time-code"]') as HTMLInputElement, '123456'));
    await flush();
    expect(verifies[0]).toEqual(['ch1', '123456', true]);
    expect(text()).toContain('painel aberto');

    // De volta à tela de login (outra sessão): o token do aparelho vai junto.
    tokens.clear();
    localStorage.removeItem('tracker.user');
    document.body.innerHTML = '';
    loginReply = () => ({ accessToken: 'acc2', refreshToken: 'ref2', expiresAt: '', user });
    await render();
    await signIn();
    expect(logins[1]).toEqual(['Ana@Farbo.test', 'senha-secreta-1', 'aparelho-1']);
    expect(text()).toContain('painel aberto');
  });

  it('desistir volta para a senha', async () => {
    loginReply = () => ({
      twoFactor: { challenge: 'ch2', kind: 'verify', method: 'email', emailHint: 'a***@farbo.test', expiresAt: '' },
    });
    await render();
    await signIn();
    expect(text()).toContain('a***@farbo.test');
    await act(async () => (Array.from(document.querySelectorAll('button')).find((b) => b.textContent === 'Voltar') as HTMLButtonElement).click());
    expect(input('Senha')).toBeTruthy();
  });
});

// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';

let loggedIn: { name: string } | null = null;
vi.mock('@/stores/AuthContext', () => ({ useAuth: () => ({ user: loggedIn, logout: async () => undefined }) }));
vi.mock('@/api/resources', () => ({
  authApi: {
    validateResetToken: async () => {
      throw new ApiError(410, 'link inválido ou expirado');
    },
  },
}));

import { ResetPasswordPage } from './ResetPasswordPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const buttons = () => Array.from(document.querySelectorAll('button')).map((b) => b.textContent);

async function open(hash: string): Promise<string> {
  window.location.hash = hash;
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <MemoryRouter initialEntries={['/redefinir-senha' + hash]}>
        <ResetPasswordPage />
      </MemoryRouter>,
    ),
  );
  await flush();
  return document.body.textContent ?? '';
}

afterEach(() => {
  document.body.innerHTML = '';
  loggedIn = null;
  window.location.hash = '';
});

describe('o link de boas-vindas (ou de redefinição) que já não vale', () => {
  it('convite já usado: explica e leva a entrar', async () => {
    const text = await open('#token=velho&boasvindas=1');
    expect(text).toContain('Este convite já foi usado');
    expect(text).toContain('Se você já criou a sua, é só entrar');
    expect(buttons()).toEqual(['Entrar', 'Pedir um novo link']);
    expect(text).not.toContain('Link inválido ou expirado');
  });

  it('já conectado neste aparelho: abre o painel', async () => {
    loggedIn = { name: 'Cliente' };
    const text = await open('#token=velho&boasvindas=1');
    expect(text).toContain('Você já está conectado');
    expect(buttons()).toEqual(['Abrir o painel']);
  });

  it('redefinição comum vencida: o aviso de sempre', async () => {
    const text = await open('#token=velho');
    expect(text).toContain('Link inválido ou expirado');
    expect(buttons()).toEqual(['Pedir um novo link']);
  });
});

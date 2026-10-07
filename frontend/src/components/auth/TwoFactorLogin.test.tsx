// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import { ToastProvider } from '@/components/ui/Toast';
import type { LoginResponse, TwoFactorChallenge } from '@/types';

const calls: unknown[][] = [];
let verify: () => Promise<LoginResponse>;
vi.mock('@/api/resources', () => ({
  twoFactorApi: {
    verify: (...args: unknown[]) => {
      calls.push(['verify', ...args]);
      return verify();
    },
    resend: async (...args: unknown[]) => {
      calls.push(['resend', ...args]);
    },
    setupEmail: async (...args: unknown[]) => {
      calls.push(['setupEmail', ...args]);
      return { secret: 'JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP', otpauthUrl: 'otpauth://totp/x', qrCode: '<svg></svg>' };
    },
    setupConfirm: async (...args: unknown[]) => {
      calls.push(['setupConfirm', ...args]);
      return { ...session, recoveryCodes: ['abcd-efgh', 'jkmn-pqrs'] };
    },
  },
}));

import { TwoFactorLogin } from './TwoFactorLogin';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => document.body.textContent ?? '';
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.startsWith(label)) as HTMLButtonElement;
const codeInput = () => document.querySelector('input[autocomplete="one-time-code"]') as HTMLInputElement;
function type(el: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(el, value);
  el.dispatchEvent(new Event('input', { bubbles: true }));
}

const session: LoginResponse = { accessToken: 'a', refreshToken: 'r', expiresAt: '', deviceToken: 'dev' };
const challenge = (over: Partial<TwoFactorChallenge> = {}): TwoFactorChallenge => ({
  challenge: 'ch1', kind: 'verify', method: 'totp', emailHint: 'l***@farbo.test', expiresAt: '', ...over,
});

async function render(ch: TwoFactorChallenge, trustByDefault = false) {
  const done: LoginResponse[] = [];
  const cancelled: (string | undefined)[] = [];
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <ToastProvider>
        <TwoFactorLogin
          challenge={ch}
          trustByDefault={trustByDefault}
          onDone={(r) => done.push(r)}
          onCancel={(m) => cancelled.push(m)}
        />
      </ToastProvider>,
    ),
  );
  return { done, cancelled };
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('verificação em duas etapas no login', () => {
  it('código do app: seis dígitos conferem sozinhos, com o aparelho confiável', async () => {
    verify = async () => session;
    const { done } = await render(challenge(), true);
    expect(text()).toContain('código de 6 dígitos do seu app autenticador');
    // Só dígitos, até seis.
    await act(async () => type(codeInput(), '12a34'));
    expect(codeInput().value).toBe('1234');
    await act(async () => type(codeInput(), '123456'));
    await flush();
    expect(calls).toEqual([['verify', 'ch1', '123456', true]]);
    expect(done).toEqual([session]);
  });

  it('código de recuperação; erro aparece; expirado volta para a senha', async () => {
    verify = async () => {
      throw new ApiError(400, 'Código incorreto. 4 tentativas restantes.');
    };
    const { cancelled } = await render(challenge());
    await act(async () => button('Usar um código de recuperação').click());
    expect(text()).toContain('códigos de recuperação');
    await act(async () => type(codeInput(), 'abcd-efgh'));
    await act(async () => button('Entrar').click());
    await flush();
    expect(calls).toEqual([['verify', 'ch1', 'abcd-efgh', false]]);
    expect(text()).toContain('4 tentativas restantes');

    verify = async () => {
      throw new ApiError(410, 'A verificação expirou. Entre de novo com a senha.', { code: 'TWO_FACTOR_EXPIRED' });
    };
    await act(async () => type(codeInput(), 'abcd-efgh'));
    await act(async () => button('Entrar').click());
    await flush();
    expect(cancelled).toEqual(['A verificação expirou. Entre de novo com a senha.']);
  });

  it('código por e-mail: mostra para onde foi e só reenvia depois de 30 s', async () => {
    vi.useFakeTimers();
    try {
      await render(challenge({ method: 'email' }));
      expect(text()).toContain('l***@farbo.test');
      expect(button('Reenviar o código').disabled).toBe(true);
      expect(text()).toContain('(30 s)');
      for (let i = 0; i < 30; i++) {
        await act(async () => {
          vi.advanceTimersByTime(1000);
        });
      }
      expect(button('Reenviar o código').disabled).toBe(false);
      await act(async () => button('Reenviar o código').click());
      expect(calls).toEqual([['resend', 'ch1']]);
    } finally {
      vi.useRealTimers();
    }
  });

  it('a equipe ativa: código do e-mail, QR Code do app e códigos de recuperação', async () => {
    const { done } = await render(challenge({ kind: 'setup', method: '' }));
    expect(text()).toContain('Passo 1 de 3');
    expect(text()).not.toContain('Confiar neste aparelho');
    await act(async () => type(codeInput(), '111222'));
    await flush();
    expect(calls[0]).toEqual(['setupEmail', 'ch1', '111222']);
    expect(text()).toContain('Passo 2 de 3');
    expect(document.querySelector('img[alt="QR Code para o app autenticador"]')).toBeTruthy();
    expect(text()).toContain('JBSW Y3DP EHPK 3PXP');

    await act(async () => (document.querySelector('input[type="checkbox"]') as HTMLInputElement).click());
    await act(async () => type(codeInput(), '333444'));
    await flush();
    expect(calls[1]).toEqual(['setupConfirm', 'ch1', '333444', true]);
    expect(text()).toContain('abcd-efgh');
    // Só continua depois de confirmar que guardou os códigos.
    expect(button('Entrar no painel').disabled).toBe(true);
    const saved = Array.from(document.querySelectorAll('input[type="checkbox"]')).at(-1) as HTMLInputElement;
    await act(async () => saved.click());
    await act(async () => button('Entrar no painel').click());
    expect(done[0]?.accessToken).toBe('a');
  });
});

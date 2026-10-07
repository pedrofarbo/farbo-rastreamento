// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import { ToastProvider } from '@/components/ui/Toast';
import type { TwoFactorStatus } from '@/types';

const calls: unknown[][] = [];
let status: TwoFactorStatus;
let disable: () => Promise<void>;
vi.mock('@/api/resources', () => ({
  twoFactorApi: {
    status: async () => status,
    start: async (...args: unknown[]) => {
      calls.push(['start', ...args]);
      return args[0] === 'totp'
        ? { secret: 'JBSWY3DPEHPK3PXP', otpauthUrl: 'otpauth://totp/x', qrCode: '<svg></svg>' }
        : { secret: '', otpauthUrl: '', qrCode: '' };
    },
    confirm: async (...args: unknown[]) => {
      calls.push(['confirm', ...args]);
      return { recoveryCodes: ['abcd-efgh'], method: 'email' };
    },
    sendCode: async () => {
      calls.push(['sendCode']);
    },
    disable: (...args: unknown[]) => {
      calls.push(['disable', ...args]);
      return disable();
    },
    recoveryCodes: async () => ({ recoveryCodes: ['wxyz-2345'] }),
    revokeDevice: async (...args: unknown[]) => {
      calls.push(['revoke', ...args]);
    },
  },
}));

import { TwoFactorSettings, deviceLabel } from './TwoFactorSettings';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => document.body.textContent ?? '';
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.startsWith(label)) as HTMLButtonElement;
const field = (label: string) => {
  const l = Array.from(document.querySelectorAll('label')).find((el) => el.textContent?.startsWith(label));
  return (l?.htmlFor ? document.getElementById(l.htmlFor) : l?.querySelector('input')) as HTMLInputElement;
};
function type(el: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(el, value);
  el.dispatchEvent(new Event('input', { bubbles: true }));
}

const base: TwoFactorStatus = {
  enabled: false, method: '', required: false, enabledAt: null, methods: ['totp', 'email'], recoveryCodesLeft: 0,
  devices: [], emailHint: 'l***@farbo.test',
};

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ToastProvider>
          <TwoFactorSettings />
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('segurança da conta', () => {
  it('o aparelho pelo navegador', () => {
    expect(deviceLabel('Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit Version/18.0 Mobile Safari/604.1')).toBe('Safari no iPhone');
    expect(deviceLabel('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit Chrome/129.0 Safari/537.36')).toBe('Chrome no Windows');
    expect(deviceLabel('Mozilla/5.0 (Windows NT 10.0) Chrome/129.0 Safari/537.36 Edg/129.0')).toBe('Edge no Windows');
    expect(deviceLabel('')).toBe('Aparelho desconhecido');
  });

  it('o cliente ativa pelo e-mail: senha, código e códigos de recuperação', async () => {
    status = base;
    await render();
    expect(text()).toContain('Desativada');
    await act(async () => (document.querySelectorAll('input[type="radio"]')[1] as HTMLInputElement).click());
    await act(async () => button('Ativar a verificação').click());
    expect(text()).toContain('l***@farbo.test');
    await act(async () => type(field('Sua senha'), 'senha-secreta-1'));
    await act(async () => button('Continuar').click());
    await flush();
    expect(calls[0]).toEqual(['start', 'email', 'senha-secreta-1']);
    expect(text()).toContain('Mandamos um código');
    await act(async () => type(document.querySelector('input[autocomplete="one-time-code"]') as HTMLInputElement, '654321'));
    await flush();
    expect(calls[1]).toEqual(['confirm', '654321']);
    expect(text()).toContain('abcd-efgh');
  });

  it('ativa com o app: cliente desativa com a senha e o código; a equipe não', async () => {
    status = {
      ...base, enabled: true, method: 'totp', recoveryCodesLeft: 2,
      devices: [{ id: 'd1', userAgent: 'Chrome/1 Windows', createdAt: '2026-10-01T10:00:00Z', lastUsedAt: '2026-10-06T10:00:00Z', expiresAt: '2026-10-31T10:00:00Z' }],
    };
    disable = async () => {
      throw new ApiError(400, 'Código incorreto.');
    };
    await render();
    expect(text()).toContain('Ativa');
    expect(text()).toContain('Restam só 2 códigos');
    expect(button('Trocar para o código por e-mail')).toBeTruthy();
    expect(text()).toContain('Chrome no Windows');
    await act(async () => button('Remover').click());
    await flush();
    expect(calls).toEqual([['revoke', 'd1']]);

    await act(async () => button('Desativar').click());
    await act(async () => type(field('Sua senha'), 'senha-secreta-1'));
    await act(async () => type(field('Código do app autenticador'), '123456'));
    await act(async () => (Array.from(document.querySelectorAll('button')).filter((b) => b.textContent === 'Desativar').at(-1) as HTMLButtonElement).click());
    await flush();
    expect(calls.at(-1)).toEqual(['disable', 'senha-secreta-1', '123456']);
    expect(text()).toContain('Código incorreto');

    // A equipe: obrigatória, só o app, sem desativar.
    document.body.innerHTML = '';
    status = { ...base, enabled: true, method: 'totp', required: true, methods: ['totp'], recoveryCodesLeft: 10 };
    await render();
    expect(text()).toContain('Obrigatória para a equipe');
    expect(button('Desativar')).toBeFalsy();
    expect(button('Trocar para')).toBeFalsy();
  });
});

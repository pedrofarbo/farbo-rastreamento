// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { SmsUsage } from '@/types';

let usage: SmsUsage;
vi.mock('@/api/resources', () => ({ smsSetupApi: { usage: async () => usage } }));

import { SmsCounter } from './SmsCounter';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

async function render(value: SmsUsage): Promise<string> {
  usage = value;
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <SmsCounter />
      </QueryClientProvider>,
    ),
  );
  await flush();
  return (host.textContent ?? '').replace(/ /g, ' ');
}

const base: SmsUsage = { enabled: true, balance: 1003, balanceError: '', activationSms: 4, activations: 250, sentLast30Days: 12 };

afterEach(() => {
  document.body.innerHTML = '';
});

describe('o contador de SMS da página de rastreadores', () => {
  it('mostra o saldo, quantos rastreadores ele ativa e o mês', async () => {
    const text = await render(base);
    expect(text).toContain('1.003 SMS');
    expect(text).toContain('250 rastreadores');
    expect(text).toContain('4 SMS por ativação (5 pedindo a configuração de volta)');
    expect(text).toContain('12 SMS');
    expect(text).toContain('Comprar SMS');
    expect(text).not.toContain('Saldo baixo');
    expect(document.querySelector('a')?.getAttribute('href')).toBe('https://painel.smsdev.com.br');
  });

  it('avisa quando o saldo está baixo', async () => {
    expect(await render({ ...base, balance: 8, activations: 2 })).toContain('Saldo baixo: dá para só 2.');
    document.body.innerHTML = '';
    const empty = await render({ ...base, balance: 3, activations: 0 });
    expect(empty).toContain('0 rastreadores');
    expect(empty).toContain('não dá para ativar nenhum rastreador');
  });

  it('sem conseguir ler o saldo, ou com o SMS desligado', async () => {
    const failed = await render({ ...base, balance: null, activations: 0, balanceError: 'CHAVE INVALIDA' });
    expect(failed).toContain('Não deu para ler o saldo: CHAVE INVALIDA');
    expect(failed).not.toContain('Saldo baixo');
    document.body.innerHTML = '';
    expect(await render({ ...base, enabled: false, balance: null })).toContain('desligado');
  });
});

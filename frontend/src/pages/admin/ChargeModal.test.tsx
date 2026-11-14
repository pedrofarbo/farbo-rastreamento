// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { CustomerDetail, Invoice } from '@/types';

const state = vi.hoisted(() => ({ detail: null as unknown }));
vi.mock('@/api/resources', () => ({ customersApi: { get: async () => state.detail } }));

import { ChargeModal, chargeMessage, openTotals } from './ChargeModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/\u00a0/g, ' ');

const invoice = (over: Partial<Invoice>): Invoice =>
  ({ id: 'i1', description: 'Plano', amountCents: 6990, dueDate: '2026-11-10', status: 'OPEN', ...over }) as Invoice;
const DETAIL = {
  id: 'c1', name: 'Ana Maria Souza', phone: '(11) 99999-0000', payAllUrl: 'https://farbo.test/pagar/conta/abc',
  invoices: [
    invoice({ id: 'i1', dueDate: '2026-11-10' }),
    invoice({ id: 'i2', amountCents: 5000, dueDate: '2026-12-10' }),
    invoice({ id: 'i3', status: 'PAID' }),
    invoice({ id: 'i4', status: 'CANCELED' }),
  ],
} as unknown as CustomerDetail;

async function render(customerId: string | null = 'c1') {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ChargeModal customerId={customerId} onClose={() => {}} />
      </QueryClientProvider>,
    ),
  );
  await act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
}
const button = (label: string) =>
  Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent === label);
const textarea = () => document.querySelector('textarea') as HTMLTextAreaElement;

afterEach(() => {
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

describe('a mensagem da cobrança', () => {
  it('as em aberto de qualquer vencimento, as vencidas e o link; só o primeiro nome', () => {
    expect(openTotals(DETAIL, '2026-11-13')).toEqual({ count: 2, cents: 11990, overdue: 1 });
    expect(chargeMessage(DETAIL, '2026-11-13').replace(/\u00a0/g, ' ')).toBe(
      'Olá, Ana! Aqui é da Farbo Rastreadores. Você tem 2 faturas em aberto, no total de R$ 119,90 (1 vencida). ' +
        'Pelo link abaixo você paga tudo de uma vez, sem login, por Pix, boleto ou cartão:\nhttps://farbo.test/pagar/conta/abc',
    );
  });

  it('uma só, nenhuma vencida; sem o cartão no link, não promete o cartão', () => {
    const one = { ...DETAIL, invoices: [invoice({ dueDate: '2026-12-10' })], cardMissing: ['celular com DDD'] };
    expect(chargeMessage(one, '2026-11-13').replace(/\u00a0/g, ' ')).toBe(
      'Olá, Ana! Aqui é da Farbo Rastreadores. Você tem 1 fatura em aberto, no total de R$ 69,90. ' +
        'Pelo link abaixo você paga tudo de uma vez, sem login:\nhttps://farbo.test/pagar/conta/abc',
    );
  });
});

describe('ChargeModal', () => {
  it('abre com a mensagem pronta; o WhatsApp leva a mensagem como ficou, e copia a mensagem ou só o link', async () => {
    state.detail = DETAIL;
    const open = vi.spyOn(window, 'open').mockImplementation(() => null);
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    await render();
    expect(text()).toContain('Cobrar Ana Maria Souza');
    expect(textarea().value).toContain('Você tem 2 faturas em aberto');
    expect(textarea().value).toContain('https://farbo.test/pagar/conta/abc');

    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(textarea(), 'Oi, Ana! Segue o link: x');
      textarea().dispatchEvent(new Event('input', { bubbles: true }));
    });
    await act(async () => button('Enviar pelo WhatsApp')!.click());
    expect(open).toHaveBeenCalledWith(
      `https://wa.me/5511999990000?text=${encodeURIComponent('Oi, Ana! Segue o link: x')}`,
      '_blank',
      'noopener,noreferrer',
    );

    await act(async () => button('Copiar mensagem')!.click());
    expect(writeText).toHaveBeenLastCalledWith('Oi, Ana! Segue o link: x');
    expect(button('Mensagem copiada')).toBeDefined();
    await act(async () => button('Copiar só o link')!.click());
    expect(writeText).toHaveBeenLastCalledWith('https://farbo.test/pagar/conta/abc');
  });

  it('sem celular, sem o WhatsApp; com o cadastro incompleto, o aviso; sem nada em aberto, diz isso', async () => {
    state.detail = { ...DETAIL, phone: '', cardMissing: ['endereço completo'] };
    await render();
    expect(button('Enviar pelo WhatsApp')).toBeUndefined();
    expect(button('Copiar mensagem')).toBeDefined();
    expect(text()).toContain('Sem celular com DDD no cadastro');
    expect(text()).toContain('O cartão não aparece no link: falta endereço completo no cadastro.');

    document.body.innerHTML = '';
    state.detail = { ...DETAIL, invoices: [invoice({ status: 'PAID' })] };
    await render();
    expect(text()).toContain('Ana Maria Souza não tem fatura em aberto.');
    expect(button('Copiar mensagem')).toBeUndefined();
  });

  it('fechado, não busca nem mostra nada', async () => {
    state.detail = DETAIL;
    await render(null);
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  });
});

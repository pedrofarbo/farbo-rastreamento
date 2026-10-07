// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import { ToastProvider } from '@/components/ui/Toast';
import type { FinanceEntry, ShippingBalance, ShippingTopUp } from '@/types';

let balance: () => Promise<ShippingBalance>;
let topUp: (valueCents: number, method: string) => Promise<ShippingTopUp>;
let entry: (id: string, code: string) => Promise<FinanceEntry>;
const topUps: { valueCents: number; method: string }[] = [];
const entries: { id: string; code: string }[] = [];
const removed: string[] = [];
vi.mock('@/api/resources', () => ({
  financeApi: {
    removeEntry: async (id: string) => {
      removed.push(id);
    },
  },
  shippingIntegrationApi: {
    balance: () => balance(),
    addBalance: (valueCents: number, method: string) => {
      topUps.push({ valueCents, method });
      return topUp(valueCents, method);
    },
    topUpEntry: (id: string, code = '') => {
      entries.push({ id, code });
      return entry(id, code);
    },
  },
}));
// A tela de Pix dos fornecedores (testada à parte): aqui, só o envio.
vi.mock('@/pages/admin/company/PixModals', () => ({
  PixPayModal: ({ entry: e, onSent, onClose }: { entry: FinanceEntry; onSent?: () => void; onClose: () => void }) => (
    <div role="dialog">
      <span>Pagar por Pix: {e.description}</span>
      <button type="button" onClick={() => onSent?.()}>
        Enviar o Pix
      </button>
      <button type="button" onClick={onClose}>
        Fechar o Pix
      </button>
    </div>
  ),
}));

import { ShippingWallet, suggestTopUp } from './ShippingWallet';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) =>
    b.textContent?.replace(/\u00a0/g, ' ').startsWith(label),
  ) as HTMLButtonElement;
function type(el: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(el, value);
  el.dispatchEvent(new Event('input', { bubbles: true }));
}

const wallet = (cents: number): ShippingBalance => ({
  balanceCents: cents, reservedCents: 0, debtsCents: 0, checkedAt: '2026-10-06T14:30:00Z', panelUrl: 'https://me.test/painel',
});
const pix: ShippingTopUp = {
  id: 'p1', providerId: 'pay-1', protocol: 'PAY-1', status: 'pending', method: 'pix', valueCents: 2000,
  link: 'https://me.test/pix/1', digitable: '', pixCode: '', entryId: null, createdAt: '2026-10-07T12:00:00Z',
  panelUrl: 'https://me.test/painel',
};
const CODE = '00020126360014br.gov.bcb.pix0114+5511999999999520400005303986540520.005802BR5905YAPAY6009SAO PAULO6304ABCD';
const bill = { id: 'e1', description: 'Recarga da carteira do Melhor Envios (PAY-1)' } as FinanceEntry;

async function render(needCents?: number, payWithAbacate = false) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <ToastProvider>
          <ShippingWallet panelUrl="https://me.test/painel" needCents={needCents} payWithAbacate={payWithAbacate} />
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  topUps.length = 0;
  entries.length = 0;
  removed.length = 0;
  vi.restoreAllMocks();
});

describe('carteira do Melhor Envios', () => {
  it('sugere cobrir o que falta, de R$ 10 em R$ 10', () => {
    expect(suggestTopUp(1240)).toBe(2000);
    expect(suggestTopUp(300)).toBe(1000);
    expect(suggestTopUp(5000)).toBe(5000);
    expect(suggestTopUp(0)).toBe(10000);
  });

  it('mostra o saldo; sem saldo para a etiqueta, avisa quanto falta e gera o Pix que cobre', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null);
    balance = async () => wallet(1000);
    topUp = async () => pix;
    await render(2240);
    expect(text()).toContain('R$ 10,00');
    expect(text()).toContain('faltam R$ 12,40');

    await act(async () => button('Adicionar saldo').click());
    // O valor sugerido cobre o que falta.
    const input = document.querySelector('input[inputmode="decimal"]') as HTMLInputElement;
    expect(input.value).toBe('20,00');
    await act(async () => button('Gerar Pix').click());
    await flush();
    expect(topUps).toEqual([{ valueCents: 2000, method: 'pix' }]);
    expect(text()).toContain('Pix de R$ 20,00 gerado (PAY-1)');
    await act(async () => button('Abrir o Pix para pagar').click());
    expect(open).toHaveBeenCalledWith('https://me.test/pix/1', '_blank', 'noopener,noreferrer');
    expect(text()).toContain('Esta tela confere sozinha');

    // O Pix caiu: a carteira cobre a etiqueta e a tela avisa.
    balance = async () => wallet(3000);
    await act(async () => button('Atualizar').click());
    await flush();
    expect(text()).toContain('Saldo creditado: a carteira agora tem R$ 30,00');
    expect(text()).not.toContain('faltam');
    // Pago: o botão de pagar sai.
    expect(button('Abrir o Pix para pagar')).toBeFalsy();
  });

  it('boleto: a linha digitável para copiar; valor fora do limite não vai', async () => {
    balance = async () => wallet(50000);
    topUp = async (valueCents) => ({ ...pix, method: 'boleto', valueCents, link: '', digitable: '34191.79001 01043' });
    await render();
    expect(text()).not.toContain('faltam');
    await act(async () => button('Adicionar saldo').click());
    const input = document.querySelector('input[inputmode="decimal"]') as HTMLInputElement;
    await act(async () => type(input, '0,50'));
    await act(async () => button('Gerar Pix').click());
    expect(text()).toContain('Informe um valor entre R$ 1,00 e R$ 10.000,00');
    expect(topUps).toEqual([]);

    await act(async () => button('R$ 200,00').click());
    const boleto = Array.from(document.querySelectorAll('input[type="radio"]'))[1] as HTMLInputElement;
    await act(async () => boleto.click());
    await act(async () => button('Gerar boleto de R$ 200,00').click());
    await flush();
    expect(topUps).toEqual([{ valueCents: 20000, method: 'boleto' }]);
    expect(text()).toContain('Boleto de R$ 200,00 gerado');
    expect(text()).toContain('34191.79001 01043');
    expect(button('Copiar a linha digitável')).toBeTruthy();
    expect(button('Abrir o boleto')).toBeFalsy();
  });

  it('sem o link do pagamento, leva à carteira no painel', async () => {
    balance = async () => wallet(0);
    topUp = async () => ({ ...pix, link: '', digitable: '' });
    await render();
    await act(async () => button('Adicionar saldo').click());
    await act(async () => button('Gerar Pix').click());
    await flush();
    const link = Array.from(document.querySelectorAll('a')).find((a) => a.textContent?.startsWith('Pagar pela carteira'));
    expect(link?.getAttribute('href')).toBe('https://me.test/painel');
  });

  it('com o copia-e-cola na resposta, paga com o saldo da AbacatePay pela tela de Pix', async () => {
    balance = async () => wallet(1000);
    topUp = async () => ({ ...pix, pixCode: CODE });
    entry = async () => bill;
    await render(2240, true);
    await act(async () => button('Adicionar saldo').click());
    expect(text()).toContain('Pelo banco ou com o saldo da AbacatePay');
    await act(async () => button('Gerar Pix').click());
    await flush();
    // O pelo banco continua, como alternativa.
    expect(button('Abrir o Pix')).toBeTruthy();
    expect(document.querySelector('textarea')).toBeNull();
    // Abriu e desistiu: a conta não fica sobrando em aberto.
    await act(async () => button('Pagar com o saldo da AbacatePay').click());
    await flush();
    await act(async () => button('Fechar o Pix').click());
    await flush();
    expect(removed).toEqual(['e1']);
    await act(async () => button('Pagar com o saldo da AbacatePay').click());
    await flush();
    expect(entries).toEqual([{ id: 'p1', code: '' }, { id: 'p1', code: '' }]);
    expect(text()).toContain('Pagar por Pix: Recarga da carteira do Melhor Envios (PAY-1)');
    await act(async () => button('Enviar o Pix').click());
    await act(async () => button('Fechar o Pix').click());
    expect(text()).toContain('Pix enviado pela AbacatePay');
    expect(button('Pagar com o saldo da AbacatePay')).toBeFalsy();
    // Pago: a conta fica (baixada como paga).
    expect(removed).toEqual(['e1']);
  });

  it('sem o copia-e-cola na resposta, cola o da página do Pix; recusa aparece', async () => {
    balance = async () => wallet(1000);
    topUp = async () => pix;
    entry = async () => {
      throw new ApiError(400, 'O Pix copia-e-cola é de R$ 50,00 e a recarga é de R$ 20,00: confira se é o Pix desta recarga.');
    };
    await render(2240, true);
    await act(async () => button('Adicionar saldo').click());
    await act(async () => button('Gerar Pix').click());
    await flush();
    expect(button('Abrir o Pix para pagar')).toBeTruthy();
    const pay = button('Pagar com o saldo da AbacatePay');
    expect(pay.disabled).toBe(true);
    const area = document.querySelector('textarea') as HTMLTextAreaElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set?.call(area, CODE);
      area.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await act(async () => button('Pagar com o saldo da AbacatePay').click());
    await flush();
    expect(entries).toEqual([{ id: 'p1', code: CODE }]);
    expect(text()).toContain('O Pix copia-e-cola é de R$ 50,00');
  });

  it('sem a AbacatePay (ou no boleto), só o pagamento no Melhor Envios', async () => {
    balance = async () => wallet(1000);
    topUp = async () => ({ ...pix, pixCode: CODE });
    await render(2240, false);
    await act(async () => button('Adicionar saldo').click());
    await act(async () => button('Gerar Pix').click());
    await flush();
    expect(button('Pagar com o saldo da AbacatePay')).toBeFalsy();
    expect(button('Abrir o Pix para pagar')).toBeTruthy();
  });

  it('sem permissão para o saldo: pede para conectar de novo; recusa da recarga aparece', async () => {
    balance = async () => {
      throw new ApiError(409, 'sem permissão', { code: 'BALANCE_FORBIDDEN' });
    };
    topUp = async () => {
      throw new ApiError(502, 'Melhor Envios: Valor mínimo de R$ 5,00');
    };
    await render(2240);
    expect(text()).toContain('desconecte e conecte o Melhor Envios de novo');
    const panel = Array.from(document.querySelectorAll('a')).find((a) => a.textContent === 'painel do Melhor Envios');
    expect(panel?.getAttribute('href')).toBe('https://me.test/painel');
    expect(text()).not.toContain('faltam');

    await act(async () => button('Adicionar saldo').click());
    await act(async () => button('Gerar Pix').click());
    await flush();
    expect(text()).toContain('Não deu para gerar a cobrança: Melhor Envios: Valor mínimo de R$ 5,00');
  });
});

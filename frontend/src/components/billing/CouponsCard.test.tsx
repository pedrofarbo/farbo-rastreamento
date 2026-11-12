// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import { ToastProvider } from '@/components/ui/Toast';
import type { Coupon, CouponInput } from '@/types';

let list: Coupon[];
const created: CouponInput[] = [];
const updated: [string, Omit<CouponInput, 'code'>][] = [];
const removed: string[] = [];
vi.mock('@/api/resources', () => ({
  couponsApi: {
    list: async () => list,
    create: async (input: CouponInput) => {
      if (input.code === 'REPETIDO') throw new ApiError(409, 'já existe um cupom com este código');
      created.push(input);
      return { ...input, id: 'novo', uses: 0, createdAt: '', updatedAt: '' };
    },
    update: async (id: string, input: Omit<CouponInput, 'code'>) => {
      updated.push([id, input]);
      return list[0];
    },
    remove: async (id: string) => {
      removed.push(id);
    },
  },
}));

import { CouponsCard, couponStatus } from './CouponsCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim().startsWith(label)) as HTMLButtonElement;
function field(label: string): HTMLInputElement {
  const el = Array.from(document.querySelectorAll('label')).find((l) => l.textContent === label);
  return document.getElementById(el!.htmlFor) as HTMLInputElement;
}
function type(el: HTMLInputElement | HTMLSelectElement, value: string) {
  const proto = el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(el, value);
  act(() => {
    el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  });
}
async function click(el: HTMLElement) {
  await act(async () => el.click());
  await flush();
}

const base: Coupon = {
  id: 'c1', code: 'BEMVINDO', description: 'Instagram', percent: 10, amountCents: null, charges: 3, validUntil: null,
  maxUses: null, active: true, uses: 2, createdAt: '', updatedAt: '',
};

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ToastProvider>
          <CouponsCard />
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

beforeEach(() => {
  list = [];
  created.length = 0;
  updated.length = 0;
  removed.length = 0;
});
afterEach(() => {
  document.body.innerHTML = '';
});

describe('os cupons de desconto (central)', () => {
  it('a situação: valendo, desligado, vencido ou esgotado', () => {
    expect(couponStatus(base, '2026-11-11').label).toBe('Valendo');
    expect(couponStatus({ ...base, active: false }, '2026-11-11').label).toBe('Desligado');
    expect(couponStatus({ ...base, validUntil: '2026-11-10' }, '2026-11-11').label).toBe('Vencido');
    expect(couponStatus({ ...base, validUntil: '2026-11-11' }, '2026-11-11').label).toBe('Valendo');
    expect(couponStatus({ ...base, maxUses: 2 }, '2026-11-11').label).toBe('Esgotado');
  });

  it('a lista com o desconto, a validade e os usos; o usado edita, mas não exclui', async () => {
    list = [
      base,
      { ...base, id: 'c2', code: 'VINTE', description: '', percent: null, amountCents: 2000, charges: 1, maxUses: 5, uses: 0 },
      { ...base, id: 'c3', code: 'SEMPRE', description: '', percent: 20, charges: null, uses: 1 },
    ];
    await render();
    expect(text()).toContain('BEMVINDOInstagram10% nas 3 primeirassem validade2Valendo');
    expect(text()).toContain('VINTER$ 20,00 na 1ª cobrança');
    expect(text()).toContain('0 de 5');
    expect(text()).toContain('SEMPRE20% para sempre');
    await click(button('Editar'));
    expect(field('Código').disabled).toBe(true);
    expect(button('Excluir')).toBeUndefined();
    type(field('Porcentagem (%)'), '15');
    await click(button('Salvar'));
    expect(updated).toEqual([
      ['c1', { description: 'Instagram', percent: 15, amountCents: null, charges: 3, validUntil: null, maxUses: null, active: true }],
    ]);
  });

  it('o novo: confere antes de mandar, mostra o erro do servidor e cria', async () => {
    await render();
    expect(text()).toContain('Nenhum cupom ainda');
    await click(button('Novo cupom'));
    type(field('Código'), 'a b');
    type(field('Porcentagem (%)'), '10');
    await click(button('Criar cupom'));
    expect(text()).toContain('O código tem de 3 a 30 letras');
    type(field('Código'), 'repetido');
    await click(button('Criar cupom'));
    expect(text()).toContain('já existe um cupom com este código');
    type(field('Código'), 'vinte');
    type(document.querySelector('select') as HTMLSelectElement, 'amount');
    type(field('Valor (R$)'), '20,00');
    type(field('Nas primeiras (cobranças)'), '2');
    type(field('Limite de usos (opcional)'), '50');
    type(field('Válido até (opcional)'), '2026-12-31');
    expect(text()).toContain('/contratar?cupom=VINTE');
    await click(button('Criar cupom'));
    expect(created).toEqual([
      { code: 'VINTE', description: '', percent: null, amountCents: 2000, charges: 2, validUntil: '2026-12-31', maxUses: 50, active: true },
    ]);
  });

  it('o para sempre: sem o número de cobranças', async () => {
    await render();
    await click(button('Novo cupom'));
    type(field('Código'), 'sempre');
    type(field('Porcentagem (%)'), '20');
    type(document.querySelectorAll('select')[1] as HTMLSelectElement, 'forever');
    expect(document.body.textContent).not.toContain('Nas primeiras (cobranças)');
    expect(text()).toContain('O próximo veículo do cliente sai no preço normal.');
    await click(button('Criar cupom'));
    expect(created).toEqual([
      { code: 'SEMPRE', description: '', percent: 20, amountCents: null, charges: null, validUntil: null, maxUses: null, active: true },
    ]);
  });
});

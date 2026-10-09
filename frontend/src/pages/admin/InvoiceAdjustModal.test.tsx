// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { Invoice, Subscription, VehicleView } from '@/types';

const calls: unknown[] = [];
vi.mock('@/api/resources', () => ({
  catalogApi: { get: async () => ({ equipmentMaxInstallments: 10 }) },
  customersApi: {
    financeInvoice: async (id: string, input: unknown) => {
      calls.push(['parcelar', id, input]);
      return {};
    },
    changeInvoiceDueDate: async (id: string, due: string) => {
      calls.push(['vencimento', id, due]);
      return {};
    },
  },
}));

import { InvoiceAdjustModal, freightOf } from './InvoiceAdjustModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const field = (label: string) => {
  const l = Array.from(document.querySelectorAll('label')).find((x) => x.textContent?.startsWith(label));
  return document.getElementById(l?.htmlFor ?? '') as HTMLInputElement | HTMLSelectElement;
};
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;
function setValue(el: HTMLInputElement | HTMLSelectElement, value: string) {
  const proto = el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, 'value')?.set?.call(el, value);
  el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
}

const invoice = (over: Partial<Invoice>): Invoice =>
  ({
    id: 'i1', customerId: 'c1', subscriptionId: null, description: 'Rastreador J16 GT06', amountCents: 15000, dueDate: '2026-10-10',
    status: 'OPEN', overdue: false, daysOverdue: 0, paidAt: null, paidVia: '', paidChargeId: null, paymentUrl: '', pixCode: '',
    createdAt: '', updatedAt: '', ...over,
  }) as Invoice;
const sub = { id: 's1', status: 'ACTIVE', installments: 0, vehicleId: 'v1', planName: 'Plano Mensal' } as Subscription;
const vehicles = [{ id: 'v1', name: 'Moto da Bia', plate: 'ACE1A11' }] as VehicleView[];
let done = '';

async function open(inv: Invoice, subs: Subscription[] = [sub]) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <InvoiceAdjustModal invoice={inv} subscriptions={subs} vehicles={vehicles} onClose={() => undefined} onDone={(m) => (done = m)} />
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
  done = '';
});

describe('alterar a fatura em aberto', () => {
  it('parcela o rastreador do pedido feito à vista', async () => {
    await open(invoice({}));
    const check = document.querySelector('input[type=checkbox]') as HTMLInputElement;
    await act(async () => check.click());
    expect(field('Veículo').value).toBe('s1');
    expect(text()).toContain('Moto da Bia · ACE1A11');
    expect(text()).toContain('Esta fatura é cancelada; o rastreador vem nas mensalidades: + R$ 15,00 em cada uma das 10 próximas mensalidades.');
    // Cancelada, o vencimento não importa.
    await act(async () => setValue(field('Vencimento') as HTMLInputElement, '2026-10-20'));
    await act(async () => button('Salvar').click());
    await flush();
    expect(calls).toEqual([['parcelar', 'i1', { subscriptionId: 's1', installments: 10, equipmentCents: 15000 }]]);
    expect(done).toBe('Fatura alterada: rastreador em 10x.');
  });

  it('com frete: o valor do rastreador já vem sem ele, e a fatura fica só com o frete; e o vencimento junto', async () => {
    await open(invoice({ amountCents: 17240, description: 'Rastreador J16 GT06 + frete Correios PAC (R$ 22,40)' }));
    await act(async () => (document.querySelector('input[type=checkbox]') as HTMLInputElement).click());
    expect((field('Valor do rastreador') as HTMLInputElement).value).toBe('150,00');
    await act(async () => setValue(field('Parcelas'), '7'));
    expect(text()).toContain(
      'Esta fatura fica com R$ 22,40 (o frete); o rastreador vem nas mensalidades: + R$ 21,48 na próxima mensalidade e R$ 21,42 em cada uma das 6 seguintes.',
    );
    await act(async () => setValue(field('Parcelas'), '5'));
    await act(async () => setValue(field('Vencimento') as HTMLInputElement, '2026-10-20'));
    await act(async () => button('Salvar').click());
    await flush();
    expect(calls).toEqual([
      ['parcelar', 'i1', { subscriptionId: 's1', installments: 5, equipmentCents: 15000 }],
      ['vencimento', 'i1', '2026-10-20'],
    ]);
  });

  it('mensalidade: só o vencimento (não tem parcelamento)', async () => {
    await open(invoice({ subscriptionId: 's1', description: 'Plano Mensal — outubro/2026', amountCents: 6990 }));
    expect(document.querySelector('input[type=checkbox]')).toBeNull();
    await act(async () => setValue(field('Vencimento') as HTMLInputElement, '2026-10-25'));
    await act(async () => button('Salvar').click());
    await flush();
    expect(calls).toEqual([['vencimento', 'i1', '2026-10-25']]);
  });

  it('veículo já parcelado, fatura só do frete ou já parcelada: sem parcelamento', async () => {
    await open(invoice({}), [{ ...sub, installments: 10 }]);
    expect(document.querySelector('input[type=checkbox]')).toBeNull();
    document.body.innerHTML = '';
    await open(invoice({ amountCents: 2240, description: 'Frete do rastreador: Correios PAC' }));
    expect(document.querySelector('input[type=checkbox]')).toBeNull();
    document.body.innerHTML = '';
    await open(invoice({ amountCents: 2240, description: 'Rastreador + frete PAC (R$ 22,40) · rastreador de R$ 150,00 em 10x sem juros, nas mensalidades' }));
    expect(document.querySelector('input[type=checkbox]')).toBeNull();
  });

  it('lê o frete da descrição', () => {
    expect(freightOf('Rastreador J16 GT06 + frete Correios SEDEX (R$ 39,90)')).toBe(3990);
    expect(freightOf('Rastreador + frete Transportadora (R$ 1.020,00)')).toBe(102000);
    expect(freightOf('Rastreador J16 GT06')).toBe(0);
  });
});

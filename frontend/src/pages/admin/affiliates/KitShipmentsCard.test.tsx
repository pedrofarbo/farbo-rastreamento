// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { Affiliate, KitShipment } from '@/types';

const calls = vi.hoisted(() => [] as unknown[][]);
const state = vi.hoisted(() => ({ list: [] as KitShipment[], devices: null as unknown[] | null }));
vi.mock('@/api/resources', () => ({
  kitShipmentsApi: {
    list: async () => state.list,
    availableDevices: async () =>
      state.devices ?? [
        { id: 'd1', imei: '869247061900101', iccid: '89550001', installed: false },
        { id: 'd2', imei: '869247061900102', iccid: '89550002', installed: false },
      ],
    availableChips: async () => [
      { id: 'c1', iccid: '89551012345678900001', carrier: 'Vivo', used: false },
      { id: 'c2', iccid: '89551012345678900002', carrier: 'Claro', used: false },
    ],
    create: async (input: unknown) => {
      calls.push(['create', input]);
      return { ...state.list[0], affiliateName: 'Rui', devices: [{}] };
    },
    prepare: async (id: string, input: unknown) => {
      calls.push(['prepare', id, input]);
      return { ...state.list[0], devices: [{}] };
    },
    ship: async (id: string, input: unknown) => {
      calls.push(['ship', id, input]);
      return state.list[0];
    },
    deliver: async (id: string) => {
      calls.push(['deliver', id]);
      return state.list[0];
    },
    cancel: async (id: string) => {
      calls.push(['cancel', id]);
      return state.list[0];
    },
  },
}));

import { KitShipmentsCard, kitTotals, sortShipments } from './KitShipmentsCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === label);
const row = (name: string) => Array.from(document.querySelectorAll('tbody tr')).find((r) => r.textContent?.includes(name))!;
const inRow = (r: Element, label: string) => Array.from(r.querySelectorAll('button')).find((b) => b.textContent?.trim() === label)!;

const shipment = (over: Partial<KitShipment>): KitShipment => ({
  id: 'k1', affiliateId: 'a1', affiliateName: 'Rui', status: 'preparing', requestedQuantity: null, requestNote: '', carrier: '',
  trackingCode: '', notes: '', shippedAt: null, deliveredAt: null, canceledAt: null, createdAt: '2026-11-03T12:00:00Z',
  devices: [], ...over,
});
const device = (id: string, installed = false) => ({ id, imei: `86924706190${id}`, iccid: '', installed });

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const affiliates = [
    { id: 'a2', name: 'Ana', active: true, handDelivery: false },
    { id: 'a1', name: 'Rui', active: true, handDelivery: true },
    { id: 'a3', name: 'Zé Inativo', active: false, handDelivery: true },
  ] as Affiliate[];
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ToastProvider>
          <KitShipmentsCard affiliates={affiliates} />
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
  state.devices = null;
});
const boxesIn = (label: string) =>
  Array.from(document.querySelectorAll(`[aria-label="${label}"] input[type="checkbox"]`)) as HTMLInputElement[];

describe('os kits dos representantes na central', () => {
  it('os pedidos primeiro e os números do cartão', () => {
    const list = [
      shipment({ id: 'old', status: 'delivered', createdAt: '2026-10-01T12:00:00Z', devices: [device('1'), device('2', true)] }),
      shipment({ id: 'ship', status: 'shipped', createdAt: '2026-11-02T12:00:00Z', devices: [device('3')] }),
      shipment({ id: 'req', status: 'requested', createdAt: '2026-11-01T12:00:00Z', requestedQuantity: 4 }),
    ];
    expect(sortShipments(list).map((k) => k.id)).toEqual(['req', 'ship', 'old']);
    expect(kitTotals(list)).toEqual({ requested: 1, inTransit: 1, stock: 1, installed: 1 });
  });

  it('atende o pedido com os rastreadores livres, envia com o rastreio e cancela com a confirmação', async () => {
    state.list = [
      shipment({ id: 'req', status: 'requested', requestedQuantity: 2, requestNote: 'até sexta', affiliateName: 'Rui' }),
      shipment({ id: 'prep', status: 'preparing', affiliateName: 'Bia', devices: [device('5')] }),
    ];
    await render();
    expect(text()).toContain('pediu 2 kits: “até sexta”');

    await act(async () => inRow(row('até sexta'), 'Atender').click());
    await flush();
    expect(text()).toContain('Atender o pedido de Rui');
    const boxes = boxesIn('Rastreadores livres');
    expect(boxes).toHaveLength(2);
    expect(boxesIn('Chips livres')).toHaveLength(2);
    await act(async () => boxes[0].click());
    expect(text()).toContain('1 de 2 livres selecionados · o pedido é de 2');
    expect(button('Marcar 2 da lista')).toBeDefined();
    await act(async () => button('Separar 1 rastreador')!.click());
    await flush();
    expect(calls).toEqual([['prepare', 'req', { deviceIds: ['d1'], chipIds: [], notes: '' }]]);

    calls.length = 0;
    await act(async () => inRow(row('Bia'), 'Marcar como enviada').click());
    await flush();
    const code = document.querySelector('input[placeholder="Ex.: QB123456789BR"]') as HTMLInputElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(code, 'qb1br');
      code.dispatchEvent(new Event('input', { bubbles: true }));
    });
    const dialog = document.querySelector('[role="dialog"]')!;
    await act(async () => inRow(dialog, 'Marcar como enviada').click());
    await flush();
    expect(calls).toEqual([['ship', 'prep', { carrier: 'Correios SEDEX', trackingCode: 'QB1BR' }]]);

    calls.length = 0;
    await act(async () => inRow(row('Bia'), 'Cancelar').click());
    expect(text()).toContain('Cancelar a remessa?');
    await act(async () => button('Sim, cancelar')!.click());
    await flush();
    expect(calls).toEqual([['cancel', 'prep']]);
  });

  it('o rastreador sem chip pede o chip avulso; a remessa leva os dois e a tabela mostra os chips', async () => {
    state.devices = [
      { id: 'd1', imei: '869247061900101', iccid: '89550001', installed: false },
      { id: 'd3', imei: '869247061900103', iccid: '', installed: false },
    ];
    state.list = [
      shipment({ id: 'req', status: 'requested', requestedQuantity: 1, affiliateName: 'Rui' }),
      shipment({
        id: 'old', status: 'delivered', affiliateName: 'Bia', devices: [device('5')],
        chips: [
          { id: 'x1', iccid: '89551012345678909991', carrier: 'Vivo', used: true, deviceImei: '869247061905' },
          { id: 'x2', iccid: '89551012345678909992', carrier: 'Vivo', used: false },
        ],
      }),
    ];
    await render();
    expect(text()).toContain('2 chips avulsos · 1 usado');
    expect(text()).toContain('no rastreador 869247061905');

    await act(async () => inRow(row('pediu 1 kit'), 'Atender').click());
    await flush();
    expect(text()).toContain('sem chip');
    await act(async () => boxesIn('Rastreadores livres')[1].click());
    expect(text()).toContain('1 rastreador marcado vai sem chip: marque mais 1 chip.');
    await act(async () => button('Marcar 1 da lista')!.click());
    expect(text()).toContain('1 rastreador marcado vai sem chip: os chips marcados vão junto.');
    expect(boxesIn('Chips livres')[0].checked).toBe(true);
    await act(async () => button('Separar 1 rastreador e 1 chip')!.click());
    await flush();
    expect(calls).toEqual([['prepare', 'req', { deviceIds: ['d3'], chipIds: ['c1'], notes: '' }]]);
  });

  it('só chips (o representante pediu chips a mais): também separa', async () => {
    state.list = [shipment({ id: 'req', status: 'requested', requestedQuantity: 2, affiliateName: 'Rui' })];
    await render();
    await act(async () => inRow(row('pediu 2 kits'), 'Atender').click());
    await flush();
    expect(button('Separar o kit')!.disabled).toBe(true);
    await act(async () => boxesIn('Chips livres')[1].click());
    await act(async () => button('Separar 1 chip')!.click());
    await flush();
    expect(calls).toEqual([['prepare', 'req', { deviceIds: [], chipIds: ['c2'], notes: '' }]]);
  });

  it('a remessa nova: só os ativos, os que entregam em mãos primeiro', async () => {
    state.list = [];
    await render();
    expect(text()).toContain('Nenhuma remessa ainda');
    await act(async () => button('Enviar kits')!.click());
    await flush();
    const options = Array.from(document.querySelectorAll('select option')).map((o) => o.textContent);
    expect(options).toEqual(['Escolha…', 'Rui · entrega em mãos', 'Ana']);
  });
});

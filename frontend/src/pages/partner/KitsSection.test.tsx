// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { KitShipment, PartnerKits } from '@/types';

const calls = vi.hoisted(() => [] as unknown[][]);
const state = vi.hoisted(() => ({ kits: null as PartnerKits | null }));
vi.mock('@/api/resources', () => ({
  publicApi: {
    partnerKits: async () => state.kits,
    partnerKitReceived: async (token: string, id: string) => {
      calls.push(['received', token, id]);
      return {};
    },
    partnerKitCancel: async (token: string, id: string) => {
      calls.push(['cancel', token, id]);
      return {};
    },
    partnerKitRequest: async (token: string, input: unknown) => {
      calls.push(['request', token, input]);
      return {};
    },
  },
}));

import { KitsSection } from './KitsSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => document.body.textContent ?? '';
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === label);

const shipment = (over: Partial<KitShipment>): KitShipment => ({
  id: 'k1', affiliateId: 'a1', affiliateName: 'Rui', status: 'shipped', requestedQuantity: null, requestNote: '',
  carrier: 'Correios SEDEX', trackingCode: 'QB123456789BR', notes: '', shippedAt: '2026-11-03T15:00:00Z', deliveredAt: null,
  canceledAt: null, createdAt: '2026-11-03T12:00:00Z', devices: [], ...over,
});
const free = (imei: string) => ({ id: imei, imei, iccid: `8955${imei}`, installed: false });

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () => createRoot(host).render(<KitsSection token="tok" />));
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('os kits na página do representante', () => {
  it('os chips avulsos: no estoque (os que ainda não foram para um rastreador) e na remessa', async () => {
    state.kits = {
      stock: [free('869247061900103')],
      chips: [{ id: 'c2', iccid: '89551012345678900002', carrier: 'Claro', used: false }],
      installed: 0,
      shipments: [
        shipment({
          status: 'delivered', deliveredAt: '2026-11-04T10:00:00Z', devices: [free('869247061900103')],
          chips: [
            { id: 'c1', iccid: '89551012345678900001', carrier: 'Vivo', used: true, deviceImei: '869247061900103' },
            { id: 'c2', iccid: '89551012345678900002', carrier: 'Claro', used: false },
          ],
        }),
      ],
    };
    await render();
    expect(text()).toContain('Chips avulsos no estoque (1)');
    expect(text()).toContain('89551012345678900002');
    expect(text()).toContain('1 rastreador e 2 chips avulsos · recebida em');
    expect(text()).toContain('no rastreador 869247061900103');
  });

  it('o estoque, a remessa a caminho com o rastreio e o recebi', async () => {
    state.kits = {
      stock: [free('869247061900103')],
      installed: 1,
      shipments: [
        shipment({ devices: [free('869247061900101'), free('869247061900102')] }),
        shipment({
          id: 'k0', status: 'delivered', deliveredAt: '2026-10-20T15:00:00Z',
          devices: [
            free('869247061900103'),
            { id: 'd4', imei: '869247061900104', iccid: '', installed: true, vehicleName: 'Uno', plate: 'ANA1A11', customerName: 'Ana' },
          ],
        }),
      ],
    };
    await render();
    const stats = Array.from(document.querySelectorAll('strong')).map((s) => s.textContent);
    expect(stats).toEqual(expect.arrayContaining(['1', '2']));
    expect(text()).toContain('Rastreadores no estoque (1)');
    expect(text()).toContain('A caminho');
    expect(text()).toContain('Correios SEDEX · QB123456789BR');
    expect(document.querySelector('a[href*="QB123456789BR"]')).not.toBeNull();
    expect(text()).toContain('instalado no Uno (ANA1A11) · Ana');
    expect(text()).toContain('1 instalado de 2');

    await act(async () => button('Recebi os kits')!.click());
    await flush();
    expect(calls).toEqual([['received', 'tok', 'k1']]);
  });

  it('pede kits e, com o pedido esperando, cancela', async () => {
    state.kits = { stock: [], installed: 0, shipments: [] };
    await render();
    const quantity = document.querySelector('input[type="number"]') as HTMLInputElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(quantity, '3');
      quantity.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await act(async () => button('Pedir à Farbo')!.click());
    await flush();
    expect(calls).toEqual([['request', 'tok', { quantity: 3, note: '' }]]);

    document.body.innerHTML = '';
    calls.length = 0;
    state.kits = {
      stock: [], installed: 0,
      shipments: [shipment({ id: 'k2', status: 'requested', requestedQuantity: 3, requestNote: 'até sexta', carrier: '', trackingCode: '' })],
    };
    await render();
    expect(text()).toContain('Esperando a Farbo');
    expect(text()).toContain('Você pediu 3 kits');
    expect(text()).toContain('Obs.: até sexta');
    expect(button('Pedir à Farbo')).toBeUndefined();
    await act(async () => button('Cancelar o pedido')!.click());
    await flush();
    expect(calls).toEqual([['cancel', 'tok', 'k2']]);
  });
});

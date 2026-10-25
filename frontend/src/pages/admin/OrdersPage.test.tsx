// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { Fulfillment } from '@/types';

const canceled: { id: string; reason: string }[] = [];
let admin = true;
const order = (over: Partial<Fulfillment> = {}): Fulfillment =>
  ({
    id: 'f1', customerId: 'c1', customerName: 'Lia Souza', vehicleId: 'v1', vehicleName: 'Honda CG 160', vehiclePlate: '',
    subscriptionId: 's1', chipStatus: 'SEPARATED', trackerStatus: 'CONFIGURING', shippingOrderId: null, shippingProtocol: '',
    shippingService: '', shippingPriceCents: null, shippingStatus: '', trackingCode: '', labelUrl: '', quotedServiceId: null,
    quotedService: '', quotedPriceCents: null, quotedDays: null, deliveryArranged: false, events: [],
    createdAt: '2026-10-25T12:00:00Z', updatedAt: '2026-10-25T12:00:00Z', ...over,
  }) as Fulfillment;
let orders: Fulfillment[] = [];

vi.mock('@/api/resources', async () => ({
  ...(await vi.importActual<typeof import('@/api/resources')>('@/api/resources')),
  fulfillmentsApi: {
    list: async () => orders,
    get: async () => orders[0],
    cancel: async (id: string, reason: string) => {
      canceled.push({ id, reason });
      return { customerId: 'c1', subscriptionId: 's1', vehicleName: 'Honda CG 160', canceledInvoices: 1, paidCents: 27000, paidInvoices: ['i1'] };
    },
  },
  shippingIntegrationApi: {
    get: async () => ({ configured: true, canConnect: true, connected: false, accountError: '', missingOrigin: [], redirectUrl: '' }),
    balance: async () => ({ balanceCents: 0 }),
  },
}));
vi.mock('@/stores/AuthContext', () => ({ useAuth: () => ({ canManage: admin, user: { role: admin ? 'admin' : 'operator' } }) }));

import { OrdersPage } from './OrdersPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === label) as HTMLButtonElement | undefined;

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter>
          <ToastProvider>
            <OrdersPage />
          </ToastProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  canceled.length = 0;
  admin = true;
});

describe('cancelar o pedido', () => {
  it('o administrador cancela com o motivo e vê quanto devolver', async () => {
    orders = [order(), order({ id: 'f2', vehicleName: 'Fiat Strada', trackerStatus: 'SHIPPED', trackingCode: 'BR123' })];
    await render();
    // Só o pedido que ainda não saiu tem o Cancelar.
    expect(Array.from(document.querySelectorAll('button')).filter((b) => b.textContent?.trim() === 'Cancelar')).toHaveLength(1);
    await act(async () => button('Cancelar')!.click());
    expect(text()).toContain('Cancelar o pedido?');
    expect(button('Cancelar o pedido')!.disabled).toBe(true); // falta o motivo

    const input = document.querySelector('input[maxlength="500"]') as HTMLInputElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, 'o cliente desistiu');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await act(async () => button('Cancelar o pedido')!.click());
    await flush();
    expect(canceled).toEqual([{ id: 'f1', reason: 'o cliente desistiu' }]);
    expect(text()).toContain('Pedido de Honda CG 160 cancelado');
    expect(text()).toContain('Já tinha sido pago R$ 270,00: devolva ao cliente');
  });

  it('o operador não vê o Cancelar', async () => {
    admin = false;
    orders = [order()];
    await render();
    expect(button('Cancelar')).toBeUndefined();
  });
});

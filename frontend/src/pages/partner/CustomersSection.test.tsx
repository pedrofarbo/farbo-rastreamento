// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { PartnerCustomer } from '@/types';

const state = vi.hoisted(() => ({ list: [] as PartnerCustomer[] }));
vi.mock('@/api/resources', () => ({ publicApi: { partnerCustomers: async () => state.list } }));

import { CustomersSection } from './CustomersSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => document.body.textContent ?? '';

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () => createRoot(host).render(<CustomersSection token="tok" />));
  await act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('os clientes na página do afiliado', () => {
  it('o nome, os veículos com a placa e o rastreador, e o pagamento mês a mês', async () => {
    state.list = [
      {
        name: 'Ana Souza', since: '2026-10-03T12:00:00Z',
        vehicles: [
          { name: 'Uno', plate: 'ANA1A11', imei: '869247061905555' },
          { name: 'Moto', plate: '', imei: '' },
        ],
        months: [
          { month: '2026-11', status: 'paid' },
          { month: '2026-10', status: 'overdue' },
          { month: '2026-09', status: 'none' },
        ],
      },
    ];
    await render();
    expect(text()).toContain('Ana Souza');
    expect(text()).toContain('cliente desde out/2026');
    expect(text()).toContain('Uno · ANA1A11 · rastreador 869247061905555');
    expect(text()).toContain('Moto · aguardando o rastreador');
    expect(text()).toContain('nov/26 · pago');
    expect(text()).toContain('out/26 · vencido');
    expect(text()).toContain('set/26 · sem fatura');
  });

  it('sem clientes ainda', async () => {
    state.list = [];
    await render();
    expect(text()).toContain('Nenhum cliente ainda');
  });
});

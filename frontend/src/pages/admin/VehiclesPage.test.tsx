// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { VehicleDirectoryEntry, VehicleDirectoryFilter } from '@/types';

const calls: { filter: VehicleDirectoryFilter; page: number }[] = [];
const vehicle = (over: Partial<VehicleDirectoryEntry> = {}): VehicleDirectoryEntry => ({
  id: 'v1', name: 'Honda CG 160', plate: 'ABC1D23', kind: 'MOTORCYCLE', brand: 'Honda', model: 'CG 160', year: 2022,
  customerId: 'c1', customerName: 'Lia Souza', customerEmail: 'lia@exemplo.com', customerDocument: '52998224725',
  organizationId: 'o1', organizationName: 'Associação Protege', deviceId: 'd1', deviceImei: '869247061900077',
  deviceStatus: 'ONLINE', lastSeenAt: new Date().toISOString(), createdAt: '2026-10-20T12:00:00Z', ...over,
});
let total = 60;

vi.mock('@/api/resources', () => ({
  vehiclesApi: {
    directory: async (filter: VehicleDirectoryFilter, page: number) => {
      calls.push({ filter, page });
      return { items: total ? [vehicle(), vehicle({ id: 'v2', name: 'Carro da central', plate: '', customerId: null, organizationId: null, deviceId: null })] : [], total, page, pageSize: 25 };
    },
  },
  organizationsApi: { list: async () => [{ id: 'o1', name: 'Associação Protege' }] },
}));

import { VehiclesPage } from './VehiclesPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === label) as HTMLButtonElement | undefined;
let location = '';
function Where() {
  const l = useLocation();
  location = l.pathname + l.search;
  return null;
}
const type = async (label: string, value: string) => {
  const field = Array.from(document.querySelectorAll('label')).find((l) => l.textContent === label)!;
  const input = document.getElementById(field.htmlFor) as HTMLInputElement | HTMLSelectElement;
  const proto = input instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
  await act(async () => {
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(input, value);
    input.dispatchEvent(new Event(input instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  });
};

async function render(url = '/veiculos') {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter initialEntries={[url]}>
          <VehiclesPage />
          <Where />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  );
  await vi.waitFor(() => expect(text()).toContain('Honda CG 160'));
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
  total = 60;
});

describe('a listagem de veículos da central', () => {
  it('mostra o dono, a associação e o rastreador, e pagina', async () => {
    await render();
    expect(calls[0]).toEqual({ filter: { plate: '', organization: '', customer: '', document: '', email: '' }, page: 1 });
    expect(text()).toContain('60 veículos');
    expect(text()).toContain('ABC1D23');
    expect(text()).toContain('529.982.247-25');
    expect(text()).toContain('lia@exemplo.com');
    expect(text()).toContain('Associação Protege');
    expect(text()).toContain('869247061900077');
    expect(text()).toContain('Online');
    // O da central: sem dono, sem placa, sem rastreador.
    expect(text()).toContain('Da central');
    expect(text()).toContain('sem placa');
    expect(text()).toContain('Sem rastreador');
    expect(document.querySelector('a[href="/veiculos/v1"]')).toBeTruthy();
    expect(document.querySelector('a[href="/clientes/c1"]')).toBeTruthy();
    expect(document.querySelector('a[href="/associacoes/o1"]')).toBeTruthy();

    expect(text()).toContain('Página 1 de 3');
    expect(button('Anterior')!.disabled).toBe(true);
    await act(async () => button('Próxima')!.click());
    await vi.waitFor(() => expect(calls.at(-1)?.page).toBe(2));
    expect(location).toBe('/veiculos?pagina=2');
  });

  it('os filtros vão para a busca e para a URL, voltando à primeira página', async () => {
    await render('/veiculos?pagina=2');
    expect(calls[0].page).toBe(2);
    await type('Placa', 'abc-1d');
    await type('CPF ou CNPJ', '529.982');
    await type('Associação', 'o1');
    await vi.waitFor(() =>
      expect(calls.at(-1)).toEqual({ filter: { plate: 'abc-1d', organization: 'o1', customer: '', document: '529.982', email: '' }, page: 1 }),
    );
    expect(location).toBe('/veiculos?placa=abc-1d&associacao=o1&documento=529.982');

    await act(async () => button('Limpar filtros')!.click());
    await vi.waitFor(() => expect(location).toBe('/veiculos'));
    expect(button('Limpar filtros')).toBeUndefined();
  });

  it('abre com os filtros da URL; sem resultado, diz que não achou', async () => {
    total = 0;
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <MemoryRouter initialEntries={['/veiculos?email=ninguem%40x.com&associacao=none']}>
            <VehiclesPage />
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    );
    await vi.waitFor(() => expect(text()).toContain('Nenhum veículo com esses filtros'));
    expect(calls[0].filter).toEqual({ plate: '', organization: 'none', customer: '', document: '', email: 'ninguem@x.com' });
    expect((document.querySelector('input[type="search"]') as HTMLInputElement).value).toBe('ninguem@x.com');
  });
});

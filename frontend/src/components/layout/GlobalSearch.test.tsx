// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { SearchResults } from '@/types';

const RESULTS: SearchResults = {
  customers: [
    { id: 'c1', name: 'Lia Souza', email: 'lia@x.test', phone: '(34) 98888-1234', document: '52998224725', active: true },
    { id: 'c2', name: 'Ana Lia', email: 'ana@x.test', phone: '', document: '', active: false },
  ],
  vehicles: [{ id: 'v1', name: 'Honda CG 160', plate: 'ABC1D23', customerId: 'c1', customerName: 'Lia Souza', deviceImei: '869247061900077' }],
  devices: [
    { id: 'd1', imei: '869247061900077', model: 'J16', status: 'ONLINE', vehicleId: 'v1', vehicleName: 'Honda CG 160', customerId: 'c1', customerName: 'Lia Souza' },
    { id: 'd2', imei: '869247061955555', model: '', status: 'OFFLINE', vehicleId: null, vehicleName: '', customerId: null, customerName: '' },
  ],
  chips: [{ id: 's1', iccid: '89553202100011112222', phoneNumber: '(34) 97777-0002', carrier: 'Algar' }],
};
const search = vi.hoisted(() => vi.fn());
vi.mock('@/api/resources', () => ({ searchApi: { search } }));

import { GlobalSearch, searchHits, searchReady } from './GlobalSearch';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');

let location = '';
function Where() {
  const l = useLocation();
  location = l.pathname + l.search;
  return null;
}

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter initialEntries={['/dashboard']}>
          <GlobalSearch />
          <Routes>
            <Route path="*" element={<Where />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  );
}

const input = () => document.querySelector<HTMLInputElement>('[role="combobox"]');
const type = (value: string) =>
  act(async () => {
    const el = input()!;
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
const key = (target: EventTarget, k: string, extra: KeyboardEventInit = {}) =>
  act(async () => {
    target.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...extra }));
  });
const options = () => Array.from(document.querySelectorAll('[role="option"]'));

afterEach(() => {
  document.body.innerHTML = '';
  search.mockReset();
});

describe('searchReady', () => {
  it('como no servidor: 2 letras ou 4 dígitos', () => {
    expect(searchReady('')).toBe(false);
    expect(searchReady('l')).toBe(false);
    expect(searchReady('12')).toBe(false);
    expect(searchReady('a1')).toBe(false);
    expect(searchReady('li')).toBe(true);
    expect(searchReady('João')).toBe(true);
    expect(searchReady('8692')).toBe(true);
    expect(searchReady('(34) 9')).toBe(false);
  });
});

describe('searchHits', () => {
  it('cada resultado leva à ficha, ao veículo ou à lista já filtrada', () => {
    const hits = searchHits(RESULTS);
    expect(hits.map((h) => [h.group, h.title, h.to])).toEqual([
      ['Clientes', 'Lia Souza', '/clientes/c1'],
      ['Clientes', 'Ana Lia (desativado)', '/clientes/c2'],
      ['Veículos', 'Honda CG 160 · ABC1D23', '/veiculos/v1'],
      ['Rastreadores', 'IMEI 869247061900077', '/veiculos/v1'],
      ['Rastreadores', 'IMEI 869247061955555', '/dispositivos?busca=869247061955555'],
      ['Chips livres', 'ICCID 8955 3202 1000 1111 2222', '/chips?busca=89553202100011112222'],
    ]);
    expect(hits[0].detail).toBe('lia@x.test · (34) 98888-1234 · 529.982.247-25');
    expect(hits[2].detail).toBe('Lia Souza · IMEI 869247061900077');
    expect(hits[4].detail).toContain('sem veículo');
  });
});

describe('GlobalSearch', () => {
  it('Ctrl+K abre; digita, escolhe com as setas e o Enter abre o resultado e fecha a busca', async () => {
    search.mockResolvedValue(RESULTS);
    await render();
    expect(input()).toBeNull();
    await key(document, 'k', { ctrlKey: true });
    expect(input()).not.toBeNull();
    expect(text()).toContain('Digite 2 letras');

    await type('lia');
    await vi.waitFor(() => expect(options()).toHaveLength(6));
    expect(search).toHaveBeenLastCalledWith('lia');
    expect(text()).toContain('Clientes');
    expect(text()).toContain('Chips livres');
    expect(options()[0].getAttribute('aria-selected')).toBe('true');

    await key(input()!, 'ArrowDown');
    await key(input()!, 'ArrowDown');
    expect(options()[2].getAttribute('aria-selected')).toBe('true');
    expect(input()!.getAttribute('aria-activedescendant')).toBe(options()[2].id);
    // Para cima do primeiro, volta ao último.
    await key(input()!, 'ArrowUp');
    await key(input()!, 'ArrowUp');
    await key(input()!, 'ArrowUp');
    expect(options()[5].getAttribute('aria-selected')).toBe('true');
    await key(input()!, 'ArrowUp');
    await key(input()!, 'Enter');
    expect(location).toBe('/dispositivos?busca=869247061955555');
    expect(input()).toBeNull();
  });

  it('o botão abre; nada encontrado diz isso; o clique no resultado abre', async () => {
    search.mockResolvedValue({ customers: [], vehicles: [], devices: [], chips: [] });
    await render();
    await act(async () => document.querySelector<HTMLButtonElement>('button[aria-label="Buscar no painel (Ctrl+K)"]')!.click());
    await type('zzz');
    await vi.waitFor(() => expect(text()).toContain('Nada encontrado.'));

    search.mockResolvedValue(RESULTS);
    await type('honda');
    await vi.waitFor(() => expect(options()).toHaveLength(6));
    await act(async () => (options()[2] as HTMLElement).click());
    expect(location).toBe('/veiculos/v1');
  });
});

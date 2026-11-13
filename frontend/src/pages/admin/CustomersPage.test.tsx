// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { CustomerSummary } from '@/types';

const state = vi.hoisted(() => ({ customers: [] as unknown[] }));
vi.mock('@/api/resources', () => ({
  customersApi: { list: async () => state.customers, mrr: async () => null, create: async () => ({}) },
  leadsApi: { stats: async () => ({ new: 0 }) },
}));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => {} }) }));
// As outras partes da tela têm os testes delas.
vi.mock('./MrrCard', () => ({ MrrCard: () => null, ForecastTile: () => null, mrrParts: () => '', signedMoney: () => '' }));
vi.mock('@/components/billing/OrderOptionsCard', () => ({ OrderOptionsCard: () => null }));
vi.mock('@/components/billing/CouponsCard', () => ({ CouponsCard: () => null }));
vi.mock('@/components/billing/CheckoutLinksCard', () => ({ CheckoutLinksCard: () => null }));
vi.mock('@/components/billing/PriceAdjustmentCard', () => ({ PriceAdjustmentCard: () => null }));
vi.mock('./affiliates/AffiliatesTab', () => ({ AffiliatesTab: () => null }));
vi.mock('./LeadsTab', () => ({ LeadsTab: () => null }));
vi.mock('./SiteAnalyticsTab', () => ({ SiteAnalyticsTab: () => null }));

import { CustomersPage } from './CustomersPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => document.body.textContent ?? '';

const customer = (id: string, name: string, over: Partial<CustomerSummary> = {}): CustomerSummary => ({
  id, name, email: `${id}@x.test`, phone: '', document: '', active: true, createdAt: '2026-10-01T12:00:00Z', activeSubscriptions: 1,
  freeSubscriptions: 0, vehicleCount: 1, sharedVehicles: 0, openInvoices: 0, overdueInvoices: 0, openAmountCents: 0, suspended: false,
  organizationId: null, organizationName: '', ...over,
});
const CUSTOMERS = [
  customer('c1', 'Ana Em Dia'),
  customer('c2', 'Bia Um Atraso', { openInvoices: 1, overdueInvoices: 1, openAmountCents: 6990 }),
  customer('c3', 'Caio Suspenso', { openInvoices: 3, overdueInvoices: 3, openAmountCents: 20970, suspended: true }),
  customer('c4', 'Duda Em Aberto', { openInvoices: 1, openAmountCents: 6990 }),
];

let location = '';
function Where() {
  const l = useLocation();
  location = l.pathname + l.search;
  return null;
}

async function render(path = '/clientes') {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route
              path="/clientes"
              element={
                <>
                  <CustomersPage />
                  <Where />
                </>
              }
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

const rows = () => Array.from(document.querySelectorAll('tbody tr strong')).map((s) => s.textContent);
const tile = () => Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.startsWith('Em atraso2'))!;
const chip = (label: string) =>
  Array.from(document.querySelectorAll<HTMLButtonElement>('[aria-label="Situação dos clientes"] button')).find((b) =>
    b.textContent?.startsWith(label),
  )!;

afterEach(() => {
  document.body.innerHTML = '';
});

describe('o filtro dos clientes em atraso', () => {
  it('o cartão "Em atraso" filtra a lista (os com mais faturas vencidas primeiro) e tira o filtro no segundo toque', async () => {
    state.customers = CUSTOMERS;
    await render();
    expect(rows()).toEqual(['Ana Em Dia', 'Bia Um Atraso', 'Caio Suspenso', 'Duda Em Aberto']);
    expect(tile().getAttribute('aria-pressed')).toBe('false');
    expect(tile().textContent).toContain('1 com acesso suspenso · ver a lista');

    await act(async () => tile().click());
    expect(rows()).toEqual(['Caio Suspenso', 'Bia Um Atraso']);
    expect(location).toBe('/clientes?situacao=atraso');
    expect(tile().getAttribute('aria-pressed')).toBe('true');
    expect(chip('Em atraso').getAttribute('aria-pressed')).toBe('true');

    await act(async () => tile().click());
    expect(rows()).toHaveLength(4);
    expect(location).toBe('/clientes');
  });

  it('o link já filtrado, os filtros com a contagem, a busca dentro do filtro e o vazio', async () => {
    state.customers = CUSTOMERS;
    await render('/clientes?situacao=atraso');
    expect(rows()).toEqual(['Caio Suspenso', 'Bia Um Atraso']);
    expect(chip('Todos').textContent).toBe('Todos (4)');
    expect(chip('Em atraso').textContent).toBe('Em atraso (2)');
    expect(chip('Acesso suspenso').textContent).toBe('Acesso suspenso (1)');

    const search = document.querySelector('input[placeholder^="Buscar"]') as HTMLInputElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(search, 'bia');
      search.dispatchEvent(new Event('input', { bubbles: true }));
    });
    expect(rows()).toEqual(['Bia Um Atraso']);
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(search, '');
      search.dispatchEvent(new Event('input', { bubbles: true }));
    });

    await act(async () => chip('Acesso suspenso').click());
    expect(rows()).toEqual(['Caio Suspenso']);
    expect(location).toBe('/clientes?situacao=suspensos');

    // Ninguém em atraso: o aviso de que está tudo em dia.
    document.body.innerHTML = '';
    state.customers = [CUSTOMERS[0], CUSTOMERS[3]];
    await render('/clientes?situacao=atraso');
    expect(text()).toContain('Nenhum cliente em atraso');
  });
});

// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { CustomerDetail, Invoice, Subscription } from '@/types';

const state = vi.hoisted(() => ({ detail: null as unknown }));
vi.mock('@/api/resources', () => ({
  customersApi: { get: async () => state.detail },
  devicesApi: { list: async () => [] },
  vehiclesApi: { list: async () => [] },
  twoFactorApi: {},
  vehicleInputFrom: () => ({}),
}));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => {} }) }));
vi.mock('@/hooks/useFleet', () => ({ useFleet: () => ({ fleet: null }) }));
// As partes da ficha com os testes delas: aqui só importa em que aba ficam.
vi.mock('./CustomerPlanCard', () => ({ CustomerPlanCard: () => <p>[plano da conta]</p> }));
vi.mock('./affiliates/CustomerReferralCard', () => ({ CustomerReferralCard: () => <p>[indicado por]</p> }));
vi.mock('./CustomerOrganizationCard', () => ({ CustomerOrganizationCard: () => <p>[associação]</p> }));
vi.mock('./CustomerTeamCard', () => ({ CustomerTeamCard: () => <p>[equipe]</p> }));
vi.mock('./LaunchPromoCard', () => ({ LaunchPromoCard: () => <p>[pré-lançamento]</p> }));
vi.mock('@/components/alerts/CustomerAlertsSection', () => ({ CustomerAlertsSection: () => <p>[alertas]</p> }));
vi.mock('@/components/billing/NewVehicleWizard', () => ({ NewVehicleWizard: () => null }));
vi.mock('@/components/billing/FleetImportModal', () => ({ FleetImportModal: () => null }));
vi.mock('@/components/fulfillment/FulfillmentAdminModal', () => ({ FulfillmentAdminModal: () => null }));
vi.mock('@/components/billing/PixPaymentModal', () => ({ PixPaymentModal: () => null }));
vi.mock('./InviteLinkModal', () => ({ InviteLinkModal: () => null }));
vi.mock('./CustomerAccessModals', () => ({ CustomerSessionsModal: () => null, CustomerPasswordModal: () => null }));

import { CustomerDetailsPage, listPt, monthlyTotal } from './CustomerDetailsPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');

const sub = (over: Partial<Subscription>): Subscription =>
  ({
    id: 's1', customerId: 'c1', planName: 'Plano Mensal', priceCents: 6990, dueDay: 10, nextDueDate: '2026-12-10', status: 'ACTIVE',
    vehicleId: null, promoPriceCents: null, promoUntil: null, periodMonths: 1, endsOn: null, installments: 0, installmentsDueCents: 0,
    ...over,
  }) as Subscription;
const invoice = (over: Partial<Invoice>): Invoice =>
  ({ id: 'i1', description: 'Plano · novembro', amountCents: 6990, dueDate: '2026-11-10', status: 'OPEN', paidAt: null, ...over }) as Invoice;

const DETAIL = {
  id: 'c1', name: 'Ana Souza', email: 'ana@x.test', phone: '(11) 99999-0000', document: '12345678909', active: true,
  createdAt: '2026-10-01T12:00:00Z', activeSubscriptions: 2, freeSubscriptions: 0, vehicleCount: 0, sharedVehicles: 0,
  openInvoices: 1, overdueInvoices: 1, openAmountCents: 6990, suspended: false, organizationId: null, organizationName: '',
  twoFactor: { enabled: false }, subscriptions: [sub({}), sub({ id: 's2', priceCents: 25000, periodMonths: 6 })],
  invoices: [invoice({})], vehicles: [], onlinePayment: true, payments: [], deliveryAddress: null, fulfillments: [],
  historyRetentionDays: null, defaultHistoryDays: 90, affiliate: null, plan: null, card: null, contract: null, contractVersion: '3',
  reminders: {}, paymentLinks: {}, payAllUrl: 'https://farbo.test/pagar/conta/abc',
} as unknown as CustomerDetail;

let location = '';
function Where() {
  const l = useLocation();
  location = l.pathname + l.search;
  return null;
}

async function render(path = '/clientes/c1') {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route
              path="/clientes/:id"
              element={
                <>
                  <CustomerDetailsPage />
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

const button = (label: string) =>
  Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent?.replace(/\u00a0/g, ' ').startsWith(label));
const click = (el: HTMLElement | undefined) => act(async () => el!.click());

afterEach(() => {
  document.body.innerHTML = '';
});

describe('CustomerDetailsPage', () => {
  it('abre nos veículos, com o resumo e as ações do dia a dia no topo', async () => {
    state.detail = DETAIL;
    await render();
    expect(text()).toContain('Ana Souza');
    // O valor por mês: o mensal e o semestral dividido por 6.
    expect(text()).toContain('R$ 111,57/mês');
    expect(button('Cobrar (R$ 69,90)')).toBeDefined();
    expect(Array.from(document.querySelectorAll('[role="tab"]')).map((t) => t.textContent)).toEqual([
      'Veículos (0)',
      'Financeiro (1 vencida)',
      'Conta e acesso',
    ]);
    expect(button('Novo veículo')).toBeDefined();
    expect(text()).not.toContain('Nova fatura avulsa');
    expect(text()).not.toContain('Acesso ao painel');
    // O acesso saiu do cabeçalho.
    expect(button('Desativar')).toBeUndefined();
  });

  it('o quadro do em aberto leva ao financeiro: faturas primeiro, depois as condições da conta', async () => {
    state.detail = DETAIL;
    await render();
    await click(button('Em aberto'));
    expect(location).toBe('/clientes/c1?aba=financeiro');
    const t = text();
    expect(t).toContain('Nova fatura avulsa');
    expect(t).toContain('Plano · novembro');
    expect(t.indexOf('Faturas')).toBeLessThan(t.indexOf('[plano da conta]'));
    expect(t).toContain('[indicado por]');
    expect(t).toContain('[associação]');
    expect(button('Novo veículo')).toBeUndefined();
  });

  it('a conta e o acesso: convite, sessões, senha e desativar, com o endereço e os alertas', async () => {
    state.detail = DETAIL;
    await render('/clientes/c1?aba=conta');
    const t = text();
    expect(t).toContain('Acesso ao painel');
    expect(t).toContain('Contrato ainda não aceito');
    for (const label of ['Reenviar convite', 'Link do convite', 'Sessões', 'Definir senha', 'Desativar']) {
      expect(button(label), label).toBeDefined();
    }
    expect(t).toContain('Endereço de entrega');
    expect(t).toContain('[alertas]');
    await click(Array.from(document.querySelectorAll<HTMLButtonElement>('[role="tab"]'))[0]);
    expect(location).toBe('/clientes/c1');
  });

  it('sem nada em aberto, nada de link de cobrança nem de vencida na aba', async () => {
    state.detail = { ...DETAIL, invoices: [invoice({ status: 'PAID' })], openInvoices: 0, overdueInvoices: 0, openAmountCents: 0 };
    await render();
    expect(button('Cobrar')).toBeUndefined();
    expect(text()).toContain('Financeiro');
    expect(text()).not.toContain('vencida');
  });
});

describe('o cadastro sem o que o cartão pede', () => {
  it('o financeiro avisa o que falta, com o atalho para corrigir; sem nada faltando, nada de aviso', async () => {
    state.detail = { ...DETAIL, cardMissing: ['celular com DDD', 'endereço completo (CEP, rua, número, cidade e UF)'] };
    await render('/clientes/c1?aba=financeiro');
    expect(text()).toContain(
      'O cartão não aparece nos links deste cliente. Falta no cadastro: celular com DDD e endereço completo (CEP, rua, número, cidade e UF). O Pix continua valendo.',
    );
    const banner = document.querySelector('[role="status"]')!;
    expect(Array.from(banner.querySelectorAll('button')).map((b) => b.textContent)).toEqual(['Editar dados', 'Cadastrar endereço']);

    document.body.innerHTML = '';
    state.detail = { ...DETAIL, cardMissing: ['endereço completo (CEP, rua, número, cidade e UF)'], deliveryAddress: { street: 'Rua A' } };
    await render('/clientes/c1?aba=financeiro');
    expect(Array.from(document.querySelector('[role="status"]')!.querySelectorAll('button')).map((b) => b.textContent)).toEqual([
      'Corrigir endereço',
    ]);

    document.body.innerHTML = '';
    state.detail = DETAIL;
    await render('/clientes/c1?aba=financeiro');
    expect(text()).not.toContain('O cartão não aparece');
  });
});

describe('listPt', () => {
  it('junta com vírgula e "e"', () => {
    expect(listPt([])).toBe('');
    expect(listPt(['nome'])).toBe('nome');
    expect(listPt(['nome', 'e-mail'])).toBe('nome e e-mail');
    expect(listPt(['nome', 'e-mail', 'celular'])).toBe('nome, e-mail e celular');
  });
});

describe('monthlyTotal', () => {
  it('soma a promoção que vale, a parte do rastreador e divide o semestral e o anual; tira a encerrada', () => {
    expect(
      monthlyTotal([
        sub({ priceCents: 6990, promoPriceCents: 2990, promoUntil: '2027-01-01' }),
        sub({ priceCents: 4990, trackerAdded: true, trackerCents: 1200, trackerUntil: '2027-06-10' }),
        sub({ priceCents: 45000, periodMonths: 12 }),
        sub({ priceCents: 6990, endsOn: '2026-12-01' }),
        sub({ priceCents: 6990, status: 'CANCELED' }),
      ]),
    ).toBe(2990 + 6190 + 3750);
  });
});

describe('cobrar pela ficha', () => {
  it('o Cobrar do cabeçalho abre a mensagem pronta com o link de todas em aberto', async () => {
    state.detail = DETAIL;
    await render();
    await click(button('Cobrar (R$ 69,90)'));
    expect(text()).toContain('Cobrar Ana Souza');
    expect((document.querySelector('textarea') as HTMLTextAreaElement).value).toContain('https://farbo.test/pagar/conta/abc');
  });
});

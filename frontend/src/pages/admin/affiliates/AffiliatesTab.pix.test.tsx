// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { AffiliatePayout, PixPlan } from '@/types';

const sent: unknown[] = [];
let payoutCalls = 0;
const payout = (over: Partial<AffiliatePayout>): AffiliatePayout => ({
  id: 'p1', affiliateId: 'a1', affiliateName: 'João Motoca', month: '2026-09', amountCents: 1200, commissions: 2,
  entryId: 'e1', status: 'OPEN', dueDate: '2026-10-20', paidOn: null, pixStatus: '', receiptUrl: '', createdAt: '2026-10-01T12:00:00Z',
  ...over,
});
const PAYOUTS: AffiliatePayout[] = [
  payout({}),
  payout({ id: 'p2', affiliateName: 'Maria Influencer', entryId: 'e2', pixStatus: 'UNKNOWN' }),
  payout({
    id: 'p3', affiliateName: 'Caio Grau', entryId: 'e3', status: 'PAID', paidOn: '2026-10-05', pixStatus: 'COMPLETE',
    receiptUrl: 'https://app.abacatepay.com/receipt/x',
  }),
];
const PLAN: PixPlan = {
  entryId: 'e1', description: 'Comissão de afiliado: João Motoca (até set/2026)', supplierName: 'João Motoca', amountCents: 1200,
  source: 'key', key: 'joao@pix.test', keyType: 'EMAIL', recipient: '', feeCents: 80, sendCents: 1280,
};
vi.mock('@/api/resources', () => ({
  affiliatesApi: {
    list: async () => [],
    settings: async () => ({ defaultCommissionCents: 600, categoryId: 'c1' }),
    closing: async () => [],
    payouts: async () => {
      payoutCalls++;
      return PAYOUTS;
    },
  },
  financeApi: {
    pixInfo: async () => ({ enabled: true, devMode: false, availableCents: 500000, balanceError: '' }),
    entryPix: async () => ({ plan: PLAN, problem: '', transfers: [] }),
    sendPix: async (id: string, token: string) => {
      sent.push([id, token]);
      return {
        id: 't1', entryId: id, providerId: 'tran_1', status: 'COMPLETE', amountCents: 1200, sentCents: 1280, deliveredCents: 1200,
        feeCents: 80, key: 'joao@pix.test', keyType: 'EMAIL', receiptUrl: '', devMode: false, error: '',
        createdAt: '2026-10-15T12:00:00Z', completedAt: '2026-10-15T12:00:01Z',
      };
    },
  },
}));
vi.mock('@/components/auth/IdentityCheck', () => ({
  IdentityCheck: ({ action, onGrant }: { action: string; onGrant: (token: string) => void }) => (
    <button type="button" onClick={() => onGrant('comprovante-1')}>
      {action}, confirmar
    </button>
  ),
}));

import { AffiliatesTab } from './AffiliatesTab';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const buttons = (label: string | RegExp) =>
  Array.from(document.querySelectorAll('button')).filter((b) =>
    typeof label === 'string' ? b.textContent === label : label.test(b.textContent ?? ''),
  );
const row = (name: string) => Array.from(document.querySelectorAll('tbody tr')).find((r) => r.textContent?.includes(name))!;

afterEach(() => {
  document.body.innerHTML = '';
  sent.length = 0;
  payoutCalls = 0;
});

describe('fechamentos dos afiliados', () => {
  it('paga por Pix pelo fechamento e mostra o que está a conferir e o comprovante', async () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <MemoryRouter>
            <ToastProvider>
              <AffiliatesTab />
            </ToastProvider>
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    );
    await flush();
    await flush();

    // A pagar: o botão do Pix e o desfazer.
    const joao = row('João Motoca');
    expect(joao.textContent).toContain('Pagar por Pix');
    expect(joao.textContent).toContain('Desfazer');
    // Um Pix sem resposta: confere-se em Empresa; nem pagar de novo, nem desfazer.
    const maria = row('Maria Influencer');
    expect(maria.textContent).toContain('Pix a conferir');
    expect(maria.textContent).not.toContain('Pagar por Pix');
    expect(maria.textContent).not.toContain('Desfazer');
    // Pago por Pix: o comprovante.
    const caio = row('Caio Grau');
    expect(caio.textContent).toContain('por Pix em 05/10/2026');
    expect(caio.querySelector('a')?.getAttribute('href')).toBe('https://app.abacatepay.com/receipt/x');
    expect(caio.textContent).not.toContain('Pagar por Pix');

    // O Pix: confere a chave e o valor, confirma e envia a conta do fechamento.
    await act(async () => buttons('Pagar por Pix')[0].click());
    await flush();
    expect(text()).toContain('joao@pix.test');
    expect(text()).toContain('R$ 12,80');
    await act(async () => buttons('Continuar')[0].click());
    const before = payoutCalls;
    await act(async () => buttons(/confirmar$/)[0].click());
    await flush();
    expect(sent).toEqual([['e1', 'comprovante-1']]);
    expect(text()).toContain('Pix enviado: R$ 12,00 para João Motoca');
    // Os fechamentos são atualizados (o pago aparece pago).
    expect(payoutCalls).toBeGreaterThan(before);
  });
});

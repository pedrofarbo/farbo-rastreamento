// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { FinanceEntry, PixPlan, PixTransfer } from '@/types';

const sent: unknown[] = [];
let plan: { plan: PixPlan | null; problem: string; transfers: PixTransfer[] };
let available: number | null = 500000;
let sendResult: () => Promise<PixTransfer>;
vi.mock('@/api/resources', () => ({
  financeApi: {
    entryPix: async () => plan,
    pixInfo: async () => ({ enabled: true, devMode: false, availableCents: available, balanceError: '' }),
    sendPix: (id: string, token: string) => {
      sent.push([id, token]);
      return sendResult();
    },
  },
}));
// A confirmação de verdade (senha ou biometria) tem os próprios testes.
vi.mock('@/components/auth/IdentityCheck', () => ({
  IdentityCheck: ({ action, onGrant }: { action: string; onGrant: (token: string) => void }) => (
    <button type="button" onClick={() => onGrant('comprovante-1')}>
      {action}, confirmar
    </button>
  ),
}));

import { PixPayModal, detectKeyType, keyPreview } from './PixModals';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string | RegExp) =>
  Array.from(document.querySelectorAll('button')).find((b) =>
    typeof label === 'string' ? b.textContent === label : label.test(b.textContent ?? ''),
  ) as HTMLButtonElement;

const ENTRY = { id: 'e1', description: 'Honorários de outubro' } as FinanceEntry;
const PLAN: PixPlan = {
  entryId: 'e1', description: 'Honorários de outubro', supplierName: 'Silva Contabilidade', amountCents: 45000,
  source: 'key', key: '52998224725', keyType: 'CPF', recipient: '', feeCents: 80, sendCents: 45080,
};

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ToastProvider>
          <PixPayModal entry={ENTRY} onClose={() => undefined} />
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  sent.length = 0;
  available = 500000;
});

describe('chave Pix', () => {
  it('o tipo pelo formato e a chave para conferir', () => {
    expect(detectKeyType('fornecedor@exemplo.com')).toBe('EMAIL');
    expect(detectKeyType('529.982.247-25')).toBe('CPF');
    expect(detectKeyType('11222333000181')).toBe('CNPJ');
    expect(detectKeyType('(11) 98888-7777')).toBe('PHONE');
    expect(detectKeyType('123e4567-e89b-12d3-a456-426614174000')).toBe('RANDOM');
    expect(detectKeyType('52998224725')).toBe('');
    expect(keyPreview('52998224725', 'CPF')).toBe('529.982.247-25');
    expect(keyPreview('11222333000181', 'CNPJ')).toBe('11.222.333/0001-81');
    expect(keyPreview('11988887777', 'PHONE')).toBe('(11) 98888-7777');
  });
});

describe('PixPayModal', () => {
  it('confere, confirma e envia', async () => {
    plan = { plan: PLAN, problem: '', transfers: [] };
    sendResult = async () => ({
      id: 't1', entryId: 'e1', providerId: 'tran_1', status: 'COMPLETE', amountCents: 45000, sentCents: 45080,
      deliveredCents: 45000, feeCents: 80, key: '52998224725',
      keyType: 'CPF', receiptUrl: 'https://app.abacatepay.com/receipt/tran_1', devMode: false, error: '', createdAt: '', completedAt: '',
    });
    await render();
    // O fornecedor recebe a conta inteira; a tarifa é por fora, paga por quem envia.
    for (const part of ['Silva Contabilidade', '529.982.247-25', 'RecebeR$ 450,00 (o valor da conta, inteiro)',
      'TarifaR$ 0,80 da AbacatePay, paga por você (até o 20º envio do mês)', 'Sai do saldoR$ 450,80', 'R$ 5.000,00 disponível']) {
      expect(text()).toContain(part);
    }
    expect(sent).toEqual([]);
    await act(async () => button('Continuar').click());
    await act(async () => button(/Para enviar R\$\s450,00 por Pix/).click());
    await flush();
    expect(sent).toEqual([['e1', 'comprovante-1']]);
    expect(text()).toContain('Pix enviado: R$ 450,00 para o fornecedor');
    expect(text()).toContain('tarifa de R$ 0,80');
    expect(text()).not.toContain('a menos que a conta');
    expect(document.querySelector('a[href="https://app.abacatepay.com/receipt/tran_1"]')).not.toBeNull();
  });

  it('tarifa cheia (depois do 20º envio) e tarifa cobrada maior que a prevista', async () => {
    plan = { plan: { ...PLAN, feeCents: 250, sendCents: 45250 }, problem: '', transfers: [] };
    sendResult = async () => ({
      id: 't2', entryId: 'e1', providerId: 'tran_2', status: 'COMPLETE', amountCents: 45000, sentCents: 45080,
      deliveredCents: 44830, feeCents: 250, key: '52998224725', keyType: 'CPF', receiptUrl: '', devMode: false, error: '',
      createdAt: '', completedAt: '',
    });
    await render();
    expect(text()).toContain('a partir do 21º envio do mês');
    expect(text()).toContain('Sai do saldoR$ 452,50');
    await act(async () => button('Continuar').click());
    await act(async () => button(/Para enviar/).click());
    await flush();
    expect(text()).toContain('chegaram R$ 448,30, R$ 1,70 a menos que a conta');
  });

  it('a recusa volta para a conferência com o motivo', async () => {
    plan = { plan: PLAN, problem: '', transfers: [] };
    sendResult = async () => {
      throw new Error('A AbacatePay recusou o Pix: Saldo insuficiente');
    };
    await render();
    await act(async () => button('Continuar').click());
    await act(async () => button(/confirmar/).click());
    await flush();
    expect(text()).toContain('A AbacatePay recusou o Pix: Saldo insuficiente');
    expect(button('Continuar')).toBeTruthy();
  });

  it('saldo baixo só avisa (a API pode atrasar); sem destino, não continua', async () => {
    plan = { plan: PLAN, problem: '', transfers: [] };
    available = 0;
    await render();
    expect(text()).toContain('A API da AbacatePay informa R$ 0,00, mas pode estar atrasada');
    expect(button('Continuar').disabled).toBe(false);
    document.body.innerHTML = '';
    plan = { plan: null, problem: 'Sem Chave não tem chave Pix: cadastre em Empresa → Cadastros.', transfers: [] };
    available = 500000;
    await render();
    expect(text()).toContain('não tem chave Pix');
    expect(button('Continuar').disabled).toBe(true);
  });
});

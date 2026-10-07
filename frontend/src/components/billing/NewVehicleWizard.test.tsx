// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { ShippingQuoteView } from '@/types';

const ordered: unknown[] = [];
let quote: ShippingQuoteView;
// O pedido recusa uma vez por falta do aceite do contrato (quem só acompanhava).
let needsContract = false;
const accepted: unknown[] = [];
vi.mock('@/api/resources', () => ({
  catalogApi: {
    get: async () => ({
      planName: 'Plano Mensal', planPriceCents: 6990, defaultDueDay: 10, equipmentName: 'Rastreador J16',
      equipmentPriceCents: 15000, setupDueDays: 3, equipmentMaxInstallments: 10, launchPromo: null,
    }),
  },
  meApi: {
    account: async () => ({
      deliveryAddress: { zipCode: '20040020', street: 'Rua da Assembleia', number: '10', complement: '', district: 'Centro', city: 'Rio de Janeiro', state: 'RJ' },
    }),
    shippingQuote: async () => quote,
    orderTracker: async (input: unknown) => {
      if (needsContract) {
        const { ApiError } = await import('@/api/client');
        throw new ApiError(403, 'aceite o contrato de prestação de serviços para continuar', { code: 'CONTRACT_REQUIRED' });
      }
      ordered.push(input);
      return { vehicle: {}, subscription: {}, setupInvoice: null, shipping: null };
    },
    saveAddress: async () => ({}),
  },
  customersApi: {},
  contractApi: {
    mine: async () => ({
      contract: { version: '1', effectiveDate: '7 de outubro de 2026', title: 'Contrato de Prestação de Serviços de Rastreamento Veicular', intro: [], sections: [], sha256: 'x' },
      required: true, accepted: null, name: 'Bia', taxId: '',
    }),
    accept: async (version: string, document: string) => {
      accepted.push([version, document]);
      needsContract = false;
      return { contract: {}, required: false, accepted: {}, name: 'Bia', taxId: document };
    },
  },
  installersApi: { publicList: async () => [] },
  publicApi: { installers: async () => [] },
}));

import { NewVehicleWizard } from './NewVehicleWizard';
import { deliveryLabel, quoteName } from './ShippingOptions';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string | RegExp) =>
  Array.from(document.querySelectorAll('button')).find((b) =>
    typeof label === 'string' ? b.textContent === label : label.test(b.textContent ?? ''),
  ) as HTMLButtonElement;

function type(input: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
}

async function openWizard(installments = 1) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter>
          <ToastProvider>
            <NewVehicleWizard open onClose={() => undefined} onDone={() => undefined} />
          </ToastProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  );
  await flush();
  // 1. Veículo; 2. Rastreador (o endereço salvo); 3. Assinatura, com o frete.
  const name = Array.from(document.querySelectorAll('input')).find((i) => i.closest('div')?.textContent?.includes('Apelido')) as HTMLInputElement;
  await act(async () => type(name, 'Moto do trabalho'));
  await act(async () => button('Continuar').click());
  await flush();
  if (installments > 1) {
    const select = document.querySelector('select') as HTMLSelectElement;
    await act(async () => {
      select.value = String(installments);
      select.dispatchEvent(new Event('change', { bubbles: true }));
    });
  }
  await act(async () => button('Continuar').click());
  await flush();
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  ordered.length = 0;
  accepted.length = 0;
  needsContract = false;
});

describe('frete no pedido', () => {
  it('formatos', () => {
    expect(quoteName({ company: 'Correios', service: 'PAC' })).toBe('Correios PAC');
    expect(deliveryLabel(7)).toBe('até 7 dias úteis');
    expect(deliveryLabel(1)).toBe('até 1 dia útil');
  });

  it('o cliente escolhe a entrega e paga junto com o equipamento', async () => {
    quote = {
      enabled: true, zipCode: '20040020', problem: '', arrange: false,
      quotes: [
        { serviceId: 1, service: 'PAC', company: 'Correios', priceCents: 2240, deliveryDays: 7, error: '' },
        { serviceId: 2, service: 'SEDEX', company: 'Correios', priceCents: 3990, deliveryDays: 2, error: '' },
      ],
    };
    await openWizard();
    // O mais barato vem marcado; o total soma o frete.
    expect(text()).toContain('Entrega para o CEP 20040-020');
    expect(text()).toContain('Frete: Correios PAC (até 7 dias úteis)R$ 22,40');
    expect(text()).toContain('Agora: equipamento e freteR$ 172,40');
    expect(button(/Confirmar pedido · R\$\s172,40/)).toBeTruthy();
    // Escolhe o SEDEX.
    const sedex = Array.from(document.querySelectorAll('input[type=radio]'))[1] as HTMLInputElement;
    await act(async () => sedex.click());
    expect(text()).toContain('Agora: equipamento e freteR$ 189,90');
    await act(async () => button(/Confirmar pedido/).click());
    await flush();
    expect(ordered).toEqual([{ vehicle: expect.objectContaining({ name: 'Moto do trabalho' }), launchPromo: false, shippingServiceId: 2 }]);
  });

  it('sem cotação, não confirma (e tenta de novo)', async () => {
    quote = { enabled: true, zipCode: '20040020', problem: 'não deu para calcular o frete agora; tente de novo em instantes', quotes: [], arrange: false };
    await openWizard();
    expect(text()).toContain('Não deu para calcular o frete');
    expect(button(/Confirmar pedido/).disabled).toBe(true);
    expect(button('Tentar de novo')).toBeTruthy();
  });

  it('em São Paulo, dá para combinar a entrega (sem frete), mesmo sem cotação', async () => {
    quote = { enabled: true, zipCode: '01305000', problem: 'não deu para calcular o frete agora; tente de novo em instantes', quotes: [], arrange: true };
    await openWizard();
    expect(text()).toContain('Combinar entrega');
    // Nada marcado ainda: escolhe combinar.
    expect(button(/Confirmar pedido/).disabled).toBe(true);
    const arrange = document.querySelector('input[type=radio]') as HTMLInputElement;
    await act(async () => arrange.click());
    expect(text()).toContain('Entrega: combinar com a Farbosem frete');
    expect(text()).toContain('Agora: equipamentoR$ 150,00');
    expect(button(/Confirmar pedido · R\$\s150,00/).disabled).toBe(false);
    await act(async () => button(/Confirmar pedido/).click());
    await flush();
    expect(ordered).toEqual([{ vehicle: expect.objectContaining({ name: 'Moto do trabalho' }), launchPromo: false, arrangeDelivery: true }]);
  });

  it('fora de São Paulo, não aparece combinar', async () => {
    quote = {
      enabled: true, zipCode: '20040020', problem: '', arrange: false,
      quotes: [{ serviceId: 1, service: 'PAC', company: 'Correios', priceCents: 2240, deliveryDays: 7, error: '' }],
    };
    await openWizard();
    expect(text()).toContain('Correios PAC');
    expect(text()).not.toContain('Combinar entrega');
  });

  it('sem o frete ligado, o pedido segue como antes', async () => {
    quote = { enabled: false, zipCode: '', problem: '', quotes: [], arrange: false };
    await openWizard();
    expect(text()).not.toContain('Entrega para o CEP');
    expect(button(/Confirmar pedido · R\$\s150,00/).disabled).toBe(false);
    await act(async () => button(/Confirmar pedido/).click());
    await flush();
    expect(ordered).toEqual([{ vehicle: expect.objectContaining({ name: 'Moto do trabalho' }), launchPromo: false }]);
  });

  it('parcelado em 10x: agora só a 1ª parcela, e o cliente confirma que fica até a última', async () => {
    quote = {
      enabled: true, zipCode: '20040020', problem: '', arrange: false,
      quotes: [{ serviceId: 1, service: 'PAC', company: 'Correios', priceCents: 2240, deliveryDays: 7, error: '' }],
    };
    await openWizard(10);
    expect(text()).toContain('Equipamento: 1ª de 10 parcelas (R$ 150,00 sem juros)R$ 15,00');
    expect(text()).toContain('Agora: 1ª parcela e freteR$ 37,40');
    expect(text()).toContain('+ R$ 15,00 do rastreador em cada uma das 9 mensalidades seguintes.');
    expect(text()).toContain('fica ativa até a mensalidade com a última parcela (a 10ª)');
    // Sem o "entendi", não confirma.
    expect(button(/Confirmar pedido · R\$\s37,40/).disabled).toBe(true);
    const agree = document.querySelector('input[type=checkbox]') as HTMLInputElement;
    await act(async () => agree.click());
    expect(button(/Confirmar pedido/).disabled).toBe(false);
    await act(async () => button(/Confirmar pedido/).click());
    await flush();
    expect(ordered).toEqual([
      { vehicle: expect.objectContaining({ name: 'Moto do trabalho' }), launchPromo: false, shippingServiceId: 1, installments: 10 },
    ]);
  });

  it('sem o aceite do contrato, o contrato abre e o pedido segue depois', async () => {
    quote = { enabled: false, zipCode: '', problem: '', quotes: [], arrange: false };
    needsContract = true;
    await openWizard();
    await act(async () => button(/Confirmar pedido/).click());
    await flush();
    await flush();
    expect(text()).toContain('Os pontos principais');
    expect(ordered).toEqual([]);
    const cpf = document.querySelector('input[inputmode="numeric"]') as HTMLInputElement;
    await act(async () => type(cpf, '52998224725'));
    expect(cpf.value).toBe('529.982.247-25');
    const agree = document.querySelector('input[type=checkbox]') as HTMLInputElement;
    await act(async () => agree.click());
    await act(async () => button('Aceitar e continuar').click());
    await flush();
    await flush();
    expect(accepted).toEqual([['1', '529.982.247-25']]);
    expect(ordered).toEqual([{ vehicle: expect.objectContaining({ name: 'Moto do trabalho' }), launchPromo: false }]);
  });
});

// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { PromoStatus, ShippingQuoteView } from '@/types';

const ordered: unknown[] = [];
let quote: ShippingQuoteView;
// A central: o cliente fora da lista de lançamento.
let promoStatus: PromoStatus;
// O pedido recusa uma vez por falta do aceite do contrato (quem só acompanhava).
let needsContract = false;
const accepted: unknown[] = [];
// Os pedidos que o assistente concluiu; o rastreador do cliente (venda ou comodato).
const done: unknown[] = [];
let terms: Record<string, unknown> = {};
vi.mock('@/api/resources', () => ({
  catalogApi: {
    get: async () => ({
      planName: 'Plano Mensal', planPriceCents: 6990, defaultDueDay: 10, equipmentName: 'Rastreador J16',
      equipmentPriceCents: 15000, setupDueDays: 3, equipmentMaxInstallments: 10, launchPromo: null,
      chargeDate: '2026-11-10', ...terms,
    }),
  },
  meApi: {
    account: async () => ({
      onlinePayment: true,
      deliveryAddress: { zipCode: '20040020', street: 'Rua da Assembleia', number: '10', complement: '', district: 'Centro', city: 'Rio de Janeiro', state: 'RJ' },
    }),
    shippingQuote: async () => quote,
    card: async () => ({ enabled: false, publicKey: '', saved: null, payer: null }),
    orderTracker: async (input: unknown) => {
      if (needsContract) {
        const { ApiError } = await import('@/api/client');
        throw new ApiError(403, 'aceite o contrato de prestação de serviços para continuar', { code: 'CONTRACT_REQUIRED' });
      }
      ordered.push(input);
      return { vehicle: {}, subscription: {}, chargeDate: '2026-11-10', setupInvoice: null, shipping: null };
    },
    saveAddress: async () => ({}),
  },
  customersApi: {
    launchPromo: async () => promoStatus,
    grantLaunchPromo: async () => {
      promoStatus = { ...promoStatus, eligible: true, reason: '', code: '', grantedAt: '2026-10-09T12:00:00Z' };
      return promoStatus;
    },
    shippingQuote: async () => ({ enabled: false, zipCode: '', problem: '', quotes: [], arrange: false }),
  },
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
            <NewVehicleWizard open onClose={() => undefined} onDone={(result) => done.push(result)} />
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

// Hoje é 23/10/2026: até a fatura de 10/11, 18 dias (de 69,90: 41,94).
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date(2026, 9, 23, 12, 0, 0));
});

afterEach(() => {
  vi.useRealTimers();
  document.body.innerHTML = '';
  ordered.length = 0;
  accepted.length = 0;
  done.length = 0;
  needsContract = false;
  terms = {};
});

describe('a fatura do mês', () => {
  it('nada é cobrado na hora: o pedido entra na próxima fatura, com o proporcional', async () => {
    quote = { enabled: false, zipCode: '', problem: '', arrange: false, quotes: [] };
    await openWizard();
    expect(text()).not.toContain('Como pagar');
    expect(text()).toContain('Nada é cobrado agora: tudo entra na sua fatura de 10/11/2026');
    expect(text()).toContain('Proporcional de hoje a 10/11/2026 (18 dias)R$ 41,94');
    expect(text()).toContain('1ª mensalidadeR$ 69,90');
    expect(text()).toContain('EquipamentoR$ 150,00');
    expect(text()).toContain('Na fatura de 10/11/2026Hoje não é cobrado nada.R$ 261,84');
    await act(async () => button('Confirmar pedido').click());
    await flush();
    expect(done).toHaveLength(1);
  });

  it('no comodato, sem equipamento: a instalação entra na fatura', async () => {
    terms = { equipmentMode: 'loan', installFeeCents: 8000, uninstallFeeCents: 5000 };
    quote = { enabled: false, zipCode: '', problem: '', arrange: false, quotes: [] };
    await openWizard();
    expect(text()).toContain('Rastreador em comodato');
    expect(text()).toContain('Ao encerrar, a desinstalação: R$ 50,00');
    expect(text()).not.toContain('EquipamentoR$');
    expect(text()).toContain('Instalação (comodato)R$ 80,00');
    expect(text()).toContain('Na fatura de 10/11/2026Hoje não é cobrado nada.R$ 191,84');
  });
});

describe('a pessoa física paga no ato', () => {
  it('a 1ª mensalidade, o rastreador e o frete agora, sem proporcional, com a forma de pagar', async () => {
    terms = { payNow: true, chargeDate: '2026-10-23' };
    quote = { enabled: false, zipCode: '', problem: '', arrange: false, quotes: [] };
    await openWizard();
    expect(text()).toContain('Hoje você paga a 1ª mensalidade e o resto do pedido');
    expect(text()).not.toContain('Proporcional');
    expect(text()).toContain('1ª mensalidadeR$ 69,90');
    expect(text()).toContain('EquipamentoR$ 150,00');
    expect(text()).toContain('AgoraA 1ª mensalidade começa hoje; a 2ª vence pelo menos um mês depois, no dia da conta.R$ 219,90');
    expect(text()).toContain('Como pagar');
    await act(async () => button('Confirmar pedido').click());
    await flush();
    expect(done).toHaveLength(1);
    expect(ordered[0]).not.toHaveProperty('equipmentMode');
  });

  it('o rastreador comprado: instalar por conta própria ou com os instaladores da Farbo (pago no pedido)', async () => {
    terms = { payNow: true, chargeDate: '2026-10-23', install: { enabled: true, priceCents: 15000 } };
    quote = { enabled: false, zipCode: '', problem: '', arrange: false, quotes: [] };
    await openWizard();
    expect(text()).not.toContain('Instalação (instaladores Farbo)');
    await act(async () => button('Voltar').click());
    await flush();
    expect(text()).toContain('Por conta própria');
    expect(text()).toContain('Com os instaladores da Farbo · R$ 150,00, pago no pedido; agendamos o dia com você.');
    const farbo = Array.from(document.querySelectorAll<HTMLInputElement>('input[name="install-choice"]'))[1];
    await act(async () => farbo.click());
    await act(async () => button('Continuar').click());
    await flush();
    expect(text()).toContain('Instalação (instaladores Farbo)R$ 150,00');
    expect(text()).toContain('R$ 369,90');
    await act(async () => button('Confirmar pedido').click());
    await flush();
    expect(ordered[0]).toMatchObject({ farboInstall: true });
  });

  it('com o comodato oferecido, o cliente escolhe: sem o rastreador, a mensalidade e a instalação do comodato', async () => {
    terms = {
      payNow: true,
      chargeDate: '2026-10-23',
      loan: { enabled: true, surchargeCents: 300, installFeeCents: 9900, uninstallFeeCents: 4900 },
      install: { enabled: true, priceCents: 15000 },
    };
    quote = { enabled: false, zipCode: '', problem: '', arrange: false, quotes: [] };
    await openWizard();
    // Volta à etapa do rastreador e escolhe o comodato.
    await act(async () => button('Voltar').click());
    await flush();
    expect(text()).toContain('Comprar o rastreador');
    expect(text()).toContain('Comodato (rastreador emprestado)');
    // A pessoa física tem a garantia de troca somada à mensalidade do plano.
    expect(text()).toContain(
      'Mensalidade de R$ 72,90, com R$ 3,00 de garantia de troca do rastreador; instalação R$ 99,00, desinstalação R$ 49,00 ao encerrar.',
    );
    const loan = Array.from(document.querySelectorAll<HTMLInputElement>('input[name="equipment-mode"]'))[1];
    await act(async () => loan.click());
    await act(async () => button('Continuar').click());
    await flush();
    expect(text()).toContain('Rastreador em comodato');
    expect(text()).toContain('A instalação (R$ 99,00) é paga no pedido.');
    expect(text()).not.toContain('EquipamentoR$');
    expect(text()).toContain('1ª mensalidadeR$ 72,90');
    expect(text()).toContain('Instalação (comodato)R$ 99,00');
    expect(text()).toContain('R$ 171,90');
    await act(async () => button('Confirmar pedido').click());
    await flush();
    expect(ordered[0]).toMatchObject({ equipmentMode: 'loan', launchPromo: false });
    // No comodato a instalação é sempre nossa, com a taxa dele: sem a escolha.
    expect(ordered[0]).not.toHaveProperty('farboInstall');
  });
});

describe('a central e a promoção de pré-lançamento', () => {
  it('libera ali mesmo para quem está fora da lista, e ela já vem marcada', async () => {
    promoStatus = {
      eligible: false, reason: 'o e-mail da conta não está na lista de lançamento', code: 'NOT_ON_LIST',
      offer: { equipmentCents: 12000, monthlyCents: 3490, insanosMonthlyCents: 2790, insanosPlanName: 'Especial Insanos MC', months: 12 },
      onList: false, grantedAt: null, claimed: false,
    };
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <MemoryRouter>
            <ToastProvider>
              <NewVehicleWizard
                open
                admin={{ customerId: 'c1', devices: [], deliveryAddress: null, currentPlan: null }}
                onClose={() => undefined}
                onDone={() => undefined}
              />
            </ToastProvider>
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    );
    await flush();
    const name = Array.from(document.querySelectorAll('input')).find((i) => i.closest('div')?.textContent?.includes('Apelido')) as HTMLInputElement;
    await act(async () => type(name, 'Moto do Caio'));
    await act(async () => button('Continuar').click());
    await flush();
    expect(text()).toContain('Promoção de pré-lançamento: o e-mail da conta não está na lista de lançamento.');
    await act(async () => button('Liberar para este cliente').click());
    await flush();
    const promo = Array.from(document.querySelectorAll('input[type=checkbox]')).find((i) =>
      i.closest('label')?.textContent?.includes('Aplicar a promoção de pré-lançamento'),
    ) as HTMLInputElement;
    expect(promo?.checked).toBe(true);
    const label = Array.from(document.querySelectorAll('label')).find((l) => l.textContent?.startsWith('Rastreador J16 (R$)'));
    const equipment = document.getElementById(label?.htmlFor ?? '') as HTMLInputElement;
    expect(equipment.value).toBe('120,00');
    expect(button('Liberar para este cliente')).toBeUndefined();
  });
});

describe('o plano do cliente no Novo veículo da central', () => {
  it('vem com o plano definido para o cliente', async () => {
    promoStatus = {
      eligible: false, reason: 'o e-mail da conta não está na lista de lançamento', code: 'NOT_ON_LIST',
      offer: { equipmentCents: 12000, monthlyCents: 3490, insanosMonthlyCents: 2790, insanosPlanName: 'Especial Insanos MC', months: 12 },
      onList: false, grantedAt: null, claimed: false,
    };
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <MemoryRouter>
            <ToastProvider>
              <NewVehicleWizard
                open
                admin={{
                  customerId: 'c1', devices: [], deliveryAddress: null, currentPlan: null,
                  accountPlan: {
                    planName: 'Especial Insanos MC', priceCents: 3990, dueDay: 15, updatedAt: '2026-10-09T12:00:00Z',
                    equipmentMode: 'sale', installFeeCents: 0, uninstallFeeCents: 0,
                  },
                }}
                onClose={() => undefined}
                onDone={() => undefined}
              />
            </ToastProvider>
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    );
    await flush();
    const name = Array.from(document.querySelectorAll('input')).find((i) => i.closest('div')?.textContent?.includes('Apelido')) as HTMLInputElement;
    await act(async () => type(name, 'Moto do Edu'));
    await act(async () => button('Continuar').click());
    await flush();
    await act(async () => button('Continuar').click());
    await flush();
    const select = (label: string) => {
      const l = Array.from(document.querySelectorAll('label')).find((x) => x.textContent === label);
      return document.getElementById(l?.htmlFor ?? '') as HTMLSelectElement | HTMLInputElement;
    };
    expect(select('Plano').value).toBe('insanos');
    expect(select('Valor mensal (R$)').value).toBe('39,90');
    expect(select('Vencimento').value).toBe('15');
  });
});

describe('frete no pedido', () => {
  it('formatos', () => {
    expect(quoteName({ company: 'Correios', service: 'PAC' })).toBe('Correios PAC');
    expect(deliveryLabel(7)).toBe('até 7 dias úteis');
    expect(deliveryLabel(1)).toBe('até 1 dia útil');
  });

  it('o cliente escolhe a entrega; o frete entra na fatura', async () => {
    quote = {
      enabled: true, zipCode: '20040020', problem: '', arrange: false,
      quotes: [
        { serviceId: 1, service: 'PAC', company: 'Correios', priceCents: 2240, deliveryDays: 7, error: '' },
        { serviceId: 2, service: 'SEDEX', company: 'Correios', priceCents: 3990, deliveryDays: 2, error: '' },
      ],
    };
    await openWizard();
    // O mais barato vem marcado; o total soma o frete, o proporcional e a 1ª mensalidade.
    expect(text()).toContain('Entrega para o CEP 20040-020');
    expect(text()).toContain('Frete: Correios PAC (até 7 dias úteis)R$ 22,40');
    expect(text()).toContain('1ª mensalidadeR$ 69,90');
    expect(text()).toContain('Na fatura de 10/11/2026Hoje não é cobrado nada.R$ 284,24');
    // Escolhe o SEDEX.
    const sedex = Array.from(document.querySelectorAll('input[type=radio]'))[1] as HTMLInputElement;
    await act(async () => sedex.click());
    expect(text()).toContain('Na fatura de 10/11/2026Hoje não é cobrado nada.R$ 301,74');
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
    expect(text()).toContain('Na fatura de 10/11/2026Hoje não é cobrado nada.R$ 261,84');
    expect(button('Confirmar pedido').disabled).toBe(false);
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
    expect(button('Confirmar pedido').disabled).toBe(false);
    await act(async () => button(/Confirmar pedido/).click());
    await flush();
    expect(ordered).toEqual([{ vehicle: expect.objectContaining({ name: 'Moto do trabalho' }), launchPromo: false }]);
  });

  it('parcelado em 10x: na fatura, o frete e a 1ª mensalidade com a 1ª parcela; o cliente confirma que fica até a última', async () => {
    quote = {
      enabled: true, zipCode: '20040020', problem: '', arrange: false,
      quotes: [{ serviceId: 1, service: 'PAC', company: 'Correios', priceCents: 2240, deliveryDays: 7, error: '' }],
    };
    await openWizard(10);
    expect(text()).toContain('Equipamento: R$ 150,00 em 10x sem jurosnas mensalidades');
    expect(text()).toContain('1ª mensalidade, com a 1ª parcelaR$ 84,90');
    expect(text()).toContain('Na fatura de 10/11/2026Hoje não é cobrado nada.R$ 149,24');
    expect(text()).toContain('+ R$ 15,00 do rastreador em cada uma das 10 primeiras mensalidades.');
    expect(text()).toContain('fica ativa até a mensalidade com a última parcela (a 10ª)');
    // Sem o "entendi", não confirma.
    expect(button('Confirmar pedido').disabled).toBe(true);
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

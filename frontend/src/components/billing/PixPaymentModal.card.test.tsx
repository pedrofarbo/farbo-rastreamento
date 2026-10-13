// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { PixApi } from '@/api/resources';
import { ToastProvider } from '@/components/ui/Toast';
import type { CardPayInput, CardView, PixCharge } from '@/types';

// O formulário da AbacatePay de mentira: monta, gera o token e guarda o que pediram.
const form = { created: [] as unknown[], tokenized: [] as unknown[], destroyed: 0, fail: '' };
vi.mock('@/services/abacatepayCard', async (original) => ({
  ...(await original<typeof import('@/services/abacatepayCard')>()),
  loadAbacatePay: async () => ({
    create: (options: unknown) => {
      form.created.push(options);
      return {
        mount: async () => undefined,
        tokenize: async (payer: unknown) => {
          form.tokenized.push(payer);
          if (form.fail) throw Object.assign(new Error('x'), { code: form.fail });
          return { token: 'ctok_1', expiresIn: 60, brand: 'VISA', lastFour: '4242' };
        },
        destroy: () => {
          form.destroyed++;
        },
      };
    },
  }),
}));

import { PixPaymentModal } from './PixPaymentModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.replace(/ /g, ' ') === label) as HTMLButtonElement;
const radio = (label: string) =>
  Array.from(document.querySelectorAll('[role="radio"]')).find((b) => b.textContent?.startsWith(label)) as HTMLButtonElement;

const payer = { name: 'Transportes Cartão Ltda', email: 'cartao@frota.test', taxId: '52998224725', cellphone: '11999990000' };
const charge = (over: Partial<PixCharge>): PixCharge =>
  ({ id: 'c1', invoiceId: 'i1', method: 'PIX', amountCents: 4990, status: 'PENDING', brCode: '000201', qrCodeImage: 'data:,',
    devMode: false, expiresAt: null, invoiceStatus: 'OPEN', boletoLine: '', boletoUrl: '', cardBrand: '', cardLast4: '', ...over }) as PixCharge;

let cardView: CardView;
let cardPays: CardPayInput[] = [];
let cardResult: () => Promise<PixCharge>;
let polled: PixCharge | null = null;
const api: PixApi = {
  invoicePix: async () => charge({}),
  invoiceBoleto: async () => charge({ id: 'b1', method: 'BOLETO' }),
  invoiceCard: async (_id, input) => {
    cardPays.push(input);
    return cardResult();
  },
  card: async () => cardView,
  charge: async (id) => polled ?? charge({ id }),
  simulateCharge: async () => charge({ status: 'PAID' }),
};

async function render(options: { initialMethod?: 'CARD'; autoDebit?: boolean } = {}) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ToastProvider>
          <PixPaymentModal
            invoice={{ id: 'i1', description: 'Plano Frota — outubro/2026', amountCents: 4990 }}
            api={api}
            audience="customer"
            onClose={() => undefined}
            onPaid={() => undefined}
            {...options}
          />
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  form.created = [];
  form.tokenized = [];
  form.fail = '';
  cardPays = [];
  polled = null;
});

describe('pagar no cartão', () => {
  it('cartão novo: o formulário da AbacatePay, o token e a autorização do débito automático', async () => {
    cardView = { enabled: true, publicKey: 'pk_teste', saved: null, payer };
    cardResult = async () => charge({ id: 'k1', method: 'CARD', status: 'APPROVED', invoiceStatus: 'PAID', cardBrand: 'VISA', cardLast4: '4242' });
    await render();
    await act(async () => radio('Cartão').click());
    await flush();
    expect(form.created).toEqual([{ publicKey: 'pk_teste', amount: 4990, locale: 'pt', icons: true }]);
    const consent = document.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    expect(consent.checked).toBe(false);
    expect(text()).toContain('Débito automático');
    await act(async () => consent.click());
    await act(async () => button('Pagar R$ 49,90').click());
    await flush();
    expect(form.tokenized).toEqual([payer]);
    expect(cardPays).toEqual([{ token: 'ctok_1', save: true, autoDebit: true }]);
    expect(text()).toContain('Pagamento recebido');
  });

  it('cartão salvo: paga com ele, sem formulário', async () => {
    cardView = {
      enabled: true, publicKey: 'pk_teste', payer,
      saved: { brand: 'VISA', last4: '4242', autoDebit: true, consentAt: null, failedAt: null, lastError: '', createdAt: '' },
    };
    cardResult = async () => charge({ id: 'k2', method: 'CARD', status: 'APPROVED', invoiceStatus: 'PAID' });
    await render();
    await act(async () => radio('Cartão').click());
    await flush();
    expect(text()).toContain('VISA final 4242');
    expect(form.created).toHaveLength(0);
    await act(async () => button('Pagar R$ 49,90').click());
    await flush();
    expect(cardPays).toEqual([{ useSaved: true }]);
  });

  it('recusado: a mensagem; sem resposta do banco: espera a confirmação', async () => {
    cardView = { enabled: true, publicKey: 'pk_teste', saved: null, payer };
    cardResult = async () => {
      throw new Error('o cartão foi recusado: Não foi possível processar o pagamento com este cartão');
    };
    await render();
    await act(async () => radio('Cartão').click());
    await flush();
    await act(async () => button('Pagar R$ 49,90').click());
    await flush();
    expect(text()).toContain('o cartão foi recusado');

    // O formulário reclama do cartão incompleto antes de ir ao servidor.
    form.fail = 'CARD_INCOMPLETE';
    await act(async () => button('Pagar R$ 49,90').click());
    await flush();
    expect(text()).toContain('Preencha todos os dados do cartão.');
    expect(cardPays).toHaveLength(1);

    form.fail = '';
    cardResult = async () => charge({ id: 'k3', method: 'CARD', status: 'CREATING' });
    polled = charge({ id: 'k3', method: 'CARD', status: 'CREATING' });
    await act(async () => button('Pagar R$ 49,90').click());
    await flush();
    expect(text()).toContain('Confirmando com o banco');
    expect(button('Pagar R$ 49,90')).toBeUndefined();
    polled = charge({ id: 'k3', method: 'CARD', status: 'APPROVED', invoiceStatus: 'PAID' });
    await act(async () => new Promise((resolve) => setTimeout(resolve, 4200)));
    await flush();
    expect(text()).toContain('Pagamento recebido');
  }, 10_000);

  it('vindo da contratação no cartão: abre no cartão, com a autorização marcada', async () => {
    cardView = { enabled: true, publicKey: 'pk_teste', saved: null, payer };
    await render({ initialMethod: 'CARD', autoDebit: true });
    expect(radio('Cartão').getAttribute('aria-checked')).toBe('true');
    expect(document.querySelector<HTMLInputElement>('input[type="checkbox"]')!.checked).toBe(true);
  });

  it('sem os dados do pagador, explica; com o cartão desligado, nem aparece', async () => {
    cardView = { enabled: true, publicKey: 'pk_teste', saved: null, payer: null };
    await render();
    await act(async () => radio('Cartão').click());
    await flush();
    expect(text()).toContain('o cadastro da conta precisa do CPF ou CNPJ, do e-mail e do celular');
    expect(form.created).toHaveLength(0);

    document.body.innerHTML = '';
    cardView = { enabled: false, publicKey: '', saved: null, payer: null };
    await render({ initialMethod: 'CARD' });
    expect(radio('Cartão')).toBeUndefined();
    // Pediu cartão desligado: volta ao Pix.
    expect(radio('Pix').getAttribute('aria-checked')).toBe('true');
  });
});

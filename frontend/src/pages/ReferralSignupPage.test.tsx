// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { currentReferral } from '@/services/referral';
import { OFFER, offerWith, withQuery } from '@/test/checkoutOffer';
import type { WaitlistInput } from '@/types';

const sent: WaitlistInput[] = [];
const looked: string[] = [];
const offerRefs: (string | undefined)[] = [];
let offer = OFFER;
vi.mock('@/api/resources', () => ({
  publicApi: {
    joinLaunch: async (input: WaitlistInput) => {
      sent.push(input);
      return { status: 'ok' };
    },
    affiliate: async (code: string) => {
      looked.push(code);
      if (code !== 'joao-moto') throw new Error('link de indicação não encontrado');
      return { name: 'João Motoca', handle: 'joao.moto', code };
    },
  },
  checkoutApi: {
    offer: async (ref?: string) => {
      offerRefs.push(ref);
      return offer;
    },
  },
}));

import { ReferralSignupPage } from './ReferralSignupPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

function type(input: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
}

async function render(path: string) {
  window.scrollTo = () => undefined;
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      withQuery(
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="/indicacao/:codigo" element={<ReferralSignupPage />} />
          </Routes>
        </MemoryRouter>,
      ),
    ),
  );
  await flush();
  return host;
}

beforeEach(() => {
  offer = OFFER;
});
afterEach(() => {
  document.body.innerHTML = '';
  sent.length = 0;
  looked.length = 0;
  offerRefs.length = 0;
  localStorage.clear();
});

describe('ReferralSignupPage', () => {
  it('mostra quem indica, guarda o link e cadastra com ele', async () => {
    const host = await render('/indicacao/Joao-Moto');
    expect(looked).toEqual(['joao-moto']);
    expect(currentReferral()).toBe('joao-moto');
    expect(host.textContent).toContain('Indicado por @joao.moto');
    // O destaque é o preço do plano no site, do servidor (pedido com o link);
    // o do Insanos MC é só com a central.
    expect(offerRefs).toContain('joao-moto');
    expect(host.textContent).toContain('Preço promocional');
    expect(host.textContent).toContain('R$34,90/mês');
    expect(host.textContent).toContain('Rastreador incluso, que fica com você: só o frete à parte');
    expect(host.textContent).not.toMatch(/sem juros|parcel|à vista/);
    expect(host.textContent).not.toContain('Insanos');
    // O botão leva à contratação (o link fica guardado no aparelho e vai junto).
    const cta = Array.from(host.querySelectorAll('a')).find((a) => a.textContent === 'Contratar agora');
    expect(cta?.getAttribute('href')).toBe('/contratar');

    // Quem prefere conversar deixa o contato (sem a opção do Insanos MC).
    await act(async () =>
      (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'Prefiro que a Farbo fale comigo') as HTMLButtonElement).click(),
    );
    expect(host.textContent).not.toContain('Sou integrante do Insanos MC');

    const inputs = () => Array.from(host.querySelectorAll('input')) as HTMLInputElement[];
    const field = (t: string) => inputs().find((i) => i.type === t && i.name !== 'website') as HTMLInputElement;
    await act(async () => {
      type(field('text'), 'Bia Lima');
      type(field('tel'), '11944443333');
      type(inputs().find((i) => i.autocomplete === 'address-level2')!, 'Campinas - SP');
      type(field('email'), 'bia@cliente.test');
    });
    const consent = Array.from(host.querySelectorAll('label'))
      .find((l) => l.textContent?.includes('Aceito que a Farbo'))!
      .querySelector('input') as HTMLInputElement;
    await act(async () => consent.click());
    await act(async () => (host.querySelector('button[type=submit]') as HTMLButtonElement).click());
    await flush();
    expect(sent).toEqual([
      {
        name: 'Bia Lima', email: 'bia@cliente.test', phone: '(11) 9-4444-3333', consent: true, website: '',
        event: '', city: 'Campinas - SP', ref: 'joao-moto',
      },
    ]);
    expect(host.textContent).toContain('Pronto, Bia!');
    // Cada um se cadastra no próprio celular: sem "cadastrar outra pessoa".
    expect(host.textContent).not.toContain('Cadastrar outra pessoa');
  });

  it('afiliado com tabela: a partir da menor mensalidade, com a moto, o carro e o caminhão', async () => {
    offer = offerWith({ planByKind: { motorcycleCents: 3990, carCents: 4990, truckCents: 5990 } });
    const host = await render('/indicacao/joao-moto');
    const text = (host.textContent ?? '').replace(/\u00a0/g, ' ');
    expect(text).toContain('A partir de');
    expect(text).toContain('R$39,90/mês');
    expect(text).toContain('Moto R$ 39,90 · Carro R$ 49,90 · Caminhão R$ 59,90');
    expect(text).toContain('o normal é R$ 69,90');
  });

  it('link desconhecido: a mesma tela, sem o selo', async () => {
    const host = await render('/indicacao/ninguem');
    expect(host.textContent).toContain('Contrate o seu rastreador');
    expect(host.textContent).not.toContain('Indicado por');
  });
});

// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { eventSlug, eventUrl } from '@/config/landing';
import { OFFER, offerWith, withQuery } from '@/test/checkoutOffer';
import type { WaitlistInput } from '@/types';

const sent: WaitlistInput[] = [];
let offer = OFFER;
vi.mock('@/api/resources', () => ({
  publicApi: {
    joinLaunch: async (input: WaitlistInput) => {
      sent.push(input);
      return { status: 'ok' };
    },
  },
  checkoutApi: { offer: async () => offer },
}));

import { EventSignupPage } from './EventSignupPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

/** Digita como o usuário (o React só vê o valor pelo evento de input). */
function type(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
  setter?.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
}

beforeEach(() => {
  offer = OFFER;
});
afterEach(() => {
  document.body.innerHTML = '';
  sent.length = 0;
});

async function render(path: string) {
  window.scrollTo = () => undefined;
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      withQuery(
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="/evento/:evento" element={<EventSignupPage />} />
          </Routes>
        </MemoryRouter>,
      ),
    ),
  );
  await flush();
  return host;
}

const text = (host: HTMLElement) => (host.textContent ?? '').replace(/\u00a0/g, ' ');
const button = (host: HTMLElement, label: string) =>
  Array.from(host.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;

describe('nome do evento no link', () => {
  it('como o servidor faz', () => {
    expect(eventSlug('Encontro Insanos MC — Out/26')).toBe('encontro-insanos-mc-out-26');
    expect(eventSlug('  Feira de Motos São Paulo  ')).toBe('feira-de-motos-sao-paulo');
    expect(eventSlug('Ação & Reação!!')).toBe('acao-reacao');
    expect(eventSlug('---')).toBe('');
    expect(eventSlug('evento muito longo '.repeat(10)).length).toBeLessThanOrEqual(60);
    expect(eventUrl('Encontro Insanos MC')).toBe('https://farborastreadores.com.br/evento/encontro-insanos-mc');
    expect(eventUrl('')).toBe('https://farborastreadores.com.br/evento');
    // O do público geral leva a marca; o do Insanos MC é o link de sempre.
    expect(eventUrl('Feira de Motos', 'geral')).toBe('https://farborastreadores.com.br/evento/feira-de-motos?publico=geral');
    expect(eventUrl('Feira de Motos', 'insanos')).toBe('https://farborastreadores.com.br/evento/feira-de-motos');
  });
});

describe('EventSignupPage', () => {
  it('o QR Code do Insanos MC: o preço deles (do servidor) e o botão para contratar, com o evento', async () => {
    const host = await render('/evento/Encontro-Insanos-MC');
    expect(text(host)).toContain('Contrate o seu rastreador');
    expect(text(host)).toContain('Preço exclusivo Insanos MC');
    expect(text(host)).toContain('R$27,90/mês');
    expect(text(host)).toContain('enquanto a assinatura durar · o normal é R$ 39,90');
    expect(text(host)).toContain('Rastreador: R$ 150 R$ 120');
    expect(text(host)).toContain('ou 10x de R$ 12,00 sem juros no Pix, direto na mensalidade');
    expect(text(host)).toContain('Não é do Insanos MC? A mensalidade é R$ 34,90.');
    // Sem o prazo nem o limite da promoção de pré-lançamento.
    expect(text(host)).not.toMatch(/primeiros|pré-lançamento/);
    const cta = Array.from(host.querySelectorAll('a')).find((a) => a.textContent === 'Contratar agora');
    expect(cta?.getAttribute('href')).toBe('/contratar?plano=insanos&evento=encontro-insanos-mc');
  });

  it('quem prefere conversar deixa o contato, com o evento, e a equipe cadastra a próxima pessoa', async () => {
    const host = await render('/evento/Encontro-Insanos-MC');
    await act(async () => button(host, 'Prefiro que a Farbo fale comigo').click());
    const inputs = () => Array.from(host.querySelectorAll('input')) as HTMLInputElement[];
    const field = (type: string) => inputs().find((i) => i.type === type && i.name !== 'website') as HTMLInputElement;
    const city = () => inputs().find((i) => i.autocomplete === 'address-level2') as HTMLInputElement;
    const submit = () => host.querySelector('button[type=submit]') as HTMLButtonElement;
    const checkbox = (label: string) =>
      Array.from(host.querySelectorAll('label'))
        .find((l) => l.textContent?.includes(label))!
        .querySelector('input') as HTMLInputElement;
    expect(submit().disabled).toBe(true);

    await act(async () => {
      type(field('text'), 'Ana Souza');
      type(field('tel'), '11987654321');
      type(field('email'), 'ana@cliente.test');
    });
    expect(field('tel').value).toBe('(11) 9-8765-4321');
    // É do Insanos MC: marca (a conta nasce no plano deles).
    await act(async () => checkbox('Sou integrante do Insanos MC').click());
    await act(async () => checkbox('Aceito que a Farbo').click());
    expect(submit().disabled).toBe(true); // falta a cidade
    await act(async () => type(city(), 'São Paulo - SP'));
    expect(submit().disabled).toBe(false);

    await act(async () => submit().click());
    await flush();
    expect(sent).toEqual([
      {
        name: 'Ana Souza', email: 'ana@cliente.test', phone: '(11) 9-8765-4321', consent: true, website: '',
        event: 'encontro-insanos-mc', city: 'São Paulo - SP', insanos: true,
      },
    ]);
    expect(text(host)).toContain('Pronto, Ana!');
    expect(text(host)).toContain('ana@cliente.test');

    // A próxima pessoa: o formulário volta vazio.
    await act(async () => button(host, 'Cadastrar outra pessoa').click());
    expect(field('text').value).toBe('');
    expect(city().value).toBe('');
    expect(checkbox('Aceito que a Farbo').checked).toBe(false);
    expect(checkbox('Sou integrante do Insanos MC').checked).toBe(false);
    expect(submit().disabled).toBe(true);
  });

  it('o QR Code do público geral destaca o plano normal, sem o Insanos', async () => {
    const host = await render('/evento/feira-de-motos?publico=geral');
    expect(text(host)).toContain('Preço promocional');
    expect(text(host)).toContain('R$34,90/mês');
    expect(text(host)).not.toContain('Insanos');
    const cta = Array.from(host.querySelectorAll('a')).find((a) => a.textContent === 'Contratar agora');
    expect(cta?.getAttribute('href')).toBe('/contratar?evento=feira-de-motos');
  });

  it('fechada na central: os preços normais e o contato direto', async () => {
    offer = offerWith({ enabled: false });
    const host = await render('/evento/feira-de-motos?publico=geral');
    expect(text(host)).toContain('R$69,90/mês');
    expect(text(host)).not.toContain('Contratar agora');
    expect(host.querySelector('button[type=submit]')).not.toBeNull();
  });
});

// @vitest-environment jsdom
import { act } from 'react';
import type { ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

let offer = OFFER;
vi.mock('@/api/resources', () => ({
  checkoutApi: { offer: async () => offer },
  publicApi: { representatives: async () => [], installers: async () => [], createLead: async () => ({}) },
}));

import { howItWorksSteps } from '@/components/landing/HowItWorksSection';
import { WHATSAPP_GREETING, whatsappUrl } from '@/config/contact';
import { ANATEL_HOMOLOGATION, ANATEL_LOOKUP_URL, CHECKOUT_PATH } from '@/config/landing';
import { LandingPage } from '@/pages/LandingPage';
import { OFFER, offerWith, withQuery } from '@/test/checkoutOffer';

import { CineHero } from './CineHero';
import { DeviceReveal } from './DeviceReveal';
import { Finale } from './Finale';
import { Journey } from './Journey';
import { LockStory } from './LockStory';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

/** Monta e espera a oferta chegar do servidor (de mentira). */
async function render(node: ReactNode) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () => createRoot(host).render(withQuery(<MemoryRouter>{node}</MemoryRouter>)));
  await act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
  return host;
}

const text = (host: HTMLElement) => host.textContent?.replace(/\s+/g, ' ') ?? '';
const linkTo = (host: HTMLElement, href: string) =>
  Array.from(host.querySelectorAll('a')).filter((a) => a.getAttribute('href') === href);

beforeEach(() => {
  offer = OFFER;
});
afterEach(() => {
  document.body.innerHTML = '';
});

describe('a abertura (CineHero)', () => {
  it('vende o bloqueio pelo celular, com o preço do servidor, o desconto, o comodato e as condições', async () => {
    const host = await render(<CineHero onOpenModal={() => {}} />);
    expect(host.querySelector('h1')?.textContent).toBe('Bloqueie seu veículo pelo celular.');
    const all = text(host);
    // OFFER: R$ 34,90 no site, R$ 69,90 o normal (50% OFF), comodato com instalação de R$ 150.
    expect(all).toContain('50% OFF');
    expect(all).toContain('De R$ 69,90');
    expect(all).toContain('34,90/mês');
    expect(all).toContain('rastreador incluso: você só paga o frete');
    expect(all).toContain('Ou em comodato: o rastreador emprestado, instalado pelos nossos técnicos por R$ 150');
    expect(all).toContain('o bloqueio é feito com o veículo parado');
    const cta = linkTo(host, CHECKOUT_PATH);
    expect(cta.map((a) => a.textContent?.trim())).toEqual(['Quero proteger meu veículo']);
    expect(cta[0].dataset.analytics).toBe('hero-contratar');
    expect(linkTo(host, '#planos')).toHaveLength(1);
  });

  it('com a contratação fechada na central, o botão abre o pré-cadastro (preço normal, sem riscado)', async () => {
    offer = offerWith({ enabled: false });
    const opened: string[] = [];
    const host = await render(<CineHero onOpenModal={(plan) => opened.push(plan ?? '')} />);
    expect(linkTo(host, CHECKOUT_PATH)).toHaveLength(0);
    expect(text(host)).not.toContain('De R$');
    expect(text(host)).toContain('69,90/mês');
    const button = Array.from(host.querySelectorAll('button')).find((b) => b.textContent?.includes('Quero proteger'));
    await act(async () => button!.click());
    expect(opened).toEqual(['Quero meu rastreador']);
  });

  it('enquanto a oferta carrega, o preço espera no lugar dele', async () => {
    offer = new Promise(() => {}) as never;
    const host = await render(<CineHero onOpenModal={() => {}} />);
    expect(host.querySelector('[aria-busy="true"]')?.textContent).toContain('R$ —');
  });
});

describe('o bloqueio acontecendo (LockStory)', () => {
  it('conta os quatro tempos, com o alerta de verdade e as condições do bloqueio', async () => {
    const host = await render(<LockStory />);
    const section = host.querySelector('section#bloqueio') as HTMLElement;
    expect(section.dataset.scene).toBe('pin');
    expect(section.dataset.steps).toBe('4');
    expect(Array.from(host.querySelectorAll('ol > li strong')).map((s) => s.textContent)).toEqual([
      'Tudo parado na garagem.',
      'Ignição ligada de madrugada?',
      'Um toque: bloquear.',
      'Parou. Não liga mais.',
    ]);
    const all = text(host);
    expect(all).toContain('Por segurança, ele age assim que o veículo para');
    expect(all).toContain('O bloqueio exige o relé instalado e é feito com o veículo parado; o desbloqueio é pelo app');
    expect(all).toContain('Desbloquear motor');
    expect(all).toContain('Compartilhar localização ao vivo');
  });
});

describe('o rastreador (DeviceReveal)', () => {
  it('a homologação da Anatel (com o número e o link para conferir), o chip, o relé e o rastreador que fica com você', async () => {
    const host = await render(<DeviceReveal />);
    const all = text(host);
    expect(all).toContain(`Certificado nº ${ANATEL_HOMOLOGATION}`);
    expect(linkTo(host, ANATEL_LOOKUP_URL)[0]?.getAttribute('rel')).toBe('noopener noreferrer');
    expect(all).toContain('Chip M2M incluso');
    expect(all).toContain('Relé de bloqueio no kit');
    expect(all).toContain('Fica com você');
    expect(all).toContain(`O ${OFFER.equipmentName} sai configurado`);
  });

  it('pelo link só de comodato: o comodato no lugar do "fica com você"', async () => {
    offer = offerWith({ equipmentModes: 'loan' });
    const host = await render(<DeviceReveal />);
    expect(text(host)).toContain('Em comodato');
    expect(text(host)).not.toContain('Fica com você');
  });
});

describe('o passo a passo (Journey)', () => {
  it('os mesmos passos da seção clássica, com a contratação pelo site aberta', async () => {
    const host = await render(<Journey />);
    const titles = Array.from(host.querySelectorAll('h3')).map((h) => h.textContent?.trim());
    expect(titles).toEqual(howItWorksSteps(true).map((s) => s.title));
    expect(titles[0]).toBe('Contrate pelo site');
    expect((host.querySelector('section') as HTMLElement).dataset.steps).toBe(String(titles.length));
  });

  it('com a contratação fechada: o pré-cadastro e o pedido no painel', async () => {
    offer = offerWith({ enabled: false });
    const host = await render(<Journey />);
    const titles = Array.from(host.querySelectorAll('h3')).map((h) => h.textContent?.trim());
    expect(titles).toEqual(howItWorksSteps(false).map((s) => s.title));
    expect(titles).toContain('Peça o rastreador pelo painel');
  });
});

describe('o fim (Finale)', () => {
  it('leva à contratação, com o WhatsApp ao lado', async () => {
    const host = await render(<Finale onOpenModal={() => {}} />);
    expect(linkTo(host, CHECKOUT_PATH)).toHaveLength(1);
    const whatsapp = linkTo(host, whatsappUrl(WHATSAPP_GREETING))[0];
    expect(whatsapp?.target).toBe('_blank');
  });

  it('fechada na central, abre o pré-cadastro', async () => {
    offer = offerWith({ enabled: false });
    const opened: string[] = [];
    const host = await render(<Finale onOpenModal={(plan) => opened.push(plan ?? '')} />);
    expect(linkTo(host, CHECKOUT_PATH)).toHaveLength(0);
    await act(async () => host.querySelector('button')!.click());
    expect(opened).toEqual(['Quero meu rastreador']);
  });
});

describe('a landing cinematográfica', () => {
  it('as cenas e as seções de sempre, na ordem, com as âncoras do menu', async () => {
    const host = await render(<LandingPage />);
    const ids = Array.from(host.querySelectorAll('main section[id]')).map((s) => s.id);
    expect(ids).toEqual([
      'beneficios',
      'bloqueio',
      'recursos',
      'rastreador',
      'como-funciona',
      'planos',
      'instalacao',
      'contratar',
      'contato',
    ]);
    // A frota (#frotas, do menu) continua dentro dos planos.
    expect(host.querySelector('#planos #frotas')).not.toBeNull();
    // Sem animação (como nos testes e com "reduzir movimento"), nada fica preso nem escondido.
    expect(host.firstElementChild?.hasAttribute('data-motion')).toBe(false);
  });
});

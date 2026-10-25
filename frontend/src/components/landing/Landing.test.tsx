// @vitest-environment jsdom
import { act } from 'react';
import type { ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

let offer = OFFER;
vi.mock('@/api/resources', () => ({ checkoutApi: { offer: async () => offer } }));

import { WHATSAPP_GREETING, WHATSAPP_NUMBER, whatsappDisplay, whatsappUrl } from '@/config/contact';
import { CHECKOUT_PATH, checkoutPath, priceLabel, priceParts } from '@/config/landing';
import { OFFER, offerWith, withQuery } from '@/test/checkoutOffer';

import { CheckoutSection } from './CheckoutSection';
import { Footer } from './Footer';
import { HeroSection } from './HeroSection';
import { Navbar } from './Navbar';
import { FLEET_FEATURES, FLEET_FEATURES_MESSAGE } from './FleetFeatures';
import { PricingSection, fleetMessage } from './PricingSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

/** Monta e espera a oferta chegar do servidor (de mentira). */
async function render(node: ReactNode) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () => createRoot(host).render(withQuery(<MemoryRouter>{node}</MemoryRouter>)));
  await act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
  return host;
}

const links = (host: HTMLElement, text: string) =>
  Array.from(host.querySelectorAll('a')).filter((a) => a.textContent?.trim() === text);

beforeEach(() => {
  offer = OFFER;
});
afterEach(() => {
  document.body.innerHTML = '';
});

describe('priceParts', () => {
  it('separa reais e centavos para o preço grande', () => {
    expect(priceParts(2990)).toEqual({ reais: '29', cents: ',90' });
    expect(priceParts(12000)).toEqual({ reais: '120', cents: ',00' });
    expect(priceParts(6905)).toEqual({ reais: '69', cents: ',05' });
  });

  it('o preço como as páginas escrevem e o link do funil', () => {
    expect(priceLabel(12000)).toBe('R$ 120');
    expect(priceLabel(3490)).toBe('R$ 34,90');
    expect(checkoutPath()).toBe(CHECKOUT_PATH);
    expect(checkoutPath({ insanos: true, event: 'encontro-sp' })).toBe(`${CHECKOUT_PATH}?plano=insanos&evento=encontro-sp`);
  });
});

// Os preços vêm da oferta do servidor; com a contratação aberta, os botões levam ao funil.
describe('landing com a contratação pelo site', () => {
  it('o hero vende o bloqueio pelo celular, com o preço do servidor, o desconto e as condições', async () => {
    const host = await render(<HeroSection onOpenModal={() => {}} />);
    const text = host.textContent ?? '';
    expect(host.querySelector('h1')?.textContent).toBe('Bloqueie seu veículo pelo celular.');
    for (const part of ['bloqueio e desbloqueio remoto do motor', 'melhor preço do mercado', '50% OFF', 'De R$ 69,90',
      'R$34,90/mês', 'rastreador R$ 120 ou 10x de R$ 12,00 sem juros, com o relé de bloqueio', 'Relé de bloqueio incluso',
      'Ou em comodato: sem comprar o rastreador, instalado pelos nossos técnicos por R$ 150 (+ R$ 3/mês).',
      'Homologado pela Anatel', 'O relé vem no kit', 'o bloqueio é feito com o veículo parado']) {
      expect(text).toContain(part);
    }
    const cta = links(host, 'Quero proteger meu veículo');
    expect(cta).toHaveLength(1);
    expect(cta[0].getAttribute('href')).toBe(CHECKOUT_PATH);
    expect(links(host, 'Ver planos')[0]?.getAttribute('href')).toBe('#planos');
  });

  it('o Contato do menu abre o WhatsApp', async () => {
    const host = await render(<Navbar onOpenModal={() => {}} />);
    const contact = links(host, 'Contato')[0];
    if (!WHATSAPP_NUMBER) {
      expect(contact?.getAttribute('href')).toBe('#contato');
      return;
    }
    expect(contact?.getAttribute('href')).toBe(whatsappUrl(WHATSAPP_GREETING));
    expect(contact?.getAttribute('target')).toBe('_blank');
  });

  it('o CTA do menu (computador e celular) é "Contratar"', async () => {
    const host = await render(<Navbar onOpenModal={() => {}} />);
    const ctas = links(host, 'Contratar');
    expect(ctas).toHaveLength(2);
    for (const cta of ctas) expect(cta.getAttribute('href')).toBe(CHECKOUT_PATH);
    expect(links(host, 'Pré-lançamento')).toHaveLength(0);
  });

  it('os planos mostram o preço do site, sem prazo, e levam à contratação', async () => {
    const host = await render(<PricingSection onOpenModal={() => {}} />);
    const text = host.textContent ?? '';
    for (const part of ['Contrate pelo site', 'R$ 34,90/mês e R$ 120 no rastreador', 'De R$ 69,90', 'De R$ 39,90', 'De R$ 150',
      'enquanto a assinatura durar', 'Insanos MC pagam R$ 27,90/mês', 'à vista, ou 10x de R$ 12,00']) {
      expect(text).toContain(part);
    }
    // Sem o prazo nem o limite da promoção de pré-lançamento.
    expect(text).not.toContain('12 primeiros meses');
    expect(text).not.toContain('500 primeiros');
    expect(links(host, 'Contratar agora').map((a) => a.getAttribute('href'))).toEqual([CHECKOUT_PATH, CHECKOUT_PATH]);
    expect(links(host, 'Contratar com o preço do Insanos')[0]?.getAttribute('href')).toBe(checkoutPath({ insanos: true }));
    // O rastreador: quem instala e o comodato.
    expect(text).toContain('Instale com o seu instalador ou com os nossos, por R$ 150.');
    expect(text).toContain('Bloqueio remoto (relé incluso)');
    expect(text).not.toContain('(opcional)');
    expect(text).toContain(
      'Ou em comodato, sem comprar: instalação R$ 150 pelos nossos instaladores e + R$ 3/mês de garantia de troca do rastreador (desinstalação R$ 85 ao encerrar).',
    );
  });

  it('outros valores na central: a página acompanha, sem nada escrito no código', async () => {
    offer = offerWith({ planCents: 2990, insanosCents: 2490, equipmentCents: 9900, maxInstallments: 1, loan: null, install: null });
    const host = await render(<PricingSection onOpenModal={() => {}} />);
    const text = host.textContent ?? '';
    expect(text).toContain('R$ 29,90/mês e R$ 99 no rastreador. Pague com Pix');
    expect(text).toContain('Insanos MC pagam R$ 24,90/mês');
    expect(text).toContain('à vista, no Pix');
    expect(text).not.toContain('comodato');
    expect(text).toContain('Instale com o instalador da sua preferência.');
  });

  it('enquanto a oferta carrega, os preços esperam no lugar deles', async () => {
    offer = new Promise(() => undefined) as unknown as typeof OFFER;
    const host = await render(<PricingSection onOpenModal={() => {}} />);
    expect(host.querySelectorAll('[aria-busy="true"]').length).toBeGreaterThan(0);
    expect(host.textContent).not.toMatch(/R\$ \d/);
  });

  it('a chamada para contratar, com o contato como alternativa', async () => {
    const opened: (string | undefined)[] = [];
    const host = await render(<CheckoutSection onOpenModal={(plan) => opened.push(plan)} />);
    expect(host.querySelector('section#contratar')).not.toBeNull();
    expect(links(host, 'Contratar agora')[0]?.getAttribute('href')).toBe(CHECKOUT_PATH);
    expect(host.textContent).toContain('R$ 150 R$ 120');
    expect(host.textContent).toContain('R$ 69,90 R$ 34,90');
    expect(host.textContent).toContain('Em comodato: instalação R$ 150 pelos nossos instaladores');
    const contact = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'Prefiro que a Farbo fale comigo');
    act(() => contact!.click());
    expect(opened).toEqual([undefined]);
  });
});

// Fechada na central: os botões abrem o pré-cadastro e os preços são os normais.
describe('landing com a contratação fechada', () => {
  beforeEach(() => {
    offer = offerWith({ enabled: false });
  });

  it('o hero, o menu e os planos abrem o pré-cadastro', async () => {
    const opened: (string | undefined)[] = [];
    const hero = await render(<HeroSection onOpenModal={(plan) => opened.push(plan)} />);
    expect(links(hero, 'Quero proteger meu veículo')).toHaveLength(0);
    // Fechada: o preço normal, sem o desconto.
    expect(hero.textContent).toContain('R$69,90/mês');
    expect(hero.textContent).not.toContain('OFF');
    const nav = await render(<Navbar onOpenModal={(plan) => opened.push(plan)} />);
    expect(links(nav, 'Contratar')).toHaveLength(0);
    const pricing = await render(<PricingSection onOpenModal={(plan) => opened.push(plan)} />);
    const text = pricing.textContent ?? '';
    expect(text).not.toContain('Contrate pelo site');
    expect(text).not.toContain('De R$');
    expect(text).toContain('R$69,90/mês por veículo');
    const want = Array.from(pricing.querySelectorAll('button')).find((b) => b.textContent === 'Quero esse plano') as HTMLButtonElement;
    act(() => want.click());
    expect(opened).toEqual(['Plano Mensal']);
  });
});

describe('os cartões dos planos', () => {
  it('o equipamento mostra o selo Anatel no lugar de "Desbloqueado"; o Insanos, sem "Parceria oficial"', async () => {
    const text = (await render(<PricingSection onOpenModal={() => {}} />)).textContent ?? '';
    expect(text).toContain('04895-25-16219');
    expect(text).not.toContain('Desbloqueado');
    expect(text).not.toMatch(/parceria oficial/i);
    expect(text).toContain('MOTOCLUBE INSANOS');
  });

  it('os planos usam as imagens novas: o escudo do Insanos e a foto do J16', async () => {
    const host = await render(<PricingSection onOpenModal={() => {}} />);
    const sources = Array.from(host.querySelectorAll('img')).map((img) => img.getAttribute('src'));
    expect(sources).toEqual(expect.arrayContaining(['/assets/insanos-escudo.webp', '/assets/rastreador-j16-farbo.webp']));
    expect(sources).toContain('/assets/anatel-logo.png');
    expect(sources).not.toContain('/assets/insanos-skull.png');
    expect(sources).not.toContain('/assets/tracker-gt06.png');
  });
});

describe('frotas', () => {
  it('no lugar do rastreador, o preço especial para frotas, com o WhatsApp e a mensagem pronta', async () => {
    const host = await render(<PricingSection onOpenModal={() => {}} />);
    const cards = Array.from(host.querySelectorAll('h3[class*="cardTitle"]')).map((h) => h.textContent);
    // Os três planos lado a lado; o rastreador, largo, embaixo deles.
    expect(cards).toEqual(['PLANO MENSAL', 'FROTAS', 'EQUIPAMENTO']);
    const text = host.textContent ?? '';
    expect(text).toContain('PARA EMPRESAS');
    expect(text).toContain('Preço especial');
    const [cta] = links(host, 'Pedir orçamento no WhatsApp');
    expect(cta?.getAttribute('href')).toBe(whatsappUrl(fleetMessage()));
    expect(cta?.getAttribute('target')).toBe('_blank');
    expect(cta?.dataset.analytics).toBe('plano-frotas');

    // Escolhido o tamanho da frota, ele vai na mensagem; de novo, sai.
    const chip = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '6 a 10') as HTMLButtonElement;
    act(() => chip.click());
    expect(chip.getAttribute('aria-pressed')).toBe('true');
    expect(links(host, 'Pedir orçamento no WhatsApp')[0].getAttribute('href')).toBe(whatsappUrl(fleetMessage('6 a 10')));
    act(() => chip.click());
    expect(links(host, 'Pedir orçamento no WhatsApp')[0].getAttribute('href')).toBe(whatsappUrl(fleetMessage()));
  });

  it('embaixo dos planos, o que a frota ganha, com o atalho do cartão e o orçamento', async () => {
    const host = await render(<PricingSection onOpenModal={() => {}} />);
    const block = host.querySelector('#frotas');
    expect(block).not.toBeNull();
    const titles = Array.from(block!.querySelectorAll('h3')).map((h) => h.textContent);
    expect(titles).toHaveLength(FLEET_FEATURES.length);
    expect(titles).toEqual(expect.arrayContaining(['Relatórios da frota', 'Manutenção preventiva', 'Equipe com acesso sob controle']));
    expect(links(host, 'Ver tudo o que a frota ganha ↓')[0].getAttribute('href')).toBe('#frotas');
    const [cta] = links(host, 'Quero um orçamento para a minha frota');
    expect(cta?.getAttribute('href')).toBe(whatsappUrl(FLEET_FEATURES_MESSAGE));
    expect(cta?.dataset.analytics).toBe('frotas-funcionalidades');
  });

  it('a mensagem do WhatsApp', () => {
    expect(fleetMessage()).toBe(
      'Olá! Vim pelo site da Farbo Rastreadores e quero o preço especial para frotas. Pode me passar um orçamento?',
    );
    expect(fleetMessage('Mais de 30')).toBe(
      'Olá! Vim pelo site da Farbo Rastreadores e quero o preço especial para frotas. Tenho mais de 30 veículos para rastrear. Pode me passar um orçamento?',
    );
  });
});

describe('rodapé', () => {
  it('a coluna de parceiros virou Regulamentação, com a homologação Anatel', async () => {
    const text = (await render(<Footer />)).textContent ?? '';
    expect(text).toContain('Regulamentação');
    expect(text).toContain('Anatel - 04895-25-16219');
    expect(text).not.toContain('Parceiros');
  });

  it('com o WhatsApp ativo, o número aparece e abre a conversa', async () => {
    const host = await render(<Footer />);
    const link = host.querySelector('a[href^="https://wa.me/"]') as HTMLAnchorElement | null;
    if (!WHATSAPP_NUMBER) {
      expect(link).toBeNull();
      return;
    }
    expect(link?.textContent).toBe(whatsappDisplay());
    expect(link?.getAttribute('href')).toBe(whatsappUrl(WHATSAPP_GREETING));
    expect(link?.getAttribute('target')).toBe('_blank');
  });

  it('o ícone do Instagram leva ao perfil @farborastreadores, em nova aba', async () => {
    const host = await render(<Footer />);
    const link = host.querySelector('a[href*="instagram.com"]') as HTMLAnchorElement;
    expect(link.getAttribute('href')).toBe('https://www.instagram.com/farborastreadores/');
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toContain('noopener');
    expect(link.getAttribute('aria-label')).toContain('@farborastreadores');
    expect(link.querySelector('svg')).not.toBeNull();
  });
});

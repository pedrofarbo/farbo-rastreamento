// @vitest-environment jsdom
import { act } from 'react';
import type { ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';

import { WHATSAPP_GREETING, WHATSAPP_NUMBER, whatsappDisplay, whatsappUrl } from '@/config/contact';
import { LAUNCH_ANCHOR, PRE_LAUNCH, priceParts } from '@/config/landing';

import { Footer } from './Footer';
import { HeroSection } from './HeroSection';
import { Navbar } from './Navbar';
import { FLEET_FEATURES, FLEET_FEATURES_MESSAGE } from './FleetFeatures';
import { PricingSection, fleetMessage } from './PricingSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function render(node: ReactNode) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  act(() => createRoot(host).render(<MemoryRouter>{node}</MemoryRouter>));
  return host;
}

const links = (host: HTMLElement, text: string) =>
  Array.from(host.querySelectorAll('a')).filter((a) => a.textContent?.trim() === text);

afterEach(() => {
  document.body.innerHTML = '';
});

describe('priceParts', () => {
  it('separa reais e centavos para o preço grande', () => {
    expect(priceParts(2990)).toEqual({ reais: '29', cents: ',90' });
    expect(priceParts(12000)).toEqual({ reais: '120', cents: ',00' });
    expect(priceParts(6905)).toEqual({ reais: '69', cents: ',05' });
  });
});

// Os testes abaixo valem com a landing em pré-lançamento (config/landing.ts).
describe.runIf(PRE_LAUNCH)('landing em pré-lançamento', () => {
  it('o CTA do hero leva à lista de lançamento', () => {
    const host = render(<HeroSection onOpenModal={() => {}} />);
    const cta = links(host, 'Entrar no pré-lançamento');
    expect(cta).toHaveLength(1);
    expect(cta[0].getAttribute('href')).toBe(LAUNCH_ANCHOR);
  });

  it('o CTA do menu (computador e celular) é "Pré-lançamento" e leva à lista', () => {
    const host = render(<Navbar onOpenModal={() => {}} />);
    const ctas = links(host, 'Pré-lançamento');
    expect(ctas).toHaveLength(2);
    for (const cta of ctas) expect(cta.getAttribute('href')).toBe(LAUNCH_ANCHOR);
    // O link de texto para a mesma seção sai: o botão já leva lá.
    expect(links(host, 'Lançamento')).toHaveLength(0);
  });

  it('os planos mostram o preço de pré-lançamento e convidam a se cadastrar', () => {
    const host = render(<PricingSection onOpenModal={() => {}} />);
    const text = host.textContent ?? '';
    for (const part of ['Cadastre-se na lista', 'R$ 34,90/mês', 'R$ 120', '500 primeiros', 'De R$ 69,90', 'nos 12 primeiros meses']) {
      expect(text).toContain(part);
    }
    for (const label of ['Quero me cadastrar', 'Garantir preço de pré-lançamento']) {
      const [link] = links(host, label);
      expect(link?.getAttribute('href')).toBe(LAUNCH_ANCHOR);
    }
    // A promoção vale também para o Insanos, com a mensalidade deles; depois
    // dela, o preço especial deles.
    expect(text).toContain('Insanos MC pagam R$ 27,90/mês');
    const insanosCard = Array.from(host.querySelectorAll('div')).find((d) =>
      d.textContent?.startsWith('INTEGRANTES DO MOTOCLUBE INSANOS'),
    );
    expect(insanosCard?.textContent).toContain('R$27,90/mês nos 12 primeiros meses');
    expect(text).toContain('De R$ 39,90');
    expect(text).toContain('depois, R$ 39,90/mês por veículo');
    expect(text).not.toContain('Quero meu desconto');
    // O rastreador: à vista ou em até 10x sem juros no Pix (no lugar de "Pagamento único").
    expect(text).toContain('R$ 120 no rastreador, ou 10x de R$ 12,00 sem juros.');
    expect(text).toContain('à vista, ou 10x de R$ 12,00sem juros no Pix');
    expect(text).not.toContain('Pagamento único');
  });

  it('o equipamento mostra o selo Anatel no lugar de "Desbloqueado"; o Insanos, sem "Parceria oficial"', () => {
    const text = render(<PricingSection onOpenModal={() => {}} />).textContent ?? '';
    expect(text).toContain('04895-25-16219');
    expect(text).not.toContain('Desbloqueado');
    expect(text).not.toMatch(/parceria oficial/i);
    expect(text).toContain('MOTOCLUBE INSANOS');
  });

  it('os planos usam as imagens novas: o escudo do Insanos e a foto do J16', () => {
    const host = render(<PricingSection onOpenModal={() => {}} />);
    const sources = Array.from(host.querySelectorAll('img')).map((img) => img.getAttribute('src'));
    expect(sources).toEqual(expect.arrayContaining(['/assets/insanos-escudo.webp', '/assets/rastreador-j16-farbo.webp']));
    expect(sources).toContain('/assets/anatel-logo.png');
    expect(sources).not.toContain('/assets/insanos-skull.png');
    expect(sources).not.toContain('/assets/tracker-gt06.png');
  });
});

describe('frotas', () => {
  it('no lugar do rastreador, o preço especial para frotas, com o WhatsApp e a mensagem pronta', () => {
    const host = render(<PricingSection onOpenModal={() => {}} />);
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

  it('embaixo dos planos, o que a frota ganha, com o atalho do cartão e o orçamento', () => {
    const host = render(<PricingSection onOpenModal={() => {}} />);
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
  it('a coluna de parceiros virou Regulamentação, com a homologação Anatel', () => {
    const text = render(<Footer />).textContent ?? '';
    expect(text).toContain('Regulamentação');
    expect(text).toContain('Anatel - 04895-25-16219');
    expect(text).not.toContain('Parceiros');
  });

  it('com o WhatsApp ativo, o número aparece e abre a conversa', () => {
    const host = render(<Footer />);
    const link = host.querySelector('a[href^="https://wa.me/"]') as HTMLAnchorElement | null;
    if (!WHATSAPP_NUMBER) {
      expect(link).toBeNull();
      return;
    }
    expect(link?.textContent).toBe(whatsappDisplay());
    expect(link?.getAttribute('href')).toBe(whatsappUrl(WHATSAPP_GREETING));
    expect(link?.getAttribute('target')).toBe('_blank');
  });

  it('o ícone do Instagram leva ao perfil @farborastreadores, em nova aba', () => {
    const host = render(<Footer />);
    const link = host.querySelector('a[href*="instagram.com"]') as HTMLAnchorElement;
    expect(link.getAttribute('href')).toBe('https://www.instagram.com/farborastreadores/');
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toContain('noopener');
    expect(link.getAttribute('aria-label')).toContain('@farborastreadores');
    expect(link.querySelector('svg')).not.toBeNull();
  });
});

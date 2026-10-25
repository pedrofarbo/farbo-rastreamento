// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

let offer = OFFER;
vi.mock('@/api/resources', () => ({ checkoutApi: { offer: async () => offer } }));

import { HIRE_VIA_WHATSAPP, WHATSAPP_NUMBER } from '@/config/contact';
import { OFFER, offerWith, withQuery } from '@/test/checkoutOffer';

import { HowItWorksSection } from './HowItWorksSection';

// O passo 1 segue a forma de contratar (pré-cadastro ou direto no WhatsApp).
const VIA_WHATSAPP = Boolean(HIRE_VIA_WHATSAPP && WHATSAPP_NUMBER);

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

beforeEach(() => {
  offer = OFFER;
});
afterEach(() => {
  document.body.innerHTML = '';
});

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () => createRoot(host).render(withQuery(<HowItWorksSection />)));
  await act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
  return host;
}

describe('Passo a passo', () => {
  it('com a contratação pelo site: contrata com Pix, entra no app e instala do seu jeito', async () => {
    const host = await render();
    const cards = Array.from(host.querySelectorAll('#como-funciona ol > li'));
    expect(cards).toHaveLength(6);

    const arts = cards.map((card) => {
      const svgs = card.querySelectorAll('svg');
      expect(svgs).toHaveLength(1);
      // O texto do card já diz tudo: o desenho fica fora do leitor de tela.
      expect(svgs[0].getAttribute('aria-hidden')).toBe('true');
      return svgs[0].getAttribute('data-art');
    });
    expect(arts).toEqual(['pedido', 'interesse', 'preparo', 'envio', 'instalacao', 'acompanhamento']);

    expect(cards.map((card) => card.querySelector('h3')?.textContent)).toEqual([
      'Contrate pelo site',
      'Entre no app na hora',
      'Preparamos na nossa base',
      'Enviamos até você',
      'Instale do seu jeito',
      'Bloqueie e acompanhe pelo celular',
    ]);
    for (const [i, card] of cards.entries()) {
      expect(card.textContent).toContain(String(i + 1).padStart(2, '0'));
      expect(card.querySelector('p')?.textContent?.length).toBeGreaterThan(80);
    }
    expect(cards[0].textContent).toContain('pague com Pix');
    expect(cards[0].textContent).toContain('comodato');
    expect(cards[2].textContent).toContain('relé de bloqueio no kit');
    expect(cards[4].textContent).toContain('técnicos da Farbo');
    expect(cards[4].textContent).toContain('Você escolhe');
    // Sem valor no texto: os preços vêm do servidor, nos planos.
    const copy = Array.from(host.querySelectorAll('#como-funciona h2, #como-funciona h3, #como-funciona p'))
      .map((el) => el.textContent)
      .join(' ');
    expect(copy).not.toMatch(/R\$\s*\d|\d+x de/);
  });

  it('com a contratação pelo site fechada: o pré-cadastro e o pedido no painel', async () => {
    offer = offerWith({ enabled: false });
    const host = await render();
    const titles = Array.from(host.querySelectorAll('#como-funciona h3')).map((h) => h.textContent);
    expect(titles.slice(0, 2)).toEqual([
      VIA_WHATSAPP ? 'Contrate pelo WhatsApp' : 'Deixe seu interesse',
      'Peça o rastreador pelo painel',
    ]);
    expect(titles).toHaveLength(6);
  });
});

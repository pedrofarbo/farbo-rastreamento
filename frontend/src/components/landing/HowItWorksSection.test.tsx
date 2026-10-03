// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it } from 'vitest';

import { WHATSAPP_NUMBER } from '@/config/contact';

import { HowItWorksSection } from './HowItWorksSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

afterEach(() => {
  document.body.innerHTML = '';
});

function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  act(() => createRoot(host).render(<HowItWorksSection />));
  return host;
}

describe('Passo a passo', () => {
  it('cada card tem a sua animação, decorativa, e mantém número, quem faz, título e texto', () => {
    const cards = Array.from(render().querySelectorAll('#como-funciona ol > li'));
    expect(cards).toHaveLength(6);

    const arts = cards.map((card) => {
      const svgs = card.querySelectorAll('svg');
      expect(svgs).toHaveLength(1);
      // O texto do card já diz tudo: o desenho fica fora do leitor de tela.
      expect(svgs[0].getAttribute('aria-hidden')).toBe('true');
      return svgs[0].getAttribute('data-art');
    });
    expect(arts).toEqual([
      WHATSAPP_NUMBER ? 'whatsapp' : 'interesse',
      'pedido',
      'preparo',
      'envio',
      'instalacao',
      'acompanhamento',
    ]);

    expect(cards.map((card) => card.querySelector('h3')?.textContent)).toEqual([
      WHATSAPP_NUMBER ? 'Contrate pelo WhatsApp' : 'Deixe seu interesse',
      'Peça o rastreador pelo painel',
      'Preparamos na nossa base',
      'Enviamos até você',
      'Instale com um parceiro',
      'Acompanhe em tempo real',
    ]);
    for (const [i, card] of cards.entries()) {
      expect(card.textContent).toContain(String(i + 1).padStart(2, '0'));
      expect(card.querySelector('p')?.textContent?.length).toBeGreaterThan(80);
    }
    expect(cards[3].textContent).toContain('Farbo');
    expect(cards[4].textContent).toContain('Prestador parceiro');
  });
});

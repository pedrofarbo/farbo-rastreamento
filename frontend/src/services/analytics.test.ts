// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';

import { percent, sectionReach } from '@/pages/admin/SiteAnalyticsTab';

import { clickEvent, externalReferrer } from './analytics';

describe('externalReferrer', () => {
  it('só conta quem veio de outro site', () => {
    expect(externalReferrer('https://www.instagram.com/', 'farborastreadores.com.br')).toBe('https://www.instagram.com/');
    expect(externalReferrer('https://farborastreadores.com.br/#planos', 'farborastreadores.com.br')).toBe('');
    expect(externalReferrer('', 'farborastreadores.com.br')).toBe('');
    expect(externalReferrer('lixo', 'farborastreadores.com.br')).toBe('');
  });
});

describe('clickEvent', () => {
  const el = (html: string) => {
    const host = document.createElement('div');
    host.innerHTML = html;
    return host.querySelector('[data-click]') as Element;
  };

  it('botão da página é cta_click; Instagram e e-mail são saída do site', () => {
    expect(clickEvent(el('<a href="#lancamento" data-analytics="hero"><span data-click>Entrar</span></a>'), 'site.test')).toEqual({
      name: 'cta_click',
      label: 'hero',
    });
    expect(clickEvent(el('<a href="https://www.instagram.com/x/" data-analytics="instagram" data-click>ig</a>'), 'site.test')).toEqual({
      name: 'outbound_click',
      label: 'instagram',
    });
    expect(clickEvent(el('<a href="mailto:a@b.com" data-analytics="email" data-click>e</a>'), 'site.test')?.name).toBe(
      'outbound_click',
    );
    expect(clickEvent(el('<button data-analytics="cta-final" data-click>ok</button>'), 'site.test')?.name).toBe('cta_click');
  });

  it('sem marcação, nada', () => {
    expect(clickEvent(el('<button data-click>qualquer</button>'), 'site.test')).toBeNull();
  });
});

describe('painel das visitas', () => {
  it('porcentagem com uma casa abaixo de 10% e sem base', () => {
    expect(percent(1, 8)).toBe('13%');
    expect(percent(1, 40)).toBe('2,5%');
    expect(percent(0, 0)).toBe('—');
  });

  it('as seções saem na ordem da página, só as que alguém viu', () => {
    const reach = sectionReach({
      sections: [
        { key: 'rodape', visitors: 2, count: 2 },
        { key: 'beneficios', visitors: 10, count: 10 },
        { key: 'planos', visitors: 6, count: 6 },
      ],
    });
    expect(reach.map((s) => [s.id, s.visitors])).toEqual([
      ['beneficios', 10],
      ['planos', 6],
      ['rodape', 2],
    ]);
  });
});

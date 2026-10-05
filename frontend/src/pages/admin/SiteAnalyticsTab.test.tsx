// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { AnalyticsOrigin, LandingAnalytics } from '@/types';

const ORIGINS: AnalyticsOrigin[] = [
  { channel: 'instagram', visitors: 30, leadOpens: 9, leads: 3, waitlist: 2, converted: 4, clicked: 12 },
  { channel: 'direto', visitors: 15, leadOpens: 1, leads: 1, waitlist: 0, converted: 1, clicked: 2 },
  { channel: 'panfleto', visitors: 5, leadOpens: 0, leads: 0, waitlist: 0, converted: 0, clicked: 0 },
];

function summary(channel: string): LandingAnalytics {
  const visitors = channel === 'instagram' ? 30 : 50;
  return {
    from: '2026-09-06', to: '2026-10-05', channel, visitors, pageviews: visitors + 10, activeNow: 1,
    leadOpens: 10, leads: 4, waitlist: 2, installerOpens: 0,
    days: [{ day: '2026-10-05', visitors, pageviews: visitors }],
    origins: ORIGINS, campaigns: [], devices: [], browsers: [], systems: [], sections: [], clicks: [],
  };
}

const asked: [number, string | undefined][] = [];
vi.mock('@/api/resources', () => ({
  analyticsApi: {
    landing: async (days: number, origin?: string) => {
      asked.push([days, origin]);
      return summary(origin ?? '');
    },
  },
}));

import { channelLabel, originOptions, SiteAnalyticsTab } from './SiteAnalyticsTab';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

afterEach(() => {
  document.body.innerHTML = '';
  asked.length = 0;
});

describe('origens', () => {
  it('nomes das origens e as opções do filtro', () => {
    expect(channelLabel('instagram')).toBe('Instagram');
    expect(channelLabel('busca')).toBe('Outros buscadores');
    expect(channelLabel('exemplo.com.br')).toBe('exemplo.com.br');
    expect(originOptions(ORIGINS, '')).toEqual(['instagram', 'direto', 'panfleto']);
    // A escolhida continua na lista mesmo sem visitas no novo período.
    expect(originOptions(ORIGINS, 'google')).toEqual(['instagram', 'direto', 'panfleto', 'google']);
  });
});

describe('SiteAnalyticsTab', () => {
  it('mostra o funil de cada origem e filtra a página pela escolhida', async () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <SiteAnalyticsTab />
        </QueryClientProvider>,
      ),
    );
    await flush();
    const text = () => host.textContent ?? '';
    for (const part of [
      'Funil por origem',
      'Instagram30 · 60%',
      '9 abriram o pré-cadastro · 3 pré-cadastros · 2 na lista · 13% viraram contato',
      'Direto15 · 30%',
      '1 abriu o pré-cadastro · 1 pré-cadastro',
      'panfleto',
      'Todas as origens',
    ]) {
      expect(text()).toContain(part);
    }
    expect(asked[0]).toEqual([30, '']);

    // Escolher o Instagram refaz a busca só com ele e avisa o filtro.
    const row = (name: string) =>
      Array.from(host.querySelectorAll('button[aria-pressed]')).find((b) => b.textContent?.startsWith(name)) as HTMLButtonElement;
    await act(async () => row('Instagram').click());
    await flush();
    expect(asked.at(-1)).toEqual([30, 'instagram']);
    expect(text()).toContain('Mostrando só quem chegou por Instagram.');
    expect(text()).toContain('Só quem veio de Instagram');
    expect(row('Instagram').getAttribute('aria-pressed')).toBe('true');
    expect((host.querySelector('select') as HTMLSelectElement).value).toBe('instagram');

    // "Ver todas as origens" desfaz; o seletor também filtra.
    const all = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'Ver todas as origens') as HTMLButtonElement;
    await act(async () => all.click());
    await flush();
    expect(asked.at(-1)).toEqual([30, '']);
    expect(text()).not.toContain('Mostrando só quem chegou');

    const select = host.querySelector('select') as HTMLSelectElement;
    await act(async () => {
      select.value = 'direto';
      select.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await flush();
    expect(asked.at(-1)).toEqual([30, 'direto']);
  });
});

// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { PublicRepresentative } from '@/types';

const state = vi.hoisted(() => ({ list: [] as PublicRepresentative[] }));
vi.mock('@/api/resources', () => ({ publicApi: { representatives: async () => state.list } }));

import { RepresentativesSection } from './RepresentativesSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => document.body.textContent ?? '';

const rep = (over: Partial<PublicRepresentative>): PublicRepresentative => ({
  name: 'Rui Representante', handle: 'rui.moto', code: 'rui', city: 'Campinas/SP', area: 'Campinas e Valinhos',
  whatsapp: '5519999990000', handDelivery: true, installs: true, ...over,
});

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter>
          <RepresentativesSection />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('os representantes oficiais na landing', () => {
  it('sem nenhum marcado pela central, a seção não aparece', async () => {
    state.list = [];
    await render();
    expect(document.querySelector('#representantes')).toBeNull();
  });

  it('a cidade, a região, o que ele faz, o Instagram, o contratar pelo link e o WhatsApp', async () => {
    state.list = [rep({}), rep({ name: 'Ana Souza', code: 'ana', handle: '', whatsapp: '', area: '', handDelivery: false, installs: false })];
    await render();
    expect(text()).toContain('Representantes oficiais');
    expect(text()).toContain('📍 Campinas/SP');
    expect(text()).toContain('Atende: Campinas e Valinhos');
    expect(text()).toContain('Entrega em mãos');
    expect(text()).toContain('Faz a instalação');
    const links = Array.from(document.querySelectorAll('a')).map((a) => [a.textContent, a.getAttribute('href')]);
    expect(links).toContainEqual(['Contratar com Rui', '/indicacao/rui']);
    expect(links).toContainEqual(['@rui.moto', 'https://instagram.com/rui.moto']);
    expect(links.find(([label]) => label === 'WhatsApp')?.[1]).toMatch(/^https:\/\/wa\.me\/5519999990000\?text=/);
    // A Ana, sem WhatsApp nem Instagram: só o contratar.
    expect(links).toContainEqual(['Contratar com Ana', '/indicacao/ana']);
    expect(links.filter(([label]) => label === 'WhatsApp')).toHaveLength(1);
    // Com poucos, sem a busca.
    expect(document.querySelector('input[type="search"]')).toBeNull();
  });

  it('com muitos, a busca pela cidade', async () => {
    state.list = Array.from({ length: 8 }, (_, i) => rep({ name: `Rep ${i}`, code: `rep${i}`, city: i === 3 ? 'Sorocaba/SP' : 'Campinas/SP' }));
    await render();
    const search = document.querySelector('input[type="search"]') as HTMLInputElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(search, 'soro');
      search.dispatchEvent(new Event('input', { bubbles: true }));
    });
    expect(document.querySelectorAll('article')).toHaveLength(1);
    expect(text()).toContain('Rep 3');
  });
});

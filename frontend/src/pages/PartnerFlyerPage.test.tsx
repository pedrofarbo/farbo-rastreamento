// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { PartnerReport } from '@/types';

let respond: () => Promise<PartnerReport>;
vi.mock('@/api/resources', async () => {
  const actual = await vi.importActual<typeof import('@/api/resources')>('@/api/resources');
  return { publicApi: { partner: () => respond(), partnerQrUrl: actual.publicApi.partnerQrUrl } };
});

import { PartnerFlyerPage } from './PartnerFlyerPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = (host: HTMLElement) => (host.textContent ?? '').replace(/ /g, ' ');
const report = (over: Partial<PartnerReport> = {}): PartnerReport => ({
  name: 'João Motoca', handle: 'joao.moto', code: 'joao-moto', active: true, commissionCents: 600, signups: 0, customers: 0,
  activeCustomers: 0, activeVehicles: 0, toReceiveCents: 0, paidCents: 0, months: [], ...over,
});

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <MemoryRouter initialEntries={['/parceiro/segredo-do-joao-0123456789/flyer']}>
        <Routes>
          <Route path="/parceiro/:token/flyer" element={<PartnerFlyerPage />} />
        </Routes>
      </MemoryRouter>,
    ),
  );
  await flush();
  return host;
}

const buttons = (host: HTMLElement) => Array.from(host.querySelectorAll('button')).map((b) => b.textContent);

afterEach(() => {
  document.body.innerHTML = '';
});

describe('PartnerFlyerPage', () => {
  it('escolhe o formato: A5 sai em PDF e PNG; stories e post, em PNG', async () => {
    respond = async () => report();
    const host = await render();
    expect(text(host)).toContain('Seu flyer');
    expect(text(host)).toContain('Mostrar “Indicado por @joao.moto”');
    expect(buttons(host)).toEqual(
      expect.arrayContaining(['Baixar PDF (para gráfica)', 'Baixar PNG']),
    );
    const stories = Array.from(host.querySelectorAll('[role=radio]')).find((b) => b.textContent?.startsWith('Stories')) as HTMLButtonElement;
    await act(async () => stories.click());
    expect(stories.getAttribute('aria-checked')).toBe('true');
    expect(buttons(host)).not.toContain('Baixar PDF (para gráfica)');
    expect(buttons(host)).toContain('Baixar PNG');
    // Stories e post vão para o Instagram; o A5 é para imprimir.
    expect(buttons(host)).toContain('Compartilhar no Instagram');
    const a5 = Array.from(host.querySelectorAll('[role=radio]')).find((b) => b.textContent?.startsWith('Flyer A5')) as HTMLButtonElement;
    await act(async () => a5.click());
    expect(buttons(host)).not.toContain('Compartilhar no Instagram');
    expect(host.querySelector('a[href="/parceiro/segredo-do-joao-0123456789"]')?.textContent).toContain('Voltar');
  });

  it('sem @, mostra o nome; pausado, sem flyer', async () => {
    respond = async () => report({ handle: '' });
    let host = await render();
    expect(text(host)).toContain('Mostrar “Indicado por João Motoca”');
    document.body.innerHTML = '';
    respond = async () => report({ active: false });
    host = await render();
    expect(text(host)).toContain('Seu link está pausado');
    expect(host.querySelector('[role=radiogroup]')).toBeNull();
  });
});

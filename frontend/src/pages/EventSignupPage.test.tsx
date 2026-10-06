// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { eventSlug, eventUrl } from '@/config/landing';
import type { WaitlistInput } from '@/types';

const sent: WaitlistInput[] = [];
vi.mock('@/api/resources', () => ({
  publicApi: {
    joinLaunch: async (input: WaitlistInput) => {
      sent.push(input);
      return { status: 'ok' };
    },
  },
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

afterEach(() => {
  document.body.innerHTML = '';
  sent.length = 0;
});

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
  it('cadastra com o evento do link e deixa cadastrar a próxima pessoa', async () => {
    window.scrollTo = () => undefined;
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <MemoryRouter initialEntries={['/evento/Encontro-Insanos-MC']}>
          <Routes>
            <Route path="/evento/:evento" element={<EventSignupPage />} />
          </Routes>
        </MemoryRouter>,
      ),
    );
    const inputs = () => Array.from(host.querySelectorAll('input')) as HTMLInputElement[];
    const field = (type: string) => inputs().find((i) => i.type === type && i.name !== 'website') as HTMLInputElement;
    const city = () => inputs().find((i) => i.autocomplete === 'address-level2') as HTMLInputElement;
    const submit = () => host.querySelector('button[type=submit]') as HTMLButtonElement;
    expect(host.textContent).toContain('Garanta o preço de pré-lançamento');
    // O destaque é o preço do Insanos MC.
    expect(host.textContent).toContain('Preço exclusivo Insanos MC');
    expect(host.textContent).toContain('R$27,90/mês');
    expect(submit().disabled).toBe(true);

    await act(async () => {
      type(field('text'), 'Ana Souza');
      type(field('tel'), '11987654321');
      type(field('email'), 'ana@cliente.test');
    });
    expect(field('tel').value).toBe('(11) 98765-4321');
    await act(async () => field('checkbox').click());
    expect(submit().disabled).toBe(true); // falta a cidade
    await act(async () => type(city(), 'São Paulo - SP'));
    expect(submit().disabled).toBe(false);

    await act(async () => submit().click());
    await flush();
    expect(sent).toEqual([
      {
        name: 'Ana Souza', email: 'ana@cliente.test', phone: '(11) 98765-4321', consent: true, website: '',
        event: 'encontro-insanos-mc', city: 'São Paulo - SP',
      },
    ]);
    expect(host.textContent).toContain('Pronto, Ana!');
    expect(host.textContent).toContain('ana@cliente.test');

    // A próxima pessoa: o formulário volta vazio.
    const again = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'Cadastrar outra pessoa') as HTMLButtonElement;
    await act(async () => again.click());
    expect(field('text').value).toBe('');
    expect(city().value).toBe('');
    expect(field('checkbox').checked).toBe(false);
    expect(submit().disabled).toBe(true);
  });

  it('o QR Code do público geral destaca o preço de pré-lançamento, sem o Insanos', async () => {
    window.scrollTo = () => undefined;
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <MemoryRouter initialEntries={['/evento/feira-de-motos?publico=geral']}>
          <Routes>
            <Route path="/evento/:evento" element={<EventSignupPage />} />
          </Routes>
        </MemoryRouter>,
      ),
    );
    expect(host.textContent).toContain('Preço de pré-lançamento');
    expect(host.textContent).toContain('R$34,90/mês');
    expect(host.textContent).not.toContain('Insanos');
  });
});

// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { Lead } from '@/types';

function lead(over: Partial<Lead>): Lead {
  return {
    id: 'l1', name: 'Ana Souza', email: 'ana@exemplo.com', phone: '(11) 98888-7777', city: 'São Paulo - SP', plan: '',
    vehicleType: '', vehicleCount: 1, message: '', status: 'NEW', notes: '', customerId: null, onLaunchList: true,
    source: 'lancamento', consentAt: '2026-10-05T12:00:00Z', createdAt: '2026-10-05T12:00:00Z', updatedAt: '2026-10-05T12:00:00Z',
    affiliateId: null, referrer: '', event: '', promoClaimed: false, ...over,
  };
}

const LIST: Lead[] = [
  lead({ id: 'a' }),
  lead({ id: 'b', name: 'Bruno', email: 'bruno@x.com', city: 'Campinas', source: 'landing', plan: 'Plano Mensal - R$ 69,90', vehicleType: 'moto', onLaunchList: false, status: 'CONTACTED' }),
  lead({ id: 'c', name: 'Carla', email: 'carla@x.com', source: 'evento', event: 'encontro-insanos-mc' }),
  lead({ id: 'd', name: 'Davi', email: 'davi@x.com', source: 'indicacao', referrer: '@joao.moto', affiliateId: 'af', promoClaimed: true, customerId: 'cu' }),
];

const removed: string[] = [];
vi.mock('@/api/resources', () => ({
  leadsApi: {
    list: async () => LIST,
    promo: async () => ({ enabled: true, used: 1, slots: 500, offer: { equipmentCents: 12000, monthlyCents: 3490, insanosMonthlyCents: 2790, insanosPlanName: 'Especial Insanos MC', months: 12 } }),
    qrImage: async () => new Blob(),
    removeFromLaunch: async (id: string) => {
      removed.push(`lista:${id}`);
    },
    remove: async (id: string) => {
      removed.push(`apagar:${id}`);
    },
  },
}));

import { byOrigin, eventLabel, interest, leadsCsv, matches, originLabel, LeadsTab } from './LeadsTab';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

afterEach(() => {
  document.body.innerHTML = '';
  removed.length = 0;
});

describe('pré-clientes', () => {
  it('a origem, o interesse, a busca e o CSV', () => {
    expect(originLabel(LIST[0])).toBe('Lista (site)');
    expect(originLabel(LIST[1])).toBe('Pré-cadastro');
    expect(originLabel(LIST[2])).toBe('Encontro Insanos Mc');
    expect(originLabel(LIST[3])).toBe('Indicação @joao.moto');
    expect(eventLabel('feira-de-motos-sp')).toBe('Feira De Motos Sp');
    expect(interest(LIST[0])).toBe('');
    expect(interest(LIST[1])).toBe('Plano Mensal - R$ 69,90 · 1 veículo (moto)');
    expect(byOrigin([...LIST, lead({ id: 'e' })])[0]).toEqual({ label: 'Lista (site)', count: 2 });
    expect(matches(LIST[0], 'sao paulo')).toBe(true);
    expect(matches(LIST[0], '98888')).toBe(true);
    expect(matches(LIST[0], 'bruno')).toBe(false);
    const csv = leadsCsv([lead({ name: 'João "Jota"' })]);
    expect(csv.startsWith('﻿Nome;E-mail;WhatsApp;Cidade da instalação;Origem;Interesse;Lista de lançamento;Situação;Recebido em\r\n')).toBe(true);
    expect(csv).toContain('"João ""Jota""";"ana@exemplo.com";"(11) 98888-7777";"São Paulo - SP";"Lista (site)";"";"Sim";"Novo";');
  });

  it('uma lista só, com os filtros, e tira da lista pelo detalhe', async () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <MemoryRouter>
            <ToastProvider>
              <LeadsTab onConvert={() => undefined} />
            </ToastProvider>
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    );
    await flush();
    const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
    const rows = () => host.querySelectorAll('tbody tr').length;
    expect(text()).toContain('4 pré-clientes');
    expect(text()).toContain('3 na lista de lançamento');
    expect(text()).toContain('1 de 500 vagas usadas');
    expect(text()).toContain('Usou a promoção');
    expect(rows()).toBe(4);
    const select = (label: string) => host.querySelector(`select[aria-label="${label}"]`) as HTMLSelectElement;
    const choose = async (label: string, value: string) =>
      act(async () => {
        const el = select(label);
        el.value = value;
        el.dispatchEvent(new Event('change', { bubbles: true }));
      });
    await choose('Lista de lançamento', 'off');
    expect(rows()).toBe(1);
    expect(text()).toContain('1 de 4 pré-clientes');
    await choose('Lista de lançamento', '');
    await choose('Origem', 'Encontro Insanos Mc');
    expect(rows()).toBe(1);
    await choose('Origem', '');
    await choose('Situação', 'CONTACTED');
    expect(rows()).toBe(1);
    await choose('Situação', '');

    // O detalhe da Ana: tirar da lista (com confirmação).
    const open = Array.from(host.querySelectorAll('tbody tr'))[0].querySelector('button') as HTMLButtonElement;
    await act(async () => open.click());
    const button = (label: string) =>
      Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;
    await act(async () => button('Tirar da lista de lançamento').click());
    expect(text()).toContain('Perde o direito à promoção');
    await act(async () => button('Tirar da lista').click());
    await flush();
    expect(removed).toEqual(['lista:a']);
  });
});

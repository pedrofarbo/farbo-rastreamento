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

const PROMO = { enabled: true, used: 1, slots: 500, offer: { equipmentCents: 12000, monthlyCents: 3490, insanosMonthlyCents: 2790, insanosPlanName: 'Especial Insanos MC', months: 12 } };

const removed: string[] = [];
vi.mock('@/stores/AuthContext', () => ({ useAuth: () => ({ user: { name: 'pedro farbo' } }) }));
vi.mock('@/api/resources', () => ({
  leadsApi: {
    list: async () => LIST,
    promo: async () => PROMO,
    qrImage: async () => new Blob(),
    removeFromLaunch: async (id: string) => {
      removed.push(`lista:${id}`);
    },
    remove: async (id: string) => {
      removed.push(`apagar:${id}`);
    },
  },
}));

import { byOrigin, eventLabel, interest, leadsCsv, matches, originLabel, whatsappLink, whatsappMessage, LeadsTab } from './LeadsTab';

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

  it('a mensagem pronta do WhatsApp: primeiro contato, retorno e cliente', () => {
    const hello = 'Olá, Ana! Tudo bem? Aqui é Pedro, da Farbo Rastreadores.';
    const close = 'Posso te explicar como funciona e tirar suas dúvidas por aqui?';
    // Pré-cadastro no plano do Insanos, na lista: o interesse e a promoção com a mensalidade deles.
    const insanos = lead({
      name: 'ANA souza', source: 'landing', plan: 'Preço Especial Insanos MC - R$ 39,90', vehicleType: 'moto', vehicleCount: 2,
      city: 'Campinas - SP',
    });
    expect(whatsappMessage(insanos, 'Pedro Farbo', PROMO)).toBe(
      [
        hello,
        'Recebemos o seu pré-cadastro no nosso site. Vi que você tem interesse no preço especial do Insanos MC para 2 motos, com instalação em Campinas - SP.',
        'Como você está na nossa lista de lançamento, tem direito à promoção de pré-lançamento: o primeiro rastreador sai por R$ 120,00, com mensalidade de R$ 27,90 nos 12 primeiros meses, enquanto houver vagas.',
        close,
      ].join('\n\n'),
    );
    // A lista do site, sem plano: só a origem e a promoção comum.
    expect(whatsappMessage(LIST[0], 'Pedro', PROMO)).toBe(
      [
        hello,
        'Você deixou seu contato no nosso site.',
        'Como você está na nossa lista de lançamento, tem direito à promoção de pré-lançamento: o primeiro rastreador sai por R$ 120,00, com mensalidade de R$ 34,90 nos 12 primeiros meses, enquanto houver vagas.',
        close,
      ].join('\n\n'),
    );
    // Promoção encerrada: não promete.
    expect(whatsappMessage(LIST[0], 'Pedro', { ...PROMO, enabled: false })).not.toContain('promoção');
    // O evento e a indicação.
    expect(whatsappMessage(LIST[2], 'Pedro', PROMO)).toContain('Você deixou seu contato com a gente no evento Encontro Insanos Mc.');
    expect(whatsappMessage({ ...LIST[3], promoClaimed: false, customerId: null }, 'Pedro')).toContain(
      'Você chegou até a gente pela indicação de @joao.moto.',
    );
    // Já em contato: o retorno.
    expect(whatsappMessage(LIST[1], 'Pedro', PROMO)).toBe(
      [hello.replace('Ana', 'Bruno'), 'Passando para saber se ficou alguma dúvida sobre o rastreador para a sua moto.', 'Se quiser, te ajudo a contratar por aqui mesmo.'].join('\n\n'),
    );
    // Já é cliente: só o oi.
    expect(whatsappMessage(LIST[3], 'Pedro', PROMO)).toBe(
      [hello.replace('Ana', 'Davi'), 'Obrigado por escolher a Farbo! Passando para saber se está tudo certo e se ficou alguma dúvida. É só chamar por aqui.'].join('\n\n'),
    );
    // Sem nome de nenhum lado.
    expect(whatsappMessage(lead({ name: '' }), '', PROMO).split('\n')[0]).toBe('Olá! Tudo bem? Aqui é da Farbo Rastreadores.');
    // O número ganha o 55; a mensagem vai no text.
    expect(whatsappLink('(11) 98888-7777', 'Oi, tudo bem?')).toBe('https://wa.me/5511988887777?text=Oi%2C%20tudo%20bem%3F');
    expect(whatsappLink('+55 11 98888-7777')).toBe('https://wa.me/5511988887777');
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
    // Cada linha com telefone tem o WhatsApp com a mensagem pronta (de quem está conectado).
    const whats = Array.from(host.querySelectorAll('tbody a')).filter((a) => a.textContent === 'WhatsApp') as HTMLAnchorElement[];
    expect(whats).toHaveLength(4);
    const url = new URL(whats[0].href);
    expect(url.origin + url.pathname).toBe('https://wa.me/5511988887777');
    expect(url.searchParams.get('text')).toContain('Olá, Ana! Tudo bem? Aqui é Pedro, da Farbo Rastreadores.');
    expect(url.searchParams.get('text')).toContain('R$ 120,00');
    expect(whats[0].target).toBe('_blank');
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

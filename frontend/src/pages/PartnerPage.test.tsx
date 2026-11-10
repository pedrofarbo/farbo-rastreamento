// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import { storedPartnerSession } from '@/services/partnerSession';
import type { PartnerReport } from '@/types';

let respond: (token: string) => Promise<PartnerReport>;
// A senha da página: a entrada confere e a sessão vai nas próximas chamadas.
const logins: string[] = [];
vi.mock('@/api/resources', async () => {
  const actual = await vi.importActual<typeof import('@/api/resources')>('@/api/resources');
  return {
    publicApi: {
      partner: (token: string) => respond(token),
      partnerQrUrl: actual.publicApi.partnerQrUrl,
      partnerLogin: async (_: string, password: string) => {
        logins.push(password);
        if (password !== 'senha-do-joao') throw new (await import('@/api/client')).ApiError(401, 'senha incorreta', { code: 'WRONG_PASSWORD' });
        return { session: 'sessao-boa', expiresAt: new Date(Date.now() + 86_400_000).toISOString() };
      },
    },
  };
});

import { PartnerPage } from './PartnerPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = (host: HTMLElement) => (host.textContent ?? '').replace(/ /g, ' ');
const TOKEN = 'segredo-do-joao-0123456789';

const report = (over: Partial<PartnerReport> = {}): PartnerReport => ({
  name: 'João Motoca', handle: 'joao.moto', code: 'joao-moto', active: true, commissionType: 'fixed', commissionCents: 600, commissionBps: 0,
  signups: 0, customers: 0, activeCustomers: 0, activeVehicles: 0, toReceiveCents: 0, paidCents: 0, months: [], ...over,
});

async function render(path = `/parceiro/${TOKEN}`) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/parceiro/:token" element={<PartnerPage />} />
          <Route path="/parceiro/:token/:section" element={<PartnerPage />} />
        </Routes>
      </MemoryRouter>,
    ),
  );
  await flush();
  return host;
}

const menu = (host: HTMLElement) =>
  Array.from(host.querySelectorAll('nav[aria-label="Seções da sua página"] a')) as HTMLAnchorElement[];
const active = (host: HTMLElement) => host.querySelector('nav[aria-label="Seções da sua página"] [aria-current="page"]')?.textContent;
async function go(host: HTMLElement, label: string) {
  await act(async () => menu(host).find((a) => a.textContent === label)!.click());
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  logins.length = 0;
  localStorage.clear();
});

describe('PartnerPage', () => {
  it('o menu: só as seções que valem para ele, cada uma no seu endereço', async () => {
    respond = async () => report();
    let host = await render();
    expect(menu(host).map((a) => [a.textContent, a.getAttribute('href')])).toEqual([
      ['Início', `/parceiro/${TOKEN}`],
      ['Divulgar', `/parceiro/${TOKEN}/divulgar`],
      ['Ganhos', `/parceiro/${TOKEN}/ganhos`],
      ['Clientes', `/parceiro/${TOKEN}/clientes`],
      ['Como usar', `/parceiro/${TOKEN}/tutorial`],
    ]);
    expect(active(host)).toBe('Início');
    expect(document.title).toBe('Suas indicações · Farbo Rastreadores');
    // Os atalhos do início levam às seções; o do tutorial também.
    const tutorial = Array.from(host.querySelectorAll('nav[aria-label="Atalhos"] a')).find((a) => a.textContent?.includes('Como usar a sua página'))!;
    expect(tutorial.getAttribute('href')).toBe(`/parceiro/${TOKEN}/tutorial`);
    // A seção que não é para ele (sem kits): volta ao início.
    document.body.innerHTML = '';
    host = await render(`/parceiro/${TOKEN}/kits`);
    expect(active(host)).toBe('Início');
    expect(text(host)).toContain('Olá, João!');

    // O representante com instalações e kits: as seções deles também.
    document.body.innerHTML = '';
    respond = async () => report({ manageInstalls: true, kits: true, protected: true });
    host = await render();
    expect(menu(host).map((a) => a.textContent)).toEqual(['Início', 'Divulgar', 'Ganhos', 'Clientes', 'Instalações', 'Kits', 'Como usar']);
  });

  it('o início com os números; o link e o QR em Divulgar; o mês a mês em Ganhos', async () => {
    respond = async (token) => {
      expect(token).toBe(TOKEN);
      return report({
        signups: 12, customers: 4, activeCustomers: 3, activeVehicles: 5, toReceiveCents: 1400, paidCents: 600,
        months: [
          { month: '2026-11', vehicles: 1, amountCents: 800, status: 'pending' },
          { month: '2026-10', vehicles: 3, amountCents: 1800, status: 'closed' },
          { month: '2026-09', vehicles: 1, amountCents: 600, status: 'paid' },
        ],
      });
    };
    const host = await render();
    let t = text(host);
    expect(t).toContain('Olá, João!');
    expect(t).toContain('R$ 6,00 por mês por veículo ativo dos clientes que você indicou');
    expect(t).toContain('A receberR$ 14,00');
    expect(t).toContain('Já recebidoR$ 6,00');
    expect(t).toContain('12Cadastros pelo link');
    expect(t).toContain('5Veículos ativos');
    expect(t).not.toContain('farborastreadores.com.br/indicacao/joao-moto');
    expect(t).not.toContain('Mês a mês');
    // Fora dos buscadores e sem passar o link adiante.
    expect(document.head.querySelector('meta[name=robots]')?.getAttribute('content')).toBe('noindex, nofollow');

    await go(host, 'Divulgar');
    expect(active(host)).toBe('Divulgar');
    expect(document.title).toBe('Divulgar · Farbo Rastreadores');
    t = text(host);
    expect(t).toContain('farborastreadores.com.br/indicacao/joao-moto');
    expect(t).not.toContain('pausado');
    // O QR Code do link, para o flyer: na tela e para baixar.
    expect(t).toContain('QR Code para o seu flyer');
    const qr = host.querySelector('img[alt="QR Code do seu link de indicação"]') as HTMLImageElement;
    expect(qr.getAttribute('src')).toBe(`/api/public/partner/${TOKEN}/qr?format=png`);
    const links = Array.from(host.querySelectorAll('a[download]')).map((a) => [a.textContent, a.getAttribute('href')]);
    expect(links).toEqual([
      ['Baixar só o QR (PNG)', `/api/public/partner/${TOKEN}/qr?format=png&download=1`],
      ['Baixar só o QR (SVG, para gráfica)', `/api/public/partner/${TOKEN}/qr?format=svg&download=1`],
    ]);
    // E o flyer pronto, com a oferta e o QR.
    const flyer = Array.from(host.querySelectorAll('a')).find((a) => a.textContent === 'Criar flyer pronto');
    expect(flyer?.getAttribute('href')).toBe(`/parceiro/${TOKEN}/flyer`);

    await go(host, 'Ganhos');
    t = text(host);
    expect(t).toContain('paga a sua comissão por Pix');
    expect(t).toContain('A receberR$ 14,00');
    expect(t).toContain('nov/20261 veículoR$ 8,00Aguardando fechamento');
    expect(t).toContain('out/20263 veículosR$ 18,00A pagar');
    expect(t).toContain('set/20261 veículoR$ 6,00Pago');

    // Sem senha na página, os clientes não aparecem: só o convite para pedir a senha.
    await go(host, 'Clientes');
    expect(text(host)).toContain('peça à Farbo para colocar senha na sua página');
  });

  it('comissão em percentual: o percentual e a tabela no início; os valores de instalação em Instalações', async () => {
    respond = async () =>
      report({
        name: 'Lia Percentual', code: 'lia', commissionType: 'percent', commissionCents: 0, commissionBps: 1250,
        planPrices: { motorcycleCents: 3990, carCents: 4990, truckCents: 5990 },
        installPrices: { mode: 'direct', installCents: 12000, uninstallCents: 8000 },
      });
    const host = await render();
    expect(text(host)).toContain('Você ganha 12,5% da mensalidade de cada veículo ativo dos clientes que você indicou');
    // A tabela dele: o que paga quem contrata pelo link.
    expect(text(host)).toContain('Quem contrata pelo seu link paga por mês, por veículo: Moto R$ 39,90 · Carro R$ 49,90 · Caminhão R$ 59,90.');
    // O representante que recebe direto: os valores dele, que ele mesmo muda.
    expect(text(host)).not.toContain('Seus valores de instalação');
    await go(host, 'Instalações');
    expect(text(host)).toContain('Seus valores de instalação');
    expect(text(host)).toContain('hoje: R$ 120,00 a instalação e R$ 80,00 a desinstalação do comodato');
    expect((Array.from(host.querySelectorAll('label')).find((l) => l.textContent?.startsWith('Instalação (R$)'))!.querySelector('input') as HTMLInputElement).value).toBe('120,00');
  });

  it('comissão em percentual com os impostos da Farbo: a da mensalidade líquida', async () => {
    respond = async () =>
      report({ name: 'Rafael Magalhães', code: 'rafael', commissionType: 'percent', commissionCents: 0, commissionBps: 4000, commissionTaxBps: 600 });
    const host = await render();
    expect(text(host)).toContain(
      'Você ganha 40% da mensalidade (líquida dos impostos da Farbo, de 6%) de cada veículo ativo dos clientes que você indicou',
    );
  });

  it('afiliado pausado: avisa no início e em Divulgar, sem QR Code', async () => {
    respond = async () => report({ active: false });
    const host = await render();
    expect(text(host)).toContain('Seu link está pausado');
    await go(host, 'Divulgar');
    expect(text(host)).toContain('Seu link está pausado');
    expect(text(host)).not.toContain('QR Code');
    expect(host.querySelector('a[download]')).toBeNull();
  });

  it('link trocado ou inválido', async () => {
    respond = async () => {
      throw new ApiError(404, 'link inválido');
    };
    const host = await render();
    expect(text(host)).toContain('Este link não vale mais');
    expect(menu(host)).toHaveLength(0);
  });

  it('página com senha: pede a senha, recusa a errada e, com a certa, mostra os números (a entrada fica no aparelho)', async () => {
    respond = async () => {
      if (storedPartnerSession(TOKEN) !== 'sessao-boa') throw new ApiError(401, 'esta página pede a senha', { code: 'PARTNER_PASSWORD' });
      return report();
    };
    const host = await render(`/parceiro/${TOKEN}/ganhos`);
    expect(text(host)).toContain('Esta página pede senha.');
    expect(text(host)).not.toContain('Olá, João');
    expect(menu(host)).toHaveLength(0);
    const input = host.querySelector('input[type="password"]') as HTMLInputElement;
    const type = (value: string) =>
      act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value);
        input.dispatchEvent(new Event('input', { bubbles: true }));
      });
    const submit = () => act(async () => (host.querySelector('button[type="submit"]') as HTMLButtonElement).click());
    await type('errada-123');
    await submit();
    await flush();
    expect(text(host)).toContain('Senha incorreta.');
    await type('senha-do-joao');
    await submit();
    await flush();
    expect(logins).toEqual(['errada-123', 'senha-do-joao']);
    // Entra na seção que pediu.
    expect(active(host)).toBe('Ganhos');
    expect(text(host)).toContain('Seus ganhos');
    expect(storedPartnerSession(TOKEN)).toBe('sessao-boa');
  });
});

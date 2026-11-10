// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import type { PartnerReport } from '@/types';

let respond: () => Promise<PartnerReport>;
vi.mock('@/api/resources', () => ({ publicApi: { partner: () => respond() } }));
// Os esquemas da ligação têm o teste deles.
vi.mock('@/components/install/J16Wiring', () => ({ HarnessDiagram: () => null, RelayDiagram: () => null }));

import { PartnerPage } from './PartnerPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/\u00a0/g, ' ');
const steps = () => Array.from(document.querySelectorAll('nav[aria-label="Etapas do tutorial"] a')).map((a) => a.textContent);

const report = (over: Partial<PartnerReport> = {}): PartnerReport => ({
  name: 'João Motoca', handle: 'joao.moto', code: 'joao-moto', active: true, commissionType: 'fixed', commissionCents: 600, commissionBps: 0,
  signups: 0, customers: 0, activeCustomers: 0, activeVehicles: 0, toReceiveCents: 0, paidCents: 0, months: [], ...over,
});

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <MemoryRouter initialEntries={['/parceiro/segredo-do-joao/tutorial']}>
        <Routes>
          <Route path="/parceiro/:token/:section" element={<PartnerPage />} />
        </Routes>
      </MemoryRouter>,
    ),
  );
  await vi.waitFor(() => expect(text()).not.toContain('Carregando'));
}

afterEach(() => {
  document.body.innerHTML = '';
  localStorage.clear();
});

describe('o tutorial do representante', () => {
  it('quem só indica: divulgar, acompanhar e receber por Pix, sem kits nem instalação', async () => {
    respond = async () => report();
    await render();
    expect(document.title).toBe('Como usar · Farbo Rastreadores');
    expect(text()).toContain('João, este é o passo a passo para divulgar, acompanhar');
    expect(steps()).toEqual([
      '1Como funciona', '2Divulgar o link', '3O que o cliente faz', '4Acompanhar', '5Receber a comissão', '6O app do cliente', '7Dúvidas',
    ]);
    expect(text()).toContain('Você ganha R$ 6,00 por mês por veículo ativo');
    expect(text()).toContain('paga a sua comissão por Pix');
    expect(text()).toContain('peça à Farbo para colocar senha na sua página');
    expect(text()).not.toContain('Pagar.me');
    expect(text()).not.toContain('Recebi os kits');
    expect(text()).not.toContain('link do instalador');
    expect(text()).not.toContain('Precisa ativar');
    expect(text()).not.toContain('Esqueci a senha');
    // Dentro da página: o menu com "Como usar" marcado, e o texto leva às seções pelo nome do menu.
    const active = document.querySelector('nav[aria-label="Seções da sua página"] [aria-current="page"]')!;
    expect(active.textContent).toBe('Como usar');
    const toShare = Array.from(document.querySelectorAll('#divulgar a')).find((a) => a.textContent === 'Divulgar')!;
    expect(toShare.getAttribute('href')).toBe('/parceiro/segredo-do-joao/divulgar');
  });

  it('representante com kits, instalações, recebedor e valores próprios: todas as partes, na ordem', async () => {
    respond = async () =>
      report({
        commissionType: 'percent', commissionBps: 1500, commissionTaxBps: 1133, kits: true, manageInstalls: true, handDelivery: true,
        protected: true, installPrices: { mode: 'direct', installCents: 8000, uninstallCents: 5000 },
        recipient: { enabled: true, status: '' },
      });
    await render();
    expect(steps()).toEqual([
      '1Como funciona', '2Divulgar o link', '3O que o cliente faz', '4Acompanhar', '5Receber a comissão', '6Seus kits', '7Agendar',
      '8No dia da instalação', '9Ativar o rastreador', '10Ligar o rastreador', '11Seus valores', '12O app do cliente', '13Dúvidas',
    ]);
    expect(text()).toContain('agendar e instalar, acompanhar');
    expect(text()).toContain('15% da mensalidade (líquida dos impostos da Farbo, de 11,33%)');
    expect(text()).toContain('prova de vida');
    expect(text()).toContain('Recebi os kits');
    expect(text()).toContain('Abrir o link do instalador');
    // A ativação: o passo a passo, as mensagens da tela e o limite do link.
    expect(text()).toContain('Ativar o rastreador (antes de instalar)');
    expect(text()).toContain('O chip já veio no rastreador');
    expect(text()).toContain('O chip veio separado');
    expect(text()).toContain('todos os números do ICCID');
    expect(text()).toContain('Este chip não está cadastrado na Farbo');
    expect(document.querySelector('#kits a[href="#ativar"]')).not.toBeNull();
    expect(text()).toContain('Esperando o rastreador conectar');
    expect(text()).toContain('O rastreador não conectou em 20 minutos');
    expect(text()).toContain('Cada link permite até 3 ativações');
    expect(document.querySelector('#link-instalador a[href="#ativar"]')).not.toBeNull();
    expect(text()).toContain('quem contrata pelo seu link não paga frete');
    expect(text()).toContain('Seus valores de instalação');
    expect(text()).toContain('Esqueci a senha da minha página');
    expect(text()).toContain('Confira se você tocou em “Recebi os kits”');
    // Cada seção do índice existe na página (a âncora leva a ela).
    for (const a of document.querySelectorAll('nav[aria-label="Etapas do tutorial"] a')) {
      expect(document.getElementById(a.getAttribute('href')!.slice(1))).not.toBeNull();
    }
  });

  it('link pausado: avisa no começo', async () => {
    respond = async () => report({ active: false });
    await render();
    expect(text()).toContain('Seu link está pausado agora');
  });

  it('página com senha: pede a senha antes do tutorial', async () => {
    respond = async () => {
      throw new ApiError(401, 'senha', { code: 'PARTNER_PASSWORD' });
    };
    await render();
    expect(text()).not.toContain('Como usar a sua página');
    expect(document.querySelector('input[type="password"]')).not.toBeNull();
  });
});

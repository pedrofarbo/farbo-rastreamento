// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import type { PartnerReport } from '@/types';

let respond: (token: string) => Promise<PartnerReport>;
vi.mock('@/api/resources', () => ({ publicApi: { partner: (token: string) => respond(token) } }));

import { PartnerPage } from './PartnerPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = (host: HTMLElement) => (host.textContent ?? '').replace(/ /g, ' ');

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <MemoryRouter initialEntries={['/parceiro/segredo-do-joao-0123456789']}>
        <Routes>
          <Route path="/parceiro/:token" element={<PartnerPage />} />
        </Routes>
      </MemoryRouter>,
    ),
  );
  await flush();
  return host;
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('PartnerPage', () => {
  it('mostra o link e os números, mês a mês', async () => {
    respond = async (token) => {
      expect(token).toBe('segredo-do-joao-0123456789');
      return {
        name: 'João Motoca', handle: 'joao.moto', code: 'joao-moto', active: true, commissionCents: 600,
        signups: 12, customers: 4, activeCustomers: 3, toReceiveCents: 1400, paidCents: 600,
        months: [
          { month: '2026-11', customers: 1, amountCents: 800, status: 'pending' },
          { month: '2026-10', customers: 3, amountCents: 1800, status: 'closed' },
          { month: '2026-09', customers: 1, amountCents: 600, status: 'paid' },
        ],
      };
    };
    const host = await render();
    const t = text(host);
    expect(t).toContain('Olá, João!');
    expect(t).toContain('R$ 6,00 por mês por cliente');
    expect(t).toContain('farborastreadores.com.br/indicacao/joao-moto');
    expect(t).toContain('A receberR$ 14,00');
    expect(t).toContain('Já recebidoR$ 6,00');
    expect(t).toContain('12Cadastros pelo link');
    expect(t).toContain('3Clientes ativos');
    expect(t).toContain('nov/20261 clienteR$ 8,00Aguardando fechamento');
    expect(t).toContain('out/20263 clientesR$ 18,00A pagar');
    expect(t).toContain('set/20261 clienteR$ 6,00Pago');
    expect(t).not.toContain('pausado');
    // Fora dos buscadores e sem passar o link adiante.
    expect(document.head.querySelector('meta[name=robots]')?.getAttribute('content')).toBe('noindex, nofollow');
  });

  it('link trocado ou inválido', async () => {
    respond = async () => {
      throw new ApiError(404, 'link inválido');
    };
    const host = await render();
    expect(text(host)).toContain('Este link não vale mais');
  });
});

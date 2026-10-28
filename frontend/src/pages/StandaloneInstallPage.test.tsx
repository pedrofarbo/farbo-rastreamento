// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import type { InstallCheck, PublicInstallerLinkView } from '@/types';

const view = (over: Partial<PublicInstallerLinkView> = {}): PublicInstallerLinkView => ({
  imei: '869247061230055', deviceModel: 'TK910', vehicleName: '', plate: '', installerName: 'Zé Instalador',
  notes: 'Moto do João', status: 'open', completedAt: null, ...over,
});
const api = vi.hoisted(() => ({
  view: null as unknown,
  checks: [] as unknown[],
  complete: null as unknown,
  calls: [] as unknown[][],
}));
// Cada resposta é um valor ou um erro (lançado).
const answer = <T,>(value: unknown): T => {
  if (value instanceof Error) throw value;
  return value as T;
};
vi.mock('@/api/resources', () => ({
  installerLinkPublicApi: {
    view: async (token: string) => {
      api.calls.push(['view', token]);
      return answer(api.view);
    },
    check: async (token: string) => {
      api.calls.push(['check', token]);
      return answer<InstallCheck>(api.checks.shift());
    },
    complete: async (token: string, note: string) => {
      api.calls.push(['complete', token, note]);
      return answer(api.complete);
    },
  },
}));

import { StandaloneInstallPage } from './StandaloneInstallPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => document.body.textContent ?? '';
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === label)!;
const noSignal: InstallCheck = { found: true, otherVehicle: false, online: false, gpsFix: false, lastPosition: null, ready: false, message: 'O rastreador ainda não conectou.' };
const signal: InstallCheck = { ...noSignal, online: true, gpsFix: true, ready: true, message: 'Rastreador conectado e mandando a posição. Pode concluir.' };

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <MemoryRouter initialEntries={['/instalador/avulsa/tok123']}>
        <Routes>
          <Route path="/instalador/avulsa/:token" element={<StandaloneInstallPage />} />
        </Routes>
      </MemoryRouter>,
    ),
  );
  await vi.waitFor(() => expect(text()).not.toContain('Carregando'));
}

afterEach(() => {
  document.body.innerHTML = '';
  api.calls = [];
  api.checks = [];
});

describe('o link da instalação avulsa', () => {
  it('mostra o rastreador, só conclui com o sinal e o link vale até concluir', async () => {
    api.view = view({ vehicleName: 'CG 160', plate: 'ABC1D23' });
    api.checks = [noSignal, signal];
    api.complete = view({ status: 'done', completedAt: '2026-10-28T15:00:00Z' });
    await render();
    expect(document.title).toBe('Instalação · Farbo Rastreadores');
    expect(text()).toContain('Rastreador TK910');
    expect(text()).toContain('IMEI 869247061230055');
    expect(text()).toContain('CG 160 · ABC1D23');
    expect(text()).toContain('Moto do João');
    expect(button('Concluir a instalação').disabled).toBe(true);

    await act(async () => button('Testar o sinal').click());
    expect(text()).toContain('O rastreador ainda não conectou.');
    expect(button('Concluir a instalação').disabled).toBe(true);

    await act(async () => button('Testar de novo').click());
    expect(text()).toContain('Pode concluir.');
    const note = document.querySelector('textarea')!;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(note, 'Debaixo do banco');
      note.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await act(async () => button('Concluir a instalação').click());
    expect(api.calls).toContainEqual(['complete', 'tok123', 'Debaixo do banco']);
    expect(text()).toContain('Instalação concluída');
    expect(text()).toContain('Este link não vale mais.');
    expect(document.querySelectorAll('button')).toHaveLength(0);
  });

  it('link concluído ou encerrado: diz o motivo', async () => {
    api.view = new ApiError(410, 'a Farbo encerrou este link de instalação');
    await render();
    expect(text()).toContain('Este link não está mais ativo');
    expect(text()).toContain('A Farbo encerrou este link de instalação. Se precisar, fale com a Farbo.');
  });

  it('encerrado no meio do caminho: o teste mostra que o link parou', async () => {
    api.view = view();
    api.checks = [new ApiError(410, 'a Farbo encerrou este link de instalação')];
    await render();
    await act(async () => button('Testar o sinal').click());
    expect(text()).toContain('Este link não está mais ativo');
  });
});

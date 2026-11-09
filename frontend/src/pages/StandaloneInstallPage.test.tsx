// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import { clearInstallPin, storedInstallPin } from '@/services/installPin';
import type { InstallCheck, PublicInstallerLinkView } from '@/types';

const view = (over: Partial<PublicInstallerLinkView> = {}): PublicInstallerLinkView => ({
  imei: '869247061230055', deviceModel: 'TK910', vehicleName: '', plate: '', installerName: 'Zé Instalador',
  notes: 'Moto do João', status: 'open', completedAt: null, needsActivation: false, ...over,
});
const api = vi.hoisted(() => ({
  view: null as unknown,
  checks: [] as unknown[],
  complete: null as unknown,
  activation: null as unknown,
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
      // Um valor, um erro ou uma função do link (a senha guardada decide).
      return answer(typeof api.view === 'function' ? (api.view as (t: string) => unknown)(token) : api.view);
    },
    check: async (token: string) => {
      api.calls.push(['check', token]);
      return answer<InstallCheck>(api.checks.shift());
    },
    complete: async (token: string, note: string) => {
      api.calls.push(['complete', token, note]);
      return answer(api.complete);
    },
    stock: async () => ({ chips: [{ id: 'c-a', iccid: '89551012345678901233', carrier: 'Vivo' }] }),
    activation: async () => api.activation,
    activate: async (token: string, chipId: string) => {
      api.calls.push(['activate', token, chipId]);
      api.activation = {
        enabled: true, left: 2,
        activation: { imei: '869247061230055', iccid: '89551012345678901233', status: 'DONE', error: '', steps: [],
          waitingSince: null, connectedAt: null, finishedAt: null },
      };
      return api.activation;
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
  clearInstallPin('tok123');
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

  it('rastreador nunca ativado: escolhe o chip, ativa e só então testa', async () => {
    api.view = view({ needsActivation: true });
    api.activation = { enabled: true, left: 3, activation: null };
    api.checks = [signal];
    await render();
    expect(text()).toContain('Este rastreador ainda não foi ativado');
    expect(text()).not.toContain('Escolha o rastreador que você vai instalar');
    expect(button('Testar o sinal').disabled).toBe(true);
    const chip = Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.includes('89551012345678901233'))!;
    await act(async () => chip.click());
    await act(async () => button('Ativar o rastreador').click());
    expect(api.calls).toContainEqual(['activate', 'tok123', 'c-a']);
    expect(text()).toContain('Rastreador ativado e conectado');
    expect(button('Testar o sinal').disabled).toBe(false);
    await act(async () => button('Testar o sinal').click());
    expect(text()).toContain('Pode concluir.');
  });

  it('o link da central pede a senha: errada avisa e não fica guardada, a certa abre e fica', async () => {
    api.view = (token: string) => {
      const pin = storedInstallPin(token);
      if (!pin) return new ApiError(401, 'digite a senha', { code: 'PIN_REQUIRED' });
      if (pin !== '246810') return new ApiError(401, 'senha errada', { code: 'PIN_WRONG' });
      return view();
    };
    await render();
    expect(text()).toContain('Digite a senha');
    expect(text()).not.toContain('Rastreador TK910');
    expect(button('Abrir').disabled).toBe(true);
    const pin = () => document.querySelector('input[inputmode="numeric"]') as HTMLInputElement;
    const typePin = async (value: string) =>
      act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(pin(), value);
        pin().dispatchEvent(new Event('input', { bubbles: true }));
      });
    // Só números, até 6.
    await typePin('12a34567');
    expect(pin().value).toBe('123456');
    await act(async () => button('Abrir').click());
    await vi.waitFor(() => expect(text()).toContain('Senha errada'));
    expect(storedInstallPin('tok123')).toBe('');
    await typePin('246810');
    await act(async () => button('Abrir').click());
    await vi.waitFor(() => expect(text()).toContain('Rastreador TK910'));
    expect(storedInstallPin('tok123')).toBe('246810');
    expect(text()).not.toContain('Digite a senha');
  });

  it('senha errada vezes demais: o link trava e pede uma senha nova à Farbo', async () => {
    api.view = new ApiError(423, 'travado', { code: 'PIN_LOCKED' });
    await render();
    expect(text()).toContain('A senha foi digitada errada vezes demais. Peça uma senha nova à Farbo');
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

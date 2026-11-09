// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import { clearInstallPin, saveInstallPin, storedInstallPin } from '@/services/installPin';
import type { InstallActivationView, InstallCheck, InstallStock, PublicInstallView } from '@/types';

const view = (over: Partial<PublicInstallView> = {}): PublicInstallView => ({
  kind: 'install', vehicleName: 'Gol do Rui', plate: 'ATV1A11', vehicleDetails: '', customerName: 'Rui', customerPhone: '',
  installerName: 'Zé', scheduledFor: '2026-11-09T15:00:00Z', address: 'Rua A, 1', notes: '', status: 'arrived', imei: '',
  vehicleImei: '', completedAt: null, photos: [], ...over,
});
const STOCK: InstallStock = {
  devices: [
    { id: 'd-novo', imei: '869247061240112', model: 'J16', iccid: '', chipId: null, activated: false },
    { id: 'd-ativo', imei: '869247061240229', model: 'J16', iccid: '8955101234567890000', chipId: null, activated: true },
  ],
  chips: [
    { id: 'c-a', iccid: '89551012345678901233', carrier: 'Vivo' },
    { id: 'c-b', iccid: '89551012345678901241', carrier: 'Claro' },
  ],
};
const sending: InstallActivationView = {
  enabled: true, left: 2,
  activation: {
    imei: '869247061240112', iccid: '89551012345678901233', status: 'SENDING', error: '',
    steps: [{ label: 'APN da operadora', status: 'delivered' }, { label: 'Servidor da Farbo', status: 'sent' }],
    waitingSince: null, connectedAt: null, finishedAt: null,
  },
};
const done: InstallActivationView = { ...sending, activation: { ...sending.activation!, status: 'DONE' } };
const signal: InstallCheck = { found: true, otherVehicle: false, online: true, gpsFix: true, lastPosition: null, ready: true, message: 'Pode concluir.' };

const api = vi.hoisted(() => ({
  view: null as unknown,
  activation: null as unknown,
  calls: [] as unknown[][],
  check: null as unknown,
  stock: null as unknown,
  chip: null as unknown,
}));
vi.mock('@/api/resources', () => ({
  installLinkApi: {
    view: async () => api.view,
    stock: async () => api.stock ?? STOCK,
    chip: async (token: string, iccid: string) => {
      api.calls.push(['chip', token, iccid]);
      if (api.chip instanceof Error) throw api.chip;
      return api.chip;
    },
    activation: async () => {
      api.calls.push(['activation']);
      return api.activation;
    },
    activate: async (token: string, deviceId: string, chipId: string) => {
      api.calls.push(['activate', token, deviceId, chipId]);
      api.activation = sending;
      return sending;
    },
    check: async (token: string, imei: string) => {
      api.calls.push(['check', token, imei]);
      if (api.check instanceof Error) throw api.check;
      return signal;
    },
    photoUrl: () => '',
  },
}));

import { InstallerPage } from './InstallerPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => document.body.textContent ?? '';
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim().startsWith(label)) as HTMLButtonElement | undefined;
const input = (label: string) =>
  Array.from(document.querySelectorAll('label')).find((l) => l.textContent?.startsWith(label))!.querySelector('input')!;
async function type(el: HTMLInputElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <MemoryRouter initialEntries={['/instalador/tok123']}>
        <Routes>
          <Route path="/instalador/:token" element={<InstallerPage />} />
        </Routes>
      </MemoryRouter>,
    ),
  );
  await vi.waitFor(() => expect(text()).not.toContain('Carregando'));
}

afterEach(() => {
  vi.useRealTimers();
  document.body.innerHTML = '';
  clearInstallPin('tok123');
  api.calls = [];
  api.check = null;
  api.stock = null;
  api.chip = null;
});

describe('o link do instalador: a ativação antes de instalar', () => {
  it('escolhe o rastreador livre pelo IMEI, o chip pelo ICCID, ativa e só então testa o sinal', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    api.view = view();
    api.activation = { enabled: true, left: 3, activation: null };
    await render();
    expect(text()).toContain('Escolha o rastreador que você vai instalar');
    expect(text()).toContain('Precisa ativar');
    expect(text()).toContain('Ativado');
    expect(button('Testar o sinal')).toBeUndefined();
    expect(text()).toContain('escolha o rastreador (e ative, se precisar)');

    // Os últimos números do IMEI filtram; o escolhido pede o chip.
    await type(input('IMEI do rastreador'), '0112');
    expect(text()).not.toContain('869247061240229');
    await act(async () => button('869247061240112')!.click());
    expect(text()).toContain('Chip e ativação');
    expect(button('Ativar o rastreador')!.disabled).toBe(true);
    await type(input('ICCID do chip'), '1241');
    expect(text()).toContain('Claro');
    expect(text()).not.toContain('Vivo');
    await type(input('ICCID do chip'), '');
    await act(async () => button('89551012345678901233')!.click());
    await act(async () => button('Ativar o rastreador')!.click());
    expect(api.calls).toContainEqual(['activate', 'tok123', 'd-novo', 'c-a']);
    expect(text()).toContain('Mandando a configuração por SMS para o chip');
    expect(text()).toContain('APN da operadora');
    expect(button('Ativação em andamento')!.disabled).toBe(true);
    expect(button('Testar o sinal')).toBeUndefined();

    // A tela acompanha: o rastreador conectou.
    api.activation = done;
    await act(async () => {
      vi.advanceTimersByTime(5000);
    });
    await vi.waitFor(() => expect(text()).toContain('Rastreador ativado e conectado'));
    await act(async () => button('Testar o sinal')!.click());
    expect(api.calls).toContainEqual(['check', 'tok123', '869247061240112']);
    expect(text()).toContain('Pode concluir.');
  });

  it('o chip encaixado na hora, fora da lista (a do representante): aparece pelo ICCID inteiro e ativa com ele', async () => {
    api.view = view();
    api.stock = { devices: [{ id: 'd-kit', imei: '869247061250111', model: 'J16', iccid: '', chipId: null, activated: false }], chips: [] };
    api.activation = { enabled: true, left: 3, activation: null };
    api.chip = { id: 'c-mao', iccid: '89551012345678901308', carrier: 'Claro' };
    await render();
    await act(async () => button('869247061250111')!.click());
    expect(text()).toContain('Digite o ICCID inteiro, impresso no chip que você encaixou no rastreador.');
    expect(button('Ativar o rastreador')!.disabled).toBe(true);
    // Pela metade, não procura.
    await type(input('ICCID do chip'), '895510123456789013');
    await new Promise((resolve) => setTimeout(resolve, 600));
    expect(api.calls.some((c) => c[0] === 'chip')).toBe(false);
    await type(input('ICCID do chip'), '89551012345678901308');
    await vi.waitFor(() => expect(text()).toContain('Chip escolhido: 89551012345678901308 · Claro'));
    expect(api.calls).toContainEqual(['chip', 'tok123', '89551012345678901308']);
    await act(async () => button('Ativar o rastreador')!.click());
    expect(api.calls).toContainEqual(['activate', 'tok123', 'd-kit', 'c-mao']);
  });

  it('o chip encaixado que não está cadastrado: diz o motivo e não ativa', async () => {
    api.view = view();
    api.activation = { enabled: true, left: 3, activation: null };
    api.chip = new ApiError(404, 'Este chip não está cadastrado na Farbo. Confira os números do ICCID ou fale com a Farbo.');
    await render();
    await act(async () => button('869247061240112')!.click());
    // Com a lista da central: os números que não batem com ela dizem como achar o de fora.
    await type(input('ICCID do chip'), '777');
    expect(text()).toContain('Chip fora da lista? Digite o ICCID inteiro impresso nele.');
    await type(input('ICCID do chip'), '89551012345678901324');
    await vi.waitFor(() => expect(text()).toContain('Este chip não está cadastrado na Farbo'));
    expect(button('Ativar o rastreador')!.disabled).toBe(true);
  });

  it('o rastreador já ativado vai direto para o teste, sem chip nem ativação', async () => {
    api.view = view();
    api.activation = { enabled: true, left: 3, activation: null };
    await render();
    await type(input('IMEI do rastreador'), '869247061240229');
    expect(text()).toContain('Rastreador já ativado');
    expect(text()).not.toContain('Chip e ativação');
    await act(async () => button('Testar o sinal')!.click());
    expect(api.calls).toContainEqual(['check', 'tok123', '869247061240229']);
  });

  it('a página recarregada no meio: volta com a ativação em andamento', async () => {
    api.view = view();
    api.activation = sending;
    await render();
    expect(text()).toContain('Mandando a configuração por SMS para o chip');
    // O rastreador da ativação já vem escolhido (dá para trocar).
    expect(text()).toContain('869247061240112J16Trocar');
    expect(input('ICCID do chip').value).toBe('89551012345678901233');
  });

  it('o veículo com o rastreador da central: instala o dele e testa', async () => {
    api.view = view({ vehicleImei: '869247061240559' });
    api.activation = { enabled: true, left: 3, activation: null };
    await render();
    expect(text()).toContain('Este veículo já tem o rastreador 869247061240559');
    expect(text()).not.toContain('Escolha o rastreador');
    await act(async () => button('Testar o sinal')!.click());
    expect(api.calls).toContainEqual(['check', 'tok123', '869247061240559']);
  });

  it('a central trocou a senha no meio: a próxima chamada volta à tela da senha', async () => {
    saveInstallPin('tok123', '135790');
    api.view = view({ vehicleImei: '869247061240559' });
    api.activation = { enabled: true, left: 3, activation: null };
    api.check = new ApiError(401, 'senha errada', { code: 'PIN_WRONG' });
    await render();
    await act(async () => button('Testar o sinal')!.click());
    expect(text()).toContain('Digite a senha');
    expect(text()).toContain('Senha errada: confira com a Farbo.');
    expect(storedInstallPin('tok123')).toBe('');
  });
});

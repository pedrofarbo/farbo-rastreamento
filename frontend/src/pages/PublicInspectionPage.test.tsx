// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import type { PublicInspection } from '@/types';

const SLOTS = [
  { id: 'front', label: 'Frente', hint: 'O veículo inteiro, de frente' },
  { id: 'rear', label: 'Traseira', hint: 'O veículo inteiro, de trás' },
  { id: 'damage_extra', label: 'Outra avaria', hint: 'Se houver', optional: true },
];
const view = (over: Partial<PublicInspection> = {}): PublicInspection => ({
  organizationName: 'Rota Segura', kind: 'entry', mode: 'self', vehicleType: 'car', vehicleName: 'Gol prata', plate: 'ABC1D23',
  firstName: 'Carla', status: 'pending', expired: false, expiresAt: '2026-11-22T12:00:00Z', requestNote: '', submittedAt: null,
  slots: SLOTS, photos: [{ slot: 'front', takenAt: '2026-11-15T12:00:00Z' }], ...over,
});

const api = vi.hoisted(() => ({ view: null as unknown, calls: [] as unknown[][] }));
vi.mock('@/api/resources', () => ({
  inspectionLinkApi: {
    view: async () => {
      if (api.view instanceof Error) throw api.view;
      return api.view;
    },
    savePhoto: async () => undefined,
    photoUrl: () => '',
    submit: async (token: string, damages: string) => {
      api.calls.push(['submit', token, damages]);
      return { ...(api.view as PublicInspection), status: 'submitted', submittedAt: '2026-11-15T12:10:00Z' };
    },
  },
}));

import { PublicInspectionPage } from './PublicInspectionPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim().startsWith(label)) as HTMLButtonElement | undefined;
/** O ângulo (o botão começa com a foto ou o ícone). */
const slotButton = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.includes(label)) as HTMLButtonElement | undefined;

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <MemoryRouter initialEntries={['/vistoria/tok123']}>
        <Routes>
          <Route path="/vistoria/:token" element={<PublicInspectionPage />} />
        </Routes>
      </MemoryRouter>,
    ),
  );
  await vi.waitFor(() => expect(text()).not.toContain('Abrindo a vistoria'));
}

afterEach(() => {
  document.body.innerHTML = '';
  api.calls = [];
});

describe('o link da vistoria', () => {
  it('mostra o roteiro, pede as fotos que faltam e a declaração antes de enviar', async () => {
    api.view = view();
    await render();
    expect(text()).toContain('Rota Segura · vistoria de adesão');
    expect(text()).toContain('Olá, Carla!');
    expect(text()).toContain('Fotos · 1 de 2 fotos');
    expect(text()).toContain('Outra avaria (se houver)');
    expect(text()).toContain('Faltam as fotos: traseira.');
    expect(button('Enviar a vistoria')!.disabled).toBe(true);

    // Sem a câmera no navegador (o jsdom), o seletor de arquivos fica como último recurso.
    await act(async () => slotButton('Traseira')!.click());
    expect(text()).toContain('Este navegador não abre a câmera por aqui');
    expect(text()).toContain('A foto escolhida assim fica marcada');
    await act(async () => button('Voltar')!.click());
    expect(text()).not.toContain('Este navegador não abre a câmera');
  });

  it('com todas as fotos e a declaração, envia com as avarias', async () => {
    api.view = view({ photos: [{ slot: 'front', takenAt: '' }, { slot: 'rear', takenAt: '' }] });
    await render();
    const send = button('Enviar a vistoria')!;
    expect(send.disabled).toBe(true);
    const checkbox = document.querySelector('input[type="checkbox"]') as HTMLInputElement;
    expect(checkbox.parentElement!.textContent).toContain('Declaro que as fotos são do veículo ABC1D23');
    const area = document.querySelector('textarea') as HTMLTextAreaElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(area, 'Risco na porta');
      area.dispatchEvent(new Event('input', { bubbles: true }));
      checkbox.click();
    });
    expect(send.disabled).toBe(false);
    await act(async () => send.click());
    expect(api.calls).toEqual([['submit', 'tok123', 'Risco na porta']]);
    expect(text()).toContain('Vistoria enviada');
    expect(text()).toContain('este mesmo link volta a abrir');
  });

  it('o técnico declara que fez a vistoria; sem o nome do associado', async () => {
    api.view = view({ mode: 'technician' });
    await render();
    expect(text()).toContain('vistoria de adesão com técnico');
    expect(text()).not.toContain('Olá, Carla');
    expect(text()).toContain('Declaro que fiz pessoalmente a vistoria do veículo ABC1D23');
  });

  it('o link vencido, o cancelado e o que não existe', async () => {
    api.view = view({ expired: true });
    await render();
    expect(text()).toContain('O link venceu. Peça um novo a Rota Segura.');
    expect(button('Enviar a vistoria')).toBeUndefined();
    document.body.innerHTML = '';

    api.view = view({ status: 'canceled' });
    await render();
    expect(text()).toContain('A vistoria foi cancelada por Rota Segura.');
    document.body.innerHTML = '';

    api.view = new ApiError(404, 'link não encontrado');
    await render();
    expect(text()).toContain('Link não encontrado: confira o endereço que você recebeu.');
  });
});

// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { AdhesionInput, PublicAdhesion } from '@/types';

const view = (over: Partial<PublicAdhesion> = {}): PublicAdhesion => ({
  organizationName: 'Porta Aberta', status: 'pending', firstName: 'Carla', plate: 'ABC1D23', vehicleName: 'Gol',
  inspectionStatus: 'pending', inspectionLink: 'https://painel/vistoria/tok', inspectionExpired: false, inspectionNote: '',
  acceptedAt: null, installPreference: '', reviewNote: '', ...over,
});

const api = vi.hoisted(() => ({ view: null as unknown, calls: [] as unknown[][] }));
vi.mock('@/api/resources', () => ({
  adhesionLinkApi: {
    view: async () => api.view,
    accept: async (token: string, version: string, preference: string) => {
      api.calls.push(['accept', token, version, preference]);
      return { ...(api.view as PublicAdhesion), status: 'submitted', acceptedAt: '2026-11-16T12:00:00Z', installPreference: preference };
    },
  },
  contractApi: {
    current: async () => ({ version: 'A3', effectiveDate: '', title: 'Termo do associado', intro: [], sections: [], sha256: 'x', changes: '' }),
  },
}));

import { PublicAdhesionFollowPage, adhesionProblem } from './PublicAdhesionPage';
import { adhesionInvite, adhesionStep } from './org/OrgAdhesionsPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim().startsWith(label)) as HTMLButtonElement | undefined;

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <MemoryRouter initialEntries={['/adesao/acompanhar/seg']}>
        <Routes>
          <Route path="/adesao/acompanhar/:token" element={<PublicAdhesionFollowPage />} />
        </Routes>
      </MemoryRouter>,
    ),
  );
  await vi.waitFor(() => expect(text()).not.toContain('Abrindo'));
}

afterEach(() => {
  document.body.innerHTML = '';
  api.calls = [];
});

describe('a adesão pelo link', () => {
  const draft: AdhesionInput = {
    name: 'Carla Nova', document: '529.982.247-25', email: 'carla@nova.test', phone: '(34) 98888-1234',
    address: { zipCode: '38400-000', street: 'Rua A', number: '1', complement: '', district: 'Centro', city: 'Uberlândia', state: 'MG' },
    vehicleType: 'car', plate: 'abc-1d23', brand: 'VW', model: 'Gol', year: 2015, color: '',
  };
  it('confere o formulário', () => {
    expect(adhesionProblem(draft)).toBeNull();
    expect(adhesionProblem({ ...draft, name: 'Carla' })).toMatch(/nome completo/);
    expect(adhesionProblem({ ...draft, document: '111.111.111-11' })).toMatch(/CPF/);
    expect(adhesionProblem({ ...draft, address: { ...draft.address, number: '' } })).toMatch(/endereço/);
    expect(adhesionProblem({ ...draft, plate: 'AB1' })).toMatch(/placa/);
    expect(adhesionProblem({ ...draft, model: ' ' })).toMatch(/modelo/);
  });

  it('o acompanhamento: primeiro a vistoria; o termo só depois dela', async () => {
    api.view = view();
    await render();
    expect(text()).toContain('Olá, Carla!');
    const link = Array.from(document.querySelectorAll('a')).find((a) => a.textContent === 'Fazer a vistoria');
    expect(link?.getAttribute('href')).toBe('https://painel/vistoria/tok');
    expect(text()).toContain('Depois da vistoria.');
    expect(button('Enviar a adesão')).toBeUndefined();
  });

  it('com a vistoria enviada: o termo, a preferência e o envio', async () => {
    api.view = view({ inspectionStatus: 'submitted' });
    await render();
    expect(text()).toContain('Vistoria enviada.');
    const send = button('Enviar a adesão')!;
    expect(send.disabled).toBe(true);
    await act(async () => button('Ler o termo do associado (versão A3)')!.click());
    expect(text()).toContain('Termo do associado');
    const area = document.querySelector('textarea') as HTMLTextAreaElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(area, 'Sábado de manhã');
      area.dispatchEvent(new Event('input', { bubbles: true }));
      (document.querySelector('input[type="checkbox"]') as HTMLInputElement).click();
    });
    await act(async () => send.click());
    expect(api.calls).toEqual([['accept', 'seg', 'A3', 'Sábado de manhã']]);
    expect(text()).toContain('Preferência para a instalação: Sábado de manhã.');
    expect(text()).toContain('Tudo enviado.');
  });

  it('aprovada e recusada', async () => {
    api.view = view({ status: 'approved', installPreference: 'à tarde' });
    await render();
    expect(text()).toContain('Adesão aprovada');
    expect(text()).toContain('Boas-vindas à Porta Aberta!');
    document.body.innerHTML = '';
    api.view = view({ status: 'rejected', reviewNote: 'Fora da área.' });
    await render();
    expect(text()).toContain('A adesão não foi aprovada por Porta Aberta. Motivo: Fora da área.');
  });

  it('no painel: o que falta e o convite para divulgar', () => {
    expect(adhesionStep({ status: 'pending', acceptedAt: null, inspection: null })).toBe('falta a vistoria');
    expect(adhesionStep({ status: 'pending', acceptedAt: null, inspection: { status: 'submitted' } as never })).toBe('falta o termo');
    expect(adhesionStep({ status: 'submitted', acceptedAt: 'x', inspection: null })).toBe('');
    expect(adhesionInvite('Porta Aberta', 'https://l')).toContain('entrar na Porta Aberta com rastreamento');
  });
});

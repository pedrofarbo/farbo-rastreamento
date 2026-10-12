// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { MaintenanceItem } from '@/types';

const calls: unknown[] = [];
let odometerKm: number | null = null;
let engineHours: number | null = null;
const tires: MaintenanceItem = {
  id: 'i1', vehicleId: 'v1', vehicleName: 'Van 01', plate: 'NOR1A11', groupId: null, name: 'Pneus', everyKm: 40000, everyMonths: null,
  everyHours: null, lastKm: null, lastHours: 80, lastDate: '2026-09-01', nextKm: null, nextHours: null, nextDate: null, remainingKm: null,
  remainingHours: null, remainingDays: null, status: 'unknown',
};
const filter: MaintenanceItem = {
  ...tires, id: 'i2', name: 'Filtro de ar', everyKm: null, everyHours: 250, nextHours: 330, remainingHours: 40, status: 'soon',
};
let items: MaintenanceItem[] = [tires];
const odometer = () => ({
  vehicleId: 'v1', trackedKm: 1234.5, km: odometerKm, setAt: odometerKm ? '2026-10-12T10:00:00Z' : null,
  trackedHours: 290.2, hours: engineHours ?? 290.2, hoursSetAt: engineHours ? '2026-10-12T10:00:00Z' : null,
});
vi.mock('@/api/resources', () => ({
  maintenanceApi: {
    vehicle: async () => ({
      odometer: odometer(),
      items,
      records: [{ id: 'r1', vehicleId: 'v1', itemId: null, name: 'Troca de óleo', doneDate: '2026-08-02', km: 39000, hours: 70, notes: 'Oficina do João', createdAt: '' }],
    }),
    setOdometer: async (id: string, km: number) => {
      calls.push(['odometer', id, km]);
      odometerKm = km;
      return odometer();
    },
    setEngineHours: async (id: string, hours: number) => {
      calls.push(['engine-hours', id, hours]);
      engineHours = hours;
      return odometer();
    },
    done: async (input: unknown) => {
      calls.push(['done', input]);
      return { done: 1 };
    },
  },
}));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => undefined }) }));

import { VehicleMaintenance } from './VehicleMaintenance';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;
const field = (label: string) => {
  const l = Array.from(document.querySelectorAll('label')).find((x) => x.textContent?.startsWith(label));
  return document.getElementById(l?.htmlFor ?? '') as HTMLInputElement;
};
function type(el: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(el, value);
  el.dispatchEvent(new Event('input', { bubbles: true }));
}

async function render(canEdit: boolean) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <VehicleMaintenance vehicle={{ id: 'v1', name: 'Van 01' }} canEdit={canEdit} />
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
  odometerKm = null;
  engineHours = null;
  items = [tires];
});

describe('manutenção do veículo', () => {
  it('pede o hodômetro, registra a feita com o km e mostra o histórico', async () => {
    await render(true);
    expect(text()).toContain('não informado');
    expect(text()).toContain('O GPS mediu 1.235 km');
    expect(text()).toContain('informe o hodômetro do veículo para contar os km');
    expect(text()).toContain('Troca de óleo · 39.000 km · Oficina do João');
    // Nada vence por horas: o horímetro não aparece.
    expect(text()).not.toContain('Horímetro');

    await act(async () => button('Informar').click());
    await act(async () => type(field('Hodômetro do veículo agora'), '48.312'));
    await act(async () => button('Salvar').click());
    await flush();
    expect(calls).toEqual([['odometer', 'v1', 48312]]);
    expect(text()).toContain('48.312 km');

    calls.length = 0;
    await act(async () => button('Registrar feita').click());
    await act(async () => type(field('Com quantos km'), '48300'));
    await act(async () => type(field('Observação'), 'Pneus dianteiros'));
    await act(async () => button('Registrar').click());
    await flush();
    expect(calls).toEqual([['done', { itemIds: ['i1'], date: expect.any(String), km: 48300, hours: null, notes: 'Pneus dianteiros' }]]);
  });

  it('com manutenção por horas, mostra o horímetro, aceita a leitura e registra as horas', async () => {
    items = [filter, tires];
    await render(true);
    expect(text()).toContain('Horímetro290 h');
    expect(text()).toContain('Horas de motor medidas pelo rastreador');
    expect(text()).toContain('A cada 250 h de motor · última em');
    expect(text()).toContain('com 80 h');
    expect(text()).toContain('faltam 40 h de motor');
    expect(text()).toContain('Troca de óleo · 39.000 km · 70 h · Oficina do João');

    // O segundo Informar é o do horímetro.
    const inform = Array.from(document.querySelectorAll('button')).filter((b) => b.textContent === 'Informar');
    await act(async () => inform[1].click());
    expect(field('Horímetro do veículo agora').value).toBe('290');
    await act(async () => type(field('Horímetro do veículo agora'), '5.000'));
    await act(async () => button('Salvar').click());
    await flush();
    expect(calls).toEqual([['engine-hours', 'v1', 5000]]);
    expect(text()).toContain('5.000 h');
    expect(text()).toContain('mais as horas de motor medidas');

    calls.length = 0;
    await act(async () => Array.from(document.querySelectorAll('button')).find((b) => b.textContent === 'Registrar feita')?.click());
    expect(text()).toContain('destas horas de motor');
    await act(async () => type(field('Com quantas horas de motor'), '4.990'));
    await act(async () => button('Registrar').click());
    await flush();
    expect(calls).toEqual([['done', { itemIds: ['i2'], date: expect.any(String), km: null, hours: 4990, notes: '' }]]);
  });

  it('quem só acompanha vê, sem botões', async () => {
    await render(false);
    expect(text()).toContain('Pneus');
    expect(button('Registrar feita')).toBeUndefined();
    expect(button('Nova manutenção')).toBeUndefined();
    expect(button('Informar')).toBeUndefined();
  });
});

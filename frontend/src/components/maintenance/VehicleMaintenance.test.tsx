// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { MaintenanceItem } from '@/types';

const calls: unknown[] = [];
let odometerKm: number | null = null;
const oil: MaintenanceItem = {
  id: 'i1', vehicleId: 'v1', vehicleName: 'Van 01', plate: 'NOR1A11', groupId: null, name: 'Pneus', everyKm: 40000, everyMonths: null,
  lastKm: null, lastDate: '2026-09-01', nextKm: null, nextDate: null, remainingKm: null, remainingDays: null, status: 'unknown',
};
vi.mock('@/api/resources', () => ({
  maintenanceApi: {
    vehicle: async () => ({
      odometer: { vehicleId: 'v1', trackedKm: 1234.5, km: odometerKm, setAt: odometerKm ? '2026-10-12T10:00:00Z' : null },
      items: [oil],
      records: [{ id: 'r1', vehicleId: 'v1', itemId: null, name: 'Troca de óleo', doneDate: '2026-08-02', km: 39000, notes: 'Oficina do João', createdAt: '' }],
    }),
    setOdometer: async (id: string, km: number) => {
      calls.push(['odometer', id, km]);
      odometerKm = km;
      return { vehicleId: id, trackedKm: 1234.5, km, setAt: '2026-10-12T10:00:00Z' };
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
});

describe('manutenção do veículo', () => {
  it('pede o hodômetro, registra a feita com o km e mostra o histórico', async () => {
    await render(true);
    expect(text()).toContain('não informado');
    expect(text()).toContain('O GPS mediu 1.235 km');
    expect(text()).toContain('informe o hodômetro do veículo para contar os km');
    expect(text()).toContain('Troca de óleo · 39.000 km · Oficina do João');

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
    expect(calls).toEqual([['done', { itemIds: ['i1'], date: expect.any(String), km: 48300, notes: 'Pneus dianteiros' }]]);
  });

  it('quem só acompanha vê, sem botões', async () => {
    await render(false);
    expect(text()).toContain('Pneus');
    expect(button('Registrar feita')).toBeUndefined();
    expect(button('Nova manutenção')).toBeUndefined();
    expect(button('Informar')).toBeUndefined();
  });
});

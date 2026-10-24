// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { MaintenanceItem } from '@/types';

const calls: unknown[] = [];
const item = (over: Partial<MaintenanceItem>): MaintenanceItem => ({
  id: 'i', vehicleId: 'v1', vehicleName: 'Van 01', plate: 'NOR1A11', groupId: 'g1', kind: 'service', name: 'Troca de óleo', everyKm: 10000,
  everyMonths: 6, everyHours: null, lastKm: 40000, lastHours: 900, lastDate: '2026-08-01', nextKm: 50000, nextHours: null,
  nextDate: '2027-02-01', remainingKm: 7000, remainingHours: null, remainingDays: 112, status: 'ok', ...over,
});
const ITEMS: MaintenanceItem[] = [
  item({ id: 'i1', status: 'due', remainingKm: -320, remainingDays: 100 }),
  item({ id: 'i2', vehicleId: 'v2', vehicleName: 'Carro 02', plate: 'SUL2B22', groupId: 'g2', name: 'Revisão', everyKm: null, everyMonths: 1, remainingKm: null, remainingDays: 8, status: 'soon' }),
  item({ id: 'i3', vehicleId: 'v3', vehicleName: 'Moto 03', plate: '', groupId: null, name: 'Pneus' }),
  item({ id: 'i4', vehicleId: 'v4', vehicleName: 'Van 04', name: 'Filtro de ar', everyKm: null, everyMonths: null, everyHours: 250,
    remainingKm: null, remainingDays: null, remainingHours: 40, status: 'soon' }),
];
vi.mock('@/api/resources', () => ({
  maintenanceApi: {
    list: async () => ({ items: ITEMS, counts: { due: 1, soon: 1, ok: 1 } }),
    create: async (input: unknown) => {
      calls.push(['create', input]);
      return { created: 2 };
    },
    done: async (input: unknown) => {
      calls.push(['done', input]);
      return { done: 2 };
    },
    update: async () => undefined,
  },
  vehiclesApi: {
    list: async () => [
      { id: 'v1', name: 'Van 01', plate: 'NOR1A11', groupId: 'g1' },
      { id: 'v4', name: 'Van 04', plate: 'NOR4D44', groupId: 'g1' },
      { id: 'v2', name: 'Carro 02', plate: 'SUL2B22', groupId: 'g2' },
      { id: 'v9', name: 'De outro', plate: 'OUT9Z99', groupId: null, shared: { shareId: 's', ownerName: 'X', canBlock: false } },
    ],
  },
}));
vi.mock('@/hooks/useFleet', () => ({
  useFleet: () => ({ groups: [{ id: 'g1', name: 'Norte', vehicles: 2 }, { id: 'g2', name: 'Sul', vehicles: 1 }] }),
  fieldLabel: () => '',
}));
vi.mock('@/stores/AuthContext', () => ({ useAuth: () => ({ canOperateAccount: true, isCustomer: true }) }));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => undefined }) }));

import { MaintenancePage } from './MaintenancePage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string | RegExp) =>
  Array.from(document.querySelectorAll('button')).find((b) =>
    typeof label === 'string' ? b.textContent === label : label.test(b.textContent ?? ''),
  ) as HTMLButtonElement;
const field = (label: string) => {
  const l = Array.from(document.querySelectorAll('label')).find((x) => x.textContent?.startsWith(label));
  return document.getElementById(l?.htmlFor ?? '') as HTMLInputElement | HTMLSelectElement;
};
function type(el: HTMLInputElement | HTMLSelectElement, value: string) {
  const proto = el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, 'value')?.set?.call(el, value);
  el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('manutenção da frota', () => {
  it('mostra o que venceu primeiro, filtra e registra várias de uma vez', async () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter>
            <MaintenancePage />
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    );
    await flush();
    expect(text()).toContain('passou 320 km');
    expect(text()).toContain('faltam 8 dias');
    expect(text()).toContain('A cada 250 h de motor');
    expect(text()).toContain('faltam 40 h de motor');

    // O contador filtra.
    await act(async () => button(/^1Vencidas$/).click());
    expect(text()).not.toContain('Revisão');
    await act(async () => button(/^1Vencidas$/).click());

    // Duas escolhidas, registradas de uma vez (cada uma com o hodômetro do veículo).
    const checks = document.querySelectorAll<HTMLInputElement>('input[type=checkbox]');
    await act(async () => checks[0].click());
    await act(async () => checks[1].click());
    await act(async () => button('Registrar feitas').click());
    expect(text()).toContain('Registrar 2 manutenções');
    expect(text()).not.toContain('Com quantos km');
    expect(text()).not.toContain('Com quantas horas de motor');
    await act(async () => button('Registrar').click());
    await flush();
    expect(calls).toEqual([['done', { itemIds: ['i1', 'i2'], date: expect.any(String), km: null, hours: null, notes: '' }]]);

    // Nova manutenção num grupo: os veículos dele (o compartilhado por outro não entra).
    calls.length = 0;
    await act(async () => button('Nova manutenção').click());
    expect(text()).toContain('Toda a frota (3)');
    await act(async () => type(field('O que fazer') as HTMLInputElement, 'Troca de óleo'));
    await act(async () => type(field('A cada (km)') as HTMLInputElement, '10.000'));
    await act(async () => type(field('Ou a cada (horas de motor)') as HTMLInputElement, '500'));
    // Em vários veículos, cada um conta do próprio horímetro: sem o campo das horas.
    expect(text()).not.toContain('Com quantas horas de motor');
    const group = Array.from(document.querySelectorAll<HTMLInputElement>('input[name="maintenance-scope"]'))[1];
    await act(async () => group.click());
    await act(async () => button('Criar em 2 veículos').click());
    await flush();
    expect(calls).toEqual([
      ['create', {
        name: 'Troca de óleo', everyKm: 10000, everyMonths: null, everyHours: 500, lastDate: expect.any(String), lastKm: null,
        lastHours: null, vehicleIds: ['v1', 'v4'],
      }],
    ]);
  });
});

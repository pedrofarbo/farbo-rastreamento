// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { InstallationAppointment, PartnerQueue } from '@/types';

const calls = vi.hoisted(() => [] as unknown[][]);
const state = vi.hoisted(() => ({ queue: null as PartnerQueue | null }));
vi.mock('@/api/resources', () => ({
  publicApi: {
    partnerInstallations: async () => state.queue,
    partnerSchedule: async (token: string, input: unknown) => {
      calls.push(['schedule', token, input]);
      return {};
    },
    partnerReschedule: async (token: string, id: string, input: unknown) => {
      calls.push(['reschedule', token, id, input]);
      return {};
    },
    partnerCancel: async (token: string, id: string) => {
      calls.push(['cancel', token, id]);
      return {};
    },
  },
}));

import { InstallationsSection, whenLabel } from './InstallationsSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => document.body.textContent ?? '';
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === label)!;

const appointment: InstallationAppointment = {
  id: 'ap1', kind: 'install', vehicleId: 'v2', vehicleName: 'Moto da Bia', plate: 'BIA1A11', vehicleDetails: 'Honda CG',
  customerId: 'c2', customerName: 'Bia Lima', customerPhone: '(11) 94444-3333', organizationName: '', installerId: null,
  installerName: 'Rui', scheduledFor: '2026-11-05T17:00:00Z', durationMin: 60, address: 'Rua B 2, Centro, São Paulo/SP',
  notes: 'portão azul', status: 'scheduled', arrivedAt: null, deviceId: null, imei: '', completedAt: null, installerNote: '',
  photos: [], createdAt: '2026-11-03T12:00:00Z', url: 'https://painel.farbo.test/instalador/abc',
};

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () => createRoot(host).render(<InstallationsSection token="tok" />));
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('as instalações dos clientes do representante', () => {
  it('mostra os agendados (com o link do instalador) e as filas; agenda, remarca e cancela', async () => {
    state.queue = {
      awaitingInstall: [
        { vehicleId: 'v1', vehicleName: 'Uno da Ana', plate: '', vehicleDetails: 'Fiat Uno', customerName: 'Ana Souza',
          customerPhone: '(11) 95555-0000', address: 'Rua A 1, Centro, São Paulo/SP', imei: '', since: '2026-11-01T12:00:00Z' },
      ],
      awaitingUninstall: [],
      appointments: [appointment],
    };
    await render();
    expect(text()).toContain('Moto da Bia · BIA1A11');
    // O dia e a hora no horário de Brasília (17h UTC = 14h).
    expect(whenLabel('2026-11-05T17:00:00Z')).toMatch(/^qui\.?, 05\/11 às 14:00$/);
    expect(text()).toContain(`Instalação · ${whenLabel('2026-11-05T17:00:00Z')}`);
    expect(text()).toContain('Obs.: portão azul');
    expect(document.querySelector('a[href="https://painel.farbo.test/instalador/abc"]')).not.toBeNull();
    expect(document.querySelector('a[href="tel:+5511955550000"]')).not.toBeNull();
    expect(text()).toContain('Nenhum rastreador para retirar.');

    // Agenda a instalação da Ana.
    await act(async () => button('Agendar a instalação').click());
    const when = document.querySelector('input[type="datetime-local"]') as HTMLInputElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(when, '2026-11-06T09:30');
      when.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await act(async () => button('Agendar').click());
    await flush();
    expect(calls[0]).toEqual([
      'schedule', 'tok', { kind: 'install', vehicleId: 'v1', scheduledFor: new Date('2026-11-06T09:30').toISOString(), notes: '' },
    ]);

    // Remarca (com o horário de antes no campo) e cancela.
    await act(async () => button('Remarcar').click());
    expect((document.querySelector('input[type="datetime-local"]') as HTMLInputElement).value).not.toBe('');
    await act(async () => button('Salvar o novo horário').click());
    await flush();
    expect(calls[1][0]).toBe('reschedule');
    expect(calls[1][3]).toMatchObject({ notes: 'portão azul' });
    await act(async () => button('Cancelar').click());
    await flush();
    expect(calls[2]).toEqual(['cancel', 'tok', 'ap1']);
  });
});

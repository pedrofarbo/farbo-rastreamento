import { describe, expect, it } from 'vitest';

import type { InstallationAppointment } from '@/types';

import { dayKey, groupByDay, installerMessage, isOpen, statusOf } from './installations';

const appt = (over: Partial<InstallationAppointment>): InstallationAppointment => ({
  id: crypto.randomUUID(),
  kind: 'install',
  vehicleId: 'v',
  vehicleName: 'Onix',
  plate: 'ABC1D23',
  vehicleDetails: 'Chevrolet Onix 2022',
  customerId: 'c',
  customerName: 'Lia Souza',
  customerPhone: '(11) 99999-0000',
  organizationName: '',
  installerId: null,
  installerName: 'Caio Instalador',
  scheduledFor: new Date(2026, 9, 24, 14, 30).toISOString(),
  durationMin: 60,
  address: 'Rua A, 10',
  notes: '',
  status: 'scheduled',
  arrivedAt: null,
  deviceId: null,
  imei: '',
  completedAt: null,
  installerNote: '',
  photos: [],
  createdAt: new Date().toISOString(),
  ...over,
});

describe('a agenda de instalação', () => {
  it('agrupa por dia, em ordem de horário', () => {
    const late = appt({ scheduledFor: new Date(2026, 9, 24, 16, 0).toISOString() });
    const early = appt({ scheduledFor: new Date(2026, 9, 24, 9, 0).toISOString() });
    const next = appt({ scheduledFor: new Date(2026, 9, 25, 8, 0).toISOString() });
    const days = groupByDay([next, late, early]);
    expect(days.map((d) => d.day)).toEqual(['2026-10-24', '2026-10-25']);
    expect(days[0].items).toEqual([early, late]);
    expect(dayKey(new Date(2026, 0, 5, 23, 59))).toBe('2026-01-05');
  });

  it('em aberto: agendada ou com o instalador no local', () => {
    expect(isOpen({ status: 'scheduled' })).toBe(true);
    expect(isOpen({ status: 'arrived' })).toBe(true);
    expect(isOpen({ status: 'done' })).toBe(false);
    expect(isOpen({ status: 'no_show' })).toBe(false);
  });

  it('a mensagem ao instalador leva o quando, o veículo, o endereço e o link', () => {
    const text = installerMessage(appt({ url: 'https://farbo.test/instalador/abc', notes: 'Portão azul' }));
    expect(text).toContain('Olá, Caio!');
    expect(text).toContain('às 14:30');
    expect(text).toContain('Onix · ABC1D23 (Chevrolet Onix 2022)');
    expect(text).toContain('📍 Rua A, 10');
    expect(text).toContain('📝 Portão azul');
    expect(text).toContain('https://farbo.test/instalador/abc');
    expect(text).toContain('Instalação Farbo agendada');
  });

  it('a desinstalação: o IMEI a retirar na mensagem e "Retirado" quando conclui', () => {
    const text = installerMessage(appt({ kind: 'uninstall', imei: '869247061230045', url: 'https://farbo.test/instalador/xyz' }));
    expect(text).toContain('Desinstalação Farbo agendada');
    expect(text).toContain('Retirar o rastreador IMEI 869247061230045');
    expect(text).toContain('a foto do rastreador retirado');
    expect(statusOf({ kind: 'uninstall', status: 'done' }).label).toBe('Retirado');
    expect(statusOf({ kind: 'install', status: 'done' }).label).toBe('Instalado');
  });
});

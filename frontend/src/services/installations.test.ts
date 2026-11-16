import { describe, expect, it } from 'vitest';

import type { InstallationAppointment } from '@/types';

import {
  dayKey,
  groupByDay,
  installSteps,
  installerMessage,
  installerPinMessage,
  isOpen,
  showSteps,
  stallText,
  statusOf,
} from './installations';

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

  it('o link com senha: a mensagem avisa que a senha vai à parte, sem ela', () => {
    const text = installerMessage(appt({ url: 'https://farbo.test/instalador/abc', pin: '246810' }));
    expect(text).toContain('A senha para abrir o link vai em outra mensagem.');
    expect(text).not.toContain('246810');
    expect(installerMessage(appt({ url: 'https://farbo.test/instalador/abc' }))).not.toContain('senha');
    expect(installerPinMessage('246810')).toBe('Senha para abrir o link da Farbo: 246810');
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

describe('o andamento pelo link e a instalação parada', () => {
  const arrivedAt = new Date(2026, 10, 16, 14, 44).toISOString();
  const checkedAt = new Date(2026, 10, 16, 15, 49).toISOString();

  it('os passos: a chegada, o rastreador, o sinal, a foto e a conclusão', () => {
    const steps = (a: Partial<InstallationAppointment>) => installSteps(appt(a)).map((s) => `${s.done ? '✓' : '○'} ${s.label}`);
    expect(steps({})).toEqual(['○ Chegada', '○ Rastreador escolhido', '○ Teste do sinal', '○ Foto do rastreador', '○ Concluir']);
    expect(
      steps({ status: 'arrived', arrivedAt, trackerImei: '860752061230249', trackerConnected: true, checkedAt, checkReady: false }),
    ).toEqual([
      '✓ Chegou às 14:44',
      '✓ Rastreador final 230249 conectado',
      '○ Teste do sinal (o das 15:49 não passou)',
      '○ Foto do rastreador',
      '○ Concluir',
    ]);
    expect(steps({ status: 'arrived', arrivedAt, trackerImei: '860752061230249', checkedAt, checkReady: true, photos: ['tracker'] })).toEqual([
      '✓ Chegou às 14:44',
      '✓ Rastreador final 230249',
      '✓ Sinal ok às 15:49',
      '✓ Foto do rastreador',
      '○ Concluir',
    ]);
    expect(steps({ kind: 'uninstall', status: 'arrived', arrivedAt })).toEqual([
      '✓ Chegou às 14:44',
      '○ Foto do rastreador retirado',
      '○ Concluir',
    ]);
  });

  it('só mostra o andamento da que está aberta e já começou pelo link', () => {
    expect(showSteps(appt({}))).toBe(false);
    expect(showSteps(appt({ status: 'arrived', arrivedAt }))).toBe(true);
    expect(showSteps(appt({ checkedAt }))).toBe(true);
    expect(showSteps(appt({ status: 'done', arrivedAt }))).toBe(false);
  });

  it('o aviso da parada: para a central e para quem instala', () => {
    expect(stallText(appt({}))).toBe('');
    expect(stallText(appt({ stall: 'late' }))).toBe('Passou do horário e o link não registrou a chegada: confirme com o instalador.');
    expect(stallText(appt({ stall: 'late' }), true)).toContain('abra o link e siga os passos até “Concluir a instalação”');
    const stalled = appt({ status: 'arrived', stall: 'stalled', arrivedAt, trackerImei: '860752061230249', checkedAt, checkReady: true });
    expect(stallText(stalled)).toBe(
      'Instalador no local desde segunda, 16/11 às 14:44 e ainda falta: foto do rastreador, concluir. ' +
        'O rastreador só fica ligado ao veículo depois de “Concluir a instalação” no link.',
    );
    expect(stallText(stalled, true)).toBe(
      'Você chegou segunda, 16/11 às 14:44 e ainda falta: foto do rastreador, concluir. ' +
        'O rastreador só fica no veículo do cliente (e no app dele) depois de “Concluir a instalação”.',
    );
  });
});

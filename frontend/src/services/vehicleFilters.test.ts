import { describe, expect, it } from 'vitest';

import type { VehicleView } from '@/types';

import { NO_GROUP, inGroup, matchesVehicle, situationOf } from './vehicleFilters';

const vehicle = (over: Partial<VehicleView> & { status?: string } = {}) => {
  const { status, ...rest } = over;
  return {
    name: 'Van 0503',
    plate: 'FRT0503',
    brand: 'Fiat',
    model: 'Ducato',
    device: status ? { imei: '869247061000503', status } : null,
    ...rest,
  } as unknown as VehicleView;
};

describe('os filtros de Meus veículos', () => {
  it('a situação: comunicando, sem sinal ou aguardando o rastreador', () => {
    expect(situationOf(vehicle({ status: 'ONLINE' }))).toBe('online');
    expect(situationOf(vehicle({ status: 'STALE' }))).toBe('offline');
    expect(situationOf(vehicle({ status: 'OFFLINE' }))).toBe('offline');
    expect(situationOf(vehicle())).toBe('pending');
  });

  it('a busca acha pelo nome, placa, marca, modelo ou IMEI, sem diferenciar maiúsculas', () => {
    const v = vehicle({ status: 'ONLINE' });
    for (const term of ['van 05', 'frt0503', 'FIAT', 'ducato', '061000503', '  ']) expect(matchesVehicle(v, term)).toBe(true);
    expect(matchesVehicle(v, 'caminhão')).toBe(false);
    expect(matchesVehicle(vehicle({ plate: '', brand: '', model: '' }), 'frt')).toBe(false);
  });

  it('a busca acha pelos campos personalizados; o grupo filtra', () => {
    const v = vehicle({ status: 'ONLINE', custom1: 'PMSP-2026/0042', custom2: 'CC-11', groupId: 'g1' } as Partial<VehicleView>);
    expect(matchesVehicle(v, 'pmsp-2026')).toBe(true);
    expect(matchesVehicle(v, 'cc-11')).toBe(true);
    expect(inGroup(v, '')).toBe(true);
    expect(inGroup(v, 'g1')).toBe(true);
    expect(inGroup(v, 'g2')).toBe(false);
    expect(inGroup(v, NO_GROUP)).toBe(false);
    expect(inGroup({ groupId: null }, NO_GROUP)).toBe(true);
  });
});

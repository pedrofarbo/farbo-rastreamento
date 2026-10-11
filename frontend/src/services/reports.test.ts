import { describe, expect, it } from 'vitest';

import type { ReportDailyRow, ReportTrip } from '@/types';

import { addDays, dayTotals, formatKm, formatSpan, periodProblem, presetPeriod, timeline } from './reports';

describe('os relatórios', () => {
  it('os atalhos de período', () => {
    const today = '2026-10-11';
    expect(presetPeriod('today', today)).toEqual({ from: today, to: today });
    expect(presetPeriod('yesterday', today)).toEqual({ from: '2026-10-10', to: '2026-10-10' });
    expect(presetPeriod('week', today)).toEqual({ from: '2026-10-05', to: today });
    expect(presetPeriod('month', today)).toEqual({ from: '2026-10-01', to: today });
    expect(presetPeriod('lastMonth', today)).toEqual({ from: '2026-09-01', to: '2026-09-30' });
    expect(presetPeriod('lastMonth', '2026-01-15')).toEqual({ from: '2025-12-01', to: '2025-12-31' });
    expect(addDays('2026-02-28', 1)).toBe('2026-03-01');
  });

  it('o período: datas, ordem e até 31 dias', () => {
    expect(periodProblem('2026-10-01', '2026-10-31')).toBeNull();
    expect(periodProblem('2026-10-01', '2026-11-01')).toBe('O período vai até 31 dias.');
    expect(periodProblem('2026-10-10', '2026-10-01')).toBe('O início do período precisa ser antes do fim.');
    expect(periodProblem('', '2026-10-01')).toBe('Escolha as duas datas.');
  });

  it('a frota em cada dia, com os dias parados zerados', () => {
    const row = (vehicleId: string, day: string, distanceKm: number, trips: number) =>
      ({ vehicleId, day, distanceKm, trips }) as ReportDailyRow;
    expect(dayTotals([row('a', '2026-10-01', 10, 2), row('b', '2026-10-01', 5.5, 1), row('a', '2026-10-03', 3, 1)], '2026-10-01', '2026-10-03')).toEqual([
      { day: '2026-10-01', km: 15.5, trips: 3 },
      { day: '2026-10-02', km: 0, trips: 0 },
      { day: '2026-10-03', km: 3, trips: 1 },
    ]);
  });

  it('as viagens com as paradas entre elas', () => {
    const trip = (start: string, end: string) =>
      ({ start, end, startLat: 0, startLon: 0, endLat: -23.5, endLon: -46.6, distanceMeters: 1000, maxSpeedKmh: 40, ongoing: false }) as ReportTrip;
    const items = timeline([trip('2026-10-01T08:00:00Z', '2026-10-01T08:30:00Z'), trip('2026-10-01T09:00:00Z', '2026-10-01T09:20:00Z')]);
    expect(items.map((i) => i.kind)).toEqual(['trip', 'stop', 'trip']);
    expect(items[1]).toMatchObject({ kind: 'stop', seconds: 1800, lat: -23.5 });
  });

  it('as durações e os km', () => {
    expect(formatSpan(3725)).toBe('1 h 02 min');
    expect(formatSpan(300)).toBe('5 min');
    expect(formatSpan(0)).toBe('—');
    expect(formatKm(12.345).replace(/ /g, ' ')).toBe('12,3 km');
  });
});

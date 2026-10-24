import { describe, expect, it } from 'vitest';

import type { BusRouteInput, TripProgress } from '@/types';

import { arrivalLabel, delayLabel, nextArrivals, nextStopName, routeProblem } from './busRoutes';

const input = (over: Partial<BusRouteInput> = {}): BusRouteInput => ({
  name: 'Linha 1',
  color: '#22c55e',
  deviationM: 500,
  public: false,
  active: true,
  stops: [
    { name: 'Terminal', latitude: -23.5, longitude: -46.6, radiusM: 80, offsetMin: 0 },
    { name: 'Centro', latitude: -23.51, longitude: -46.61, radiusM: 80, offsetMin: 15 },
  ],
  ...over,
});

describe('a linha', () => {
  it('confere o nome, os pontos e os horários', () => {
    expect(routeProblem(input())).toBe('');
    expect(routeProblem(input({ name: ' ' }))).toMatch(/nome à linha/);
    expect(routeProblem(input({ stops: input().stops.slice(0, 1) }))).toMatch(/pelo menos 2/);
    const [a, b] = input().stops;
    expect(routeProblem(input({ stops: [a, { ...b, offsetMin: 0 }, { ...b, name: 'Fim', offsetMin: -1 }] }))).toMatch(/ponto 3/);
    expect(routeProblem(input({ stops: [a, { ...b, name: '' }] }))).toMatch(/ponto 2/);
  });

  it('o atraso em texto', () => {
    expect(delayLabel(null)).toBe('—');
    expect(delayLabel(1)).toBe('no horário');
    expect(delayLabel(6)).toBe('6 min atrasado');
    expect(delayLabel(-3)).toBe('3 min adiantado');
  });
});

describe('a próxima chegada em cada ponto', () => {
  const now = Date.parse('2026-10-24T12:00:00Z');
  const trip = (stops: TripProgress['stops'], nextSeq: number | null): TripProgress => ({
    tripId: crypto.randomUUID(),
    vehicleId: '',
    vehicleName: 'Ônibus',
    departsAt: '2026-10-24T11:50:00Z',
    status: 'running',
    stops,
    nextSeq,
    delayMin: 2,
    offRoute: false,
    position: null,
  });
  const stops = [
    { id: '1', seq: 1, name: 'Terminal', latitude: 0, longitude: 0, radiusM: 80, offsetMin: 0 },
    { id: '2', seq: 2, name: 'Centro', latitude: 0, longitude: 0, radiusM: 80, offsetMin: 15 },
  ];

  it('o ônibus que chega primeiro, sem os pontos já passados', () => {
    const running = trip(
      [
        { seq: 1, name: 'Terminal', planned: '2026-10-24T11:50:00Z', arrivedAt: '2026-10-24T11:52:00Z', delayMin: 2, eta: null },
        { seq: 2, name: 'Centro', planned: '2026-10-24T12:05:00Z', arrivedAt: null, delayMin: null, eta: '2026-10-24T12:07:00Z' },
      ],
      2,
    );
    const later = trip(
      [
        { seq: 1, name: 'Terminal', planned: '2026-10-24T12:20:00Z', arrivedAt: null, delayMin: null, eta: null },
        { seq: 2, name: 'Centro', planned: '2026-10-24T12:35:00Z', arrivedAt: null, delayMin: null, eta: null },
      ],
      1,
    );
    const next = nextArrivals({ stops, trips: [later, running] }, now);
    expect(next).toEqual([Date.parse('2026-10-24T12:20:00Z'), Date.parse('2026-10-24T12:07:00Z')]);
    expect(arrivalLabel(next[1], now)).toBe('em 7 min');
    expect(arrivalLabel(null, now)).toBe('sem previsão');
    expect(arrivalLabel(now + 30_000, now)).toBe('chegando');
    expect(nextStopName(running)).toBe('Centro');
  });
});

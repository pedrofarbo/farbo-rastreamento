// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';

import { closestPosition, speedChart, toLocalInput } from './ClaimReportPage';

describe('relatório de sinistro', () => {
  const from = new Date('2026-10-23T14:00:00Z');
  const to = new Date('2026-10-23T14:10:00Z');
  const positions = [
    { gpsTimestamp: '2026-10-23T14:00:00Z', speedKmh: 0 },
    { gpsTimestamp: '2026-10-23T14:05:00Z', speedKmh: 62 },
    { gpsTimestamp: '2026-10-23T14:10:00Z', speedKmh: 30 },
  ];

  it('acha a posição mais perto do momento', () => {
    expect(closestPosition(positions, new Date('2026-10-23T14:06:00Z'))).toBe(positions[1]);
    expect(closestPosition(positions, new Date('2026-10-23T14:09:00Z'))).toBe(positions[2]);
    expect(closestPosition([], from)).toBeNull();
  });

  it('desenha a velocidade na escala de 20 em 20 km/h', () => {
    const chart = speedChart(positions, from, to, 600, 100);
    expect(chart.max).toBe(62);
    expect(chart.top).toBe(80);
    expect(chart.points).toBe('0.0,100.0 300.0,22.5 600.0,62.5');
    expect(speedChart([], from, to, 600, 100)).toEqual({ points: '', top: 20, max: 0 });
    // O limite cadastrado cabe na escala (com folga para o tracejado não ficar na borda).
    expect(speedChart(positions, from, to, 600, 100, 80).top).toBe(100);
  });

  it('escreve o momento no formato do campo', () => {
    expect(toLocalInput(new Date(2026, 9, 23, 9, 5))).toBe('2026-10-23T09:05');
  });
});

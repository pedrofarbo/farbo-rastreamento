// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';

import { fleetScore, scoreLabel, usageTotals } from './ReportsPage';

describe('relatórios: direção e utilização', () => {
  it('dá o rótulo da nota', () => {
    expect(scoreLabel(92).label).toBe('ótima');
    expect(scoreLabel(70).tone).toBe('ok');
    expect(scoreLabel(55).label).toBe('regular');
    expect(scoreLabel(12).tone).toBe('bad');
    expect(scoreLabel(null).label).toBe('poucos km');
  });

  it('pesa a nota da frota pelos km', () => {
    expect(fleetScore([{ score: 100, distanceKm: 300 }, { score: 60, distanceKm: 100 }, { score: null, distanceKm: 5 }])).toBe(90);
    expect(fleetScore([{ score: null, distanceKm: 5 }])).toBeNull();
  });

  it('soma a utilização da frota', () => {
    const row = { movingSeconds: 3000, idleSeconds: 1000, periodDays: 10, activeDays: 5 };
    const totals = usageTotals([row, { ...row, activeDays: 0, movingSeconds: 0, idleSeconds: 0 }, { ...row, periodDays: 0, activeDays: 0 }] as never);
    expect(totals.usePercent).toBe(25);
    expect(totals.neverMoved).toBe(1);
    expect(totals.idlePercent).toBe(25);
    expect(totals.idleSeconds).toBe(2000);
  });
});

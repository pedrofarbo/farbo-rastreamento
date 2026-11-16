// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';

import { normalizePlate, prospectProblem } from '@/pages/org/OrgInspectionsPage';

import { FRESH_MS, freshFix, stampLayout, watermarkLines } from './inspectionCamera';

describe('a foto da vistoria', () => {
  it('a marca: a data, a hora, a placa e o lugar (ou sem localização)', () => {
    const when = new Date(2026, 10, 5, 9, 7);
    expect(watermarkLines(when, 'ABC1D23', { latitude: -18.91861, longitude: -48.27722, accuracy: 12.4, at: 0 })).toEqual([
      '05/11/2026 09:07 · ABC1D23',
      '-18.91861, -48.27722 (±12 m) · vistoria Farbo',
    ]);
    expect(watermarkLines(when, '', null)).toEqual(['05/11/2026 09:07', 'sem localização · vistoria Farbo']);
  });

  it('a posição vale por alguns minutos', () => {
    const fix = { latitude: 1, longitude: 2, accuracy: null, at: 1_000 };
    expect(freshFix(fix, 1_000 + FRESH_MS)).toBe(fix);
    expect(freshFix(fix, 1_001 + FRESH_MS)).toBeNull();
    expect(freshFix(null)).toBeNull();
  });

  it('a faixa da marca acompanha a largura da foto', () => {
    expect(stampLayout(1600, 2)).toEqual({ font: 36, pad: 22, band: 2 * (36 + 11) + 22 });
    expect(stampLayout(300, 2).font).toBe(14);
  });
});

describe('o pedido da vistoria por link', () => {
  const draft = {
    kind: 'entry' as const, mode: 'self' as const, personName: 'Carla', personPhone: '', plate: 'abc-1d23',
    vehicleType: 'car' as const, vehicleDesc: '', note: '',
  };
  it('confere o nome e a placa (antiga ou Mercosul)', () => {
    expect(normalizePlate(' abc-1d23 ')).toBe('ABC1D23');
    expect(prospectProblem(draft)).toBeNull();
    expect(prospectProblem({ ...draft, plate: 'ABC1234' })).toBeNull();
    expect(prospectProblem({ ...draft, plate: 'AB123' })).toMatch(/placa/);
    expect(prospectProblem({ ...draft, personName: ' ' })).toMatch(/nome/);
  });
});

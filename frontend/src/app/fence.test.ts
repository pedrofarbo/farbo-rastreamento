import { describe, expect, it } from 'vitest';

import type { Geofence } from '@/types';

import { fenceProblem, fencesOf, formatRadius, notifyLabel, RADIUS_STEPS, radiusStep, zoomForRadius } from './fence';

const form = { name: 'Casa', latitude: -23.55, longitude: -46.63, radiusMeters: 200, vehicleIds: ['v1'], notifyEnter: true, notifyExit: true };

describe('cerca do app', () => {
  it('raio: o passo mais próximo e o texto em português', () => {
    expect(RADIUS_STEPS[radiusStep(200)]).toBe(200);
    expect(RADIUS_STEPS[radiusStep(260)]).toBe(300);
    expect(RADIUS_STEPS[radiusStep(1)]).toBe(50);
    expect(RADIUS_STEPS[radiusStep(1e6)]).toBe(50000);
    expect(formatRadius(50)).toBe('50 m');
    expect(formatRadius(1000)).toBe('1 km');
    expect(formatRadius(1500)).toBe('1,5 km');
  });

  it('zoom: raio maior, zoom menor; sempre dentro do que o mapa tem', () => {
    const near = zoomForRadius(100, -23.55, 360);
    const far = zoomForRadius(20000, -23.55, 360);
    expect(near).toBeGreaterThan(far);
    expect(zoomForRadius(1, 0, 360)).toBe(18);
    expect(zoomForRadius(5e7, 0, 360)).toBe(3);
    // O círculo cabe: diâmetro em pixels menor que a altura do mapa.
    const metersPerPixel = (156543.03392 * Math.cos((-23.55 * Math.PI) / 180)) / 2 ** near;
    expect((2 * 100) / metersPerPixel).toBeLessThan(360);
  });

  it('o que impede salvar', () => {
    expect(fenceProblem(form)).toBeNull();
    expect(fenceProblem({ ...form, name: '  ' })).toMatch(/nome/);
    expect(fenceProblem({ ...form, vehicleIds: [] })).toMatch(/veículo/);
  });

  it('avisos e cercas de um veículo', () => {
    expect(notifyLabel({ notifyEnter: true, notifyExit: false })).toBe('Avisa só ao entrar');
    expect(notifyLabel({ notifyEnter: false, notifyExit: false })).toMatch(/Sem aviso/);
    const fences = [{ id: 'a', vehicleIds: ['v1'] }, { id: 'b', vehicleIds: ['v2'] }] as Geofence[];
    expect(fencesOf(fences, 'v1').map((f) => f.id)).toEqual(['a']);
  });
});

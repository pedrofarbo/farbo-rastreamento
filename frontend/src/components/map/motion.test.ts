import { describe, expect, it } from 'vitest';

import {
  MAX_GLIDE_GAP_S,
  TRAIL_MAX_POINTS,
  distanceMeters,
  easeInOutCubic,
  interpolate,
  nextTrail,
  shouldGlide,
  trailOpacities,
} from './motion';

// Um ponto a cada 30 s, andando ~300 m para o norte (36 km/h).
const at = (seconds: number, north = seconds * 10) => ({
  gpsTimestamp: new Date(Date.UTC(2026, 9, 8, 12, 0, 0) + seconds * 1000).toISOString(),
  latitude: -23.55 + north / 111_195,
  longitude: -46.63,
});

describe('o veículo em movimento no mapa', () => {
  it('o deslize sai e chega devagar, e o caminho é em linha reta', () => {
    expect(easeInOutCubic(0)).toBe(0);
    expect(easeInOutCubic(1)).toBe(1);
    expect(easeInOutCubic(0.5)).toBeCloseTo(0.5);
    expect(easeInOutCubic(0.1)).toBeLessThan(0.1);
    expect(easeInOutCubic(2)).toBe(1);
    expect(interpolate({ lat: 0, lng: 0 }, { lat: 10, lng: -20 }, 0.25)).toEqual({ lat: 2.5, lng: -5 });
    expect(distanceMeters(at(0), at(30))).toBeCloseTo(300, 0);
  });

  it('desliza só entre pontos em sequência, perto no tempo e numa velocidade possível', () => {
    expect(shouldGlide(at(0), at(30))).toBe(true);
    // Depois de horas parado (o rastreador manda a cada 3600 s): vai direto.
    expect(shouldGlide(at(0), at(3600, 300))).toBe(false);
    expect(shouldGlide(at(0), at(MAX_GLIDE_GAP_S + 1, 300))).toBe(false);
    // Salto do GPS: 10 km em 30 s.
    expect(shouldGlide(at(0), at(30, 10_000))).toBe(false);
    // Mesmo ponto, ponto repetido ou fora de ordem.
    expect(shouldGlide(at(0), at(30, 0))).toBe(false);
    expect(shouldGlide(at(30), at(30))).toBe(false);
    expect(shouldGlide(at(30), at(0))).toBe(false);
  });

  it('o rastro: só andando, os últimos 5 minutos, recomeça num salto', () => {
    let trail = nextTrail([], at(0), true);
    for (let s = 30; s <= 600; s += 30) trail = nextTrail(trail, at(s), true);
    // 5 min a 30 s: 11 pontos (de 300 s a 600 s).
    expect(trail.map((p) => p.gpsTimestamp)).toEqual(Array.from({ length: 11 }, (_, i) => at(300 + i * 30).gpsTimestamp));
    expect(trail.length).toBeLessThanOrEqual(TRAIL_MAX_POINTS);
    // O mesmo ponto de novo: nada muda.
    expect(nextTrail(trail, at(600), true)).toBe(trail);
    // Salto: começa de novo no ponto novo.
    expect(nextTrail(trail, at(630, 50_000), true)).toEqual([at(630, 50_000)]);
    // Parou: sem rastro.
    expect(nextTrail(trail, at(630), false)).toEqual([]);
  });

  it('o rastro clareia pela idade: um degrau por ponto, sem sumir de uma vez', () => {
    expect(trailOpacities(0)).toEqual([]);
    expect(trailOpacities(1)).toEqual([0.85]);
    // O trecho que acabou de ser percorrido continua forte quando chega o próximo.
    const two = trailOpacities(2);
    expect(two[1]).toBeCloseTo(0.85);
    expect(two[0]).toBeGreaterThan(0.75);
    const full = trailOpacities(TRAIL_MAX_POINTS - 1);
    expect(full[full.length - 1]).toBeCloseTo(0.85);
    expect(full[0]).toBeLessThan(0.2);
    expect(full[0]).toBeGreaterThanOrEqual(0.12);
    expect([...full].sort((a, b) => a - b)).toEqual(full);
  });
});

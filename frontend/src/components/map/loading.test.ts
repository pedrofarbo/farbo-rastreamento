import { describe, expect, it } from 'vitest';

import { NO_TILES, mapPhase, trackTiles, vehiclesLabel } from './loading';
import type { TileEvent } from './loading';

const run = (...events: TileEvent[]) => events.reduce(trackTiles, NO_TILES);

describe('o carregamento do mapa', () => {
  it('carrega até o "load" do Leaflet, com a porcentagem das imagens da tela', () => {
    const half = run('start', 'start', 'start', 'start', 'loaded', 'loaded');
    expect(mapPhase(half)).toBe('loading');
    expect(half.peak).toBe(50);
    // Todas chegaram, mas sem o "load" ainda: segura em 95%.
    expect(run('start', 'loaded').peak).toBe(95);
    const done = run('start', 'start', 'loaded', 'loaded', 'settled');
    expect(mapPhase(done)).toBe('ready');
    expect(done.peak).toBe(100);
  });

  it('a barra nunca volta: o mapa mudou de lugar e pediu mais imagens', () => {
    const before = run('start', 'start', 'loaded', 'loaded');
    const moved = ['start', 'start', 'start', 'start', 'aborted'].reduce(
      (p, e) => trackTiles(p, e as TileEvent),
      before,
    );
    expect(moved.peak).toBe(before.peak);
  });

  it('sem nenhuma imagem, só erros: sem o mapa de fundo (os veículos seguem)', () => {
    const offline = run('start', 'start', 'failed', 'failed', 'settled');
    expect(mapPhase(offline)).toBe('offline');
    // Uma imagem chega depois: o aviso some.
    expect(mapPhase(trackTiles(offline, 'loaded'))).toBe('ready');
    // Alguns erros, mas o resto chegou: pronto.
    expect(mapPhase(run('start', 'start', 'failed', 'loaded', 'settled'))).toBe('ready');
  });

  it('o tempo acabou: não fica preso carregando', () => {
    expect(mapPhase(run('start', 'timeout'))).toBe('ready');
    expect(mapPhase(run('start', 'failed', 'timeout'))).toBe('offline');
    expect(mapPhase(run('timeout'))).toBe('ready');
  });

  it('conta os veículos por extenso', () => {
    expect(vehiclesLabel(0)).toBe('');
    expect(vehiclesLabel(1)).toBe('1 veículo');
    expect(vehiclesLabel(1000)).toBe('1.000 veículos');
  });
});

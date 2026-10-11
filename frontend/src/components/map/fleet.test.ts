import { describe, expect, it } from 'vitest';

import type { VehicleView } from '@/types';

import { ANIMATE_MAX, CLUSTER_MAX_ZOOM, FLEET_SIZE, fleetLayout } from './fleet';
import type { MapView } from './fleet';

const vehicle = (id: string, latitude: number, longitude: number, over: Partial<VehicleView> = {}) =>
  ({ id, name: id, lastPosition: { latitude, longitude }, theft: null, ...over }) as unknown as VehicleView;

// 1 grau = 1000 px: dois veículos a menos de 0,06° caem na mesma célula de 60 px.
const project = (latitude: number, longitude: number) => ({ x: longitude * 1000, y: -latitude * 1000 });
const view = (zoom = 12): MapView => ({ zoom, bounds: { south: -24, west: -47, north: -23, east: -46 } });

/** `n` veículos espalhados num quadrado de 0,5°, cada um na sua célula. */
const spread = (n: number) =>
  Array.from({ length: n }, (_, i) => vehicle(`v${i}`, -23.9 + (i % 7) * 0.07, -46.9 + Math.floor(i / 7) * 0.07));

describe('fleetLayout', () => {
  it('numa conta comum, desenha todos, soltos e deslizando', () => {
    const list = [vehicle('a', -23.5, -46.6), vehicle('b', -23.5001, -46.6001), vehicle('longe', 10, 10)];
    const layout = fleetLayout(list, null, view(), project);
    expect(layout.singles.map((v) => v.id)).toEqual(['a', 'b', 'longe']);
    expect(layout.clusters).toEqual([]);
    expect(layout.animate).toBe(true);
  });

  it('ignora quem ainda não tem posição', () => {
    const layout = fleetLayout([vehicle('a', -23.5, -46.6), { ...vehicle('b', 0, 0), lastPosition: null }], null, view(), project);
    expect(layout.singles.map((v) => v.id)).toEqual(['a']);
  });

  it('numa frota, junta os próximos num grupo com o total, o centro e a área', () => {
    const garage = Array.from({ length: FLEET_SIZE }, (_, i) => vehicle(`g${i}`, -23.5 + i * 0.0001, -46.6));
    const layout = fleetLayout([...garage, vehicle('solo', -23.2, -46.2)], null, view(), project);
    expect(layout.singles.map((v) => v.id)).toEqual(['solo']);
    expect(layout.clusters).toHaveLength(1);
    const [group] = layout.clusters;
    expect(group.count).toBe(FLEET_SIZE);
    expect(group.bounds.south).toBeCloseTo(-23.5);
    expect(group.bounds.north).toBeCloseTo(-23.5 + (FLEET_SIZE - 1) * 0.0001);
    expect(group.latitude).toBeCloseTo((group.bounds.south + group.bounds.north) / 2);
    expect(group.alert).toBe(false);
  });

  it('o grupo fica vermelho se algum veículo dele está em modo roubo', () => {
    const garage = Array.from({ length: FLEET_SIZE + 1 }, (_, i) =>
      vehicle(`g${i}`, -23.5, -46.6, i === 3 ? ({ theft: { mode: 'ACTIVE' } } as unknown as Partial<VehicleView>) : {}),
    );
    expect(fleetLayout(garage, null, view(), project).clusters[0].alert).toBe(true);
  });

  it('o escolhido sai do grupo e aparece mesmo fora da tela', () => {
    const garage = Array.from({ length: FLEET_SIZE }, (_, i) => vehicle(`g${i}`, -23.5, -46.6));
    const list = [...garage, vehicle('fora', 10, 10)];
    expect(fleetLayout(list, 'g0', view(), project).singles.map((v) => v.id)).toEqual(['g0']);
    expect(fleetLayout(list, 'fora', view(), project).singles.map((v) => v.id)).toEqual(['fora']);
    expect(fleetLayout(list, 'g0', view(), project).clusters[0].count).toBe(FLEET_SIZE - 1);
  });

  it('não desenha o que está longe da tela', () => {
    const list = [...spread(FLEET_SIZE), vehicle('rio', -22.9, -43.2), vehicle('perto', -23.0, -46.0)];
    const ids = fleetLayout(list, null, view(), project).singles.map((v) => v.id);
    expect(ids).not.toContain('rio');
    // Um pouco além da borda entra (o arrastar não mostra o mapa vazio).
    expect(ids).toContain('perto');
  });

  it('no zoom de rua, cada um no seu lugar', () => {
    const garage = Array.from({ length: FLEET_SIZE + 1 }, (_, i) => vehicle(`g${i}`, -23.5, -46.6));
    const layout = fleetLayout(garage, null, view(CLUSTER_MAX_ZOOM), project);
    expect(layout.clusters).toEqual([]);
    expect(layout.singles).toHaveLength(FLEET_SIZE + 1);
  });

  it('com muitos soltos na tela, param de deslizar', () => {
    expect(fleetLayout(spread(ANIMATE_MAX), null, view(), project).animate).toBe(true);
    expect(fleetLayout(spread(ANIMATE_MAX + 1), null, view(), project).animate).toBe(false);
  });
});

// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import type { PublicTheftView } from '@/types';

let answer: () => Promise<PublicTheftView>;
const asked: string[] = [];
vi.mock('@/api/resources', () => ({
  theftApi: {
    publicView: (token: string) => {
      asked.push(token);
      return answer();
    },
  },
}));
// O mapa (Leaflet) não roda no jsdom.
vi.mock('react-leaflet', () => ({
  MapContainer: ({ children }: { children: unknown }) => <div data-map>{children as never}</div>,
  Marker: () => <span data-marker />,
  useMap: () => ({ setView: () => undefined, getZoom: () => 16 }),
}));
vi.mock('@/components/map/OsmTiles', () => ({ OsmTiles: () => null }));

import { PublicTheftPage } from './PublicTheftPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/\u00a0/g, ' ');

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <MemoryRouter initialEntries={['/localizar/tok123']}>
        <Routes>
          <Route path="/localizar/:token" element={<PublicTheftPage />} />
        </Routes>
      </MemoryRouter>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  asked.length = 0;
});

describe('link público do modo roubo', () => {
  it('mostra o veículo e onde ele está, sem login', async () => {
    answer = async () => ({
      vehicle: { name: 'Moto do trabalho', plate: 'ROU1B23', brand: 'Honda', model: 'CG 160', year: 2022, color: 'Vermelha' },
      activatedAt: '2026-10-06T22:10:00Z', expiresAt: '2026-10-09T22:10:00Z',
      position: { latitude: -23.55, longitude: -46.65, speedKmh: 38, heading: 90, gpsTimestamp: new Date().toISOString(), receivedAt: new Date().toISOString() },
      address: 'Rua Augusta, 500 - Consolação, São Paulo - SP', ignition: true, online: true,
    });
    await render();
    expect(asked).toEqual(['tok123']);
    expect(text()).toContain('Moto do trabalho');
    expect(text()).toContain('ROU1B23');
    expect(text()).toContain('Honda · CG 160 · 2022 · Vermelha');
    expect(text()).toContain('Rua Augusta, 500');
    expect(text()).toContain('38 km/h');
    expect(text()).toContain('conectado');
    const maps = document.querySelector('a[href^="https://www.google.com/maps/"]') as HTMLAnchorElement;
    expect(maps.href).toContain('-23.550000,-46.650000');
    expect(document.querySelector('meta[name="robots"]')?.getAttribute('content')).toBe('noindex, nofollow');
  });

  it('encerrado: avisa que o link não vale mais', async () => {
    answer = async () => {
      throw new ApiError(410, 'este link de localização não está mais ativo');
    };
    await render();
    expect(text()).toContain('Este link não está mais ativo');
    expect(text()).not.toContain('Carregando');
  });
});

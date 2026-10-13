// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import type { Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Geofence, VehicleView } from '@/types';

// O mapa de verdade (Leaflet) não roda no jsdom: guarda o que ele recebeu.
const mapProps: Record<string, unknown>[] = [];
vi.mock('./TrackerMap', () => ({
  TrackerMap: (props: Record<string, unknown>) => {
    mapProps.push(props);
    return <div data-testid="mapa" />;
  },
}));
vi.mock('@/components/ui/Address', () => ({
  Address: ({ className }: { className?: string }) => <span className={className}>Rua Augusta, 500 — São Paulo</span>,
}));

import { FleetMapFullscreen } from './FleetMapFullscreen';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find(
    (b) => b.getAttribute('aria-label') === label || b.textContent === label,
  ) as HTMLButtonElement | undefined;
const key = (k: string) => act(() => void document.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true })));

function vehicle(id: string, over: Partial<VehicleView> = {}): VehicleView {
  return {
    id, name: `Van ${id}`, plate: `FRT${id}`, device: { status: 'ONLINE' }, state: { acc: true },
    lastPosition: { latitude: -23.56, longitude: -46.65, speedKmh: 47.5, acc: true, gpsTimestamp: new Date().toISOString() },
    ...over,
  } as unknown as VehicleView;
}

const VEHICLES = [
  vehicle('1', { ownerId: 'c-frota', ownerName: 'Transportes Frota Ltda' }),
  vehicle('2', { device: { status: 'OFFLINE' } } as Partial<VehicleView>),
  vehicle('3'),
];
const FENCES = [{ id: 'f1', name: 'Base', active: true } as Geofence];

let fullscreenElement: Element | null = null;
const exitFullscreen = vi.fn(async () => {
  fullscreenElement = null;
});
let root: Root | null = null;

beforeEach(() => {
  Object.defineProperty(document, 'fullscreenElement', { configurable: true, get: () => fullscreenElement });
  Object.defineProperty(document, 'exitFullscreen', { configurable: true, value: exitFullscreen });
});

afterEach(() => {
  act(() => root?.unmount());
  root = null;
  document.body.innerHTML = '';
  mapProps.length = 0;
  fullscreenElement = null;
  exitFullscreen.mockClear();
});

function render(selectedId: string | null, onClose = vi.fn(), onSelect = vi.fn(), customerLinks = false) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  root = createRoot(host);
  act(() =>
    root!.render(
      <MemoryRouter>
        <FleetMapFullscreen
          vehicles={VEHICLES}
          selectedId={selectedId}
          onSelect={onSelect}
          geofences={FENCES}
          title="Rota Norte"
          customerLinks={customerLinks}
          onClose={onClose}
        />
      </MemoryRouter>,
    ),
  );
  return { onClose, onSelect };
}

describe('o mapa da central em tela cheia', () => {
  it('a frota no mapa, o grupo e quantos online, e o veículo escolhido no cartão', () => {
    render('1');
    expect(text()).toContain('Rota Norte');
    expect(text()).toContain('2 de 3 online');
    const last = mapProps.at(-1)!;
    expect(last.vehicles).toBe(VEHICLES);
    expect(last.selectedId).toBe('1');
    expect(last.showControls).toBe(false);
    expect(text()).toContain('Van 1');
    expect(text()).toContain('FRT1');
    expect(text()).toContain('48 km/h');
    expect(text()).toContain('Ignição ligada');
    expect(text()).toContain('Rua Augusta, 500');
    expect(document.querySelector('a')?.getAttribute('href')).toBe('/veiculos/1');
    // O anterior e o próximo, na ordem da lista.
    expect(text()).toContain('1 de 3');
  });

  it('de quem é o veículo: o nome, e para o admin o link da ficha do cliente', () => {
    render('1');
    expect(text()).toContain('Transportes Frota Ltda');
    expect(document.querySelector('a[href="/clientes/c-frota"]')).toBeNull();
    act(() => root?.unmount());
    root = null;
    document.body.innerHTML = '';
    render('1', vi.fn(), vi.fn(), true);
    expect(document.querySelector('a[href="/clientes/c-frota"]')?.textContent).toBe('Transportes Frota Ltda');
    // Sem dono conhecido (o veículo do próprio cliente), nada.
    act(() => root?.unmount());
    root = null;
    document.body.innerHTML = '';
    render('3');
    expect(text()).not.toContain('Transportes Frota Ltda');
  });

  it('sem veículo escolhido, um convite para tocar num veículo', () => {
    render(null);
    expect(text()).toContain('Toque num veículo no mapa para ver os dados dele.');
    expect(button('Mostrar a frota inteira')).toBeTruthy();
  });

  it('as cercas saem e voltam', () => {
    render('1');
    expect(mapProps.at(-1)!.geofences).toEqual(FENCES);
    act(() => button('Cercas')!.click());
    expect(mapProps.at(-1)!.geofences).toEqual([]);
    act(() => button('Cercas')!.click());
    expect(mapProps.at(-1)!.geofences).toEqual(FENCES);
  });

  it('fecha no X ou no Esc, uma vez só; o Esc com a busca aberta só fecha a busca', () => {
    const { onClose } = render('1');
    act(() => button('Trocar de veículo (1 de 3)')!.click());
    expect(document.querySelector('[role="dialog"][aria-label="Trocar de veículo"]')).toBeTruthy();
    key('Escape');
    expect(document.querySelector('[role="dialog"][aria-label="Trocar de veículo"]')).toBeNull();
    expect(onClose).not.toHaveBeenCalled();

    key('Escape');
    act(() => button('Sair da tela cheia')!.click());
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('sair da tela cheia do navegador fecha o mapa; fechar o mapa sai dela', () => {
    fullscreenElement = document.documentElement;
    const { onClose } = render('1');
    fullscreenElement = null;
    act(() => void document.dispatchEvent(new Event('fullscreenchange')));
    expect(onClose).toHaveBeenCalledTimes(1);

    act(() => root?.unmount());
    root = null;
    fullscreenElement = document.documentElement;
    render('1');
    act(() => root?.unmount());
    root = null;
    expect(exitFullscreen).toHaveBeenCalledTimes(1);
  });
});

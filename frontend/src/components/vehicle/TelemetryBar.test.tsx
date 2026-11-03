// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it } from 'vitest';

import type { DeviceState, Position } from '@/types';

import { TelemetryBar } from './TelemetryBar';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const state = (over: Partial<DeviceState> = {}): DeviceState => ({
  deviceId: 'd1', overspeed: false, acc: false, gpsValid: true, relayOn: false, batteryPercent: 83, batteryVoltage: 12.4,
  gsmLevel: 4, insideFences: [], lastHeartbeatAt: null, updatedAt: '2026-10-25T19:09:27Z', ...over,
});

function render(s: DeviceState, position: Position | null = null) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  act(() => createRoot(host).render(<TelemetryBar position={position} state={s} />));
  return (host.textContent ?? '').replace(/ /g, ' ');
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('a bateria na barra do veículo', () => {
  it('com a voltagem: a bateria do veículo e a interna do rastreador, lado a lado', () => {
    const text = render(state());
    expect(text).toContain('Bateria do veículo12,4 V');
    expect(text).toContain('Bateria interna83%');
  });

  it('sem a voltagem: só a interna', () => {
    const text = render(state({ batteryVoltage: null }));
    expect(text).not.toContain('Bateria do veículo');
    expect(text).toContain('Bateria interna83%');
  });
});

describe('a velocidade na barra do veículo', () => {
  const at = (minutesAgo: number, speedKmh: number) =>
    ({ gpsTimestamp: new Date(Date.now() - minutesAgo * 60_000).toISOString(), speedKmh, acc: true }) as Position;

  it('a da posição recente', () => {
    expect(render(state({ acc: true }), at(1, 42))).toContain('Velocidade42 km/h');
  });

  it('na garagem, sem GPS há dias e com a ignição desligada: parado, não a velocidade da chegada', () => {
    expect(render(state({ acc: false, gpsValid: false }), at(3 * 24 * 60, 6))).toContain('Velocidade0 km/h');
  });
});

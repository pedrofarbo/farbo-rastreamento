// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it } from 'vitest';

import type { DeviceState } from '@/types';

import { TelemetryBar } from './TelemetryBar';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const state = (over: Partial<DeviceState> = {}): DeviceState => ({
  deviceId: 'd1', overspeed: false, acc: false, gpsValid: true, relayOn: false, batteryPercent: 83, batteryVoltage: 12.4,
  gsmLevel: 4, insideFences: [], lastHeartbeatAt: null, updatedAt: '2026-10-25T19:09:27Z', ...over,
});

function render(s: DeviceState) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  act(() => createRoot(host).render(<TelemetryBar position={null} state={s} />));
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

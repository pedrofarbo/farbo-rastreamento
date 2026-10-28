import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { DeviceState, Position, VehicleView } from '@/types';

import {
  applyVehiclePatches,
  createUpdateBatcher,
  withPosition,
  withRelay,
  withState,
  withStatus,
  type VehiclePatch,
} from './vehicleUpdates';

const position = (deviceId: string, latitude: number) => ({ deviceId, latitude, longitude: -46.6 }) as Position;

const vehicle = (id: string, deviceId: string | null): VehicleView =>
  ({
    id,
    deviceId,
    connected: true,
    lastPosition: null,
    device: deviceId ? { id: deviceId, status: 'ONLINE' } : null,
    state: deviceId ? { relayOn: false } : null,
  }) as unknown as VehicleView;

describe('applyVehiclePatches', () => {
  it('aplica cada rastreador na ordem em que as mensagens chegaram', () => {
    const list = [vehicle('v1', 'd1'), vehicle('v2', 'd2'), vehicle('v3', null)];
    const batch = new Map<string, VehiclePatch[]>([
      // Caiu depois da última posição: fica desconectado.
      ['d1', [withPosition(position('d1', -23.1)), withStatus('OFFLINE')]],
      // Caiu e voltou mandando posição: conectado de novo.
      ['d2', [withStatus('OFFLINE'), withPosition(position('d2', -23.2)), withRelay(true)]],
    ]);
    const [v1, v2, v3] = applyVehiclePatches(list, batch) ?? [];
    expect(v1).toMatchObject({ connected: false, lastPosition: { latitude: -23.1 }, device: { status: 'OFFLINE' } });
    expect(v2).toMatchObject({ connected: true, lastPosition: { latitude: -23.2 }, state: { relayOn: true } });
    expect(v3).toBe(list[2]); // sem rastreador: o mesmo objeto
  });

  it('sem GPS, o veículo não sai do lugar no mapa e o estado mostra o GPS sem fixo', () => {
    // Recém-instalado: o rastreador repete a posição da fábrica (China) até o GPS pegar sinal.
    const china = { ...position('d1', 22.6), longitude: 113.83, gpsValid: false };
    const [v1] =
      applyVehiclePatches(
        [{ ...vehicle('v1', 'd1'), connected: false }],
        new Map([['d1', [withPosition(china), withState({ gpsValid: false } as DeviceState)]]]),
      ) ?? [];
    expect(v1).toMatchObject({ connected: true, lastPosition: null, state: { gpsValid: false } });
    const [v2] = applyVehiclePatches([v1], new Map([['d1', [withPosition({ ...position('d1', -23.5), gpsValid: true })]]])) ?? [];
    expect(v2.lastPosition).toMatchObject({ latitude: -23.5 });
  });

  it('sem nada pendente ou sem lista, devolve o que já havia', () => {
    const list = [vehicle('v1', 'd1')];
    expect(applyVehiclePatches(list, new Map())).toBe(list);
    expect(applyVehiclePatches(undefined, new Map([['d1', [withStatus('ONLINE')]]]))).toBeUndefined();
  });
});

describe('createUpdateBatcher', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('parado entra na hora; com movimento, junta tudo em no máximo uma vez por intervalo', () => {
    const flushed: Map<string, VehiclePatch[]>[] = [];
    const batcher = createUpdateBatcher((batch) => flushed.push(batch), 1000, () => Date.now());

    batcher.queue('d1', withPosition(position('d1', 1)));
    vi.advanceTimersByTime(0);
    expect(flushed).toHaveLength(1);

    // 500 mensagens no segundo seguinte viram uma atualização só.
    for (let i = 0; i < 500; i++) {
      batcher.queue(`d${i % 50}`, withPosition(position(`d${i % 50}`, i)));
      vi.advanceTimersByTime(1);
    }
    vi.advanceTimersByTime(1000);
    expect(flushed).toHaveLength(2);
    expect(flushed[1].size).toBe(50);
    expect(flushed[1].get('d0')).toHaveLength(10);

    // Cancelado (a tela saiu): o que estava pendente não entra.
    batcher.queue('d1', withStatus('OFFLINE'));
    batcher.cancel();
    vi.advanceTimersByTime(5000);
    expect(flushed).toHaveLength(2);
  });
});

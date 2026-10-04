import type { DeviceStatus, Position, VehicleView } from '@/types';

/** Uma mudança vinda do tempo real, aplicada a um veículo. */
export type VehiclePatch = (vehicle: VehicleView) => VehicleView;

export const withPosition =
  (position: Position): VehiclePatch =>
  (vehicle) => ({ ...vehicle, lastPosition: position, connected: true });

export const withStatus =
  (status: DeviceStatus): VehiclePatch =>
  (vehicle) =>
    vehicle.device
      ? {
          ...vehicle,
          device: { ...vehicle.device, status },
          connected: status === 'ONLINE' ? vehicle.connected : false,
        }
      : vehicle;

export const withRelay =
  (relayOn: boolean | null): VehiclePatch =>
  (vehicle) => (vehicle.state ? { ...vehicle, state: { ...vehicle.state, relayOn } } : vehicle);

/**
 * Aplica as mudanças acumuladas (por rastreador, na ordem em que chegaram)
 * percorrendo a lista uma vez só.
 */
export function applyVehiclePatches(
  current: VehicleView[] | undefined,
  batch: Map<string, VehiclePatch[]>,
): VehicleView[] | undefined {
  if (!current || batch.size === 0) return current;
  return current.map((vehicle) => {
    const patches = vehicle.deviceId ? batch.get(vehicle.deviceId) : undefined;
    return patches ? patches.reduce((next, patch) => patch(next), vehicle) : vehicle;
  });
}

/**
 * Junta as mensagens do tempo real antes de mexer na lista. Com milhares de
 * rastreadores chegam centenas de posições por segundo, e aplicar uma a uma
 * refaria a lista inteira (e o mapa) a cada mensagem. Parado, a primeira
 * entra na hora; com movimento, no máximo uma vez por intervalMs.
 */
export function createUpdateBatcher(
  apply: (batch: Map<string, VehiclePatch[]>) => void,
  intervalMs: number,
  now: () => number = Date.now,
) {
  let pending = new Map<string, VehiclePatch[]>();
  let timer: ReturnType<typeof setTimeout> | undefined;
  let lastFlush = Number.NEGATIVE_INFINITY;

  const flush = () => {
    timer = undefined;
    lastFlush = now();
    const batch = pending;
    pending = new Map();
    apply(batch);
  };

  return {
    queue(deviceId: string, patch: VehiclePatch) {
      const list = pending.get(deviceId);
      if (list) list.push(patch);
      else pending.set(deviceId, [patch]);
      if (timer === undefined) timer = setTimeout(flush, Math.max(0, lastFlush + intervalMs - now()));
    },
    cancel() {
      clearTimeout(timer);
      timer = undefined;
      pending = new Map();
    },
  };
}

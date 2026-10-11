import type { VehicleView } from '@/types';

/** A situação do veículo para o filtro: comunicando, sem sinal ou aguardando o rastreador. */
export type Situation = 'online' | 'offline' | 'pending';

export function situationOf(vehicle: VehicleView): Situation {
  if (!vehicle.device) return 'pending';
  return vehicle.device.status === 'ONLINE' ? 'online' : 'offline';
}

/** A busca de veículos: pelo nome, placa, marca, modelo ou IMEI. */
export function matchesVehicle(vehicle: VehicleView, term: string): boolean {
  const t = term.trim().toLowerCase();
  if (!t) return true;
  return [vehicle.name, vehicle.plate, vehicle.brand, vehicle.model, vehicle.device?.imei].some((v) =>
    v?.toLowerCase().includes(t),
  );
}

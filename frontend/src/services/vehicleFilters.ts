import type { VehicleView } from '@/types';

/** A situação do veículo para o filtro: comunicando, sem sinal ou aguardando o rastreador. */
export type Situation = 'online' | 'offline' | 'pending';

export function situationOf(vehicle: VehicleView): Situation {
  if (!vehicle.device) return 'pending';
  return vehicle.device.status === 'ONLINE' ? 'online' : 'offline';
}

/** A busca de veículos: pelo nome, placa, marca, modelo, IMEI ou os campos personalizados. */
export function matchesVehicle(vehicle: VehicleView, term: string): boolean {
  const t = term.trim().toLowerCase();
  if (!t) return true;
  return [vehicle.name, vehicle.plate, vehicle.brand, vehicle.model, vehicle.device?.imei, vehicle.custom1, vehicle.custom2].some(
    (v) => v?.toLowerCase().includes(t),
  );
}

/** O filtro de grupo: '' todos, NO_GROUP os sem grupo, ou o id do grupo. */
export const NO_GROUP = 'sem-grupo';

export function inGroup(vehicle: Pick<VehicleView, 'groupId'>, filter: string): boolean {
  if (!filter) return true;
  if (filter === NO_GROUP) return !vehicle.groupId;
  return vehicle.groupId === filter;
}

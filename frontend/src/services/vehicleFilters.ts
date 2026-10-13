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
  return [
    vehicle.name,
    vehicle.plate,
    vehicle.brand,
    vehicle.model,
    vehicle.device?.imei,
    vehicle.custom1,
    vehicle.custom2,
    vehicle.ownerName,
  ].some((v) => v?.toLowerCase().includes(t));
}

/** O texto da busca: na central (com o dono de cada veículo), também pelo cliente. */
export function searchPlaceholder(vehicles: Pick<VehicleView, 'ownerName'>[]): string {
  return vehicles.some((v) => v.ownerName) ? 'Buscar por nome, placa, IMEI ou cliente' : 'Buscar por nome, placa ou IMEI';
}

/** O filtro de grupo: '' todos, NO_GROUP os sem grupo, ou o id do grupo. */
export const NO_GROUP = 'sem-grupo';

export function inGroup(vehicle: Pick<VehicleView, 'groupId'>, filter: string): boolean {
  if (!filter) return true;
  if (filter === NO_GROUP) return !vehicle.groupId;
  return vehicle.groupId === filter;
}

// Bem mais rápido que localeCompare a cada comparação (milhares por ordenação).
const collator = new Intl.Collator('pt-BR');
const STATUS_RANK: Record<string, number> = { ONLINE: 0, STALE: 1, OFFLINE: 2 };

/**
 * A ordem da lista de veículos (e do anterior/próximo): os online primeiro —
 * é o que o operador precisa ver sem rolar —, depois sem sinal, e por nome.
 */
export function sortVehicles<T extends Pick<VehicleView, 'name' | 'device'>>(vehicles: T[]): T[] {
  return [...vehicles].sort((a, b) => {
    const byStatus =
      (STATUS_RANK[a.device?.status ?? 'OFFLINE'] ?? 3) - (STATUS_RANK[b.device?.status ?? 'OFFLINE'] ?? 3);
    return byStatus !== 0 ? byStatus : collator.compare(a.name, b.name);
  });
}

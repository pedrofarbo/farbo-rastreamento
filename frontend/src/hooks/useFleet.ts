import { useQuery } from '@tanstack/react-query';

import { fleetApi } from '@/api/resources';
import { useAuth } from '@/stores/AuthContext';
import type { FleetSettings, VehicleGroup } from '@/types';

export const fleetKey = ['me', 'fleet'] as const;

/** Os nomes dos campos personalizados: o que a conta deu, ou o genérico. */
export function fieldLabel(fleet: FleetSettings | undefined, n: 1 | 2): string {
  const label = n === 1 ? fleet?.labels.label1 : fleet?.labels.label2;
  return label?.trim() || `Campo personalizado ${n}`;
}

/**
 * A organização da frota da conta do cliente (grupos e campos
 * personalizados). Para a central, nada.
 */
export function useFleet() {
  const { isCustomer } = useAuth();
  const query = useQuery({ queryKey: fleetKey, queryFn: fleetApi.get, enabled: isCustomer, staleTime: 60_000 });
  const groups: VehicleGroup[] = query.data?.groups ?? [];
  const groupName = (id: string | null | undefined) => (id ? (groups.find((g) => g.id === id)?.name ?? '') : '');
  return { fleet: query.data, groups, groupName, loading: query.isLoading };
}

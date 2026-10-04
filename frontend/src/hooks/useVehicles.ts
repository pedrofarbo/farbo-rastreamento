import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';

import { vehiclesApi } from '@/api/resources';
import { useRealtimeEvent } from '@/hooks/useRealtime';
import type { DeviceStatus, Position, VehicleView } from '@/types';

import { applyVehiclePatches, createUpdateBatcher, withPosition, withRelay, withStatus } from './vehicleUpdates';

export const vehiclesKey = ['vehicles'] as const;
export const vehicleKey = (id: string) => ['vehicle', id] as const;

/** A lista da frota se atualiza no máximo uma vez por segundo. */
const FLUSH_MS = 1000;

/**
 * Lista de veículos que se mantém viva.
 *
 * A consulta HTTP é o ponto de partida; a partir daí o WebSocket atualiza o
 * cache em memória. O refetch periódico é longo de propósito: ele existe só
 * para corrigir divergências se alguma mensagem se perder.
 */
export function useVehicles() {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: vehiclesKey,
    queryFn: vehiclesApi.list,
    refetchInterval: 60_000,
    staleTime: 10_000,
  });

  // As mensagens se acumulam e entram juntas na lista (ver createUpdateBatcher).
  const [batcher] = useState(() =>
    createUpdateBatcher((batch) => {
      queryClient.setQueryData<VehicleView[]>(vehiclesKey, (current) => applyVehiclePatches(current, batch));
      queryClient.invalidateQueries({ queryKey: ['vehicle'], exact: false, refetchType: 'none' });
    }, FLUSH_MS),
  );
  useEffect(() => () => batcher.cancel(), [batcher]);

  useRealtimeEvent(['position.updated'], (message) => {
    const position = message.data as Position | undefined;
    if (!position?.deviceId) return;
    batcher.queue(position.deviceId, withPosition(position));
  });

  useRealtimeEvent(['device.online', 'device.offline', 'device.stale'], (message) => {
    const payload = message.data as { deviceId?: string; status?: DeviceStatus } | undefined;
    const deviceId = payload?.deviceId ?? message.deviceId;
    if (!deviceId) return;

    const status: DeviceStatus =
      payload?.status ??
      (message.type === 'device.offline'
        ? 'OFFLINE'
        : message.type === 'device.stale'
          ? 'STALE'
          : 'ONLINE');
    batcher.queue(deviceId, withStatus(status));
  });

  useRealtimeEvent(['engine.status.changed'], (message) => {
    const payload = message.data as { deviceId?: string; relayOn?: boolean } | undefined;
    if (!payload?.deviceId || payload.relayOn === undefined) return;
    batcher.queue(payload.deviceId, withRelay(payload.relayOn ?? null));
  });

  return query;
}

/** Detalhe de um veículo, também atualizado pelo tempo real. */
export function useVehicle(id: string | undefined) {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: vehicleKey(id ?? ''),
    queryFn: () => vehiclesApi.get(id as string),
    enabled: Boolean(id),
    refetchInterval: 60_000,
  });

  const deviceId = query.data?.deviceId ?? null;

  useRealtimeEvent(['position.updated'], (message) => {
    const position = message.data as Position | undefined;
    if (!deviceId || position?.deviceId !== deviceId || !id) return;

    queryClient.setQueryData<VehicleView>(vehicleKey(id), (current) =>
      current ? { ...current, lastPosition: position, connected: true } : current,
    );
  });

  useRealtimeEvent(['engine.status.changed'], (message) => {
    const payload = message.data as { deviceId?: string; relayOn?: boolean } | undefined;
    if (!deviceId || payload?.deviceId !== deviceId || !id) return;

    queryClient.setQueryData<VehicleView>(vehicleKey(id), (current) =>
      current?.state
        ? { ...current, state: { ...current.state, relayOn: payload.relayOn ?? null } }
        : current,
    );
  });

  // Comandos mudam o histórico do veículo: recarrega a lista ao confirmar.
  useRealtimeEvent(['command.acknowledged', 'command.failed', 'command.sent'], () => {
    if (!id) return;
    queryClient.invalidateQueries({ queryKey: ['commands', id] });
  });

  useEffect(() => {
    if (!id) return;
    queryClient.invalidateQueries({ queryKey: vehicleKey(id), refetchType: 'none' });
  }, [id, queryClient]);

  return query;
}

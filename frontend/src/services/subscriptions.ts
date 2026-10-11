import type { Subscription } from '@/types';

/**
 * A assinatura de cada veículo: a ativa ou, sem ela, a encerrada mais
 * recente (para mostrar "Assinatura encerrada").
 */
export function subscriptionsByVehicle(subscriptions: Subscription[]): Map<string, Subscription> {
  const byVehicle = new Map<string, Subscription>();
  for (const sub of subscriptions) {
    if (!sub.vehicleId) continue;
    const current = byVehicle.get(sub.vehicleId);
    const better =
      !current ||
      (current.status !== 'ACTIVE' && (sub.status === 'ACTIVE' || sub.createdAt > current.createdAt));
    if (better) byVehicle.set(sub.vehicleId, sub);
  }
  return byVehicle;
}

/** Assinaturas ativas sem veículo: só existem em dados de antes do fluxo único. */
export function subscriptionsWithoutVehicle(subscriptions: Subscription[]): Subscription[] {
  return subscriptions.filter((sub) => sub.status === 'ACTIVE' && !sub.vehicleId);
}

/**
 * O vencimento da conta: o dia das assinaturas ativas, que vencem todas no
 * mesmo (numa conta de antes da regra, com dias diferentes, o mais comum; no
 * empate, o da mais antiga — como o servidor). Null sem nenhuma ativa.
 */
export function accountDueDay(subscriptions: Subscription[]): number | null {
  const days = new Map<number, { count: number; oldest: string }>();
  for (const sub of subscriptions) {
    if (sub.status !== 'ACTIVE') continue;
    const day = days.get(sub.dueDay);
    if (day) {
      day.count += 1;
      if (sub.createdAt < day.oldest) day.oldest = sub.createdAt;
    } else days.set(sub.dueDay, { count: 1, oldest: sub.createdAt });
  }
  let best: number | null = null;
  for (const [day, info] of days) {
    const current = best === null ? null : days.get(best)!;
    if (!current || info.count > current.count || (info.count === current.count && info.oldest < current.oldest)) best = day;
  }
  return best;
}

/** Os dias de vencimento das assinaturas ativas (mais de um: conta de antes da regra, para unificar). */
export function activeDueDays(subscriptions: Subscription[]): number[] {
  return [...new Set(subscriptions.filter((s) => s.status === 'ACTIVE').map((s) => s.dueDay))].sort((a, b) => a - b);
}

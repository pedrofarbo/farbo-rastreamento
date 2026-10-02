import type { AlertKind, AlertKindInfo, AlertNotificationStatus } from '@/types';
import type { BadgeTone } from '@/components/ui/Badge';

/** Tipos que só aparecem no histórico (não são escolhas da tela). */
const HISTORY_ONLY: Record<string, string> = {
  ENGINE_BLOCKED: 'Motor bloqueado',
  ENGINE_UNBLOCKED: 'Motor liberado',
  GEOFENCE_ENTER: 'Entrou na cerca',
  GEOFENCE_EXIT: 'Saiu da cerca',
  TEST: 'E-mail de teste',
};

/** Nome de um tipo do histórico, pelo catálogo que veio do servidor. */
export function alertKindLabel(kind: string, catalog: AlertKindInfo[]): string {
  if (HISTORY_ONLY[kind]) return HISTORY_ONLY[kind];
  return catalog.find((info) => info.kind === kind)?.label ?? kind;
}

export const ALERT_STATUS: Record<AlertNotificationStatus, { label: string; tone: BadgeTone }> = {
  SENT: { label: 'Enviado', tone: 'success' },
  PENDING: { label: 'Enviando', tone: 'neutral' },
  FAILED: { label: 'Não entregue', tone: 'danger' },
};

/** "HH:MM" válido (00:00 a 23:59). */
export function isClock(value: string): boolean {
  return /^([01]?\d|2[0-3]):[0-5]\d$/.test(value.trim());
}

/** Mesmas escolhas, em qualquer ordem? */
export function sameKinds(a: AlertKind[], b: AlertKind[]): boolean {
  return a.length === b.length && a.every((kind) => b.includes(kind));
}

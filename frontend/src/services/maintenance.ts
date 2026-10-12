import type { MaintenanceItem, MaintenanceStatus } from '@/types';

/** As manutenções mais comuns, para sugerir no nome. */
export const MAINTENANCE_SUGGESTIONS = [
  'Troca de óleo',
  'Filtro de ar',
  'Filtro de combustível',
  'Pneus',
  'Alinhamento e balanceamento',
  'Freios',
  'Correia dentada',
  'Revisão',
  'Licenciamento',
  'Seguro',
];

export const STATUS_LABEL: Record<MaintenanceStatus, string> = {
  due: 'Vencida',
  soon: 'Perto',
  ok: 'Em dia',
  unknown: 'Sem hodômetro',
};

/** 48312 → "48.312 km". */
export function formatKm(km: number): string {
  return `${Math.round(km).toLocaleString('pt-BR')} km`;
}

/** "A cada 10.000 km ou 6 meses". */
export function intervalLabel(item: Pick<MaintenanceItem, 'everyKm' | 'everyMonths'>): string {
  const parts: string[] = [];
  if (item.everyKm) parts.push(formatKm(item.everyKm));
  if (item.everyMonths) parts.push(item.everyMonths === 1 ? '1 mês' : `${item.everyMonths} meses`);
  return `A cada ${parts.join(' ou ')}`;
}

/** Quanto falta (ou passou): "faltam 900 km · faltam 8 dias", "passou 320 km". */
export function remainingLabel(item: Pick<MaintenanceItem, 'remainingKm' | 'remainingDays' | 'status'>): string {
  const parts: string[] = [];
  if (item.remainingKm !== null) {
    const km = Math.round(item.remainingKm);
    parts.push(km <= 0 ? `passou ${formatKm(-km)}` : `faltam ${formatKm(km)}`);
  }
  if (item.remainingDays !== null) {
    const d = item.remainingDays;
    parts.push(d < 0 ? `venceu há ${-d} ${-d === 1 ? 'dia' : 'dias'}` : d === 0 ? 'vence hoje' : `faltam ${d} ${d === 1 ? 'dia' : 'dias'}`);
  }
  if (parts.length === 0 && item.status === 'unknown') return 'informe o hodômetro do veículo para contar os km';
  return parts.join(' · ');
}

/** Um número digitado ("10.000", "10000", "") → número inteiro, null (vazio) ou NaN (inválido). */
export function parseWholeNumber(text: string): number | null {
  const clean = text.replace(/[.\s]/g, '').replace(',', '.');
  if (clean === '') return null;
  const n = Number(clean);
  return Number.isFinite(n) ? Math.round(n) : NaN;
}

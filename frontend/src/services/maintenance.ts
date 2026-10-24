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
];

/** Os documentos mais comuns, com de quantos em quantos meses renovam. */
export const DOCUMENT_PRESETS: { name: string; months: number }[] = [
  { name: 'IPVA', months: 12 },
  { name: 'Licenciamento (CRLV)', months: 12 },
  { name: 'Seguro', months: 12 },
  { name: 'CNH do motorista', months: 60 },
  { name: 'Aferição do tacógrafo', months: 24 },
  { name: 'Extintor', months: 12 },
];

/** "2026-11-05" → "05/11/2026". */
export function brDate(iso: string): string {
  const [y, m, d] = iso.split('-');
  return d && m && y ? `${d}/${m}/${y}` : iso;
}

/** De quanto em quanto tempo o documento renova: "todo ano", "a cada 5 anos", "a cada 6 meses". */
export function renewLabel(months: number): string {
  if (months === 12) return 'Renova todo ano';
  if (months % 12 === 0) return `Renova a cada ${months / 12} anos`;
  return months === 1 ? 'Renova todo mês' : `Renova a cada ${months} meses`;
}

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

/** 1234.4 → "1.234 h" (horas de motor). */
export function formatHours(hours: number): string {
  return `${Math.round(hours).toLocaleString('pt-BR')} h`;
}

/** "A cada 10.000 km ou 6 meses", "A cada 10.000 km, 250 h de motor ou 6 meses". */
export function intervalLabel(
  item: Pick<MaintenanceItem, 'everyKm' | 'everyMonths' | 'everyHours'> & Partial<Pick<MaintenanceItem, 'kind'>>,
): string {
  if (item.kind === 'document' && item.everyMonths) return renewLabel(item.everyMonths);
  const parts: string[] = [];
  if (item.everyKm) parts.push(formatKm(item.everyKm));
  if (item.everyHours) parts.push(`${formatHours(item.everyHours)} de motor`);
  if (item.everyMonths) parts.push(item.everyMonths === 1 ? '1 mês' : `${item.everyMonths} meses`);
  const last = parts.pop();
  return `A cada ${parts.length > 0 ? `${parts.join(', ')} ou ${last}` : last}`;
}

/** Quanto falta (ou passou): "faltam 900 km · faltam 8 dias", "passou 320 km", "faltam 40 h de motor". */
export function remainingLabel(
  item: Pick<MaintenanceItem, 'remainingKm' | 'remainingHours' | 'remainingDays' | 'status'> &
    Partial<Pick<MaintenanceItem, 'kind' | 'nextDate'>>,
): string {
  if (item.kind === 'document' && item.nextDate && item.remainingDays !== null && item.remainingDays > 0) {
    const d = item.remainingDays;
    return `vence em ${brDate(item.nextDate)} (faltam ${d} ${d === 1 ? 'dia' : 'dias'})`;
  }
  const parts: string[] = [];
  if (item.remainingKm !== null) {
    const km = Math.round(item.remainingKm);
    parts.push(km <= 0 ? `passou ${formatKm(-km)}` : `faltam ${formatKm(km)}`);
  }
  if (item.remainingHours != null) {
    const hours = Math.round(item.remainingHours);
    parts.push(hours <= 0 ? `passou ${formatHours(-hours)} de motor` : `faltam ${formatHours(hours)} de motor`);
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

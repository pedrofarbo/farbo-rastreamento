import type { ReportDailyRow, ReportTrip } from '@/types';

/** O período mais longo de um relatório (o mesmo do servidor). */
export const MAX_REPORT_DAYS = 31;

export type PeriodPreset = 'today' | 'yesterday' | 'week' | 'month' | 'lastMonth';

export const PRESETS: { id: PeriodPreset; label: string }[] = [
  { id: 'today', label: 'Hoje' },
  { id: 'yesterday', label: 'Ontem' },
  { id: 'week', label: '7 dias' },
  { id: 'month', label: 'Este mês' },
  { id: 'lastMonth', label: 'Mês passado' },
];

const pad = (n: number) => String(n).padStart(2, '0');
const iso = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const parse = (day: string) => {
  const [y, m, d] = day.split('-').map(Number);
  return new Date(y, m - 1, d);
};

/** O dia AAAA-MM-DD mais `n` dias. */
export function addDays(day: string, n: number): string {
  const d = parse(day);
  d.setDate(d.getDate() + n);
  return iso(d);
}

/** O período de um atalho, a partir de hoje (AAAA-MM-DD). */
export function presetPeriod(preset: PeriodPreset, today: string): { from: string; to: string } {
  switch (preset) {
    case 'today':
      return { from: today, to: today };
    case 'yesterday': {
      const y = addDays(today, -1);
      return { from: y, to: y };
    }
    case 'week':
      return { from: addDays(today, -6), to: today };
    case 'month':
      return { from: `${today.slice(0, 8)}01`, to: today };
    case 'lastMonth': {
      const firstOfMonth = parse(`${today.slice(0, 8)}01`);
      const lastDay = new Date(firstOfMonth.getFullYear(), firstOfMonth.getMonth(), 0);
      return { from: iso(new Date(lastDay.getFullYear(), lastDay.getMonth(), 1)), to: iso(lastDay) };
    }
  }
}

/** Os dias do período, do primeiro ao último. */
export function daysOf(from: string, to: string): string[] {
  const out: string[] = [];
  for (let d = from; d <= to && out.length <= MAX_REPORT_DAYS; d = addDays(d, 1)) out.push(d);
  return out;
}

/** O que há de errado com o período, ou null. */
export function periodProblem(from: string, to: string): string | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(from) || !/^\d{4}-\d{2}-\d{2}$/.test(to)) return 'Escolha as duas datas.';
  if (to < from) return 'O início do período precisa ser antes do fim.';
  if (daysOf(from, to).length > MAX_REPORT_DAYS) return `O período vai até ${MAX_REPORT_DAYS} dias.`;
  return null;
}

/** A frota inteira em cada dia do período (os dias sem movimento ficam zerados). */
export function dayTotals(rows: ReportDailyRow[], from: string, to: string): { day: string; km: number; trips: number }[] {
  const byDay = new Map<string, { km: number; trips: number }>();
  for (const r of rows) {
    const t = byDay.get(r.day) ?? { km: 0, trips: 0 };
    t.km += r.distanceKm;
    t.trips += r.trips;
    byDay.set(r.day, t);
  }
  return daysOf(from, to).map((day) => ({ day, ...(byDay.get(day) ?? { km: 0, trips: 0 }) }));
}

/** Uma viagem ou a parada entre duas, na ordem. */
export type TimelineItem =
  | { kind: 'trip'; trip: ReportTrip }
  | { kind: 'stop'; start: string; end: string; lat: number; lon: number; seconds: number };

/** As viagens com as paradas entre elas (onde chegou, até sair de novo). */
export function timeline(trips: ReportTrip[]): TimelineItem[] {
  const out: TimelineItem[] = [];
  trips.forEach((trip, i) => {
    if (i > 0) {
      const prev = trips[i - 1];
      const seconds = (Date.parse(trip.start) - Date.parse(prev.end)) / 1000;
      if (seconds > 0) out.push({ kind: 'stop', start: prev.end, end: trip.start, lat: prev.endLat, lon: prev.endLon, seconds });
    }
    out.push({ kind: 'trip', trip });
  });
  return out;
}

/** Duração legível: 3725 → "1 h 02 min"; 300 → "5 min"; 0 → "—". */
export function formatSpan(seconds: number): string {
  if (seconds <= 0) return '—';
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} min`;
  return `${Math.floor(minutes / 60)} h ${String(minutes % 60).padStart(2, '0')} min`;
}

/** "12,3 km" (vírgula decimal). */
export function formatKm(km: number): string {
  return `${km.toLocaleString('pt-BR', { minimumFractionDigits: 1, maximumFractionDigits: 1 })} km`;
}

/** O ponto no OpenStreetMap. */
export function mapLink(lat: number, lon: number): string {
  return `https://www.openstreetmap.org/?mlat=${lat.toFixed(6)}&mlon=${lon.toFixed(6)}#map=17/${lat.toFixed(6)}/${lon.toFixed(6)}`;
}

/** Dia e hora, sem os segundos: "05/10, 06:45". */
export function formatWhen(value: string): string {
  return new Date(value).toLocaleString('pt-BR', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
}

/** Só a hora: "08:40". */
export function formatClock(value: string): string {
  return new Date(value).toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' });
}

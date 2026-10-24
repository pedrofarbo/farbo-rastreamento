/**
 * Os itinerários: a situação da viagem, o atraso em texto, a próxima chegada
 * em cada ponto (para o passageiro) e a conferência dos pontos da linha.
 */
import type { BadgeTone } from '@/components/ui/Badge';
import type { BusRouteInput, PublicRouteView, TripProgress, TripStatus } from '@/types';

export const TRIP_STATUS: Record<TripStatus, { label: string; tone: BadgeTone }> = {
  scheduled: { label: 'Programada', tone: 'neutral' },
  running: { label: 'Em viagem', tone: 'accent' },
  finished: { label: 'Encerrada', tone: 'success' },
  canceled: { label: 'Cancelada', tone: 'neutral' },
};

/** 0 → "no horário"; 4 → "4 min atrasado"; -3 → "3 min adiantado". */
export function delayLabel(min: number | null): string {
  if (min === null) return '—';
  if (Math.abs(min) <= 1) return 'no horário';
  return min > 0 ? `${min} min atrasado` : `${-min} min adiantado`;
}

export function delayTone(min: number | null): BadgeTone {
  if (min === null || Math.abs(min) <= 2) return 'success';
  return min > 10 ? 'danger' : min > 0 ? 'warning' : 'accent';
}

/** O que falta para salvar a linha (vazio: pode salvar). */
export function routeProblem(input: BusRouteInput): string {
  if (!input.name.trim()) return 'Dê um nome à linha.';
  if (input.stops.length < 2) return 'A linha precisa de pelo menos 2 pontos (clique no mapa para pôr).';
  for (let i = 0; i < input.stops.length; i++) {
    const st = input.stops[i];
    if (!st.name.trim()) return `Dê um nome ao ponto ${i + 1}.`;
    if (i > 0 && st.offsetMin < input.stops[i - 1].offsetMin) {
      return `O horário do ponto ${i + 1} vem antes do anterior: os minutos desde a saída só aumentam.`;
    }
  }
  return '';
}

/** A próxima chegada prevista em cada ponto (o ônibus mais perto de chegar), em ms; nulo: nenhum previsto. */
export function nextArrivals(view: Pick<PublicRouteView, 'stops' | 'trips'>, now = Date.now()): (number | null)[] {
  return view.stops.map((stop) => {
    let best: number | null = null;
    for (const trip of view.trips) {
      const sp = trip.stops.find((s) => s.seq === stop.seq);
      if (!sp || sp.arrivedAt) continue;
      const at = Date.parse(sp.eta ?? sp.planned);
      if (at < now - 60_000) continue;
      if (best === null || at < best) best = at;
    }
    return best;
  });
}

/** "agora", "em 4 min", "às 14:30" (mais de uma hora). */
export function arrivalLabel(at: number | null, now = Date.now()): string {
  if (at === null) return 'sem previsão';
  const min = Math.round((at - now) / 60_000);
  if (min <= 1) return 'chegando';
  if (min < 60) return `em ${min} min`;
  return `às ${new Date(at).toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' })}`;
}

/** O ponto em que o ônibus está indo (o nome), ou vazio. */
export function nextStopName(trip: TripProgress): string {
  if (trip.nextSeq === null) return '';
  return trip.stops.find((s) => s.seq === trip.nextSeq)?.name ?? '';
}

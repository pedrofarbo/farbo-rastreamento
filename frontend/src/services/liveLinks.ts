import type { LiveLink, VehicleView } from '@/types';

// Os textos da localização ao vivo por um tempo (o cartão do veículo e a
// página do link).

/** "1 h", "24 h". */
export const durationLabel = (hours: number) => `${hours} h`;

const hourMinute = new Intl.DateTimeFormat('pt-BR', { hour: '2-digit', minute: '2-digit' });
const dayMonth = new Intl.DateTimeFormat('pt-BR', { day: '2-digit', month: '2-digit' });

/** Até quando o link vale: "até 18:40" hoje, "até 24/10 às 09:15" depois. */
export function untilLabel(expiresAt: string, now = new Date()): string {
  const end = new Date(expiresAt);
  const time = hourMinute.format(end);
  return end.toDateString() === now.toDateString() ? `até ${time}` : `até ${dayMonth.format(end)} às ${time}`;
}

/** Quantas vezes abriram: "ainda não aberto", "aberto 1 vez", "aberto 3 vezes". */
export function viewsLabel(views: number): string {
  if (views === 0) return 'ainda não aberto';
  return views === 1 ? 'aberto 1 vez' : `aberto ${views} vezes`;
}

/** A mensagem que vai junto com o link. */
export function liveShareText(vehicle: Pick<VehicleView, 'name'>, link: Pick<LiveLink, 'url' | 'expiresAt'>): string {
  return `Acompanhe ${vehicle.name} ao vivo (${untilLabel(link.expiresAt)}): ${link.url ?? ''}`;
}

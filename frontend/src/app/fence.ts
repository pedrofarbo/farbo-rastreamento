import type { Geofence } from '@/types';

/** Raios do controle deslizante: finos perto (casa, escola), largos longe
 * (bairro, cidade). Os limites são os do backend para o cliente. */
export const RADIUS_STEPS = [50, 100, 150, 200, 300, 500, 750, 1000, 1500, 2000, 3000, 5000, 10000, 20000, 50000];
export const DEFAULT_RADIUS = 200;
export const MAX_FENCES = 20;

/** Sugestões de nome (um toque preenche). */
export const NAME_SUGGESTIONS = ['Casa', 'Trabalho', 'Escola', 'Garagem'];

/** Posição do raio no controle: o passo mais próximo. */
export function radiusStep(meters: number): number {
  let best = 0;
  RADIUS_STEPS.forEach((step, index) => {
    if (Math.abs(step - meters) < Math.abs(RADIUS_STEPS[best] - meters)) best = index;
  });
  return best;
}

/** "200 m", "1 km", "1,5 km". */
export function formatRadius(meters: number): string {
  if (meters < 1000) return `${Math.round(meters)} m`;
  const km = meters / 1000;
  return `${Number.isInteger(km) ? km : km.toFixed(1).replace('.', ',')} km`;
}

/** Zoom em que o círculo inteiro cabe com folga na altura do mapa. */
export function zoomForRadius(meters: number, latitude: number, sizePx: number): number {
  const metersPerPixelAtZoom0 = 156543.03392 * Math.cos((latitude * Math.PI) / 180);
  const fit = Math.log2((metersPerPixelAtZoom0 * sizePx * 0.6) / (2 * meters));
  return Math.max(3, Math.min(18, Math.floor(fit)));
}

/** De que lado a cerca avisa, em palavras. */
export function notifyLabel(fence: Pick<Geofence, 'notifyEnter' | 'notifyExit'>): string {
  if (fence.notifyEnter && fence.notifyExit) return 'Avisa ao entrar e ao sair';
  if (fence.notifyEnter) return 'Avisa só ao entrar';
  if (fence.notifyExit) return 'Avisa só ao sair';
  return 'Sem aviso (só no histórico)';
}

export interface FenceForm {
  name: string;
  latitude: number;
  longitude: number;
  radiusMeters: number;
  vehicleIds: string[];
  notifyEnter: boolean;
  notifyExit: boolean;
}

/** O que impede salvar, ou null. */
export function fenceProblem(form: FenceForm): string | null {
  if (!form.name.trim()) return 'Dê um nome para a cerca.';
  if (form.name.trim().length > 100) return 'Use um nome com até 100 caracteres.';
  if (form.vehicleIds.length === 0) return 'Escolha ao menos um veículo.';
  return null;
}

/** As cercas que vigiam um veículo. */
export function fencesOf(fences: Geofence[], vehicleId: string): Geofence[] {
  return fences.filter((fence) => fence.vehicleIds.includes(vehicleId));
}

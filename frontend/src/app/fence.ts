import type { Geofence } from '@/types';

/** Raios do controle deslizante: finos perto (casa, escola), largos longe
 * (bairro, cidade). Os limites são os do backend para o cliente. */
export const RADIUS_STEPS = [50, 100, 150, 200, 300, 500, 750, 1000, 1500, 2000, 3000, 5000, 10000, 20000, 50000];
export const DEFAULT_RADIUS = 200;

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
  shape: 'circle' | 'polygon';
  latitude: number;
  longitude: number;
  radiusMeters: number;
  polygon: [number, number][] | null;
  vehicleIds: string[];
  notifyEnter: boolean;
  notifyExit: boolean;
  scheduleDays: number[];
  scheduleStart: number | null;
  scheduleEnd: number | null;
}

/** O formulário de uma cerca que existe (o desenho volta inteiro ao salvar). */
export function formFromFence(fence: Geofence): FenceForm {
  return {
    name: fence.name,
    shape: fence.shape ?? 'circle',
    latitude: fence.latitude,
    longitude: fence.longitude,
    radiusMeters: fence.radiusMeters,
    polygon: fence.polygon ?? null,
    vehicleIds: fence.vehicleIds,
    notifyEnter: fence.notifyEnter,
    notifyExit: fence.notifyExit,
    scheduleDays: fence.scheduleDays ?? [],
    scheduleStart: fence.scheduleStart ?? null,
    scheduleEnd: fence.scheduleEnd ?? null,
  };
}

/** O que impede salvar, ou null. */
export function fenceProblem(form: FenceForm): string | null {
  if (!form.name.trim()) return 'Dê um nome para a cerca.';
  if (form.name.trim().length > 100) return 'Use um nome com até 100 caracteres.';
  if (form.shape === 'polygon' && (form.polygon?.length ?? 0) < 3) return 'Desenhe a área com pelo menos 3 pontos.';
  if (form.vehicleIds.length === 0) return 'Escolha ao menos um veículo.';
  if ((form.scheduleStart === null) !== (form.scheduleEnd === null)) return 'Informe o início e o fim do horário.';
  if (form.scheduleStart !== null && form.scheduleStart === form.scheduleEnd) {
    return 'O horário precisa de início e fim diferentes.';
  }
  return null;
}

/** O tamanho, em palavras: "200 m" no círculo, "Área desenhada" no polígono. */
export function shapeLabel(fence: Pick<Geofence, 'shape' | 'radiusMeters'>): string {
  return fence.shape === 'polygon' ? 'Área desenhada' : formatRadius(fence.radiusMeters);
}

export const DAY_NAMES = ['Dom', 'Seg', 'Ter', 'Qua', 'Qui', 'Sex', 'Sáb'];

/** 480 → "08:00". */
export function minutesToClock(minutes: number): string {
  return `${String(Math.floor(minutes / 60)).padStart(2, '0')}:${String(minutes % 60).padStart(2, '0')}`;
}

/** "08:00" → 480; null se não for hora. */
export function clockToMinutes(clock: string): number | null {
  const m = /^(\d{1,2}):(\d{2})$/.exec(clock.trim());
  if (!m) return null;
  const h = Number(m[1]);
  const min = Number(m[2]);
  return h < 24 && min < 60 ? h * 60 + min : null;
}

/** Os dias em palavras: "Todos os dias", "Seg a sex", "Sáb e dom", "Seg, qua e sex". */
export function daysLabel(days: number[]): string {
  const set = [...new Set(days)].sort((a, b) => a - b);
  if (set.length === 0 || set.length === 7) return 'Todos os dias';
  if (set.join() === '1,2,3,4,5') return 'Seg a sex';
  if (set.join() === '0,6') return 'Sáb e dom';
  const names = set.map((d, i) => (i === 0 ? DAY_NAMES[d] : DAY_NAMES[d].toLowerCase()));
  return names.length === 1 ? names[0] : `${names.slice(0, -1).join(', ')} e ${names[names.length - 1]}`;
}

/** Quando a cerca avisa: "Avisa sempre", "Seg a sex, das 08:00 às 18:00". */
export function scheduleLabel(fence: Pick<Geofence, 'scheduleDays' | 'scheduleStart' | 'scheduleEnd'>): string {
  const days = fence.scheduleDays ?? [];
  const hasHours = fence.scheduleStart !== null && fence.scheduleEnd !== null && fence.scheduleStart !== undefined;
  if (days.length === 0 && !hasHours) return 'Avisa sempre';
  const hours = hasHours ? `das ${minutesToClock(fence.scheduleStart!)} às ${minutesToClock(fence.scheduleEnd!)}` : 'o dia todo';
  return `${daysLabel(days)}, ${hours}`;
}

/** As cercas que vigiam um veículo. */
export function fencesOf(fences: Geofence[], vehicleId: string): Geofence[] {
  return fences.filter((fence) => fence.vehicleIds.includes(vehicleId));
}

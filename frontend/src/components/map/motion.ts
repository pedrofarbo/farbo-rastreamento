import type { Position } from '@/types';

/**
 * O veículo em movimento no mapa. O rastreador manda a posição a cada ~30 s
 * andando (e a cada 3600 s desligado): em vez de pular de um ponto ao outro,
 * o marcador desliza até o ponto novo — e chega nele, a posição mostrada é
 * sempre a reportada — deixando um rastro dos últimos minutos.
 */

/** Quanto dura o deslize até o ponto novo. */
export const GLIDE_MS = 2500;
/** Intervalo máximo entre dois pontos para deslizar: mais que isso, o carro
 * estava parado ou sem sinal, e o marcador vai direto. */
export const MAX_GLIDE_GAP_S = 120;
/** Velocidade média acima disso entre dois pontos é salto de GPS. */
const MAX_GLIDE_KMH = 300;
/** O rastro: os pontos dos últimos minutos de movimento. */
export const TRAIL_WINDOW_S = 300;
export const TRAIL_MAX_POINTS = 12;

type Point = Pick<Position, 'latitude' | 'longitude' | 'gpsTimestamp'>;

/** Acelera e freia: o deslize sai e chega devagar. */
export function easeInOutCubic(t: number): number {
  const x = Math.min(1, Math.max(0, t));
  return x < 0.5 ? 4 * x * x * x : 1 - (-2 * x + 2) ** 3 / 2;
}

/** O ponto a uma fração t do caminho entre a e b (trechos de 30 s: em linha reta basta). */
export function interpolate(
  a: { lat: number; lng: number },
  b: { lat: number; lng: number },
  t: number,
): { lat: number; lng: number } {
  return { lat: a.lat + (b.lat - a.lat) * t, lng: a.lng + (b.lng - a.lng) * t };
}

/** Distância em metros (haversine). */
export function distanceMeters(a: Pick<Point, 'latitude' | 'longitude'>, b: Pick<Point, 'latitude' | 'longitude'>): number {
  const rad = Math.PI / 180;
  const dLat = (b.latitude - a.latitude) * rad;
  const dLon = (b.longitude - a.longitude) * rad;
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a.latitude * rad) * Math.cos(b.latitude * rad) * Math.sin(dLon / 2) ** 2;
  return 2 * 6_371_000 * Math.asin(Math.min(1, Math.sqrt(h)));
}

/** Segundos de a até b (pelo relógio do GPS). */
function gapSeconds(a: Point, b: Point): number {
  return (Date.parse(b.gpsTimestamp) - Date.parse(a.gpsTimestamp)) / 1000;
}

/**
 * Se o marcador desliza de prev até next: pontos em sequência (o novo depois
 * do anterior), perto no tempo e numa velocidade possível. Depois de horas
 * parado, ou num salto do GPS, ele vai direto.
 */
export function shouldGlide(prev: Point, next: Point): boolean {
  const gap = gapSeconds(prev, next);
  if (!(gap > 0) || gap > MAX_GLIDE_GAP_S) return false;
  const meters = distanceMeters(prev, next);
  return meters > 0 && (meters / gap) * 3.6 <= MAX_GLIDE_KMH;
}

/**
 * O rastro depois do ponto novo: só enquanto anda; um ponto que não continua
 * o caminho (horas depois, salto) começa um rastro novo; ficam os pontos dos
 * últimos TRAIL_WINDOW_S segundos, no máximo TRAIL_MAX_POINTS.
 */
export function nextTrail<T extends Point>(trail: T[], next: T, moving: boolean): T[] {
  if (!moving) return [];
  const last = trail[trail.length - 1];
  if (last && last.gpsTimestamp === next.gpsTimestamp) return trail;
  const kept = last && shouldGlide(last, next) ? trail : [];
  const newest = Date.parse(next.gpsTimestamp);
  return [...kept, next]
    .filter((p) => newest - Date.parse(p.gpsTimestamp) <= TRAIL_WINDOW_S * 1000)
    .slice(-TRAIL_MAX_POINTS);
}

/**
 * A opacidade de cada trecho do rastro, do mais antigo ao que chega no carro:
 * forte perto dele e mais clara a cada trecho para trás, pela idade (a cada
 * ponto novo, os trechos antigos só clareiam um degrau, sem sumir de uma vez).
 */
export function trailOpacities(segments: number): number[] {
  return Array.from({ length: Math.max(0, segments) }, (_, i) =>
    Math.max(0.12, 0.85 * (1 - (segments - 1 - i) / TRAIL_MAX_POINTS)),
  );
}

/** Quem pede menos movimento no sistema não vê o deslize nem as ondas. */
export function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia('(prefers-reduced-motion: reduce)').matches
    : false;
}

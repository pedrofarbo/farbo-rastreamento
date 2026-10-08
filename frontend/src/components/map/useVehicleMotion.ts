import { useEffect, useRef } from 'react';
import type { RefObject } from 'react';
import { useMap } from 'react-leaflet';
import L from 'leaflet';

import type { Position } from '@/types';

import { GLIDE_MS, easeInOutCubic, interpolate, nextTrail, prefersReducedMotion, shouldGlide, trailOpacities } from './motion';

/** O verde da marca, o mesmo do trajeto. */
const TRAIL_COLOR = '#3be558';

/**
 * Leva o marcador do veículo até cada posição nova: deslizando quando o
 * carro anda (e o ponto continua o caminho), direto nos outros casos. Deixa o
 * rastro dos últimos minutos, que some na cauda e acompanha o carro. Seguindo
 * o veículo, o mapa vai junto, quadro a quadro.
 *
 * O marcador é posicionado só por aqui: o <Marker> recebe a posição inicial e
 * nunca outra, senão o react-leaflet o mandaria direto ao ponto novo.
 */
export function useVehicleMotion(
  markerRef: RefObject<L.Marker | null>,
  position: Position,
  moving: boolean,
  follow: boolean,
) {
  const map = useMap();
  const last = useRef<Position | null>(null);
  const trail = useRef<Position[]>([]);
  const layers = useRef<L.Polyline[]>([]);
  const frame = useRef(0);
  const followRef = useRef(follow);
  followRef.current = follow;

  useEffect(() => {
    const marker = markerRef.current;
    if (!marker) return;
    const prev = last.current;
    last.current = position;
    trail.current = nextTrail(trail.current, position, moving);

    const to = L.latLng(position.latitude, position.longitude);
    const from = marker.getLatLng();
    cancelAnimationFrame(frame.current);
    const glide = prev !== null && moving && !prefersReducedMotion() && shouldGlide(prev, position) && !from.equals(to);

    // O rastro: os trechos entre os pontos, mais o último, que termina no
    // carro. Os trechos já desenhados são reaproveitados (apagar e desenhar
    // de novo faz o mapa piscar).
    const points = trail.current;
    const opacities = trailOpacities(points.length - 1);
    for (let i = 0; i < points.length - 1; i++) {
      const head = i === points.length - 2;
      const latLngs: L.LatLngExpression[] = [
        [points[i].latitude, points[i].longitude],
        head && glide ? from : [points[i + 1].latitude, points[i + 1].longitude],
      ];
      const existing = layers.current[i];
      if (existing) {
        existing.setLatLngs(latLngs);
        existing.setStyle({ opacity: opacities[i] });
      } else {
        layers.current.push(
          L.polyline(latLngs, { color: TRAIL_COLOR, weight: 4, opacity: opacities[i], lineCap: 'round', interactive: false }).addTo(map),
        );
      }
    }
    for (const extra of layers.current.splice(Math.max(0, points.length - 1))) extra.remove();
    const headLayer = layers.current[layers.current.length - 1];

    if (!glide) {
      marker.setLatLng(to);
      if (followRef.current && prev !== null) map.setView(to, map.getZoom(), { animate: true });
      return;
    }
    const start = performance.now();
    const step = (now: number) => {
      const t = Math.min(1, (now - start) / GLIDE_MS);
      const at = interpolate(from, to, easeInOutCubic(t));
      marker.setLatLng(at);
      if (headLayer) {
        const tail = points[points.length - 2];
        headLayer.setLatLngs([[tail.latitude, tail.longitude], at]);
      }
      if (followRef.current) map.setView(at, map.getZoom(), { animate: false });
      if (t < 1) frame.current = requestAnimationFrame(step);
    };
    frame.current = requestAnimationFrame(step);
    // Só a posição (e o andar) disparam o movimento; o ref e o mapa não mudam.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [position.gpsTimestamp, position.latitude, position.longitude, moving]);

  // Ao sair do mapa: para o deslize e apaga o rastro.
  useEffect(
    () => () => {
      cancelAnimationFrame(frame.current);
      for (const layer of layers.current) layer.remove();
      layers.current = [];
    },
    [],
  );
}

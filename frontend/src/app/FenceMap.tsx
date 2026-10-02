import { useEffect, useRef, useState } from 'react';
import { MapContainer, Marker, useMap } from 'react-leaflet';
import L from 'leaflet';

import { vehicleIcon } from '@/components/map/markers';
import { OsmTiles } from '@/components/map/OsmTiles';
import type { VehicleView } from '@/types';

import { zoomForRadius } from './fence';
import styles from './FenceMap.module.css';

/** Verde da marca, o mesmo das cercas no mapa principal. */
const BRAND_GREEN = '#3be558';

/** Para onde o mapa vai; trocar a chave leva de novo ao mesmo ponto. */
export interface FenceFocus {
  lat: number;
  lon: number;
  key: number;
}

interface FenceMapProps {
  focus: FenceFocus;
  radius: number;
  vehicles: VehicleView[];
  /** O centro do mapa é o centro da cerca: chamado quando o mapa para. */
  onCenter: (lat: number, lon: number) => void;
}

/**
 * Editor da cerca no mapa: o pino fica parado no meio da tela e o cliente
 * arrasta o mapa por baixo dele (como escolher o ponto num app de corrida).
 * O círculo acompanha o centro enquanto o mapa se move, e o zoom acompanha o
 * raio para a cerca caber inteira.
 */
export function FenceMap({ focus, radius, vehicles, onCenter }: FenceMapProps) {
  const [moving, setMoving] = useState(false);
  const located = vehicles.filter((vehicle) => vehicle.lastPosition);

  return (
    <div className={`${styles.wrapper} ${moving ? styles.moving : ''}`}>
      <MapContainer
        center={[focus.lat, focus.lon]}
        zoom={16}
        className={styles.map}
        zoomControl={false}
        // Zoom de pinça, roda e toque duplo sem tirar o centro do lugar: o
        // centro é a cerca.
        touchZoom="center"
        scrollWheelZoom="center"
        doubleClickZoom="center"
      >
        <OsmTiles />
        {located.map((vehicle) => {
          const position = vehicle.lastPosition!;
          return (
            <Marker
              key={vehicle.id}
              position={[position.latitude, position.longitude]}
              interactive={false}
              icon={vehicleIcon({
                ignition: vehicle.state?.acc ?? position.acc ?? null,
                heading: position.heading,
                moving: false,
                selected: false,
                blocked: vehicle.state?.relayOn === true,
                online: vehicle.device?.status === 'ONLINE',
              })}
            />
          );
        })}
        <FenceController focus={focus} radius={radius} onCenter={onCenter} onMoving={setMoving} />
      </MapContainer>

      <div className={styles.pin} aria-hidden="true">
        <svg viewBox="0 0 32 44" width="32" height="44">
          <path
            d="M16 1C8 1 2 7 2 14.6 2 25 16 42 16 42s14-17 14-27.4C30 7 24 1 16 1z"
            fill={BRAND_GREEN}
            stroke="#060907"
            strokeWidth="2"
          />
          <circle cx="16" cy="15" r="5" fill="#060907" />
        </svg>
      </div>
      <span className={styles.dot} aria-hidden="true" />
      <p className={styles.hint}>Arraste o mapa: a cerca fica no centro</p>
    </div>
  );
}

function FenceController({
  focus,
  radius,
  onCenter,
  onMoving,
}: {
  focus: FenceFocus;
  radius: number;
  onCenter: (lat: number, lon: number) => void;
  onMoving: (moving: boolean) => void;
}) {
  const map = useMap();
  const circle = useRef<L.Circle | null>(null);
  const callbacks = useRef({ onCenter, onMoving });
  callbacks.current = { onCenter, onMoving };
  const radiusRef = useRef(radius);
  radiusRef.current = radius;

  const fitZoom = (latitude: number) => {
    const size = map.getSize();
    return zoomForRadius(radiusRef.current, latitude, Math.min(size.x, size.y) || 320);
  };

  useEffect(() => {
    const ring = L.circle(map.getCenter(), {
      radius: radiusRef.current,
      color: BRAND_GREEN,
      weight: 2,
      fillColor: BRAND_GREEN,
      fillOpacity: 0.14,
      interactive: false,
    }).addTo(map);
    circle.current = ring;

    const move = () => ring.setLatLng(map.getCenter());
    const start = () => callbacks.current.onMoving(true);
    const end = () => {
      callbacks.current.onMoving(false);
      const center = map.getCenter();
      callbacks.current.onCenter(center.lat, center.lng);
    };
    map.on('move', move);
    map.on('movestart', start);
    map.on('moveend', end);
    // O mapa nasce dentro de uma tela que ainda está deslizando para dentro:
    // mede de novo quando o tamanho muda.
    const observer = new ResizeObserver(() => map.invalidateSize());
    observer.observe(map.getContainer());
    return () => {
      map.off('move', move);
      map.off('movestart', start);
      map.off('moveend', end);
      observer.disconnect();
      ring.remove();
      circle.current = null;
    };
  }, [map]);

  // Raio novo: o círculo muda e o zoom acompanha (sem sair do centro).
  useEffect(() => {
    circle.current?.setRadius(radius);
    map.setZoom(fitZoom(map.getCenter().lat));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [map, radius]);

  // Foco novo (veículo, minha localização): vai até lá. Só a chave dispara:
  // o centro que o cliente arrastou não pode puxar o mapa de volta.
  useEffect(() => {
    map.setView([focus.lat, focus.lon], fitZoom(focus.lat));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [map, focus.key]);

  return null;
}

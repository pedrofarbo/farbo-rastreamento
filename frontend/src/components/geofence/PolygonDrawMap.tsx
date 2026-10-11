import { useEffect } from 'react';
import { CircleMarker, MapContainer, Marker, Polygon, Polyline, useMap, useMapEvents } from 'react-leaflet';
import L from 'leaflet';

import { vehicleIcon } from '@/components/map/markers';
import { OsmTiles } from '@/components/map/OsmTiles';
import type { VehicleView } from '@/types';

import styles from './PolygonDrawMap.module.css';

const BRAND_GREEN = '#3be558';

/**
 * Desenhar a área da cerca: cada clique no mapa põe um ponto (na ordem da
 * borda); com 3 ou mais, a área aparece preenchida. Arrastar o mapa não põe
 * ponto. Os veículos aparecem para orientar.
 */
export function PolygonDrawMap({
  points,
  onChange,
  center,
  vehicles,
}: {
  points: [number, number][];
  onChange: (points: [number, number][]) => void;
  center: [number, number];
  vehicles: VehicleView[];
}) {
  return (
    <div className={styles.wrapper}>
      <MapContainer center={center} zoom={16} className={styles.map} doubleClickZoom={false}>
        <OsmTiles />
        {vehicles
          .filter((v) => v.lastPosition)
          .map((v) => (
            <Marker
              key={v.id}
              position={[v.lastPosition!.latitude, v.lastPosition!.longitude]}
              interactive={false}
              icon={vehicleIcon({
                ignition: v.state?.acc ?? v.lastPosition!.acc ?? null,
                heading: v.lastPosition!.heading,
                moving: false,
                selected: false,
                blocked: v.state?.relayOn === true,
                online: v.device?.status === 'ONLINE',
                kind: v.kind,
              })}
            />
          ))}
        {points.length >= 3 ? (
          <Polygon positions={points} pathOptions={{ color: BRAND_GREEN, weight: 2, fillOpacity: 0.15 }} interactive={false} />
        ) : (
          points.length === 2 && <Polyline positions={points} pathOptions={{ color: BRAND_GREEN, weight: 2 }} interactive={false} />
        )}
        {points.map((p, i) => (
          <CircleMarker
            key={`${i}-${p[0]}-${p[1]}`}
            center={p}
            radius={i === points.length - 1 ? 6 : 4}
            pathOptions={{ color: '#060907', weight: 2, fillColor: BRAND_GREEN, fillOpacity: 1 }}
            interactive={false}
          />
        ))}
        <ClickToAdd onAdd={(p) => onChange([...points, p])} />
        <FitOnce points={points} />
      </MapContainer>
      <p className={styles.hint}>
        {points.length === 0
          ? 'Clique no mapa para pôr o primeiro ponto da área'
          : points.length < 3
            ? `Mais ${3 - points.length} ${3 - points.length === 1 ? 'ponto' : 'pontos'} para fechar a área`
            : `${points.length} pontos · clique para pôr mais`}
      </p>
    </div>
  );
}

function ClickToAdd({ onAdd }: { onAdd: (p: [number, number]) => void }) {
  useMapEvents({
    click: (e) => onAdd([Number(e.latlng.lat.toFixed(6)), Number(e.latlng.lng.toFixed(6))]),
  });
  return null;
}

/** Ao abrir uma área que já existe, enquadra o desenho (uma vez). */
function FitOnce({ points }: { points: [number, number][] }) {
  const map = useMap();
  useEffect(() => {
    if (points.length >= 3) map.fitBounds(L.latLngBounds(points), { padding: [40, 40], maxZoom: 18 });
    // Só na abertura: pôr pontos não mexe no enquadramento.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [map]);
  return null;
}

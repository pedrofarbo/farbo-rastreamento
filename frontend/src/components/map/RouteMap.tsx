import { useEffect, useRef } from 'react';
import { CircleMarker, MapContainer, Polyline, Tooltip, useMap, useMapEvents } from 'react-leaflet';

import { OsmTiles } from './OsmTiles';

interface RoutePoint {
  name: string;
  latitude: number;
  longitude: number;
}

interface Bus {
  id: string;
  label: string;
  latitude: number;
  longitude: number;
  offRoute?: boolean;
}

/** Enquadra os pontos da linha na primeira vez (e quando o número deles muda). */
function FitBounds({ points }: { points: RoutePoint[] }) {
  const map = useMap();
  const count = useRef(-1);
  useEffect(() => {
    if (points.length === count.current) return;
    count.current = points.length;
    if (points.length === 1) map.setView([points[0].latitude, points[0].longitude], 15);
    else if (points.length > 1) map.fitBounds(points.map((p) => [p.latitude, p.longitude] as [number, number]), { padding: [24, 24] });
  }, [map, points]);
  return null;
}

function ClickToAdd({ onAdd }: { onAdd: (lat: number, lon: number) => void }) {
  useMapEvents({ click: (e) => onAdd(e.latlng.lat, e.latlng.lng) });
  return null;
}

/**
 * O mapa de uma linha: os pontos em ordem, ligados por retas, e os ônibus
 * (a última posição de cada viagem). Com onAdd, clicar no mapa põe um ponto.
 */
export function RouteMap({
  stops,
  color,
  buses = [],
  onAdd,
  className,
  center = [-23.55, -46.63],
}: {
  stops: RoutePoint[];
  color: string;
  buses?: Bus[];
  onAdd?: (lat: number, lon: number) => void;
  className?: string;
  center?: [number, number];
}) {
  const first = stops[0];
  return (
    <MapContainer
      center={first ? [first.latitude, first.longitude] : center}
      zoom={first ? 14 : 11}
      scrollWheelZoom
      className={className}
      style={onAdd ? { cursor: 'crosshair' } : undefined}
    >
      <OsmTiles />
      {stops.length > 1 && (
        <Polyline positions={stops.map((s) => [s.latitude, s.longitude] as [number, number])} pathOptions={{ color, weight: 5, opacity: 0.7 }} />
      )}
      {stops.map((s, i) => (
        <CircleMarker
          key={`${i}-${s.latitude}-${s.longitude}`}
          center={[s.latitude, s.longitude]}
          radius={8}
          pathOptions={{ color: '#ffffff', weight: 2, fillColor: color, fillOpacity: 1 }}
        >
          <Tooltip direction="top" offset={[0, -6]}>
            {i + 1}. {s.name || 'Ponto sem nome'}
          </Tooltip>
        </CircleMarker>
      ))}
      {buses.map((b) => (
        <CircleMarker
          key={b.id}
          center={[b.latitude, b.longitude]}
          radius={11}
          pathOptions={{ color: b.offRoute ? '#ef4444' : '#0f172a', weight: 3, fillColor: '#facc15', fillOpacity: 1 }}
        >
          <Tooltip direction="top" offset={[0, -8]} permanent>
            🚌 {b.label}
          </Tooltip>
        </CircleMarker>
      ))}
      <FitBounds points={stops} />
      {onAdd && <ClickToAdd onAdd={onAdd} />}
    </MapContainer>
  );
}

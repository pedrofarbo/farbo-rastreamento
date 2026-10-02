import { TileLayer } from 'react-leaflet';

/** Mapa base do OpenStreetMap, igual em todos os mapas do painel e do app. */
export function OsmTiles() {
  return (
    <TileLayer
      attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>'
      url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
      maxZoom={19}
      // O OpenStreetMap exige Referer nos tiles: vale mesmo que a página
      // (ou o proxy na frente) use uma política mais restrita.
      referrerPolicy="strict-origin-when-cross-origin"
    />
  );
}

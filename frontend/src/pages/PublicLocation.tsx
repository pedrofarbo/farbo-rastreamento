import { useEffect, useState } from 'react';
import { MapContainer, Marker, useMap } from 'react-leaflet';

import { ApiError } from '@/api/client';
import { vehicleIcon } from '@/components/map/markers';
import { OsmTiles } from '@/components/map/OsmTiles';
import { formatCoordinates, formatDateTime, formatRelative, formatSpeed } from '@/services/format';
import type { PublicTheftView } from '@/types';

import styles from './PublicTheft.module.css';

/**
 * O que os links públicos de localização têm em comum (o do modo roubo e o
 * da localização ao vivo por um tempo): a página fora dos buscadores, a
 * consulta a cada poucos segundos e o mapa com o endereço.
 */

/** De quanto em quanto tempo a página pergunta a posição de novo. */
export const REFRESH_MS = 10_000;

/** O título da aba, fora dos buscadores e sem passar o link adiante no Referer. */
export function useNoIndex(title: string) {
  useEffect(() => {
    document.title = title;
    const metas = [
      Object.assign(document.createElement('meta'), { name: 'robots', content: 'noindex, nofollow' }),
      Object.assign(document.createElement('meta'), { name: 'referrer', content: 'no-referrer' }),
    ];
    metas.forEach((m) => document.head.appendChild(m));
    return () => metas.forEach((m) => m.remove());
  }, [title]);
}

/**
 * Pergunta a posição a cada REFRESH_MS. ended: o link não vale mais (410 ou
 * 404); error: sem conexão agora (tenta de novo sozinho).
 */
export function usePublicPoll<T>(load: (token: string) => Promise<T>, token: string) {
  const [view, setView] = useState<T | null>(null);
  const [ended, setEnded] = useState(false);
  const [error, setError] = useState('');
  const [, tick] = useState(0);

  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const run = () => {
      load(token)
        .then((v) => {
          if (!alive) return;
          setView(v);
          setError('');
          timer = setTimeout(run, REFRESH_MS);
        })
        .catch((err) => {
          if (!alive) return;
          if (err instanceof ApiError && (err.status === 410 || err.status === 404)) {
            setEnded(true);
            return;
          }
          setError('Sem conexão agora: tentando de novo…');
          timer = setTimeout(run, REFRESH_MS);
        });
    };
    run();
    return () => {
      alive = false;
      if (timer) clearTimeout(timer);
    };
  }, [load, token]);

  // "há 12 s" anda sozinho entre uma consulta e outra.
  useEffect(() => {
    const id = setInterval(() => tick((n) => n + 1), 5_000);
    return () => clearInterval(id);
  }, []);

  return { view, ended, error };
}

/** Segue o veículo: centraliza a cada posição nova. */
function Follow({ lat, lon }: { lat: number; lon: number }) {
  const map = useMap();
  useEffect(() => {
    map.setView([lat, lon], Math.max(map.getZoom(), 16));
  }, [map, lat, lon]);
  return null;
}

type Location = Pick<PublicTheftView, 'position' | 'address' | 'ignition' | 'online'>;

/** O mapa e o cartão da última posição (ou o aviso de que ainda não há). */
export function LocationCards({ view, error, mapClass }: { view: Location; error: string; mapClass?: string }) {
  const p = view.position;
  if (!p) {
    return (
      <section className={styles.card}>
        <p className={styles.text}>O rastreador ainda não mandou posição. A página atualiza sozinha.</p>
      </section>
    );
  }
  const maps = `https://www.google.com/maps/search/?api=1&query=${p.latitude.toFixed(6)},${p.longitude.toFixed(6)}`;
  return (
    <>
      <div className={`${styles.map} ${mapClass ?? ''}`}>
        <MapContainer center={[p.latitude, p.longitude]} zoom={16} scrollWheelZoom className={styles.leaflet}>
          <OsmTiles />
          <Marker
            position={[p.latitude, p.longitude]}
            icon={vehicleIcon({
              ignition: view.ignition,
              heading: p.heading,
              moving: p.speedKmh > 3,
              selected: true,
              blocked: false,
              online: view.online,
            })}
          />
          <Follow lat={p.latitude} lon={p.longitude} />
        </MapContainer>
      </div>
      <section className={styles.card} aria-live="polite">
        <span className={styles.label}>Última posição</span>
        <strong className={styles.address}>{view.address || formatCoordinates(p.latitude, p.longitude)}</strong>
        <dl className={styles.facts}>
          <div>
            <dt>Atualizada</dt>
            <dd>{formatRelative(p.gpsTimestamp)}</dd>
          </div>
          <div>
            <dt>Velocidade</dt>
            <dd>{formatSpeed(p.speedKmh)}</dd>
          </div>
          <div>
            <dt>Rastreador</dt>
            <dd>{view.online ? 'conectado' : 'sem conexão'}</dd>
          </div>
        </dl>
        <span className={styles.muted}>
          {formatCoordinates(p.latitude, p.longitude)} · {formatDateTime(p.gpsTimestamp)}
        </span>
        <a className={styles.button} href={maps} target="_blank" rel="noreferrer noopener">
          Abrir no Google Maps
        </a>
        {error && <span className={styles.muted}>{error}</span>}
      </section>
    </>
  );
}

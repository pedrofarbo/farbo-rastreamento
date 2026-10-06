import { useEffect, useState } from 'react';
import { MapContainer, Marker, useMap } from 'react-leaflet';
import { useParams } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { theftApi } from '@/api/resources';
import { vehicleIcon } from '@/components/map/markers';
import { OsmTiles } from '@/components/map/OsmTiles';
import { formatCoordinates, formatDateTime, formatRelative, formatSpeed } from '@/services/format';
import type { PublicTheftView } from '@/types';

import base from './PreLaunch.module.css';
import styles from './PublicTheft.module.css';

/** De quanto em quanto tempo a página pergunta a posição de novo. */
const REFRESH_MS = 10_000;

/** Segue o veículo: centraliza a cada posição nova. */
function Follow({ lat, lon }: { lat: number; lon: number }) {
  const map = useMap();
  useEffect(() => {
    map.setView([lat, lon], Math.max(map.getZoom(), 16));
  }, [map, lat, lon]);
  return null;
}

/**
 * O link do modo roubo (/localizar/<link secreto>): a posição ao vivo do
 * veículo roubado, sem login, para a polícia e para quem ajuda. Só o
 * veículo e onde ele está — nada do dono. Para de funcionar quando o modo
 * roubo é desligado. Não vai para buscadores nem passa adiante no Referer.
 */
export function PublicTheftPage() {
  const { token = '' } = useParams();
  const [view, setView] = useState<PublicTheftView | null>(null);
  const [ended, setEnded] = useState(false);
  const [error, setError] = useState('');
  const [, tick] = useState(0);

  useEffect(() => {
    document.title = 'Veículo roubado · localização ao vivo';
    const metas = [
      Object.assign(document.createElement('meta'), { name: 'robots', content: 'noindex, nofollow' }),
      Object.assign(document.createElement('meta'), { name: 'referrer', content: 'no-referrer' }),
    ];
    metas.forEach((m) => document.head.appendChild(m));
    return () => metas.forEach((m) => m.remove());
  }, []);

  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const load = () => {
      theftApi
        .publicView(token)
        .then((v) => {
          if (!alive) return;
          setView(v);
          setError('');
          timer = setTimeout(load, REFRESH_MS);
        })
        .catch((err) => {
          if (!alive) return;
          if (err instanceof ApiError && (err.status === 410 || err.status === 404)) {
            setEnded(true);
            return;
          }
          setError('Sem conexão agora: tentando de novo…');
          timer = setTimeout(load, REFRESH_MS);
        });
    };
    load();
    return () => {
      alive = false;
      if (timer) clearTimeout(timer);
    };
  }, [token]);

  // "há 12 s" anda sozinho entre uma consulta e outra.
  useEffect(() => {
    const id = setInterval(() => tick((n) => n + 1), 5_000);
    return () => clearInterval(id);
  }, []);

  const p = view?.position ?? null;
  const vehicle = view?.vehicle;
  const details = vehicle ? [vehicle.brand, vehicle.model, vehicle.year, vehicle.color].filter(Boolean).join(' · ') : '';
  const maps = p ? `https://www.google.com/maps/search/?api=1&query=${p.latitude.toFixed(6)},${p.longitude.toFixed(6)}` : '';

  return (
    <main className={`${base.page} ${styles.page}`}>
      <header className={base.header}>
        <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={base.logo} />
        <span className={styles.badge}>Veículo roubado</span>
      </header>

      {ended ? (
        <section className={styles.card}>
          <h1 className={styles.title}>Este link não está mais ativo</h1>
          <p className={styles.text}>
            O dono encerrou o modo roubo (o veículo pode ter sido recuperado) ou o prazo acabou. A localização não aparece
            mais aqui.
          </p>
        </section>
      ) : !view ? (
        <p className={base.hint} role="status">
          {error || 'Carregando a localização…'}
        </p>
      ) : (
        <>
          <section className={styles.card}>
            <h1 className={styles.title}>{vehicle?.name}</h1>
            {vehicle?.plate && <span className={styles.plate}>{vehicle.plate}</span>}
            {details && <p className={styles.text}>{details}</p>}
            <p className={styles.muted}>Modo roubo ativado em {formatDateTime(view.activatedAt)}.</p>
          </section>

          {p ? (
            <>
              <div className={styles.map}>
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
          ) : (
            <section className={styles.card}>
              <p className={styles.text}>O rastreador ainda não mandou posição. A página atualiza sozinha.</p>
            </section>
          )}

          <p className={styles.muted}>
            A página atualiza sozinha a cada {REFRESH_MS / 1000} s. Em caso de emergência, ligue 190.
          </p>
        </>
      )}
    </main>
  );
}

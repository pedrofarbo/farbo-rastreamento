import { useParams } from 'react-router-dom';

import { busRoutesApi } from '@/api/resources';
import { RouteMap } from '@/components/map/RouteMap';
import { arrivalLabel, delayLabel, nextArrivals, nextStopName } from '@/services/busRoutes';
import { formatTime } from '@/services/format';
import { hourLabel } from '@/services/installations';

import base from './PreLaunch.module.css';
import { REFRESH_MS, useNoIndex, usePublicPoll } from './PublicLocation';
import local from './PublicRoutePage.module.css';
import styles from './PublicTheft.module.css';

/**
 * O link do passageiro (/linha/<link secreto>): o mapa da linha, os ônibus
 * rodando agora (sem placa) e a próxima chegada prevista em cada ponto.
 * Atualiza sozinha; para de funcionar quando a empresa desliga o link.
 */
export function PublicRoutePage() {
  const { token = '' } = useParams();
  useNoIndex('Onde está o ônibus · Farbo Rastreadores');
  const { view, ended, error } = usePublicPoll(busRoutesApi.publicView, token);
  const now = Date.now();
  const arrivals = view ? nextArrivals(view, now) : [];
  const running = view?.trips.filter((t) => t.status === 'running') ?? [];
  const soon = view?.trips.filter((t) => t.status === 'scheduled') ?? [];
  const buses = running
    .filter((t) => t.position)
    .map((t, i) => ({ id: t.tripId, label: `Ônibus ${i + 1}`, latitude: t.position!.latitude, longitude: t.position!.longitude }));

  return (
    <main className={`${base.page} ${styles.page} ${styles.live}`}>
      <header className={base.header}>
        <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={base.logo} />
        <span className={`${styles.badge} ${styles.badgeLive}`}>Ao vivo</span>
      </header>

      {ended ? (
        <section className={styles.card}>
          <h1 className={styles.title}>Este link não está mais ativo</h1>
          <p className={styles.text}>A empresa desligou o acompanhamento desta linha. Peça o link novo a ela.</p>
        </section>
      ) : !view ? (
        <p className={base.hint} role="status">
          {error || 'Carregando a linha…'}
        </p>
      ) : (
        <>
          <section className={styles.card}>
            <h1 className={styles.title}>{view.name}</h1>
            <p className={styles.muted}>
              {running.length === 0
                ? soon.length > 0
                  ? `Nenhum ônibus rodando agora. Próxima saída às ${hourLabel(soon[0].departsAt)}.`
                  : 'Nenhum ônibus rodando agora.'
                : `${running.length} ônibus rodando agora.`}
            </p>
            {running.map((t, i) => (
              <div key={t.tripId} className={local.bus}>
                <strong>Ônibus {i + 1}</strong>
                {nextStopName(t) && <span>indo para {nextStopName(t)}</span>}
                {t.delayMin !== null && (
                  <span className={`${local.pill} ${t.delayMin > 2 ? local.late : ''}`}>{delayLabel(t.delayMin)}</span>
                )}
              </div>
            ))}
          </section>

          <div className={local.map}>
            <RouteMap stops={view.stops} color={view.color} buses={buses} className={local.leaflet} />
          </div>

          <section className={styles.card}>
            <span className={styles.label}>Próxima chegada em cada ponto</span>
            <ol className={local.stops}>
              {view.stops.map((s, i) => (
                <li key={s.seq} className={local.stop}>
                  <span className={local.dot} style={{ background: view.color }} aria-hidden="true" />
                  <span className={local.stopName}>{s.name}</span>
                  <span className={`${local.eta} ${arrivals[i] === null ? local.etaNone : ''}`}>{arrivalLabel(arrivals[i], now)}</span>
                </li>
              ))}
            </ol>
          </section>

          <p className={styles.muted}>
            Previsão pelo horário da linha e o atraso de agora. Atualizada às {formatTime(view.updatedAt).slice(0, 5)}; a página
            atualiza sozinha a cada {REFRESH_MS / 1000} s.
          </p>
        </>
      )}
    </main>
  );
}

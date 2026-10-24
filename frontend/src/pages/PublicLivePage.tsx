import { useParams } from 'react-router-dom';

import { liveLinksApi } from '@/api/resources';
import { untilLabel } from '@/services/liveLinks';

import base from './PreLaunch.module.css';
import { LocationCards, REFRESH_MS, useNoIndex, usePublicPoll } from './PublicLocation';
import styles from './PublicTheft.module.css';

/**
 * A localização ao vivo por um tempo (/ao-vivo/<link secreto>): o cliente
 * mandou para alguém acompanhar o veículo por algumas horas, sem login. Só o
 * veículo, onde ele está e o primeiro nome de quem mandou. Para de funcionar
 * no prazo ou quando o cliente encerra.
 */
export function PublicLivePage() {
  const { token = '' } = useParams();
  useNoIndex('Localização ao vivo · Farbo Rastreadores');
  const { view, ended, error } = usePublicPoll(liveLinksApi.publicView, token);
  const vehicle = view?.vehicle;
  const details = vehicle ? [vehicle.brand, vehicle.model, vehicle.color].filter(Boolean).join(' · ') : '';

  return (
    <main className={`${base.page} ${styles.page} ${styles.live}`}>
      <header className={base.header}>
        <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={base.logo} />
        <span className={`${styles.badge} ${styles.badgeLive}`}>Ao vivo</span>
      </header>

      {ended ? (
        <section className={styles.card}>
          <h1 className={styles.title}>Este link não está mais ativo</h1>
          <p className={styles.text}>
            O prazo acabou ou quem mandou encerrou o compartilhamento. Se precisar continuar acompanhando, peça um link
            novo.
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
            <p className={styles.muted}>
              {view.sharedBy ? `${view.sharedBy} compartilhou a localização com você` : 'Localização compartilhada com você'}{' '}
              {untilLabel(view.expiresAt)}.
            </p>
          </section>

          <LocationCards view={view} error={error} mapClass={styles.mapLive} />

          <p className={styles.muted}>A página atualiza sozinha a cada {REFRESH_MS / 1000} s.</p>
        </>
      )}
    </main>
  );
}

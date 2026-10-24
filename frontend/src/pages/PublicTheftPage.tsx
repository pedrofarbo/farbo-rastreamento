import { useParams } from 'react-router-dom';

import { theftApi } from '@/api/resources';
import { formatDateTime } from '@/services/format';

import base from './PreLaunch.module.css';
import { LocationCards, REFRESH_MS, useNoIndex, usePublicPoll } from './PublicLocation';
import styles from './PublicTheft.module.css';

/**
 * O link do modo roubo (/localizar/<link secreto>): a posição ao vivo do
 * veículo roubado, sem login, para a polícia e para quem ajuda. Só o
 * veículo e onde ele está — nada do dono. Para de funcionar quando o modo
 * roubo é desligado. Não vai para buscadores nem passa adiante no Referer.
 */
export function PublicTheftPage() {
  const { token = '' } = useParams();
  useNoIndex('Veículo roubado · localização ao vivo');
  const { view, ended, error } = usePublicPoll(theftApi.publicView, token);

  const vehicle = view?.vehicle;
  const details = vehicle ? [vehicle.brand, vehicle.model, vehicle.year, vehicle.color].filter(Boolean).join(' · ') : '';

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

          <LocationCards view={view} error={error} />

          <p className={styles.muted}>
            A página atualiza sozinha a cada {REFRESH_MS / 1000} s. Em caso de emergência, ligue 190.
          </p>
        </>
      )}
    </main>
  );
}

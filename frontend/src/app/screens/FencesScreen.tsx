import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';

import { geofencesApi } from '@/api/resources';
import { isSuspendedError, SuspendedNotice } from '@/components/billing/SuspendedNotice';
import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { useVehicles } from '@/hooks/useVehicles';

import { formatRadius, MAX_FENCES, notifyLabel } from '../fence';
import { BackIcon, ChevronIcon } from '../icons';
import fence from './Fence.module.css';
import styles from './Screen.module.css';

export const fencesKey = ['geofences'] as const;

/** As cercas do cliente: aviso quando o veículo entra ou sai de um lugar. */
export function FencesScreen() {
  const navigate = useNavigate();
  const fences = useQuery({ queryKey: fencesKey, queryFn: geofencesApi.list });
  const vehicles = useVehicles();

  if (isSuspendedError(fences.error)) return <SuspendedNotice message={fences.error?.message} />;

  const list = fences.data ?? [];
  const names = new Map((vehicles.data ?? []).map((v) => [v.id, v.name]));
  const full = list.length >= MAX_FENCES;

  // Aberto direto (link ou recarregar), não há tela anterior no app.
  const goBack = () => {
    const state = window.history.state as { idx?: number } | null;
    if (state?.idx) navigate(-1);
    else navigate('/alertas', { replace: true });
  };

  return (
    <div className={styles.screen}>
      <div className={styles.navbar}>
        <button type="button" className={styles.back} onClick={goBack} aria-label="Voltar">
          <BackIcon />
        </button>
        <div className={styles.navTitle}>
          <h1>Cercas</h1>
        </div>
      </div>
      <p className={styles.lead}>
        Desenhe um círculo em volta de casa, do trabalho ou da escola e receba um aviso quando o veículo entrar ou sair
        dele — por e-mail e, com as notificações ligadas, no celular.
      </p>

      {fences.isLoading ? (
        <Spinner label="Carregando cercas" />
      ) : list.length === 0 ? (
        <section className={styles.section}>
          <h2 className={styles.sectionTitle}>Nenhuma cerca ainda</h2>
          <p className={styles.muted}>Comece pela sua casa: com o carro na garagem, crie a cerca e escolha o tamanho.</p>
        </section>
      ) : (
        <div className={fence.cards}>
          {list.map((item) => {
            const vehicleNames = item.vehicleIds.map((id) => names.get(id)).filter(Boolean);
            return (
              <Link key={item.id} to={`/cercas/${item.id}`} className={fence.fenceCard}>
                <span className={fence.fenceTop}>
                  <span className={fence.ring} aria-hidden="true" />
                  <span className={fence.fenceName}>{item.name}</span>
                  <span className={styles.chevron}>
                    <ChevronIcon />
                  </span>
                </span>
                <span className={fence.fenceMeta}>
                  <span>Raio de {formatRadius(item.radiusMeters)}</span>
                  {vehicleNames.length > 0 && <span>{vehicleNames.join(', ')}</span>}
                </span>
                <span className={styles.muted}>{notifyLabel(item)}</span>
              </Link>
            );
          })}
        </div>
      )}

      <Button variant="primary" onClick={() => navigate('/cercas/nova')} disabled={full || fences.isLoading} block>
        Nova cerca
      </Button>
      {full && <p className={styles.muted}>Você chegou ao limite de {MAX_FENCES} cercas: apague uma para criar outra.</p>}
    </div>
  );
}

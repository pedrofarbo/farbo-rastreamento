import { useNavigate } from 'react-router-dom';

import { TwoFactorSettings } from '@/components/auth/TwoFactorSettings';

import { BackIcon } from '../icons';
import styles from './Screen.module.css';

/** Segurança (em Conta): a verificação em duas etapas. */
export function SecurityScreen() {
  const navigate = useNavigate();
  return (
    <div className={styles.screen}>
      <div className={styles.navbar}>
        <button type="button" className={styles.back} onClick={() => navigate('/conta')} aria-label="Voltar">
          <BackIcon />
        </button>
        <div className={styles.navTitle}>
          <h1>Segurança</h1>
        </div>
      </div>
      <section className={styles.section} aria-labelledby="duas-etapas">
        <h2 id="duas-etapas" className={styles.sectionTitle}>
          Verificação em duas etapas
        </h2>
        <TwoFactorSettings />
      </section>
    </div>
  );
}

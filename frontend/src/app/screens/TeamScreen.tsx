import { useNavigate } from 'react-router-dom';

import { TeamManager } from '@/components/account/TeamManager';

import { BackIcon } from '../icons';
import styles from './Screen.module.css';

/** Equipe (em Conta): quem da empresa acessa a conta (só o titular). */
export function TeamScreen() {
  const navigate = useNavigate();
  return (
    <div className={styles.screen}>
      <div className={styles.navbar}>
        <button type="button" className={styles.back} onClick={() => navigate('/conta')} aria-label="Voltar">
          <BackIcon />
        </button>
        <div className={styles.navTitle}>
          <h1>Equipe</h1>
        </div>
      </div>
      <section className={styles.section}>
        <TeamManager />
      </section>
    </div>
  );
}

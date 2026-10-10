import { TeamManager } from '@/components/account/TeamManager';
import { Card } from '@/components/ui/Card';

import styles from '../Page.module.css';

/** A equipe da conta empresa, no painel (só o titular). */
export function TeamPage() {
  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Equipe</h1>
            <p className={styles.description}>
              Quem da sua empresa acessa os veículos da conta, e o que cada pessoa pode fazer.
            </p>
          </div>
        </header>
        <Card>
          <TeamManager />
        </Card>
      </div>
    </div>
  );
}

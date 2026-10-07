import { TwoFactorSettings } from '@/components/auth/TwoFactorSettings';
import { Card } from '@/components/ui/Card';
import { useAuth } from '@/stores/AuthContext';

import styles from './Page.module.css';

/** Segurança da conta (painel): a verificação em duas etapas de quem está conectado. */
export function SecurityPage() {
  const { user } = useAuth();
  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Segurança da conta</h1>
            <p className={styles.description}>{user?.email}</p>
          </div>
        </header>
        <Card title="Verificação em duas etapas" subtitle="Além da senha, um código para entrar.">
          <TwoFactorSettings />
        </Card>
      </div>
    </div>
  );
}

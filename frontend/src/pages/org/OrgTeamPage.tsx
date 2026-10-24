import { orgApi } from '@/api/resources';
import { OrgTeamManager } from '@/components/organization/OrgTeamManager';
import { Card } from '@/components/ui/Card';
import { useAuth } from '@/stores/AuthContext';

import styles from '../Page.module.css';

/** A equipe da associação ou seguradora (só o Administrador). */
export function OrgTeamPage() {
  const { user, orgMembership } = useAuth();
  const name = orgMembership?.name ?? 'organização';
  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Equipe</h1>
            <p className={styles.description}>
              Quem da {name} entra no painel, cada pessoa com o próprio e-mail e senha, e o que cada uma pode fazer.
              Tudo o que fazem fica no histórico (o bloqueio, com o nome de quem bloqueou).
            </p>
          </div>
        </header>
        <Card>
          <OrgTeamManager api={orgApi.team} queryKey={['org', 'team']} confirmIdentity selfId={user?.id} orgName={name} />
        </Card>
      </div>
    </div>
  );
}

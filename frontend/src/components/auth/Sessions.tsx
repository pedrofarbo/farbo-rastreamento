import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { authApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateTime, formatRelative } from '@/services/format';
import { deviceLabel } from '@/services/userAgent';
import type { AuthSession } from '@/types';

import styles from './Sessions.module.css';

export const mySessionsKey = ['auth', 'sessions'] as const;

/** A lista dos aparelhos: qual é, se é este e quando foi usado por último. */
export function SessionList({ sessions }: { sessions: AuthSession[] }) {
  return (
    <ul className={styles.list}>
      {sessions.map((s) => (
        <li key={s.id} className={styles.item}>
          <span className={styles.device}>
            <strong>{deviceLabel(s.userAgent)}</strong>
            {s.current && <span className={styles.current}>este aparelho</span>}
          </span>
          <span className={styles.muted} title={formatDateTime(s.lastSeenAt)}>
            usado por último {formatRelative(s.lastSeenAt)}
          </span>
        </li>
      ))}
    </ul>
  );
}

/**
 * Os aparelhos com a conta de quem está logado e "sair dos outros": eles
 * caem na hora, e este continua.
 */
export function MySessions() {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const sessions = useQuery({ queryKey: mySessionsKey, queryFn: authApi.sessions });
  const signOut = useMutation({
    mutationFn: authApi.signOutOthers,
    onSuccess: ({ signedOut }) => {
      void queryClient.invalidateQueries({ queryKey: mySessionsKey });
      notify({
        tone: 'success',
        title: signedOut === 1 ? '1 aparelho desconectado' : `${signedOut} aparelhos desconectados`,
        description: 'Só este aparelho continua com a conta aberta.',
      });
    },
    onError: (error: Error) => notify({ tone: 'error', title: 'Não foi possível desconectar', description: error.message }),
  });

  const list = sessions.data?.sessions ?? [];
  const others = list.filter((s) => !s.current).length;
  return (
    <div className={styles.box}>
      <p className={styles.muted}>
        Onde a sua conta está aberta. Se não reconhecer algum aparelho, saia dos outros e troque a senha em “Esqueci minha
        senha”, na tela de entrada.
      </p>
      {sessions.isLoading ? (
        <Spinner label="Carregando os aparelhos" />
      ) : sessions.isError ? (
        <p className={styles.muted}>Não foi possível carregar os aparelhos.</p>
      ) : (
        <SessionList sessions={list} />
      )}
      {others > 0 ? (
        <Button variant="secondary" loading={signOut.isPending} onClick={() => signOut.mutate()} block>
          Sair dos outros aparelhos ({others})
        </Button>
      ) : (
        sessions.isSuccess && <p className={styles.muted}>Só este aparelho está com a conta aberta.</p>
      )}
    </div>
  );
}

import { useNavigate } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { Button } from '@/components/ui/Button';
import { useAuth } from '@/stores/AuthContext';

import styles from './Billing.module.css';

/** A API responde 402 com este código quando o cliente está suspenso por atraso. */
export function isSuspendedError(error: unknown): boolean {
  if (!(error instanceof ApiError) || error.status !== 402) return false;
  const body = error.body as { code?: string } | undefined;
  return body?.code === 'ACCOUNT_SUSPENDED';
}

/**
 * Tela do cliente com acesso suspenso por fatura atrasada: explica o motivo e
 * leva para as faturas, que continuam acessíveis.
 */
export function SuspendedNotice({ message }: { message?: string }) {
  const navigate = useNavigate();
  // Da equipe, quem não vê as faturas (Visualizador, restrito a grupos) fala com o titular.
  const { canSeeBilling } = useAuth();
  // A API manda "acesso suspenso: há fatura…"; o título já diz a primeira parte.
  const detail = message?.replace(/^acesso suspenso:\s*/i, '').trim();
  return (
    <div className={styles.suspendedWrap}>
      <div className={styles.suspendedCard} role="alert">
        <div className={styles.suspendedIcon} aria-hidden="true">
          !
        </div>
        <h1 className={styles.suspendedTitle}>Acesso suspenso</h1>
        <p className={styles.suspendedText}>
          {detail
            ? detail.charAt(0).toUpperCase() + detail.slice(1) + '.'
            : 'Há fatura vencida além do prazo de tolerância.'}
        </p>
        <p className={styles.suspendedText}>
          Seus veículos continuam sendo rastreados normalmente; o acesso volta assim que o
          pagamento for confirmado.
        </p>
        {canSeeBilling ? (
          <Button variant="primary" size="large" onClick={() => navigate('/faturas')}>
            Ver minhas faturas
          </Button>
        ) : (
          <p className={styles.suspendedText}>Fale com o titular da conta para regularizar.</p>
        )}
      </div>
    </div>
  );
}

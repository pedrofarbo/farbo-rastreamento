import { useQuery } from '@tanstack/react-query';

import { financeApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { formatMoney } from '@/services/format';

import { financeKey } from './EntryModals';
import styles from './Company.module.css';

/** De quanto em quanto tempo o saldo é consultado de novo, com a tela aberta. */
const REFRESH_MS = 60_000;

const timeFormatter = new Intl.DateTimeFormat('pt-BR', { hour: '2-digit', minute: '2-digit' });

/**
 * O saldo na AbacatePay agora, no topo da tela da empresa: o que já pode sair
 * e, se houver, o que ainda vai ser liberado e o bloqueado. Atualiza sozinho
 * a cada minuto, ao voltar para a tela e no botão. É o dinheiro de verdade, não
 * o saldo do caixa (o que a central lança). Some sem a AbacatePay configurada.
 */
export function GatewayBalanceCard() {
  const balance = useQuery({
    queryKey: [...financeKey, 'balance'],
    queryFn: financeApi.balance,
    refetchInterval: REFRESH_MS,
    refetchOnWindowFocus: true,
    staleTime: 0,
  });
  const b = balance.data;
  if (!b?.enabled) return null;

  const failed = b.availableCents === null;
  const extras = [
    b.pendingCents ? `${formatMoney(b.pendingCents)} a liberar` : '',
    b.blockedCents ? `${formatMoney(b.blockedCents)} bloqueado` : '',
  ].filter(Boolean);

  return (
    <section className={styles.balance} aria-label="Saldo na AbacatePay" aria-live="polite">
      <div className={styles.balanceHead}>
        <span className={styles.balanceLabel}>Saldo na AbacatePay</span>
        {b.devMode && <Badge tone="warning">testes</Badge>}
      </div>
      {failed ? (
        <span className={styles.danger}>Não deu para consultar: {b.error || 'sem resposta'}</span>
      ) : (
        <>
          <strong className={styles.balanceAmount}>{formatMoney(b.availableCents)}</strong>
          <span className={styles.muted}>
            disponível{extras.length > 0 ? ` · ${extras.join(' · ')}` : ''}
          </span>
        </>
      )}
      <span className={styles.balanceFoot}>
        <span className={styles.muted}>atualizado às {timeFormatter.format(new Date(b.checkedAt))}</span>
        <Button size="small" variant="ghost" loading={balance.isFetching} onClick={() => void balance.refetch()}>
          Atualizar
        </Button>
      </span>
    </section>
  );
}

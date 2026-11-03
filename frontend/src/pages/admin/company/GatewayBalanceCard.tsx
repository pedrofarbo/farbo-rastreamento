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

/** Os gateways com saldo no topo da tela da empresa. */
const GATEWAYS = {
  abacatepay: { label: 'Saldo na AbacatePay', key: 'balance', fetch: financeApi.balance },
  pagarme: { label: 'Saldo na Pagar.me', key: 'balance-pagarme', fetch: financeApi.pagarmeBalance },
} as const;

/**
 * O saldo no gateway agora (a AbacatePay ou a Pagar.me), no topo da tela da
 * empresa: o que já pode sair e, se houver, o que ainda vai ser liberado e o
 * bloqueado. Atualiza sozinho a cada minuto, ao voltar para a tela e no botão.
 * É o dinheiro de verdade, não o saldo do caixa (o que a central lança). Some
 * sem o gateway configurado.
 */
export function GatewayBalanceCard({ gateway = 'abacatepay' }: { gateway?: keyof typeof GATEWAYS }) {
  const { label, key, fetch } = GATEWAYS[gateway];
  const balance = useQuery({
    queryKey: [...financeKey, key],
    queryFn: fetch,
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
    <section className={styles.balance} aria-label={label} aria-live="polite">
      <div className={styles.balanceHead}>
        <span className={styles.balanceLabel}>{label}</span>
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

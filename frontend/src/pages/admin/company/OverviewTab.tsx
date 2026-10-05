import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';

import { financeApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { formatDateOnly, formatMoney } from '@/services/format';
import type { FinanceEntry, FinanceSum } from '@/types';

import { Projections } from './CashFlowTab';
import { PayModal, financeKey } from './EntryModals';
import { entrySituation, installmentLabel, monthLabel } from './labels';
import styles from './Company.module.css';

function count(sum: FinanceSum, one: string, many: string): string {
  return sum.count === 0 ? 'nenhuma' : `${sum.count} ${sum.count === 1 ? one : many}`;
}

/** A empresa num olhar: o caixa, o que vence, o resultado do mês e o estoque. */
export function OverviewTab({ onOpen }: { onOpen: (tab: string) => void }) {
  const overview = useQuery({ queryKey: [...financeKey, 'overview'], queryFn: financeApi.overview, refetchInterval: 60_000 });
  const [paying, setPaying] = useState<FinanceEntry | null>(null);
  const data = overview.data;
  if (!data) return <Spinner label="Carregando a visão geral" />;

  return (
    <div className={styles.tab}>
      <div className={billing.tiles}>
        <div className={`${billing.tile} ${data.balanceCents < 0 ? billing.tileDanger : ''}`}>
          <span className={billing.tileLabel}>Saldo hoje</span>
          <span className={billing.tileValue}>{formatMoney(data.balanceCents)}</span>
          <span className={billing.tileHint}>Pelo que entrou e saiu desde o saldo inicial</span>
        </div>
        <div className={`${billing.tile} ${data.overdue.count > 0 ? billing.tileDanger : ''}`}>
          <span className={billing.tileLabel}>Contas vencidas</span>
          <span className={billing.tileValue}>{formatMoney(data.overdue.cents)}</span>
          <span className={billing.tileHint}>{count(data.overdue, 'conta', 'contas')}</span>
        </div>
        <div className={billing.tile}>
          <span className={billing.tileLabel}>Vencem hoje</span>
          <span className={billing.tileValue}>{formatMoney(data.dueToday.cents)}</span>
          <span className={billing.tileHint}>
            {count(data.dueToday, 'conta', 'contas')} · mais {formatMoney(data.dueWeek.cents)} em 7 dias
          </span>
        </div>
        <div className={`${billing.tile} ${data.monthResultCents < 0 ? billing.tileDanger : ''}`}>
          <span className={billing.tileLabel}>Resultado de {monthLabel(data.month.month)}</span>
          <span className={billing.tileValue}>{formatMoney(data.monthResultCents)}</span>
          <span className={billing.tileHint}>
            Entrou {formatMoney(data.month.inCents)} · saiu {formatMoney(data.month.outCents)}
          </span>
        </div>
      </div>

      {data.receivableOverdue.count > 0 && (
        <p className={`${billing.banner} ${billing.bannerWarning}`}>
          {data.receivableOverdue.count} recebimento(s) atrasado(s) somando {formatMoney(data.receivableOverdue.cents)} (faturas
          dos clientes e outras receitas).
        </p>
      )}

      <div className={styles.grid}>
        <Card
          title="Contas a pagar vencendo"
          subtitle="Vencidas e as dos próximos 7 dias"
          actions={
            <Button size="small" variant="ghost" onClick={() => onOpen('pagar')}>
              Ver todas
            </Button>
          }
        >
          {data.upcoming.length === 0 ? (
            <EmptyState icon="✅" title="Nada vencendo" description="Nenhuma conta vencida ou para os próximos 7 dias." />
          ) : (
            <ul className={styles.list}>
              {data.upcoming.map((e) => {
                const situation = entrySituation(e, data.today);
                const part = installmentLabel(e);
                return (
                  <li key={e.id} className={styles.listItem}>
                    <span className={styles.desc}>
                      <strong>
                        {e.description}
                        {part && <span className={styles.muted}> · {part}</span>}
                      </strong>
                      <span className={styles.muted}>
                        {formatDateOnly(e.dueDate)}
                        {e.supplierName ? ` · ${e.supplierName}` : ''}
                      </span>
                    </span>
                    <span className={styles.listEnd}>
                      <span className={styles.amount}>{formatMoney(e.amountCents)}</span>
                      <Badge tone={situation.tone}>{situation.label}</Badge>
                      <Button size="small" variant="primary" onClick={() => setPaying(e)}>
                        Pagar
                      </Button>
                    </span>
                  </li>
                );
              })}
            </ul>
          )}
        </Card>

        <Card
          title="Projeção do caixa"
          actions={
            <Button size="small" variant="ghost" onClick={() => onOpen('caixa')}>
              Fluxo de caixa
            </Button>
          }
        >
          <Projections list={data.projections} />
        </Card>

        <Card
          title="Estoque"
          subtitle={`${formatMoney(data.stockValueCents)} parados em estoque`}
          actions={
            <Button size="small" variant="ghost" onClick={() => onOpen('estoque')}>
              Abrir estoque
            </Button>
          }
        >
          {data.lowStock.length === 0 ? (
            <p className={styles.muted}>Nenhum item abaixo do mínimo.</p>
          ) : (
            <ul className={styles.list}>
              {data.lowStock.map((i) => (
                <li key={i.id} className={styles.listItem}>
                  <span>{i.name}</span>
                  <span className={styles.listEnd}>
                    <span className={styles.amount}>
                      {i.quantity} de {i.minQuantity}
                    </span>
                    <Badge tone="warning" dot>
                      Comprar
                    </Badge>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>

      {paying && <PayModal entry={paying} onClose={() => setPaying(null)} />}
    </div>
  );
}

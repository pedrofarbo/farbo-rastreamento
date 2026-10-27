import { useState } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { financeApi } from '@/api/resources';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { formatDateOnly, formatDateTime, formatMoney } from '@/services/format';
import type { CustomerPaymentMethod } from '@/types';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

import pageStyles from '../../Page.module.css';
import { financeKey } from './EntryModals';
import { PAID_VIA_LABELS, addMonths, monthLabel, todayISO } from './labels';
import styles from './Company.module.css';

/** Quantos meses o seletor oferece, do corrente para trás. */
const MONTHS_BACK = 24;

const METHODS: CustomerPaymentMethod[] = ['PIX', 'BOLETO', 'CARD', 'MANUAL'];

/**
 * Os pagamentos dos clientes (as faturas pagas) de um mês, pelo dia em que
 * entraram no caixa: o total é o mesmo das faturas no fluxo de caixa e no
 * resultado. Só leitura: a fatura se mexe na ficha do cliente.
 */
export function CustomerPaymentsPanel() {
  const current = todayISO().slice(0, 7);
  const [month, setMonth] = useState(current);
  const [search, setSearch] = useState('');
  const [method, setMethod] = useState<CustomerPaymentMethod | ''>('');
  const months = Array.from({ length: MONTHS_BACK }, (_, i) => addMonths(current, -i));

  const filter = { month, q: search.trim(), method };
  const payments = useQuery({
    queryKey: [...financeKey, 'customer-payments', filter],
    queryFn: () => financeApi.customerPayments(filter),
    placeholderData: keepPreviousData,
  });
  const data = payments.data;
  const filtered = Boolean(filter.q || method);
  const paged = usePagedList(data?.payments ?? [], 'payments-received', filter);

  return (
    <>
      <div className={styles.filters}>
        <select
          className={`${styles.select} ${styles.monthSelect}`}
          aria-label="Mês"
          value={month}
          onChange={(e) => setMonth(e.target.value)}
        >
          {months.map((m) => (
            <option key={m} value={m}>
              {monthLabel(m)}
            </option>
          ))}
        </select>
        <input
          className={styles.search}
          type="search"
          placeholder="Buscar pelo cliente ou pela fatura"
          aria-label="Buscar"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select
          className={styles.select}
          aria-label="Forma de pagamento"
          value={method}
          onChange={(e) => setMethod(e.target.value as CustomerPaymentMethod | '')}
        >
          <option value="">Todas as formas</option>
          {METHODS.map((m) => (
            <option key={m} value={m}>
              {PAID_VIA_LABELS[m]}
            </option>
          ))}
        </select>
      </div>

      <p className={pageStyles.note}>
        As faturas pagas pelos clientes, no dia em que o dinheiro entrou (horário de Brasília): o total do mês é o
        mesmo das faturas no fluxo de caixa e no resultado. Para dar baixa, cancelar ou estornar, abra a ficha do
        cliente.
      </p>

      {data && (
        <div className={styles.summary} aria-live="polite">
          <span>
            <strong>{data.count}</strong> {data.count === 1 ? 'pagamento' : 'pagamentos'}
          </span>
          <span>
            Total <strong>{formatMoney(data.cents)}</strong>
          </span>
          {data.byMethod.length > 1 &&
            data.byMethod.map((m) => (
              <span key={m.method}>
                {PAID_VIA_LABELS[m.method]} <strong>{formatMoney(m.cents)}</strong>
              </span>
            ))}
        </div>
      )}

      <Card flush>
        {payments.isLoading ? (
          <Spinner label="Carregando" />
        ) : payments.isError ? (
          <EmptyState icon="⚠️" title="Não deu para carregar" description={(payments.error as Error).message} />
        ) : !data || data.payments.length === 0 ? (
          <EmptyState
            icon="💰"
            title="Nada por aqui"
            description={
              filtered
                ? 'Nenhum pagamento com esses filtros.'
                : `Nenhum pagamento de cliente em ${monthLabel(month)}.`
            }
          />
        ) : (
          <>
            <div className={pageStyles.tableWrap}>
              <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
                <thead>
                  <tr>
                    <th>Pago em</th>
                    <th>Cliente</th>
                    <th>Fatura</th>
                    <th>Forma</th>
                    <th className={styles.right}>Valor</th>
                  </tr>
                </thead>
                <tbody>
                  {paged.items.map((p) => (
                    <tr key={p.invoiceId}>
                      <td data-label="Pago em">{formatDateTime(p.paidAt)}</td>
                      <td data-label="Cliente">
                        <Link to={`/clientes/${p.customerId}`}>{p.customerName || 'Cliente'}</Link>
                      </td>
                      <td data-label="Fatura">
                        <span className={styles.desc}>
                          <strong>{p.description}</strong>
                          <span className={styles.muted}>vencimento {formatDateOnly(p.dueDate)}</span>
                        </span>
                      </td>
                      <td data-label="Forma">{PAID_VIA_LABELS[p.method]}</td>
                      <td data-label="Valor" className={styles.right}>
                        <span className={styles.amount}>{formatMoney(p.amountCents)}</span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pager {...paged.pager} />
          </>
        )}
      </Card>
      {data?.truncated && (
        <p className={styles.muted}>
          Mostrando os {data.payments.length} mais recentes de {data.count}; busque pelo cliente para achar os outros.
          O total conta todos.
        </p>
      )}
    </>
  );
}

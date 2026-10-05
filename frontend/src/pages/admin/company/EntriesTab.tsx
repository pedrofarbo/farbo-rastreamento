import { useState } from 'react';
import { keepPreviousData, useMutation, useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { financeApi } from '@/api/resources';
import type { EntryFilter } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateOnly, formatMoney } from '@/services/format';
import type { EntryKind, FinanceEntry } from '@/types';

import pageStyles from '../../Page.module.css';
import { AttachmentsModal, EntryModal, KIND_WORDS, PayModal, financeKey, useFinanceLookups, useRefreshFinance } from './EntryModals';
import { RecurrenceModal } from './RegistryTab';
import { METHOD_LABELS, entrySituation, installmentLabel, todayISO } from './labels';
import styles from './Company.module.css';

const STATUSES: { value: EntryFilter['status']; label: string }[] = [
  { value: 'open', label: 'Em aberto' },
  { value: 'overdue', label: 'Vencidas' },
  { value: 'paid', label: 'Pagas' },
  { value: 'canceled', label: 'Canceladas' },
  { value: 'all', label: 'Todas' },
];

/** Soma o que a lista mostra: o valor de cada conta, ou o pago nas pagas. */
export function totals(list: FinanceEntry[]): { count: number; cents: number; overdueCents: number } {
  let cents = 0;
  let overdueCents = 0;
  for (const e of list) {
    cents += e.status === 'PAID' ? (e.paidCents ?? e.amountCents) : e.amountCents;
    if (e.overdue) overdueCents += e.amountCents;
  }
  return { count: list.length, cents, overdueCents };
}

/** Contas a pagar (PAYABLE) ou receitas avulsas (RECEIVABLE). */
export function EntriesTab({ kind }: { kind: EntryKind }) {
  const { notify } = useToast();
  const refresh = useRefreshFinance();
  const { categories, suppliers } = useFinanceLookups();
  const [status, setStatus] = useState<EntryFilter['status']>('open');
  const [search, setSearch] = useState('');
  const [category, setCategory] = useState('');
  const [supplier, setSupplier] = useState('');
  const [editing, setEditing] = useState<FinanceEntry | null | undefined>(undefined);
  const [paying, setPaying] = useState<FinanceEntry | null>(null);
  const [files, setFiles] = useState<FinanceEntry | null>(null);
  const [recurring, setRecurring] = useState(false);
  const words = KIND_WORDS[kind];
  const today = todayISO();

  const filter: EntryFilter = { kind, status, q: search.trim(), category, supplier };
  const entries = useQuery({
    queryKey: [...financeKey, 'entries', filter],
    queryFn: () => financeApi.entries(filter),
    placeholderData: keepPreviousData,
  });
  const list = entries.data ?? [];
  const sum = totals(list);

  const action = useMutation({
    mutationFn: async ({ op, entry }: { op: 'reopen' | 'cancel' | 'remove'; entry: FinanceEntry }) => {
      if (op === 'reopen') await financeApi.reopen(entry.id);
      else if (op === 'cancel') await financeApi.cancel(entry.id);
      else await financeApi.removeEntry(entry.id);
    },
    onSuccess: (_, { op, entry }) => {
      refresh();
      const titles = { reopen: 'Reaberta', cancel: 'Cancelada', remove: 'Excluída' };
      notify({ tone: 'success', title: titles[op], description: entry.description });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível', description: err.message }),
  });

  const kindCategories = categories.filter((c) => c.kind === (kind === 'PAYABLE' ? 'EXPENSE' : 'INCOME'));

  return (
    <div className={styles.tab}>
      <div className={styles.toolbar}>
        <div className={styles.chips} role="group" aria-label="Situação">
          {STATUSES.map((s) => (
            <button
              key={s.value}
              type="button"
              className={`${styles.chip} ${status === s.value ? styles.chipActive : ''}`}
              aria-pressed={status === s.value}
              onClick={() => setStatus(s.value)}
            >
              {s.value === 'paid' && kind === 'RECEIVABLE' ? 'Recebidas' : s.label}
            </button>
          ))}
        </div>
        <div className={pageStyles.actions}>
          {kind === 'PAYABLE' && <Button onClick={() => setRecurring(true)}>Conta recorrente</Button>}
          <Button variant="primary" onClick={() => setEditing(null)}>
            {kind === 'PAYABLE' ? 'Nova conta' : 'Nova receita'}
          </Button>
        </div>
      </div>

      <div className={styles.filters}>
        <input
          className={styles.search}
          type="search"
          placeholder="Buscar pela descrição, fornecedor ou observação"
          aria-label="Buscar"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select className={styles.select} aria-label="Categoria" value={category} onChange={(e) => setCategory(e.target.value)}>
          <option value="">Todas as categorias</option>
          {kindCategories.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </select>
        {kind === 'PAYABLE' && (
          <select className={styles.select} aria-label="Fornecedor" value={supplier} onChange={(e) => setSupplier(e.target.value)}>
            <option value="">Todos os fornecedores</option>
            {suppliers.map((x) => (
              <option key={x.id} value={x.id}>
                {x.name}
              </option>
            ))}
          </select>
        )}
      </div>

      {kind === 'RECEIVABLE' && (
        <p className={pageStyles.note}>
          As mensalidades e faturas dos clientes entram sozinhas no caixa e no resultado quando são pagas (veja em{' '}
          <Link to="/clientes">Clientes</Link>). Aqui ficam as outras receitas: venda de equipamento, aporte, empréstimo.
        </p>
      )}

      <div className={styles.summary} aria-live="polite">
        <span>
          <strong>{sum.count}</strong> {sum.count === 1 ? 'lançamento' : 'lançamentos'}
        </span>
        <span>
          Total <strong>{formatMoney(sum.cents)}</strong>
        </span>
        {sum.overdueCents > 0 && (
          <span className={styles.danger}>
            Vencido <strong className={styles.danger}>{formatMoney(sum.overdueCents)}</strong>
          </span>
        )}
      </div>

      <Card flush>
        {entries.isLoading ? (
          <Spinner label="Carregando" />
        ) : list.length === 0 ? (
          <EmptyState
            icon={kind === 'PAYABLE' ? '🧾' : '💰'}
            title="Nada por aqui"
            description={
              status === 'open'
                ? kind === 'PAYABLE'
                  ? 'Nenhuma conta em aberto. Lance as contas da empresa para acompanhar os vencimentos.'
                  : 'Nenhuma receita em aberto.'
                : 'Nenhum lançamento com esses filtros.'
            }
          />
        ) : (
          <div className={pageStyles.tableWrap}>
            <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
              <thead>
                <tr>
                  <th>Vencimento</th>
                  <th>Descrição</th>
                  <th className={styles.right}>Valor</th>
                  <th>Situação</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {list.map((e) => {
                  const situation = entrySituation(e, today);
                  const part = installmentLabel(e);
                  return (
                    <tr key={e.id}>
                      <td data-label="Vencimento">{formatDateOnly(e.dueDate)}</td>
                      <td data-label="Descrição">
                        <span className={styles.desc}>
                          <strong>
                            {e.description}
                            {part && <span className={styles.muted}> · {part}</span>}
                          </strong>
                          <span className={styles.muted}>
                            {[e.categoryName, e.supplierName, e.recurrenceId ? 'recorrente' : ''].filter(Boolean).join(' · ')}
                          </span>
                        </span>
                      </td>
                      <td data-label="Valor" className={styles.right}>
                        <span className={styles.amount}>{formatMoney(e.amountCents)}</span>
                        {e.status === 'PAID' && e.paidCents !== null && (
                          <div className={styles.muted}>
                            {e.paidCents !== e.amountCents ? `${formatMoney(e.paidCents)} · ` : ''}
                            {formatDateOnly(e.paidOn)}
                            {e.paymentMethod ? ` · ${METHOD_LABELS[e.paymentMethod]}` : ''}
                          </div>
                        )}
                      </td>
                      <td data-label="Situação">
                        <Badge tone={situation.tone} dot={situation.tone === 'danger'}>
                          {situation.label}
                        </Badge>
                      </td>
                      <td data-label="">
                        <div className={styles.rowActions}>
                          {e.status === 'OPEN' && (
                            <Button size="small" variant="primary" onClick={() => setPaying(e)}>
                              {words.pay}
                            </Button>
                          )}
                          {e.status === 'OPEN' && (
                            <Button size="small" variant="ghost" onClick={() => setEditing(e)}>
                              Editar
                            </Button>
                          )}
                          <Button size="small" variant="ghost" onClick={() => setFiles(e)}>
                            Anexos{e.attachments.length > 0 ? ` (${e.attachments.length})` : ''}
                          </Button>
                          {e.status === 'OPEN' ? (
                            <Button size="small" variant="ghost" onClick={() => action.mutate({ op: 'cancel', entry: e })}>
                              Cancelar
                            </Button>
                          ) : (
                            <Button size="small" variant="ghost" onClick={() => action.mutate({ op: 'reopen', entry: e })}>
                              Reabrir
                            </Button>
                          )}
                          {e.status !== 'PAID' && (
                            <Button
                              size="small"
                              variant="ghost"
                              onClick={() => {
                                if (window.confirm(`Excluir "${e.description}"? Os anexos vão junto.`)) {
                                  action.mutate({ op: 'remove', entry: e });
                                }
                              }}
                            >
                              Excluir
                            </Button>
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {editing !== undefined && <EntryModal kind={kind} entry={editing} onClose={() => setEditing(undefined)} />}
      {paying && <PayModal entry={paying} onClose={() => setPaying(null)} />}
      {files && <AttachmentsModal entry={files} onClose={() => setFiles(null)} />}
      {recurring && <RecurrenceModal recurrence={null} onClose={() => setRecurring(false)} />}
    </div>
  );
}

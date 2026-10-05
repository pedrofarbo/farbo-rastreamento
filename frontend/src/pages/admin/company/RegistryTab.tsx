import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';

import { financeApi } from '@/api/resources';
import type { SupplierInput } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Field, SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { centsToInput, formatDateOnly, formatMoney, parseMoney } from '@/services/format';
import type { DreGroup, EntryKind, FinanceCategory, FinanceRecurrence, PixKeyType, Supplier } from '@/types';

import pageStyles from '../../Page.module.css';
import { financeKey, useFinanceLookups, useRefreshFinance } from './EntryModals';
import { KEY_TYPE_LABELS, detectKeyType } from './PixModals';
import { EXPENSE_GROUPS, GROUP_HINTS, GROUP_LABELS, INCOME_GROUPS, todayISO } from './labels';
import styles from './Company.module.css';

/** "02558157000162" → "02.558.157/0001-62"; CPF com a máscara dele. */
export function formatDocument(digits: string): string {
  if (digits.length === 14) return digits.replace(/^(\d{2})(\d{3})(\d{3})(\d{4})(\d{2})$/, '$1.$2.$3/$4-$5');
  if (digits.length === 11) return digits.replace(/^(\d{3})(\d{3})(\d{3})(\d{2})$/, '$1.$2.$3-$4');
  return digits;
}

/** Fornecedores, categorias e contas recorrentes. */
export function RegistryTab() {
  return (
    <div className={styles.tab}>
      <RecurrencesCard />
      <SuppliersCard />
      <CategoriesCard />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Contas recorrentes
// ---------------------------------------------------------------------------

function RecurrencesCard() {
  const { notify } = useToast();
  const refresh = useRefreshFinance();
  const recurrences = useQuery({ queryKey: [...financeKey, 'recurrences'], queryFn: financeApi.recurrences });
  const [editing, setEditing] = useState<FinanceRecurrence | null | undefined>(undefined);
  const end = useMutation({
    mutationFn: (r: FinanceRecurrence) => financeApi.endRecurrence(r.id),
    onSuccess: (r) => {
      refresh();
      notify({ tone: 'success', title: 'Recorrência encerrada', description: r.description });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível encerrar', description: err.message }),
  });
  const list = recurrences.data ?? [];

  return (
    <Card
      title="Contas recorrentes"
      subtitle="Aluguel, internet, dados dos chips: lançadas sozinhas todo mês, 35 dias antes do vencimento."
      actions={
        <Button variant="primary" size="small" onClick={() => setEditing(null)}>
          Nova recorrente
        </Button>
      }
      flush
    >
      {recurrences.isLoading ? (
        <Spinner label="Carregando" />
      ) : list.length === 0 ? (
        <EmptyState icon="🔁" title="Nenhuma conta recorrente" description="Cadastre as contas que se repetem todo mês." />
      ) : (
        <div className={pageStyles.tableWrap}>
          <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
            <thead>
              <tr>
                <th>Descrição</th>
                <th>Todo dia</th>
                <th className={styles.right}>Valor</th>
                <th>Próxima</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {list.map((r) => (
                <tr key={r.id}>
                  <td data-label="Descrição">
                    <span className={styles.desc}>
                      <strong>{r.description}</strong>
                      <span className={styles.muted}>
                        {[r.kind === 'PAYABLE' ? 'A pagar' : 'A receber', r.categoryName, r.supplierName].filter(Boolean).join(' · ')}
                      </span>
                    </span>
                  </td>
                  <td data-label="Todo dia">{r.dueDay}</td>
                  <td data-label="Valor" className={`${styles.right} ${styles.amount}`}>
                    {formatMoney(r.amountCents)}
                  </td>
                  <td data-label="Próxima">
                    {r.active ? (
                      <>
                        {formatDateOnly(r.nextDueDate)}
                        {r.endsOn && <div className={styles.muted}>até {formatDateOnly(r.endsOn)}</div>}
                      </>
                    ) : (
                      <Badge tone="neutral">Encerrada</Badge>
                    )}
                  </td>
                  <td data-label="">
                    {r.active && (
                      <div className={styles.rowActions}>
                        <Button size="small" variant="ghost" onClick={() => setEditing(r)}>
                          Editar
                        </Button>
                        <Button
                          size="small"
                          variant="ghost"
                          onClick={() => {
                            if (window.confirm(`Encerrar "${r.description}"? As contas dela que ainda não venceram saem; as vencidas e as pagas ficam.`)) {
                              end.mutate(r);
                            }
                          }}
                        >
                          Encerrar
                        </Button>
                      </div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {editing !== undefined && <RecurrenceModal recurrence={editing} onClose={() => setEditing(undefined)} />}
    </Card>
  );
}

/** Nova conta recorrente, ou a edição de uma (vale para as próximas e as em aberto que não venceram). */
export function RecurrenceModal({ recurrence, onClose }: { recurrence: FinanceRecurrence | null; onClose: () => void }) {
  const { notify } = useToast();
  const refresh = useRefreshFinance();
  const { categories, suppliers } = useFinanceLookups();
  const [kind, setKind] = useState<EntryKind>(recurrence?.kind ?? 'PAYABLE');
  const [description, setDescription] = useState(recurrence?.description ?? '');
  const [amount, setAmount] = useState(recurrence ? centsToInput(recurrence.amountCents) : '');
  const [categoryId, setCategoryId] = useState(recurrence?.categoryId ?? '');
  const [supplierId, setSupplierId] = useState(recurrence?.supplierId ?? '');
  const [firstDueDate, setFirstDueDate] = useState(todayISO());
  const [endsOn, setEndsOn] = useState(recurrence?.endsOn ?? '');
  const [error, setError] = useState('');

  const save = useMutation({
    mutationFn: () => {
      const amountCents = parseMoney(amount);
      if (!description.trim()) throw new Error('Descreva a conta.');
      if (!amountCents || amountCents <= 0) throw new Error('Valor inválido.');
      if (!categoryId) throw new Error('Escolha a categoria.');
      const common = { description, categoryId, supplierId: supplierId || null, amountCents, endsOn: endsOn || null };
      return recurrence
        ? financeApi.updateRecurrence(recurrence.id, common)
        : financeApi.createRecurrence({ ...common, kind, firstDueDate });
    },
    onSuccess: (r) => {
      refresh();
      notify({ tone: 'success', title: recurrence ? 'Recorrência atualizada' : 'Recorrência criada', description: r.description });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });

  const options = categories.filter((c) => c.kind === (kind === 'PAYABLE' ? 'EXPENSE' : 'INCOME') && (c.active || c.id === categoryId));
  const day = Number(firstDueDate.slice(8, 10));

  return (
    <Modal
      open
      title={recurrence ? 'Editar conta recorrente' : 'Nova conta recorrente'}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Salvar
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        {!recurrence && (
          <SelectField label="Tipo" value={kind} onChange={(e) => {
            setKind(e.target.value as EntryKind);
            setCategoryId('');
          }}>
            <option value="PAYABLE">A pagar (despesa)</option>
            <option value="RECEIVABLE">A receber (receita)</option>
          </SelectField>
        )}
        <TextField label="Descrição" value={description} onChange={(e) => setDescription(e.target.value)} maxLength={200} placeholder="Ex.: Aluguel da sala" />
        <div className={pageStyles.formRow}>
          <TextField label="Valor por mês (R$)" value={amount} onChange={(e) => setAmount(e.target.value)} inputMode="decimal" placeholder="0,00" />
          {!recurrence && (
            <TextField
              label="Primeiro vencimento"
              type="date"
              value={firstDueDate}
              onChange={(e) => setFirstDueDate(e.target.value)}
              hint={day > 28 ? 'Escolha um dia até 28 (para existir em todos os meses).' : `Vence todo dia ${day || '—'}.`}
            />
          )}
        </div>
        <div className={pageStyles.formRow}>
          <SelectField label="Categoria" value={categoryId} onChange={(e) => setCategoryId(e.target.value)}>
            <option value="">Escolha…</option>
            {options.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </SelectField>
          <SelectField label="Fornecedor" value={supplierId} onChange={(e) => setSupplierId(e.target.value)}>
            <option value="">Nenhum</option>
            {suppliers
              .filter((x) => x.active || x.id === supplierId)
              .map((x) => (
                <option key={x.id} value={x.id}>
                  {x.name}
                </option>
              ))}
          </SelectField>
        </div>
        <TextField
          label="Termina em (opcional)"
          type="date"
          value={endsOn}
          onChange={(e) => setEndsOn(e.target.value)}
          hint="O último vencimento. Em branco: até encerrar."
        />
        {recurrence && <p className={styles.muted}>A mudança vale para as próximas e para as contas dela em aberto que ainda não venceram.</p>}
        {error && <p className={styles.danger}>{error}</p>}
      </div>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Fornecedores
// ---------------------------------------------------------------------------

const EMPTY_SUPPLIER: SupplierInput = {
  name: '',
  document: '',
  email: '',
  phone: '',
  pixKey: '',
  pixKeyType: '',
  notes: '',
  active: true,
};

function SuppliersCard() {
  const { suppliers } = useFinanceLookups();
  const [editing, setEditing] = useState<Supplier | null | undefined>(undefined);
  return (
    <Card
      title="Fornecedores"
      actions={
        <Button variant="primary" size="small" onClick={() => setEditing(null)}>
          Novo fornecedor
        </Button>
      }
      flush
    >
      {suppliers.length === 0 ? (
        <EmptyState icon="🏢" title="Nenhum fornecedor" description="Operadora dos chips, fábrica dos rastreadores, contador..." />
      ) : (
        <div className={pageStyles.tableWrap}>
          <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
            <thead>
              <tr>
                <th>Fornecedor</th>
                <th>Documento</th>
                <th>Contato</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {suppliers.map((x) => (
                <tr key={x.id}>
                  <td data-label="Fornecedor">
                    <span className={styles.desc}>
                      <strong>{x.name}</strong>
                      {!x.active && <span className={styles.muted}>Inativo</span>}
                    </span>
                  </td>
                  <td data-label="Documento">{x.document ? formatDocument(x.document) : '—'}</td>
                  <td data-label="Contato">
                    <span className={styles.desc}>
                      <span>{[x.email, x.phone].filter(Boolean).join(' · ') || '—'}</span>
                      {x.pixKey && (
                        <span className={styles.muted}>
                          Pix{x.pixKeyType ? ` (${KEY_TYPE_LABELS[x.pixKeyType]})` : ''}: {x.pixKey}
                        </span>
                      )}
                    </span>
                  </td>
                  <td data-label="">
                    <div className={styles.rowActions}>
                      <Button size="small" variant="ghost" onClick={() => setEditing(x)}>
                        Editar
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {editing !== undefined && <SupplierModal supplier={editing} onClose={() => setEditing(undefined)} />}
    </Card>
  );
}

function SupplierModal({ supplier, onClose }: { supplier: Supplier | null; onClose: () => void }) {
  const { notify } = useToast();
  const refresh = useRefreshFinance();
  const [draft, setDraft] = useState<SupplierInput>(supplier ? { ...supplier, document: formatDocument(supplier.document) } : EMPTY_SUPPLIER);
  const [error, setError] = useState('');
  const set = <K extends keyof SupplierInput>(key: K, value: SupplierInput[K]) => setDraft((d) => ({ ...d, [key]: value }));
  const save = useMutation({
    mutationFn: () => financeApi.saveSupplier(supplier?.id ?? null, draft),
    onSuccess: (x) => {
      refresh();
      notify({ tone: 'success', title: supplier ? 'Fornecedor atualizado' : 'Fornecedor cadastrado', description: x.name });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });
  return (
    <Modal
      open
      title={supplier ? 'Editar fornecedor' : 'Novo fornecedor'}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Salvar
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        <TextField label="Nome" value={draft.name} onChange={(e) => set('name', e.target.value)} maxLength={200} />
        <div className={pageStyles.formRow}>
          <TextField label="CNPJ ou CPF (opcional)" value={draft.document} onChange={(e) => set('document', e.target.value)} inputMode="numeric" />
          <TextField label="Chave Pix (opcional)" value={draft.pixKey} onChange={(e) => set('pixKey', e.target.value)} maxLength={140} />
        </div>
        {draft.pixKey.trim() !== '' && (
          <SelectField
            label="Tipo da chave Pix"
            value={draft.pixKeyType}
            onChange={(e) => set('pixKeyType', e.target.value as SupplierInput['pixKeyType'])}
            hint={
              draft.pixKeyType
                ? 'Usado para pagar este fornecedor por Pix pela AbacatePay.'
                : detectKeyType(draft.pixKey)
                  ? `Pelo formato: ${KEY_TYPE_LABELS[detectKeyType(draft.pixKey) as PixKeyType]}.`
                  : 'Não dá para saber pelo formato (11 dígitos podem ser CPF ou celular): escolha o tipo.'
            }
          >
            <option value="">Pelo formato da chave</option>
            {(['CPF', 'CNPJ', 'PHONE', 'EMAIL', 'RANDOM'] as const).map((t) => (
              <option key={t} value={t}>
                {KEY_TYPE_LABELS[t]}
              </option>
            ))}
          </SelectField>
        )}
        <div className={pageStyles.formRow}>
          <TextField label="E-mail (opcional)" type="email" value={draft.email} onChange={(e) => set('email', e.target.value)} />
          <TextField label="Telefone (opcional)" value={draft.phone} onChange={(e) => set('phone', e.target.value)} maxLength={40} />
        </div>
        <Field label="Observações (opcional)">
          {(id) => <textarea id={id} className={styles.textarea} value={draft.notes} maxLength={2000} onChange={(e) => set('notes', e.target.value)} />}
        </Field>
        <label className={styles.check}>
          <input type="checkbox" checked={draft.active} onChange={(e) => set('active', e.target.checked)} />
          Ativo (aparece na escolha das contas)
        </label>
        {error && <p className={styles.danger}>{error}</p>}
      </div>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Categorias
// ---------------------------------------------------------------------------

function CategoriesCard() {
  const { categories } = useFinanceLookups();
  const [editing, setEditing] = useState<FinanceCategory | null | undefined>(undefined);
  const groups = [...INCOME_GROUPS, ...EXPENSE_GROUPS];
  return (
    <Card
      title="Categorias"
      subtitle="Cada categoria entra numa linha do resultado do mês."
      actions={
        <Button variant="primary" size="small" onClick={() => setEditing(null)}>
          Nova categoria
        </Button>
      }
    >
      <ul className={styles.list}>
        {groups.map((group) => {
          const items = categories.filter((c) => c.group === group);
          if (items.length === 0) return null;
          return (
            <li key={group} className={styles.listItem}>
              <span className={styles.desc}>
                <strong>{GROUP_LABELS[group]}</strong>
                <span className={styles.muted}>{GROUP_HINTS[group]}</span>
              </span>
              <span className={styles.rowActions}>
                {items.map((c) => (
                  <Button key={c.id} size="small" variant="ghost" onClick={() => setEditing(c)} title="Editar">
                    {c.active ? c.name : `${c.name} (inativa)`}
                  </Button>
                ))}
              </span>
            </li>
          );
        })}
      </ul>
      {editing !== undefined && <CategoryModal category={editing} onClose={() => setEditing(undefined)} />}
    </Card>
  );
}

function CategoryModal({ category, onClose }: { category: FinanceCategory | null; onClose: () => void }) {
  const { notify } = useToast();
  const refresh = useRefreshFinance();
  const [name, setName] = useState(category?.name ?? '');
  const [group, setGroup] = useState<DreGroup>(category?.group ?? 'OPERATING');
  const [active, setActive] = useState(category?.active ?? true);
  const [error, setError] = useState('');
  // Uma categoria não muda de lado (despesa ↔ receita): os lançamentos dela iriam junto.
  const groups = !category ? [...EXPENSE_GROUPS, ...INCOME_GROUPS] : category.kind === 'EXPENSE' ? EXPENSE_GROUPS : INCOME_GROUPS;
  const save = useMutation({
    mutationFn: () => financeApi.saveCategory(category?.id ?? null, { name, group, active }),
    onSuccess: (c) => {
      refresh();
      notify({ tone: 'success', title: category ? 'Categoria atualizada' : 'Categoria criada', description: c.name });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });
  return (
    <Modal
      open
      title={category ? 'Editar categoria' : 'Nova categoria'}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Salvar
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        <TextField label="Nome" value={name} onChange={(e) => setName(e.target.value)} maxLength={80} />
        <SelectField label="Linha do resultado" value={group} onChange={(e) => setGroup(e.target.value as DreGroup)} hint={GROUP_HINTS[group]}>
          {groups.map((g) => (
            <option key={g} value={g}>
              {INCOME_GROUPS.includes(g) ? 'Receita · ' : 'Despesa · '}
              {GROUP_LABELS[g]}
            </option>
          ))}
        </SelectField>
        <label className={styles.check}>
          <input type="checkbox" checked={active} onChange={(e) => setActive(e.target.checked)} />
          Ativa (aparece na escolha dos lançamentos)
        </label>
        {error && <p className={styles.danger}>{error}</p>}
      </div>
    </Modal>
  );
}

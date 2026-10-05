import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';

import { financeApi } from '@/api/resources';
import type { MovementInput, StockItemInput } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Field, SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateOnly, formatMoney, parseMoney } from '@/services/format';
import type { PaymentMethod, StockItem, StockKind, StockMoveType } from '@/types';

import pageStyles from '../../Page.module.css';
import { financeKey, useFinanceLookups, useRefreshFinance } from './EntryModals';
import { METHOD_LABELS, MOVE_LABELS, STOCK_KIND_LABELS, addDays, splitInstallments, todayISO } from './labels';
import styles from './Company.module.css';

/** O estoque de rastreadores, chips e acessórios, com o custo médio. */
export function StockTab() {
  const stock = useQuery({ queryKey: [...financeKey, 'stock'], queryFn: financeApi.stockItems });
  const movements = useQuery({ queryKey: [...financeKey, 'movements'], queryFn: () => financeApi.movements() });
  const [editing, setEditing] = useState<StockItem | null | undefined>(undefined);
  const [moving, setMoving] = useState<{ item: StockItem | null; type: StockMoveType } | null>(null);

  if (stock.isLoading) return <Spinner label="Carregando o estoque" />;
  const items = stock.data?.items ?? [];
  const trackers = stock.data?.trackers;
  const value = items.reduce((sum, i) => sum + i.valueCents, 0);
  const units = items.reduce((sum, i) => sum + i.quantity, 0);

  return (
    <div className={styles.tab}>
      <div className={billing.tiles}>
        <div className={billing.tile}>
          <span className={billing.tileLabel}>Valor em estoque</span>
          <span className={billing.tileValue}>{formatMoney(value)}</span>
          <span className={billing.tileHint}>{units} unidade(s) pelo custo médio</span>
        </div>
        {trackers && (
          <div className={billing.tile}>
            <span className={billing.tileLabel}>Rastreadores no sistema</span>
            <span className={billing.tileValue}>{trackers.withVehicle}</span>
            <span className={billing.tileHint}>
              em veículos · {trackers.withoutVehicle} cadastrado(s) sem veículo
            </span>
          </div>
        )}
      </div>

      <Card
        title="Itens"
        actions={
          <div className={pageStyles.actions}>
            <Button size="small" onClick={() => setEditing(null)}>
              Novo item
            </Button>
            {items.length > 0 && (
              <Button size="small" variant="primary" onClick={() => setMoving({ item: null, type: 'IN' })}>
                Registrar compra
              </Button>
            )}
          </div>
        }
        flush
      >
        {items.length === 0 ? (
          <EmptyState
            icon="📦"
            title="Nenhum item"
            description="Cadastre o que a empresa estoca (o modelo de rastreador, o chip da operadora) e registre as compras."
          />
        ) : (
          <div className={pageStyles.tableWrap}>
            <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
              <thead>
                <tr>
                  <th>Item</th>
                  <th className={styles.right}>Em estoque</th>
                  <th className={styles.right}>Custo médio</th>
                  <th className={styles.right}>Valor</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {items.map((i) => (
                  <tr key={i.id}>
                    <td data-label="Item">
                      <span className={styles.desc}>
                        <strong>{i.name}</strong>
                        <span className={styles.muted}>
                          {STOCK_KIND_LABELS[i.kind]}
                          {i.minQuantity > 0 ? ` · mínimo ${i.minQuantity}` : ''}
                          {!i.active ? ' · inativo' : ''}
                        </span>
                      </span>
                    </td>
                    <td data-label="Em estoque" className={styles.right}>
                      <span className={styles.amount}>{i.quantity}</span>{' '}
                      {i.low && (
                        <Badge tone="warning" dot>
                          Comprar
                        </Badge>
                      )}
                    </td>
                    <td data-label="Custo médio" className={styles.right}>
                      {formatMoney(i.avgCostCents)}
                    </td>
                    <td data-label="Valor" className={`${styles.right} ${styles.amount}`}>
                      {formatMoney(i.valueCents)}
                    </td>
                    <td data-label="">
                      <div className={styles.rowActions}>
                        <Button size="small" variant="primary" onClick={() => setMoving({ item: i, type: 'IN' })}>
                          Entrada
                        </Button>
                        <Button size="small" variant="ghost" onClick={() => setMoving({ item: i, type: 'OUT' })} disabled={i.quantity === 0}>
                          Saída
                        </Button>
                        <Button size="small" variant="ghost" onClick={() => setMoving({ item: i, type: 'ADJUST' })}>
                          Acertar
                        </Button>
                        <Button size="small" variant="ghost" onClick={() => setEditing(i)}>
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
      </Card>

      <Card title="Movimentos recentes" subtitle="Não se editam: um erro se corrige com um acerto. A saída vai para o resultado pelo custo médio do dia." flush>
        {(movements.data ?? []).length === 0 ? (
          <EmptyState icon="🔄" title="Nenhum movimento" description="As compras, instalações e acertos aparecem aqui." />
        ) : (
          <div className={pageStyles.tableWrap}>
            <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
              <thead>
                <tr>
                  <th>Data</th>
                  <th>Item</th>
                  <th>Movimento</th>
                  <th className={styles.right}>Quantidade</th>
                  <th className={styles.right}>Custo</th>
                </tr>
              </thead>
              <tbody>
                {(movements.data ?? []).map((m) => (
                  <tr key={m.id}>
                    <td data-label="Data">{formatDateOnly(m.occurredOn)}</td>
                    <td data-label="Item">
                      <span className={styles.desc}>
                        <strong>{m.itemName}</strong>
                        {(m.notes || m.supplierName) && <span className={styles.muted}>{[m.supplierName, m.notes].filter(Boolean).join(' · ')}</span>}
                      </span>
                    </td>
                    <td data-label="Movimento">{MOVE_LABELS[m.type]}</td>
                    <td data-label="Quantidade" className={`${styles.right} ${styles.amount} ${m.quantity < 0 ? styles.negative : styles.positive}`}>
                      {m.quantity > 0 ? `+${m.quantity}` : m.quantity}
                    </td>
                    <td data-label="Custo" className={styles.right}>
                      {formatMoney(Math.abs(m.totalCents))}
                      <div className={styles.muted}>{formatMoney(m.unitCostCents)} / un.</div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {editing !== undefined && <StockItemModal item={editing} onClose={() => setEditing(undefined)} />}
      {moving && <MovementModal items={items} item={moving.item} type={moving.type} onClose={() => setMoving(null)} />}
    </div>
  );
}

function StockItemModal({ item, onClose }: { item: StockItem | null; onClose: () => void }) {
  const { notify } = useToast();
  const refresh = useRefreshFinance();
  const [draft, setDraft] = useState<StockItemInput>(
    item ? { name: item.name, kind: item.kind, minQuantity: item.minQuantity, active: item.active } : { name: '', kind: 'TRACKER', minQuantity: 0, active: true },
  );
  const [error, setError] = useState('');
  const save = useMutation({
    mutationFn: () => financeApi.saveStockItem(item?.id ?? null, draft),
    onSuccess: (saved) => {
      refresh();
      notify({ tone: 'success', title: item ? 'Item atualizado' : 'Item cadastrado', description: saved.name });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });
  return (
    <Modal
      open
      title={item ? 'Editar item' : 'Novo item do estoque'}
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
        <TextField label="Nome" value={draft.name} maxLength={80} placeholder="Ex.: Rastreador J16" onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
        <div className={pageStyles.formRow}>
          <SelectField label="Tipo" value={draft.kind} onChange={(e) => setDraft({ ...draft, kind: e.target.value as StockKind })}>
            {Object.entries(STOCK_KIND_LABELS).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </SelectField>
          <TextField
            label="Estoque mínimo"
            type="number"
            min={0}
            value={String(draft.minQuantity)}
            hint="Abaixo disso, o painel avisa para comprar."
            onChange={(e) => setDraft({ ...draft, minQuantity: Math.max(0, Number(e.target.value) || 0) })}
          />
        </div>
        <label className={styles.check}>
          <input type="checkbox" checked={draft.active} onChange={(e) => setDraft({ ...draft, active: e.target.checked })} />
          Ativo
        </label>
        {error && <p className={styles.danger}>{error}</p>}
      </div>
    </Modal>
  );
}

/** Entrada (compra, com a conta a pagar), saída, perda ou acerto. */
function MovementModal({
  items,
  item,
  type: initialType,
  onClose,
}: {
  items: StockItem[];
  item: StockItem | null;
  type: StockMoveType;
  onClose: () => void;
}) {
  const { notify } = useToast();
  const refresh = useRefreshFinance();
  const { categories, suppliers } = useFinanceLookups();
  const today = todayISO();
  const [itemId, setItemId] = useState(item?.id ?? items.find((i) => i.active)?.id ?? '');
  const [type, setType] = useState<StockMoveType>(initialType);
  const [quantity, setQuantity] = useState('');
  const [direction, setDirection] = useState<1 | -1>(1);
  const [unitCost, setUnitCost] = useState('');
  const [occurredOn, setOccurredOn] = useState(today);
  const [supplierId, setSupplierId] = useState('');
  const [notes, setNotes] = useState('');
  const purchaseCategory = categories.find((c) => c.group === 'INVESTMENT' && c.active);
  const [withPayable, setWithPayable] = useState(true);
  const [categoryId, setCategoryId] = useState('');
  const [dueDate, setDueDate] = useState(addDays(today, 7));
  const [installments, setInstallments] = useState('1');
  const [paid, setPaid] = useState(false);
  const [method, setMethod] = useState<PaymentMethod>('');
  const [error, setError] = useState('');

  const current = items.find((i) => i.id === itemId);
  const qty = Math.floor(Number(quantity)) || 0;
  const unitCents = parseMoney(unitCost) ?? 0;
  const total = qty * unitCents;
  const parts = Math.max(1, Math.min(60, Number(installments) || 1));
  const chosenCategory = categoryId || purchaseCategory?.id || '';

  const save = useMutation({
    mutationFn: () => {
      if (!itemId) throw new Error('Escolha o item.');
      if (qty <= 0) throw new Error('Informe a quantidade.');
      if (type === 'IN' && unitCents <= 0) throw new Error('Informe o custo por unidade.');
      const input: MovementInput = {
        itemId,
        type,
        quantity: type === 'ADJUST' ? qty * direction : qty,
        unitCostCents: type === 'IN' ? unitCents : 0,
        occurredOn,
        supplierId: type === 'IN' && supplierId ? supplierId : null,
        notes,
        payable:
          type === 'IN' && withPayable
            ? { categoryId: chosenCategory, dueDate, installments: paid ? 1 : parts, paidOn: paid ? occurredOn : null, paymentMethod: paid ? method : '' }
            : null,
      };
      return financeApi.move(input);
    },
    onSuccess: ({ movement, payables }) => {
      refresh();
      notify({
        tone: 'success',
        title: 'Movimento registrado',
        description: `${movement.itemName}: ${movement.quantity > 0 ? '+' : ''}${movement.quantity}${payables.length ? ` · ${payables.length} conta(s) a pagar lançada(s)` : ''}`,
      });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });

  return (
    <Modal
      open
      title="Movimentar o estoque"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Registrar
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        <div className={pageStyles.formRow}>
          <SelectField label="Item" value={itemId} onChange={(e) => setItemId(e.target.value)}>
            {items
              .filter((i) => i.active || i.id === itemId)
              .map((i) => (
                <option key={i.id} value={i.id}>
                  {i.name} ({i.quantity} em estoque)
                </option>
              ))}
          </SelectField>
          <SelectField label="Movimento" value={type} onChange={(e) => setType(e.target.value as StockMoveType)}>
            {Object.entries(MOVE_LABELS).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </SelectField>
        </div>
        <div className={pageStyles.formRow}>
          <TextField label="Quantidade" type="number" min={1} value={quantity} onChange={(e) => setQuantity(e.target.value)} />
          {type === 'ADJUST' && (
            <SelectField label="Na contagem" value={String(direction)} onChange={(e) => setDirection(Number(e.target.value) as 1 | -1)}>
              <option value="1">Sobrou (entra)</option>
              <option value="-1">Faltou (sai)</option>
            </SelectField>
          )}
          {type === 'IN' && (
            <TextField label="Custo por unidade (R$)" value={unitCost} onChange={(e) => setUnitCost(e.target.value)} inputMode="decimal" placeholder="0,00" />
          )}
          <TextField label="Data" type="date" value={occurredOn} max={today} onChange={(e) => setOccurredOn(e.target.value)} />
        </div>
        {type !== 'IN' && current && (
          <p className={styles.muted}>Sai pelo custo médio de hoje: {formatMoney(current.avgCostCents)} por unidade.</p>
        )}

        {type === 'IN' && (
          <>
            <SelectField label="Fornecedor" value={supplierId} onChange={(e) => setSupplierId(e.target.value)}>
              <option value="">Nenhum</option>
              {suppliers
                .filter((x) => x.active)
                .map((x) => (
                  <option key={x.id} value={x.id}>
                    {x.name}
                  </option>
                ))}
            </SelectField>
            {total > 0 && <p className={styles.preview}>Total da compra: {formatMoney(total)}</p>}
            <label className={styles.check}>
              <input type="checkbox" checked={withPayable} onChange={(e) => setWithPayable(e.target.checked)} />
              Lançar a conta a pagar desta compra
            </label>
            {withPayable && (
              <>
                <div className={pageStyles.formRow}>
                  <SelectField label="Categoria" value={chosenCategory} onChange={(e) => setCategoryId(e.target.value)}>
                    {categories
                      .filter((c) => c.kind === 'EXPENSE' && c.active)
                      .map((c) => (
                        <option key={c.id} value={c.id}>
                          {c.name}
                        </option>
                      ))}
                  </SelectField>
                  <TextField label="Vencimento" type="date" value={dueDate} onChange={(e) => setDueDate(e.target.value)} />
                  {!paid && (
                    <TextField label="Parcelas" type="number" min={1} max={60} value={installments} onChange={(e) => setInstallments(e.target.value)} />
                  )}
                </div>
                {!paid && parts > 1 && total > 0 && (
                  <p className={styles.preview}>
                    {parts} parcelas de {formatMoney(splitInstallments(total, parts)[parts - 1])}, uma por mês.
                  </p>
                )}
                <label className={styles.check}>
                  <input type="checkbox" checked={paid} onChange={(e) => setPaid(e.target.checked)} />
                  Já está paga (na data da compra)
                </label>
                {paid && (
                  <SelectField label="Forma" value={method} onChange={(e) => setMethod(e.target.value as PaymentMethod)}>
                    <option value="">Não informar</option>
                    {Object.entries(METHOD_LABELS).map(([value, label]) => (
                      <option key={value} value={value}>
                        {label}
                      </option>
                    ))}
                  </SelectField>
                )}
              </>
            )}
          </>
        )}
        <Field label="Observações (opcional)">
          {(id) => (
            <textarea
              id={id}
              className={styles.textarea}
              value={notes}
              maxLength={2000}
              placeholder={type === 'OUT' ? 'Ex.: instalação no veículo ABC1D23' : ''}
              onChange={(e) => setNotes(e.target.value)}
            />
          )}
        </Field>
        {error && <p className={styles.danger}>{error}</p>}
      </div>
    </Modal>
  );
}

import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { financeApi } from '@/api/resources';
import type { EntryInput } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Field, SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { useToast } from '@/components/ui/Toast';
import { centsToInput, formatBytes, formatDateOnly, formatMoney, parseMoney } from '@/services/format';
import type { EntryKind, FinanceAttachment, FinanceEntry, PaymentMethod } from '@/types';

import pageStyles from '../../Page.module.css';
import { METHOD_LABELS, splitInstallments, todayISO } from './labels';
import styles from './Company.module.css';

export const financeKey = ['finance'] as const;

/** Atualiza tudo da gestão (listas, caixa, resultado, visão geral, menu). */
export function useRefreshFinance() {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: financeKey });
}

export function useFinanceLookups() {
  const categories = useQuery({ queryKey: [...financeKey, 'categories'], queryFn: financeApi.categories, staleTime: 60_000 });
  const suppliers = useQuery({ queryKey: [...financeKey, 'suppliers'], queryFn: financeApi.suppliers, staleTime: 60_000 });
  return { categories: categories.data ?? [], suppliers: suppliers.data ?? [] };
}

export const KIND_WORDS: Record<EntryKind, { one: string; paid: string; pay: string; paidOn: string }> = {
  PAYABLE: { one: 'conta', paid: 'Já está paga', pay: 'Pagar', paidOn: 'Data do pagamento' },
  RECEIVABLE: { one: 'receita', paid: 'Já foi recebida', pay: 'Receber', paidOn: 'Data do recebimento' },
};

function MethodOptions() {
  return (
    <>
      <option value="">Não informar</option>
      {Object.entries(METHOD_LABELS).map(([value, label]) => (
        <option key={value} value={value}>
          {label}
        </option>
      ))}
    </>
  );
}

interface EntryDraft {
  description: string;
  amount: string;
  dueDate: string;
  categoryId: string;
  supplierId: string;
  installments: string;
  paid: boolean;
  paidOn: string;
  paymentMethod: PaymentMethod;
  paymentCode: string;
  notes: string;
}

function draftFrom(e: FinanceEntry | null): EntryDraft {
  const today = todayISO();
  return {
    description: e?.description ?? '',
    amount: e ? centsToInput(e.amountCents) : '',
    dueDate: e?.dueDate ?? today,
    categoryId: e?.categoryId ?? '',
    supplierId: e?.supplierId ?? '',
    installments: '1',
    paid: false,
    paidOn: today,
    paymentMethod: '',
    paymentCode: e?.paymentCode ?? '',
    notes: e?.notes ?? '',
  };
}

/** Nova conta (ou receita), parcelada ou não, ou a edição de uma em aberto. */
export function EntryModal({
  kind,
  entry,
  onClose,
}: {
  kind: EntryKind;
  /** null: nova. */
  entry: FinanceEntry | null;
  onClose: () => void;
}) {
  const { notify } = useToast();
  const refresh = useRefreshFinance();
  const { categories, suppliers } = useFinanceLookups();
  const [draft, setDraft] = useState<EntryDraft>(() => draftFrom(entry));
  const [error, setError] = useState('');
  const words = KIND_WORDS[kind];
  const set = <K extends keyof EntryDraft>(key: K, value: EntryDraft[K]) => setDraft((d) => ({ ...d, [key]: value }));

  const options = categories.filter(
    (c) => c.kind === (kind === 'PAYABLE' ? 'EXPENSE' : 'INCOME') && (c.active || c.id === draft.categoryId),
  );
  const amountCents = parseMoney(draft.amount);
  const installments = Math.max(1, Math.min(60, Number(draft.installments) || 1));
  const parts = amountCents ? splitInstallments(amountCents, installments) : [];

  const save = useMutation({
    mutationFn: async () => {
      if (!draft.description.trim()) throw new Error('Descreva o lançamento.');
      if (!amountCents || amountCents <= 0) throw new Error('Valor inválido. Use, por exemplo, 150,00.');
      if (!draft.categoryId) throw new Error('Escolha a categoria.');
      if (!draft.dueDate) throw new Error('Informe o vencimento.');
      const base = {
        description: draft.description,
        categoryId: draft.categoryId,
        supplierId: draft.supplierId || null,
        amountCents,
        dueDate: draft.dueDate,
        paymentCode: draft.paymentCode,
        notes: draft.notes,
      };
      if (entry) {
        await financeApi.updateEntry(entry.id, base);
        return 1;
      }
      const input: EntryInput = {
        ...base,
        kind,
        installments,
        paidOn: draft.paid && installments === 1 ? draft.paidOn : null,
        paymentMethod: draft.paid ? draft.paymentMethod : '',
      };
      return (await financeApi.createEntries(input)).length;
    },
    onSuccess: (count) => {
      refresh();
      notify({
        tone: 'success',
        title: entry ? 'Lançamento atualizado' : count > 1 ? `${count} parcelas lançadas` : 'Lançamento criado',
        description: draft.description,
      });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });

  return (
    <Modal
      open
      title={entry ? `Editar ${words.one}` : kind === 'PAYABLE' ? 'Nova conta a pagar' : 'Nova receita'}
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
      <form
        className={pageStyles.form}
        onSubmit={(event) => {
          event.preventDefault();
          save.mutate();
        }}
      >
        <TextField
          label="Descrição"
          value={draft.description}
          onChange={(e) => set('description', e.target.value)}
          placeholder={kind === 'PAYABLE' ? 'Ex.: Aluguel de outubro' : 'Ex.: Venda de 2 rastreadores'}
          maxLength={200}
          autoFocus
        />
        <div className={pageStyles.formRow}>
          <TextField
            label={entry || installments === 1 ? 'Valor (R$)' : 'Valor total (R$)'}
            value={draft.amount}
            onChange={(e) => set('amount', e.target.value)}
            inputMode="decimal"
            placeholder="0,00"
          />
          <TextField
            label={!entry && installments > 1 ? 'Primeiro vencimento' : 'Vencimento'}
            type="date"
            value={draft.dueDate}
            onChange={(e) => set('dueDate', e.target.value)}
          />
        </div>
        <div className={pageStyles.formRow}>
          <SelectField label="Categoria" value={draft.categoryId} onChange={(e) => set('categoryId', e.target.value)}>
            <option value="">Escolha…</option>
            {options.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </SelectField>
          <SelectField
            label={kind === 'PAYABLE' ? 'Fornecedor' : 'De quem (opcional)'}
            value={draft.supplierId}
            onChange={(e) => set('supplierId', e.target.value)}
          >
            <option value="">Nenhum</option>
            {suppliers
              .filter((x) => x.active || x.id === draft.supplierId)
              .map((x) => (
                <option key={x.id} value={x.id}>
                  {x.name}
                </option>
              ))}
          </SelectField>
        </div>

        {!entry && (
          <>
            <TextField
              label="Parcelas"
              type="number"
              min={1}
              max={60}
              value={draft.installments}
              onChange={(e) => set('installments', e.target.value)}
              hint="Uma por mês, a partir do primeiro vencimento."
            />
            {installments > 1 && parts.length > 0 && (
              <p className={styles.preview}>
                {installments} parcelas de {formatMoney(parts[parts.length - 1])}
                {parts[0] !== parts[parts.length - 1] ? ` (a primeira de ${formatMoney(parts[0])})` : ''}, a partir de{' '}
                {formatDateOnly(draft.dueDate)}.
              </p>
            )}
            {installments === 1 && (
              <label className={styles.check}>
                <input type="checkbox" checked={draft.paid} onChange={(e) => set('paid', e.target.checked)} />
                {words.paid}
              </label>
            )}
            {draft.paid && installments === 1 && (
              <div className={pageStyles.formRow}>
                <TextField label={words.paidOn} type="date" value={draft.paidOn} max={todayISO()} onChange={(e) => set('paidOn', e.target.value)} />
                <SelectField
                  label="Forma"
                  value={draft.paymentMethod}
                  onChange={(e) => set('paymentMethod', e.target.value as PaymentMethod)}
                >
                  <MethodOptions />
                </SelectField>
              </div>
            )}
          </>
        )}

        {kind === 'PAYABLE' && (
          <TextField
            label="Linha digitável do boleto ou Pix copia-e-cola (opcional)"
            value={draft.paymentCode}
            onChange={(e) => set('paymentCode', e.target.value)}
            maxLength={500}
          />
        )}
        <Field label="Observações (opcional)">
          {(id) => (
            <textarea id={id} className={styles.textarea} value={draft.notes} maxLength={2000} onChange={(e) => set('notes', e.target.value)} />
          )}
        </Field>
        {error && <p className={styles.danger}>{error}</p>}
        <button type="submit" hidden />
      </form>
    </Modal>
  );
}

/** Dar baixa: quando, quanto (com juros ou desconto) e como. */
export function PayModal({ entry, onClose }: { entry: FinanceEntry; onClose: () => void }) {
  const { notify } = useToast();
  const refresh = useRefreshFinance();
  const words = KIND_WORDS[entry.kind];
  const [paidOn, setPaidOn] = useState(todayISO());
  const [amount, setAmount] = useState(centsToInput(entry.amountCents));
  const [method, setMethod] = useState<PaymentMethod>(entry.paymentCode ? 'BOLETO' : '');
  const [error, setError] = useState('');

  const pay = useMutation({
    mutationFn: () => {
      const paidCents = parseMoney(amount);
      if (!paidCents || paidCents <= 0) throw new Error('Valor inválido.');
      return financeApi.pay(entry.id, { paidOn, paidCents, method });
    },
    onSuccess: () => {
      refresh();
      notify({ tone: 'success', title: entry.kind === 'PAYABLE' ? 'Conta paga' : 'Receita recebida', description: entry.description });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });

  return (
    <Modal
      open
      title={`${words.pay}: ${entry.description}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={pay.isPending} onClick={() => pay.mutate()}>
            Confirmar
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        <p className={styles.muted}>
          {formatMoney(entry.amountCents)} · vence {formatDateOnly(entry.dueDate)}
          {entry.supplierName ? ` · ${entry.supplierName}` : ''}
        </p>
        {entry.paymentCode && <PaymentCode code={entry.paymentCode} />}
        <div className={pageStyles.formRow}>
          <TextField label={words.paidOn} type="date" value={paidOn} max={todayISO()} onChange={(e) => setPaidOn(e.target.value)} />
          <TextField
            label="Valor pago (R$)"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            inputMode="decimal"
            hint="Com juros ou desconto, se houve."
          />
        </div>
        <SelectField label="Forma" value={method} onChange={(e) => setMethod(e.target.value as PaymentMethod)}>
          <MethodOptions />
        </SelectField>
        {error && <p className={styles.danger}>{error}</p>}
      </div>
    </Modal>
  );
}

/** O código do boleto ou do Pix, com o botão de copiar. */
export function PaymentCode({ code }: { code: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Field label="Código para pagar">
      {(id) => (
        <div className={styles.code}>
          <input id={id} className={styles.search} value={code} readOnly onFocus={(e) => e.target.select()} />
          <Button
            size="small"
            onClick={() => {
              void navigator.clipboard?.writeText(code).then(() => setCopied(true));
            }}
          >
            {copied ? 'Copiado' : 'Copiar'}
          </Button>
        </div>
      )}
    </Field>
  );
}

const MAX_FILE = 5 * 1024 * 1024;

/** Os anexos da conta: boleto, nota fiscal, comprovante. */
export function AttachmentsModal({ entry, onClose }: { entry: FinanceEntry; onClose: () => void }) {
  const { notify } = useToast();
  const refresh = useRefreshFinance();
  const [list, setList] = useState<FinanceAttachment[]>(entry.attachments);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => setList(entry.attachments), [entry]);

  const upload = useMutation({
    mutationFn: (file: File) => {
      if (file.size > MAX_FILE) throw new Error('O arquivo precisa ter até 5 MB.');
      return financeApi.attach(entry.id, file);
    },
    onSuccess: (attachment) => {
      setList((current) => [...current, attachment]);
      refresh();
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Anexo não enviado', description: err.message }),
  });
  const remove = useMutation({
    mutationFn: (id: string) => financeApi.removeAttachment(id),
    onSuccess: (_, id) => {
      setList((current) => current.filter((a) => a.id !== id));
      refresh();
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível excluir', description: err.message }),
  });

  return (
    <Modal
      open
      title={`Anexos: ${entry.description}`}
      onClose={onClose}
      footer={
        <Button variant="ghost" onClick={onClose}>
          Fechar
        </Button>
      }
    >
      <div className={pageStyles.form}>
        {list.length === 0 ? (
          <p className={styles.muted}>Nenhum anexo. Guarde aqui o boleto, a nota fiscal ou o comprovante.</p>
        ) : (
          <ul className={styles.attachments}>
            {list.map((a) => (
              <li key={a.id} className={styles.attachment}>
                <span className={styles.attachmentName}>
                  {a.filename} <span className={styles.muted}>· {formatBytes(a.sizeBytes)}</span>
                </span>
                <span className={styles.listEnd}>
                  <Button
                    size="small"
                    variant="ghost"
                    onClick={() =>
                      financeApi
                        .downloadAttachment(a.id)
                        .catch((err: Error) => notify({ tone: 'error', title: 'Não foi possível baixar', description: err.message }))
                    }
                  >
                    Baixar
                  </Button>
                  <Button size="small" variant="ghost" loading={remove.isPending && remove.variables === a.id} onClick={() => remove.mutate(a.id)}>
                    Excluir
                  </Button>
                </span>
              </li>
            ))}
          </ul>
        )}
        <input
          ref={input}
          type="file"
          accept="application/pdf,image/png,image/jpeg"
          hidden
          onChange={(e) => {
            const file = e.target.files?.[0];
            if (file) upload.mutate(file);
            e.target.value = '';
          }}
        />
        <Button loading={upload.isPending} onClick={() => input.current?.click()}>
          Anexar arquivo (PDF, PNG ou JPG, até 5 MB)
        </Button>
      </div>
    </Modal>
  );
}

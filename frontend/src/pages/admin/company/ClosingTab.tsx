import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { closingApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Field, TextField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { centsToInput, formatDateOnly, formatMoney, parseMoney, todayISO } from '@/services/format';
import { formatTaxId } from '@/services/taxid';
import type { ClosingLine } from '@/types';

import pageStyles from '../../Page.module.css';

/** O mês passado (AAAA-MM): o que se fecha no começo do mês. */
export function lastMonth(today: string): string {
  const [y, m] = today.split('-').map(Number);
  const date = new Date(y, m - 2, 1);
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}`;
}

/** "2026-10" → "outubro de 2026". */
export function monthLabel(month: string): string {
  const [y, m] = month.split('-').map(Number);
  return new Date(y, m - 1, 1).toLocaleDateString('pt-BR', { month: 'long', year: 'numeric' });
}

/**
 * O fechamento do mês (competência): o que cada cliente e cada associação ou
 * seguradora teve faturado no mês — as faturas que vencem nele, pagas ou não
 * — e a nota fiscal de cada um. A nota é emitida no emissor da empresa; aqui
 * fica o número, a data, o valor e o PDF (ou o XML), que o cliente baixa na
 * tela de faturas. A planilha vai para a contabilidade.
 */
export function ClosingTab() {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [month, setMonth] = useState(() => lastMonth(todayISO()));
  const [editing, setEditing] = useState<ClosingLine | null>(null);
  const [removing, setRemoving] = useState<ClosingLine | null>(null);
  const key = ['closing', month] as const;
  const data = useQuery({ queryKey: key, queryFn: () => closingApi.month(month), enabled: /^\d{4}-\d{2}$/.test(month) });

  const remove = useMutation({
    mutationFn: (line: ClosingLine) => closingApi.removeNote(line.note!.id),
    onSuccess: () => {
      setRemoving(null);
      void queryClient.invalidateQueries({ queryKey: key });
      notify({ tone: 'success', title: 'Nota tirada do fechamento' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para tirar', description: err.message }),
  });
  const exportCsv = useMutation({
    mutationFn: () => closingApi.exportCsv(month),
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para baixar', description: err.message }),
  });

  const m = data.data;
  return (
    <>
      <div className={pageStyles.actions} style={{ justifyContent: 'space-between', alignItems: 'flex-end' }}>
        <label className={fieldStyles.field} style={{ maxWidth: 220 }}>
          <span className={fieldStyles.label}>Mês</span>
          <input className={fieldStyles.input} type="month" value={month} onChange={(e) => setMonth(e.target.value)} />
        </label>
        <Button variant="secondary" loading={exportCsv.isPending} disabled={!m || m.lines.length === 0} onClick={() => exportCsv.mutate()}>
          Baixar a planilha (contabilidade)
        </Button>
      </div>

      {m && (
        <div className={billing.tiles}>
          <div className={billing.tile}>
            <span className={billing.tileLabel}>Faturado em {monthLabel(m.month)}</span>
            <span className={billing.tileValue}>{formatMoney(m.amountCents)}</span>
            <span className={billing.tileHint}>{m.lines.length} pagadores</span>
          </div>
          <div className={billing.tile}>
            <span className={billing.tileLabel}>Pago</span>
            <span className={billing.tileValue}>{formatMoney(m.paidCents)}</span>
            <span className={billing.tileHint}>{formatMoney(m.amountCents - m.paidCents)} em aberto</span>
          </div>
          <div className={`${billing.tile} ${m.missingNotes > 0 ? billing.tileDanger : ''}`}>
            <span className={billing.tileLabel}>Notas fiscais</span>
            <span className={billing.tileValue}>
              {m.notes} de {m.notes + m.missingNotes}
            </span>
            <span className={billing.tileHint}>{m.missingNotes > 0 ? `${m.missingNotes} por emitir` : 'Todas registradas'}</span>
          </div>
        </div>
      )}

      <Card
        title="Fechamento por pagador"
        subtitle="Competência: as faturas que vencem no mês, pagas ou não. Emita a nota no emissor e registre aqui o número e o arquivo."
        flush
      >
        {data.isLoading ? (
          <Spinner label="Fechando o mês" />
        ) : !m || m.lines.length === 0 ? (
          <EmptyState icon="🧾" title="Nada faturado neste mês" />
        ) : (
          <div className={pageStyles.tableWrap}>
            <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
              <thead>
                <tr>
                  <th>Pagador</th>
                  <th>Faturado</th>
                  <th>Pago</th>
                  <th>Nota fiscal</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {m.lines.map((line) => (
                  <tr key={line.payerId}>
                    <td>
                      <Link to={line.kind === 'organization' ? `/associacoes/${line.payerId}` : `/clientes/${line.payerId}`}>
                        <strong>{line.name}</strong>
                      </Link>{' '}
                      {line.kind === 'organization' && <Badge tone="accent">Associação</Badge>}
                      <div className={billing.muted}>
                        {line.document ? formatTaxId(line.document) : 'sem CPF/CNPJ'} · {line.invoices}{' '}
                        {line.invoices === 1 ? 'linha' : 'linhas'}
                      </div>
                    </td>
                    <td data-label="Faturado" className={billing.amount}>
                      {formatMoney(line.amountCents)}
                    </td>
                    <td data-label="Pago">
                      {line.paidCents >= line.amountCents ? (
                        <Badge tone="success">Pago</Badge>
                      ) : (
                        <span>{formatMoney(line.paidCents)}</span>
                      )}
                    </td>
                    <td data-label="Nota fiscal">
                      {line.note ? (
                        <>
                          Nº {line.note.number} · {formatDateOnly(line.note.issuedOn)}
                          <div className={billing.muted}>
                            {formatMoney(line.note.amountCents)}
                            {line.note.amountCents !== line.amountCents && ' (diferente do faturado)'}
                          </div>
                        </>
                      ) : (
                        <Badge tone="warning" dot>
                          Por emitir
                        </Badge>
                      )}
                    </td>
                    <td>
                      <div className={pageStyles.actions}>
                        {line.note?.hasFile && (
                          <Button size="small" variant="ghost" onClick={() => void closingApi.downloadNote(line.note!.id)}>
                            Baixar
                          </Button>
                        )}
                        <Button size="small" variant={line.note ? 'ghost' : 'primary'} onClick={() => setEditing(line)}>
                          {line.note ? 'Trocar' : 'Registrar nota'}
                        </Button>
                        {line.note && (
                          <Button size="small" variant="ghost" onClick={() => setRemoving(line)}>
                            Tirar
                          </Button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <NoteModal
        line={editing}
        month={month}
        onClose={() => setEditing(null)}
        onSaved={() => {
          setEditing(null);
          void queryClient.invalidateQueries({ queryKey: key });
        }}
      />
      <Modal
        open={removing !== null}
        title="Tirar a nota?"
        onClose={() => setRemoving(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRemoving(null)}>
              Voltar
            </Button>
            <Button variant="danger" loading={remove.isPending} onClick={() => removing && remove.mutate(removing)}>
              Tirar a nota
            </Button>
          </>
        }
      >
        <p>
          A nota Nº {removing?.note?.number} de {removing?.name} sai do fechamento e da tela de faturas dele. Para a
          nota emitida, cancele também no emissor.
        </p>
      </Modal>
    </>
  );
}

/** Registrar (ou trocar) a nota de um pagador no mês. */
function NoteModal({
  line,
  month,
  onClose,
  onSaved,
}: {
  line: ClosingLine | null;
  month: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { notify } = useToast();
  const [number, setNumber] = useState('');
  const [issuedOn, setIssuedOn] = useState(todayISO());
  const [amount, setAmount] = useState('');
  const [file, setFile] = useState<File | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    if (!line) return;
    setNumber(line.note?.number ?? '');
    setIssuedOn(line.note?.issuedOn ?? todayISO());
    setAmount(centsToInput(line.note?.amountCents ?? line.amountCents));
    setFile(null);
    setError('');
  }, [line]);

  const save = useMutation({
    mutationFn: () => {
      const cents = parseMoney(amount);
      if (!number.trim()) throw new Error('Informe o número da nota.');
      if (cents === null) throw new Error('Valor da nota inválido (ex.: 219,90).');
      if (!line?.note?.hasFile && !file) throw new Error('Anexe o PDF ou o XML da nota: é o que o cliente baixa.');
      return closingApi.saveNote({ customerId: line!.payerId, month, number: number.trim(), issuedOn, amountCents: cents, file });
    },
    onSuccess: (note) => {
      notify({ tone: 'success', title: `Nota Nº ${note.number} registrada`, description: `${line?.name} já pode baixar na tela de faturas.` });
      onSaved();
    },
    onError: (err: Error) => setError(err.message),
  });

  return (
    <Modal
      open={line !== null}
      title={`Nota fiscal de ${line?.name ?? ''} · ${monthLabel(month)}`}
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
        {line && (
          <p className={pageStyles.note}>
            Faturado no mês: {formatMoney(line.amountCents)} ({line.invoices} {line.invoices === 1 ? 'linha' : 'linhas'}).
            {line.document ? ` CPF/CNPJ: ${formatTaxId(line.document)}.` : ' Sem CPF/CNPJ no cadastro.'} E-mail: {line.email}.
          </p>
        )}
        {error && <div className={pageStyles.note}>{error}</div>}
        <div className={pageStyles.formRow}>
          <TextField label="Número da nota" value={number} maxLength={60} onChange={(e) => setNumber(e.target.value)} />
          <TextField label="Emitida em" type="date" value={issuedOn} onChange={(e) => setIssuedOn(e.target.value)} />
          <TextField label="Valor (R$)" inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} />
        </div>
        <Field
          label="Arquivo da nota (PDF ou XML)"
          hint={line?.note?.hasFile ? 'Sem arquivo novo, fica o que já está registrado.' : 'Até 5 MB.'}
        >
          {(id) => (
            <input
              id={id}
              type="file"
              accept="application/pdf,application/xml,text/xml,.xml,image/png,image/jpeg"
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            />
          )}
        </Field>
      </div>
    </Modal>
  );
}

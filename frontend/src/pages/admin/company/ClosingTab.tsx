import { useEffect, useState, type ReactNode } from 'react';
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

/** As faturas do pagador cuja nota da Notaas ainda não saiu: na fila, na prefeitura ou esperando créditos. */
function autoPending(line: ClosingLine): number {
  return (line.autoNotes ?? []).filter((n) => n.status === 'pending' || n.status === 'processing' || n.status === 'blocked').length;
}

/** A nota do pagador no mês: a da Notaas (que sai sozinha), a registrada à mão ou a que falta. */
function NoteCell({ line }: { line: ClosingLine }) {
  const auto = line.autoNotes ?? [];
  const issued = auto.filter((n) => n.status === 'issued');
  const refused = auto.filter((n) => n.status === 'error').length;
  const pending = autoPending(line);
  const upcoming = line.upcoming ?? 0;
  const parts: ReactNode[] = [];
  if (issued.length > 0) {
    parts.push(
      <div key="issued">
        {issued.length === 1 ? 'Nº ' : 'Nºs '}
        {issued.map((n) => n.number).join(', ')} · Notaas
        <div className={billing.muted}>{formatMoney(issued.reduce((sum, n) => sum + n.amountCents, 0))}</div>
      </div>,
    );
  }
  if (pending > 0) {
    parts.push(
      <div key="pending" className={billing.muted}>
        {pending === 1 ? '1 nota saindo pela Notaas' : `${pending} notas saindo pela Notaas`}
      </div>,
    );
  }
  if (upcoming > 0) {
    parts.push(
      <div key="upcoming" className={billing.muted}>
        {upcoming === 1 ? '1 fatura ganha a nota da Notaas ao ser paga' : `${upcoming} faturas ganham a nota da Notaas ao serem pagas`}
      </div>,
    );
  }
  if (refused > 0) {
    parts.push(
      <div key="refused">
        <Badge tone="danger" dot>
          {refused === 1 ? 'Nota da Notaas recusada' : `${refused} notas da Notaas recusadas`}
        </Badge>
        <div className={billing.muted}>Corrija e emita de novo na ficha</div>
      </div>,
    );
  }
  if (line.note) {
    parts.push(
      <div key="note">
        Nº {line.note.number} · {formatDateOnly(line.note.issuedOn)}
        <div className={billing.muted}>
          {formatMoney(line.note.amountCents)}
          {auto.length === 0 && line.note.amountCents !== line.amountCents && ' (diferente do faturado)'}
        </div>
      </div>,
    );
  } else if (line.needsNote !== false) {
    parts.push(
      <Badge key="missing" tone="warning" dot>
        Por emitir
      </Badge>,
    );
  }
  if (parts.length === 0) return <span className={billing.muted}>Sem nota de serviço (frete ou avulsa)</span>;
  return <div>{parts}</div>;
}

/**
 * O fechamento do mês (competência): o que cada cliente e cada associação ou
 * seguradora teve faturado no mês — as faturas que vencem nele, pagas ou não
 * — e a nota fiscal de cada um. A NFS-e de cada fatura paga sai sozinha pela
 * Notaas (a da contratação também) e só aparece aqui; a que a Notaas não
 * emite (paga antes de ela começar) é emitida no emissor da empresa, e aqui
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
            <span className={billing.tileLabel}>Notas por emitir</span>
            <span className={billing.tileValue}>{m.missingNotes}</span>
            <span className={billing.tileHint}>
              {m.autoNotes ?? 0} emitidas pela Notaas · {m.notes} registradas aqui
            </span>
          </div>
        </div>
      )}

      <Card
        title="Fechamento por pagador"
        subtitle="Competência: as faturas que vencem no mês, pagas ou não. A nota da Notaas sai sozinha quando a fatura é paga, a da contratação também. Registre aqui só a que estiver Por emitir: emita no emissor e guarde o número e o arquivo."
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
                      <NoteCell line={line} />
                    </td>
                    <td>
                      <div className={pageStyles.actions}>
                        {line.note?.hasFile && (
                          <Button size="small" variant="ghost" onClick={() => void closingApi.downloadNote(line.note!.id)}>
                            Baixar
                          </Button>
                        )}
                        {(line.note || line.needsNote !== false) && (
                          <Button size="small" variant={line.note ? 'ghost' : 'primary'} onClick={() => setEditing(line)}>
                            {line.note ? 'Trocar' : 'Registrar nota'}
                          </Button>
                        )}
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

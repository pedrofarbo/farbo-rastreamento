import { useRef, useState } from 'react';
import { useMutation } from '@tanstack/react-query';

import { ApiError } from '@/api/client';
import { orgApi } from '@/api/resources';
import own from '@/components/billing/FleetImportModal.module.css';
import { readSheet } from '@/components/billing/FleetImportModal';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { MAX_ASSOCIATE_ROWS, associateTemplateCsv, parseAssociateSheet } from '@/services/associateImport';
import type { ParsedAssociates } from '@/services/associateImport';
import type { SheetProblem } from '@/services/fleetImport';

import pageStyles from '@/pages/Page.module.css';

/** Quantos problemas aparecem de uma vez. */
const SHOWN_PROBLEMS = 40;

/** Os problemas do servidor vêm pela posição da linha; a tela mostra a linha da planilha. */
export function serverProblems(error: unknown, sheet: ParsedAssociates): SheetProblem[] | null {
  if (!(error instanceof ApiError) || error.status !== 422) return null;
  const body = error.body as { problems?: { row: number; message: string }[] } | undefined;
  if (!body?.problems) return null;
  return body.problems.map((p) => ({ line: sheet.rows[p.row]?.line ?? 0, message: p.message }));
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/**
 * Os associados por planilha (CSV do Excel): uma linha por veículo, as
 * linhas com o mesmo e-mail são o mesmo associado. O servidor confere tudo
 * antes (e-mails e placas que já existem, CPF); com um problema, nada entra.
 * Cada associado recebe o convite do app e cada veículo entra na fila de
 * instalação.
 */
export function AssociateImportModal({
  open,
  onClose,
  onDone,
  importRows = orgApi.importAssociates,
}: {
  open: boolean;
  onClose: () => void;
  onDone: (result: { associates: number; vehicles: number }) => void;
  /** Para onde vai a planilha: a associação (o padrão) ou a central, dentro de uma organização. */
  importRows?: typeof orgApi.importAssociates;
}) {
  const fileRef = useRef<HTMLInputElement>(null);
  const [fileName, setFileName] = useState('');
  const [sheet, setSheet] = useState<ParsedAssociates | null>(null);
  const [checked, setChecked] = useState<{ associates: number; vehicles: number } | null>(null);
  const [problems, setProblems] = useState<SheetProblem[]>([]);
  const [error, setError] = useState('');

  const reset = () => {
    setFileName('');
    setSheet(null);
    setChecked(null);
    setProblems([]);
    setError('');
    if (fileRef.current) fileRef.current.value = '';
  };
  const close = () => {
    reset();
    onClose();
  };

  const conferir = useMutation({
    mutationFn: (parsed: ParsedAssociates) => importRows(parsed.rows, true),
    onSuccess: (result) => {
      setChecked(result);
      setProblems([]);
    },
    onError: (err, parsed) => {
      const list = serverProblems(err, parsed);
      if (list) setProblems(list);
      else setError(err instanceof Error ? err.message : 'Não deu para conferir a planilha.');
    },
  });

  const importar = useMutation({
    mutationFn: () => importRows(sheet?.rows ?? [], false),
    onSuccess: (result) => {
      reset();
      onDone(result);
    },
    onError: (err) => {
      const list = sheet ? serverProblems(err, sheet) : null;
      setChecked(null);
      if (list) setProblems(list);
      else setError(err instanceof Error ? err.message : 'Não deu para importar.');
    },
  });

  const choose = async (file: File | undefined) => {
    if (!file) return;
    setError('');
    setChecked(null);
    setFileName(file.name);
    const parsed = parseAssociateSheet(await readSheet(file));
    setSheet(parsed);
    setProblems(parsed.problems);
    if (parsed.problems.length === 0) conferir.mutate(parsed);
  };

  const download = () => {
    const blob = new Blob([associateTemplateCsv()], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = 'modelo-associados.csv';
    link.click();
    URL.revokeObjectURL(url);
  };

  const summary = checked
    ? `${plural(checked.associates, 'associado', 'associados')} e ${plural(checked.vehicles, 'veículo', 'veículos')}`
    : '';

  return (
    <Modal
      open={open}
      wide
      title="Importar associados por planilha"
      onClose={close}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={importar.isPending}>
            Cancelar
          </Button>
          <Button variant="primary" disabled={!checked} loading={importar.isPending} onClick={() => importar.mutate()}>
            {checked ? `Cadastrar ${summary}` : 'Cadastrar'}
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        {error && <div className={pageStyles.note}>{error}</div>}
        <p className={pageStyles.description}>
          Uma linha por veículo, até {MAX_ASSOCIATE_ROWS} por vez; quem tem mais de um veículo aparece em mais de uma
          linha, com o mesmo e-mail. Salve a planilha como CSV (no Excel: Salvar como → CSV). <strong>Nome</strong> e{' '}
          <strong>E-mail</strong> são obrigatórios:
        </p>
        <ul className={own.columns}>
          <li>
            <strong>Telefone</strong> e <strong>CPF</strong> do associado.
          </li>
          <li>
            <strong>Veículo</strong> (o nome, como "Moto do João") e <strong>Placa</strong>, <strong>Tipo</strong> (carro ou
            moto), <strong>Marca</strong>, <strong>Modelo</strong>, <strong>Ano</strong>, <strong>Cor</strong>. Sem
            veículo, só o associado entra.
          </li>
        </ul>
        <div className={own.pick}>
          <Button variant="secondary" onClick={download}>
            Baixar modelo
          </Button>
          <label className={own.file}>
            <input
              ref={fileRef}
              type="file"
              accept=".csv,.txt,text/csv,text/plain"
              onChange={(e) => {
                const file = e.target.files?.[0];
                // Limpa: a mesma planilha, corrigida, pode ser escolhida de novo.
                e.target.value = '';
                void choose(file);
              }}
            />
            <span>{fileName || 'Escolher a planilha (CSV)'}</span>
          </label>
          {conferir.isPending && <Spinner label="Conferindo a planilha" />}
        </div>
        {sheet && sheet.ignored.length > 0 && problems.length === 0 && (
          <p className={own.hint}>Colunas ignoradas: {sheet.ignored.join(', ')}.</p>
        )}
        {problems.length > 0 && (
          <div className={own.problems} role="alert">
            <strong>
              {problems.length === 1 ? 'Um problema' : `${problems.length} problemas`} na planilha. Corrija e escolha o
              arquivo de novo — nada foi cadastrado.
            </strong>
            <ul>
              {problems.slice(0, SHOWN_PROBLEMS).map((p, i) => (
                <li key={i}>
                  {p.line > 0 && <span className={own.line}>Linha {p.line}</span>}
                  {p.message}
                </li>
              ))}
            </ul>
            {problems.length > SHOWN_PROBLEMS && <p>e mais {problems.length - SHOWN_PROBLEMS}.</p>}
          </div>
        )}
        {checked && (
          <div className={pageStyles.note} role="status">
            Planilha conferida: {summary}. Cada associado recebe por e-mail o convite para criar a senha do app, e cada
            veículo entra na fila de instalação.
          </div>
        )}
      </div>
    </Modal>
  );
}

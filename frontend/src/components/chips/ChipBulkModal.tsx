import { useId, useMemo, useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { chipsApi } from '@/api/resources';
import type { ChipBulkResult } from '@/api/resources';
import styles from '@/components/devices/DeviceBulkModal.module.css';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { useToast } from '@/components/ui/Toast';
import { EMPTY_CHIP_DEFAULTS, parseChipBatch, templateChipCsv } from '@/services/chipImport';
import type { ChipDefaults } from '@/services/chipImport';
import { formatICCID } from '@/services/format';

import own from './ChipBulkModal.module.css';

/** Lê o arquivo em UTF-8 ou Windows-1252 (Excel em português). */
async function readSheetFile(file: File): Promise<string> {
  const bytes = await file.arrayBuffer();
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  } catch {
    return new TextDecoder('windows-1252').decode(bytes);
  }
}

const MAX_ROWS_SHOWN = 50;
const plural = (n: number) => `${n} chip${n === 1 ? '' : 's'}`;

/**
 * A importação de um lote de chips: a planilha (ou os ICCIDs colados) com a
 * prévia de cada linha; a operadora, o APN e o lote valem para todos os que
 * deixarem em branco.
 */
export function ChipBulkModal({ open, onClose, existingIccids }: { open: boolean; onClose: () => void; existingIccids: string[] }) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const fileInput = useRef<HTMLInputElement>(null);
  const onlyValidId = useId();
  const [tab, setTab] = useState<'paste' | 'file'>('paste');
  const [pasteText, setPasteText] = useState('');
  const [file, setFile] = useState<{ name: string; text: string } | null>(null);
  const [defaults, setDefaults] = useState<ChipDefaults>(EMPTY_CHIP_DEFAULTS);
  const [onlyValid, setOnlyValid] = useState(true);
  const [serverErrors, setServerErrors] = useState<ChipBulkResult['errors']>([]);

  const text = tab === 'file' ? (file?.text ?? '') : pasteText;
  const parsed = useMemo(() => parseChipBatch(text, existingIccids, defaults), [text, existingIccids, defaults]);
  const toSend = onlyValid ? parsed.rows.filter((r) => r.status === 'valid') : parsed.rows;

  const close = () => {
    setPasteText('');
    setFile(null);
    setDefaults(EMPTY_CHIP_DEFAULTS);
    setServerErrors([]);
    if (fileInput.current) fileInput.current.value = '';
    onClose();
  };
  const save = useMutation({
    mutationFn: () => chipsApi.createBulk(toSend.map((r) => r.input)),
    onSuccess: (res) => {
      void queryClient.invalidateQueries({ queryKey: ['chips'] });
      if (res.errors.length === 0) {
        notify({ tone: 'success', title: `${plural(res.created)} no estoque` });
        close();
        return;
      }
      // Os recusados ficam na tela, com a linha da planilha.
      setServerErrors(res.errors.map((e) => ({ ...e, index: toSend[e.index]?.lineNumber ?? e.index + 1 })));
      notify({
        tone: res.created > 0 ? 'warning' : 'error',
        title: res.created > 0 ? `${plural(res.created)} no estoque` : 'Nenhum chip entrou',
        description: `${res.errors.length} recusado(s): veja o motivo abaixo.`,
      });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para importar', description: err.message }),
  });

  const downloadTemplate = () => {
    const url = URL.createObjectURL(new Blob([templateChipCsv()], { type: 'text/csv;charset=utf-8;' }));
    const link = document.createElement('a');
    link.href = url;
    link.download = 'modelo-chips.csv';
    link.click();
    URL.revokeObjectURL(url);
  };
  const pick = async (f: File | null) => {
    if (!f) return;
    try {
      setFile({ name: f.name, text: await readSheetFile(f) });
      setServerErrors([]);
    } catch {
      notify({ tone: 'error', title: 'Não deu para ler o arquivo' });
    }
  };
  const setDefault = (key: keyof ChipDefaults) => (value: string) => setDefaults((d) => ({ ...d, [key]: value }));

  return (
    <Modal
      open={open}
      wide
      title="Importar chips em massa"
      onClose={close}
      footer={
        <>
          <Button variant="ghost" onClick={close}>
            Cancelar
          </Button>
          <Button variant="primary" disabled={toSend.length === 0} loading={save.isPending} onClick={() => save.mutate()}>
            Importar {plural(toSend.length)}
          </Button>
        </>
      }
    >
      <div className={styles.container}>
        <p className={styles.intro}>
          Cole os ICCIDs (um por linha) ou envie a planilha do lote. O que a linha deixar em branco vem de "Para todos os
          chips", abaixo.
        </p>

        <div className={styles.topActions}>
          <div className={styles.tabs} role="tablist">
            {(
              [
                ['paste', 'Colar ICCIDs'],
                ['file', 'Planilha (CSV)'],
              ] as const
            ).map(([key, label]) => (
              <button
                key={key}
                type="button"
                role="tab"
                aria-selected={tab === key}
                className={`${styles.tab} ${tab === key ? styles.tabActive : ''}`}
                onClick={() => {
                  setTab(key);
                  setServerErrors([]);
                }}
              >
                {label}
              </button>
            ))}
          </div>
          <button type="button" className={styles.templateBtn} onClick={downloadTemplate}>
            <span aria-hidden="true">📥</span> Baixar modelo CSV
          </button>
        </div>

        {tab === 'paste' ? (
          <div className={styles.textareaWrap}>
            <textarea
              className={styles.textarea}
              aria-label="ICCIDs do lote"
              placeholder={`Um ICCID por linha:
89553202100093795330
8955 3202 1000 9379 5348 SP

Ou com colunas (ICCID;Linha;Operadora;APN;Usuário;Senha;Observações):
89553202100093795330;(34) 99999-0001;Algar;ss.algar.br;algar;algar;Lote 12`}
              value={pasteText}
              onChange={(e) => {
                setPasteText(e.target.value);
                setServerErrors([]);
              }}
            />
            <span className={styles.textareaHint}>Dá para copiar a coluna da planilha da operadora e colar aqui.</span>
          </div>
        ) : file ? (
          <div className={styles.selectedFileInfo}>
            <span className={styles.fileName}>{file.name}</span>
            <Button
              size="small"
              variant="ghost"
              onClick={() => {
                setFile(null);
                setServerErrors([]);
                if (fileInput.current) fileInput.current.value = '';
              }}
            >
              Trocar arquivo
            </Button>
          </div>
        ) : (
          <div className={styles.dropzone}>
            <input ref={fileInput} type="file" accept=".csv,.txt,.tsv" aria-label="Planilha do lote" onChange={(e) => pick(e.target.files?.[0] ?? null)} />
            <span className={styles.dropzoneIcon} aria-hidden="true">
              📄
            </span>
            <span className={styles.dropzoneTitle}>Clique para escolher a planilha</span>
            <span className={styles.dropzoneHint}>.csv, .txt ou .tsv, separados por ponto e vírgula, vírgula ou tabulação</span>
          </div>
        )}

        <fieldset className={own.defaults}>
          <legend>Para todos os chips (quando a linha deixar em branco)</legend>
          <div className={own.defaultsGrid}>
            <TextField label="Operadora" list="chip-bulk-carriers" value={defaults.carrier} onChange={(e) => setDefault('carrier')(e.target.value)} />
            <datalist id="chip-bulk-carriers">
              {['Algar', 'Vivo', 'Claro', 'TIM'].map((c) => (
                <option key={c} value={c} />
              ))}
            </datalist>
            <TextField label="APN" value={defaults.apn} onChange={(e) => setDefault('apn')(e.target.value)} />
            <TextField label="Usuário APN" value={defaults.apnUser} onChange={(e) => setDefault('apnUser')(e.target.value)} />
            <TextField
              label="Senha APN"
              type="password"
              autoComplete="new-password"
              value={defaults.apnPassword}
              onChange={(e) => setDefault('apnPassword')(e.target.value)}
            />
            <TextField
              label="Observações"
              placeholder="Lote, nota fiscal..."
              value={defaults.notes}
              onChange={(e) => setDefault('notes')(e.target.value)}
            />
          </div>
          <Button
            type="button"
            size="small"
            onClick={() => setDefaults((d) => ({ ...d, carrier: 'Algar', apn: 'ss.algar.br', apnUser: 'algar', apnPassword: 'algar' }))}
          >
            Chip Algar (ss.algar.br)
          </Button>
        </fieldset>

        {serverErrors.length > 0 && (
          <div className={styles.serverErrors} role="alert">
            <div className={styles.serverErrorsTitle}>Recusados pelo servidor:</div>
            <ul className={styles.serverErrorsList}>
              {serverErrors.map((e) => (
                <li key={`${e.index}-${e.iccid}`}>
                  <strong>Linha {e.index}</strong> ({formatICCID(e.iccid) || 'sem ICCID'}): {e.error}
                </li>
              ))}
            </ul>
          </div>
        )}

        {parsed.rows.length > 0 && (
          <>
            <div className={styles.statsBar}>
              <div className={styles.statGroup}>
                <span className={styles.statPill}>Total: {parsed.rows.length}</span>
                <span className={`${styles.statPill} ${styles.statValid}`}>Prontos: {parsed.validCount}</span>
                {parsed.duplicateCount > 0 && (
                  <span className={`${styles.statPill} ${styles.statWarning}`}>Repetidos: {parsed.duplicateCount}</span>
                )}
                {parsed.errorCount > 0 && <span className={`${styles.statPill} ${styles.statError}`}>Erros: {parsed.errorCount}</span>}
              </div>
              {(parsed.duplicateCount > 0 || parsed.errorCount > 0) && (
                <label className={styles.filterOption} htmlFor={onlyValidId}>
                  <input id={onlyValidId} type="checkbox" checked={onlyValid} onChange={(e) => setOnlyValid(e.target.checked)} />
                  <span>Importar só os prontos ({parsed.validCount})</span>
                </label>
              )}
            </div>
            <div className={styles.tableWrap}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>Linha</th>
                    <th>Situação</th>
                    <th>ICCID</th>
                    <th>Número</th>
                    <th>Operadora e APN</th>
                    <th>Observação</th>
                  </tr>
                </thead>
                <tbody>
                  {parsed.rows.slice(0, MAX_ROWS_SHOWN).map((row) => (
                    <tr key={row.lineNumber} className={onlyValid && row.status !== 'valid' ? styles.rowDimmed : ''}>
                      <td>{row.lineNumber}</td>
                      <td>
                        {row.status === 'valid' ? (
                          <Badge tone="success" dot>
                            Pronto
                          </Badge>
                        ) : row.status === 'duplicate' ? (
                          <Badge tone="warning" dot>
                            Repetido
                          </Badge>
                        ) : (
                          <Badge tone="danger" dot>
                            Inválido
                          </Badge>
                        )}
                      </td>
                      <td className={`${styles.mono} ${own.iccid}`}>{row.iccid ? formatICCID(row.iccid) : '—'}</td>
                      <td className={styles.mono}>{row.phoneNumber || '—'}</td>
                      <td>{[row.carrier, row.apn].filter(Boolean).join(' · ') || '—'}</td>
                      <td className={row.status === 'error' ? styles.errorCell : row.status === 'duplicate' ? styles.duplicateCell : ''}>
                        {row.message || 'Pronto para importar'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {parsed.rows.length > MAX_ROWS_SHOWN && (
              <span className={styles.textareaHint}>
                Mostrando as primeiras {MAX_ROWS_SHOWN} linhas de {parsed.rows.length}.
              </span>
            )}
          </>
        )}
      </div>
    </Modal>
  );
}

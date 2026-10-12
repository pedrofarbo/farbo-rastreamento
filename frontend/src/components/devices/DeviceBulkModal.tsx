import { useId, useMemo, useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { devicesApi } from '@/api/resources';
import type { DeviceBulkError, DeviceBulkResult } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { useToast } from '@/components/ui/Toast';
import { parseTrackerBatch, templateTrackerCsv } from '@/services/deviceBulk';
import type { Device } from '@/types';

import styles from './DeviceBulkModal.module.css';

interface DeviceBulkModalProps {
  open: boolean;
  onClose: () => void;
  onDone?: (count: number) => void;
  existingDevices?: Device[];
}

/** Lê o arquivo em UTF-8 ou Windows-1252 (Excel em português). */
async function readSheetFile(file: File): Promise<string> {
  const bytes = await file.arrayBuffer();
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  } catch {
    return new TextDecoder('windows-1252').decode(bytes);
  }
}

export function DeviceBulkModal({
  open,
  onClose,
  onDone,
  existingDevices = [],
}: DeviceBulkModalProps) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const onlyValidId = useId();

  const [tab, setTab] = useState<'file' | 'paste'>('file');
  const [fileText, setFileText] = useState('');
  const [fileName, setFileName] = useState('');
  const [fileSize, setFileSize] = useState('');
  const [pasteText, setPasteText] = useState('');
  const [isDragging, setIsDragging] = useState(false);
  const [onlyValid, setOnlyValid] = useState(true);
  const [serverErrors, setServerErrors] = useState<DeviceBulkError[]>([]);

  const rawText = tab === 'file' ? fileText : pasteText;

  const parsed = useMemo(() => {
    return parseTrackerBatch(rawText, existingDevices);
  }, [rawText, existingDevices]);

  const rowsToSubmit = useMemo(() => {
    if (onlyValid) {
      return parsed.rows.filter((r) => r.status === 'valid');
    }
    return parsed.rows;
  }, [parsed.rows, onlyValid]);

  const handleDownloadTemplate = () => {
    const csv = templateTrackerCsv();
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.setAttribute('download', 'modelo-rastreadores.csv');
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  };

  const handleFileChange = async (file: File | null) => {
    if (!file) return;
    try {
      const text = await readSheetFile(file);
      setFileName(file.name);
      setFileSize(`${(file.size / 1024).toFixed(1)} KB`);
      setFileText(text);
      setServerErrors([]);
    } catch {
      notify({ tone: 'error', title: 'Erro ao ler arquivo', description: 'Não foi possível ler o arquivo selecionado.' });
    }
  };

  const saveBulk = useMutation({
    mutationFn: async () => {
      const payload = rowsToSubmit.map((r) => r.input);
      return devicesApi.createBulk(payload);
    },
    onSuccess: (result: DeviceBulkResult) => {
      queryClient.invalidateQueries({ queryKey: ['devices'] });
      if (result.errors.length > 0) {
        setServerErrors(result.errors);
        if (result.created > 0) {
          notify({
            tone: 'warning',
            title: `${result.created} rastreador${result.created > 1 ? 'es' : ''} cadastrado${result.created > 1 ? 's' : ''}`,
            description: `${result.errors.length} item(ns) apresentaram erros (veja os detalhes abaixo).`,
          });
          onDone?.(result.created);
        } else {
          notify({
            tone: 'error',
            title: 'Nenhum rastreador cadastrado',
            description: 'Verifique os erros reportados abaixo.',
          });
        }
      } else {
        notify({
          tone: 'success',
          title: `${result.created} rastreador${result.created > 1 ? 'es' : ''} cadastrado${result.created > 1 ? 's' : ''} com sucesso`,
        });
        handleClose();
        onDone?.(result.created);
      }
    },
    onError: (err: Error) => {
      notify({ tone: 'error', title: 'Falha no cadastro em massa', description: err.message });
    },
  });

  const handleClose = () => {
    setFileText('');
    setFileName('');
    setFileSize('');
    setPasteText('');
    setServerErrors([]);
    if (fileInputRef.current) fileInputRef.current.value = '';
    onClose();
  };

  const maxDisplayRows = 50;
  const displayedRows = parsed.rows.slice(0, maxDisplayRows);

  return (
    <Modal
      open={open}
      wide
      title="Adicionar rastreadores em massa"
      onClose={handleClose}
      footer={
        <>
          <Button variant="ghost" onClick={handleClose}>
            Cancelar
          </Button>
          <Button
            variant="primary"
            disabled={rowsToSubmit.length === 0}
            loading={saveBulk.isPending}
            onClick={() => saveBulk.mutate()}
          >
            Cadastrar {rowsToSubmit.length} rastreador{rowsToSubmit.length !== 1 ? 'es' : ''}
          </Button>
        </>
      }
    >
      <div className={styles.container}>
        <p className={styles.intro}>
          Cadastre vários rastreadores no sistema enviando um arquivo CSV/planilha ou colando os dados diretamente.
          O protocolo é opcional e detectado automaticamente no primeiro pacote.
        </p>

        <div className={styles.topActions}>
          <div className={styles.tabs} role="tablist">
            <button
              type="button"
              role="tab"
              aria-selected={tab === 'file'}
              className={`${styles.tab} ${tab === 'file' ? styles.tabActive : ''}`}
              onClick={() => {
                setTab('file');
                setServerErrors([]);
              }}
            >
              Planilha (CSV)
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={tab === 'paste'}
              className={`${styles.tab} ${tab === 'paste' ? styles.tabActive : ''}`}
              onClick={() => {
                setTab('paste');
                setServerErrors([]);
              }}
            >
              Colar dados
            </button>
          </div>

          <button
            type="button"
            className={styles.templateBtn}
            onClick={handleDownloadTemplate}
            title="Baixar modelo de planilha CSV para preenchimento"
          >
            <span>📥</span> Baixar modelo CSV
          </button>
        </div>

        {tab === 'file' ? (
          <div>
            {!fileName ? (
              <div
                className={`${styles.dropzone} ${isDragging ? styles.dropzoneActive : ''}`}
                onDragOver={(e) => {
                  e.preventDefault();
                  setIsDragging(true);
                }}
                onDragLeave={() => setIsDragging(false)}
                onDrop={(e) => {
                  e.preventDefault();
                  setIsDragging(false);
                  if (e.dataTransfer.files?.[0]) {
                    handleFileChange(e.dataTransfer.files[0]);
                  }
                }}
              >
                <input
                  ref={fileInputRef}
                  type="file"
                  accept=".csv,.txt,.tsv"
                  onChange={(e) => handleFileChange(e.target.files?.[0] ?? null)}
                />
                <span className={styles.dropzoneIcon} aria-hidden="true">
                  📄
                </span>
                <span className={styles.dropzoneTitle}>
                  Clique para selecionar ou arraste o arquivo aqui
                </span>
                <span className={styles.dropzoneHint}>
                  Formatos aceitos: .csv, .txt ou .tsv (separados por ponto e vírgula, vírgula ou tabulação)
                </span>
              </div>
            ) : (
              <div className={styles.selectedFileInfo}>
                <div>
                  <span className={styles.fileName}>{fileName}</span>
                  <span className={styles.fileMeta}>({fileSize})</span>
                </div>
                <Button
                  size="small"
                  variant="ghost"
                  onClick={() => {
                    setFileName('');
                    setFileSize('');
                    setFileText('');
                    setServerErrors([]);
                    if (fileInputRef.current) fileInputRef.current.value = '';
                  }}
                >
                  Trocar arquivo
                </Button>
              </div>
            )}
          </div>
        ) : (
          <div className={styles.textareaWrap}>
            <textarea
              className={styles.textarea}
              placeholder={`Cole os dados dos rastreadores aqui.

Exemplos aceitos:
- Um IMEI por linha:
869247061230001
869247061230002

- Ou com colunas (IMEI;Linha;Modelo;ICCID;Protocolo):
869247061230001;(34) 99999-0001;TK910;89553202100093795330;gt06
869247061230002;(34) 99999-0002;TK910;;`}
              value={pasteText}
              onChange={(e) => {
                setPasteText(e.target.value);
                setServerErrors([]);
              }}
            />
            <span className={styles.textareaHint}>
              Dica: você pode copiar colunas de uma tabela do Excel ou Google Sheets e colar diretamente.
            </span>
          </div>
        )}

        {serverErrors.length > 0 && (
          <div className={styles.serverErrors}>
            <div className={styles.serverErrorsTitle}>Problemas no processamento do servidor:</div>
            <ul className={styles.serverErrorsList}>
              {serverErrors.map((err, i) => (
                <li key={`${err.imei}-${i}`}>
                  <strong>{err.imei || `Linha ${err.index + 1}`}:</strong> {err.error}
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
                <span className={`${styles.statPill} ${styles.statValid}`}>
                  Prontos: {parsed.validCount}
                </span>
                {parsed.duplicateCount > 0 && (
                  <span className={`${styles.statPill} ${styles.statWarning}`}>
                    Repetidos: {parsed.duplicateCount}
                  </span>
                )}
                {parsed.errorCount > 0 && (
                  <span className={`${styles.statPill} ${styles.statError}`}>
                    Erros: {parsed.errorCount}
                  </span>
                )}
              </div>

              {(parsed.duplicateCount > 0 || parsed.errorCount > 0) && (
                <label className={styles.filterOption} htmlFor={onlyValidId}>
                  <input
                    id={onlyValidId}
                    type="checkbox"
                    checked={onlyValid}
                    onChange={(e) => setOnlyValid(e.target.checked)}
                  />
                  <span>Cadastrar apenas os válidos ({parsed.validCount})</span>
                </label>
              )}
            </div>

            <div className={styles.tableWrap}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>Linha</th>
                    <th>Situação</th>
                    <th>IMEI</th>
                    <th>Linha do chip</th>
                    <th>Modelo</th>
                    <th>ICCID</th>
                    <th>Protocolo</th>
                    <th>Observação</th>
                  </tr>
                </thead>
                <tbody>
                  {displayedRows.map((row) => {
                    const isSkipped = onlyValid && row.status !== 'valid';
                    return (
                      <tr key={row.lineNumber} className={isSkipped ? styles.rowDimmed : ''}>
                        <td>{row.lineNumber}</td>
                        <td>
                          {row.status === 'valid' && (
                            <Badge tone="success" dot>
                              Pronto
                            </Badge>
                          )}
                          {row.status === 'duplicate' && (
                            <Badge tone="warning" dot>
                              Repetido
                            </Badge>
                          )}
                          {row.status === 'error' && (
                            <Badge tone="danger" dot>
                              Inválido
                            </Badge>
                          )}
                        </td>
                        <td className={styles.mono}>{row.imei || '—'}</td>
                        <td className={styles.mono}>{row.phoneNumber || '—'}</td>
                        <td>{row.model || '—'}</td>
                        <td className={styles.mono}>{row.iccid || '—'}</td>
                        <td className={styles.mono}>{row.protocol || 'automático'}</td>
                        <td
                          className={
                            row.status === 'error'
                              ? styles.errorCell
                              : row.status === 'duplicate'
                                ? styles.duplicateCell
                                : ''
                          }
                        >
                          {row.message || 'Pronto para cadastrar'}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>

            {parsed.rows.length > maxDisplayRows && (
              <span className={styles.textareaHint}>
                Mostrando as primeiras {maxDisplayRows} linhas de {parsed.rows.length} no total.
              </span>
            )}
          </>
        )}
      </div>
    </Modal>
  );
}

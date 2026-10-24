import { useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';

import { ApiError } from '@/api/client';
import { catalogApi, customersApi } from '@/api/resources';
import type { FleetImportRequest } from '@/api/resources';
import { nextChargeDate, suggestedPlan } from '@/components/billing/NewVehicleWizard';
import type { WizardAdmin } from '@/components/billing/NewVehicleWizard';
import { SubscriptionFields, subscriptionFromDraft } from '@/components/billing/SubscriptionFields';
import type { SubscriptionDraft } from '@/components/billing/SubscriptionFields';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { MAX_IMPORT_ROWS, parseFleetSheet, templateCsv } from '@/services/fleetImport';
import type { ParsedSheet, SheetProblem } from '@/services/fleetImport';
import { centsToInput, formatDateOnly, formatMoney, parseMoney, todayISO } from '@/services/format';
import type { FleetImportResult, FleetSettings, ImportProblem } from '@/types';

import pageStyles from '@/pages/Page.module.css';
import styles from './Billing.module.css';
import own from './FleetImportModal.module.css';

const STEPS = ['Planilha', 'Conferência', 'Cobrança'] as const;

/** Quantas linhas da planilha a conferência mostra (o resto só conta). */
const PREVIEW_ROWS = 8;
/** Quantos problemas aparecem de uma vez. */
const SHOWN_PROBLEMS = 40;

/** O texto do arquivo: UTF-8 (o "CSV UTF-8" do Excel) ou, se não for, Windows-1252 (o CSV comum do Excel em português). */
export async function readSheet(file: File): Promise<string> {
  const bytes = await file.arrayBuffer();
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  } catch {
    return new TextDecoder('windows-1252').decode(bytes);
  }
}

function problemsOf(error: unknown): ImportProblem[] | null {
  if (!(error instanceof ApiError) || error.status !== 422) return null;
  const body = error.body as { problems?: ImportProblem[] } | undefined;
  return body?.problems ?? null;
}

/**
 * A frota por planilha, pela central: o arquivo (CSV do Excel), a
 * conferência no servidor (placas, rastreadores, grupos) sem gravar nada e,
 * por fim, o plano e o equipamento de todos — uma assinatura por veículo, no
 * vencimento da conta, e uma fatura só para os aparelhos.
 */
export function FleetImportModal({
  open,
  admin,
  fleet,
  onClose,
  onDone,
}: {
  open: boolean;
  admin: WizardAdmin;
  fleet?: FleetSettings;
  onClose: () => void;
  onDone: (result: FleetImportResult) => void;
}) {
  const catalog = useQuery({ queryKey: ['catalog'], queryFn: catalogApi.get, enabled: open });
  const fileRef = useRef<HTMLInputElement>(null);
  const [step, setStep] = useState(0);
  const [fileName, setFileName] = useState('');
  const [sheet, setSheet] = useState<ParsedSheet | null>(null);
  const [check, setCheck] = useState<FleetImportResult | null>(null);
  const [problems, setProblems] = useState<SheetProblem[]>([]);
  const [equipment, setEquipment] = useState<string | null>(null);
  const [plan, setPlan] = useState<SubscriptionDraft | null>(null);
  const [error, setError] = useState('');

  const c = catalog.data;
  const planDraft =
    plan ?? (c ? suggestedPlan(c, admin.currentPlan, admin.accountPlan ?? null, admin.accountDueDay ?? null) : null);
  // No comodato (o plano do cliente), não há equipamento a cobrar.
  const loan = admin.accountPlan?.equipmentMode === 'loan';
  const equipmentText = loan ? '0' : (equipment ?? (c ? centsToInput(c.equipmentPriceCents) : ''));
  const equipmentCents = equipmentText.trim() === '' ? 0 : parseMoney(equipmentText);
  // Tudo entra na próxima fatura do mês da conta.
  const chargeDate = nextChargeDate(planDraft?.dueDay ?? c?.defaultDueDay ?? 10, todayISO());

  /** O pedido com o plano e o equipamento da tela (a conferência usa os mesmos). */
  const request = (dryRun: boolean): FleetImportRequest => {
    const subscription = planDraft ? subscriptionFromDraft(planDraft) : 'Carregando o plano.';
    if (typeof subscription === 'string') throw new Error(subscription);
    if (equipmentCents === null) throw new Error('Valor do equipamento inválido. Use, por exemplo, 150,00 (ou 0 para não cobrar).');
    return { rows: sheet?.rows ?? [], equipmentCents, plan: subscription, dryRun };
  };

  // O problema do servidor vem pela posição do veículo; a tela mostra a linha da planilha.
  const fromServer = (list: ImportProblem[], lines: number[]): SheetProblem[] =>
    list.map((p) => ({ line: p.line > 0 ? (lines[p.line - 1] ?? p.line) : 0, message: p.message }));

  const conferir = useMutation({
    mutationFn: (parsed: ParsedSheet) =>
      customersApi.importFleet(admin.customerId, { ...request(true), rows: parsed.rows }),
    onSuccess: (result) => {
      setCheck(result);
      setProblems([]);
      setStep(1);
    },
    onError: (err, parsed) => {
      const list = problemsOf(err);
      if (list) {
        setCheck(null);
        setProblems(fromServer(list, parsed.lines));
        setStep(1);
      } else {
        setError(err instanceof Error ? err.message : 'Não deu para conferir a planilha.');
      }
    },
  });

  const importar = useMutation({
    mutationFn: () => customersApi.importFleet(admin.customerId, request(false)),
    onSuccess: (result) => {
      reset();
      onDone(result);
    },
    onError: (err) => {
      const list = problemsOf(err);
      if (list) {
        // Alguém usou uma placa ou um rastreador no meio do caminho.
        setProblems(fromServer(list, sheet?.lines ?? []));
        setCheck(null);
        setStep(1);
      } else {
        setError(err instanceof Error ? err.message : 'Não deu para importar.');
      }
    },
  });

  const reset = () => {
    setStep(0);
    setFileName('');
    setSheet(null);
    setCheck(null);
    setProblems([]);
    setEquipment(null);
    setPlan(null);
    setError('');
    if (fileRef.current) fileRef.current.value = '';
  };

  const close = () => {
    reset();
    onClose();
  };

  const choose = async (file: File | undefined) => {
    if (!file) return;
    setError('');
    setCheck(null);
    setFileName(file.name);
    const parsed = parseFleetSheet(await readSheet(file), fleet);
    setSheet(parsed);
    setProblems(parsed.problems);
    if (parsed.problems.length === 0) conferir.mutate(parsed);
  };

  const download = () => {
    const blob = new Blob([templateCsv(fleet)], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = 'modelo-frota.csv';
    link.click();
    URL.revokeObjectURL(url);
  };

  const count = sheet?.rows.length ?? 0;
  const vehiclesLabel = `${count} ${count === 1 ? 'veículo' : 'veículos'}`;
  const customLabels = [fleet?.labels.label1.trim(), fleet?.labels.label2.trim()].filter(Boolean);

  const problemList = problems.length > 0 && (
    <div className={own.problems} role="alert">
      <strong>
        {problems.length === 1 ? 'Um problema' : `${problems.length} problemas`} na planilha. Corrija e escolha o
        arquivo de novo — nada foi gravado.
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
  );

  const footer = (
    <>
      {step === 0 ? (
        <Button variant="ghost" onClick={close}>
          Cancelar
        </Button>
      ) : (
        <Button variant="ghost" onClick={() => setStep(step - 1)} disabled={importar.isPending}>
          Voltar
        </Button>
      )}
      {step === 1 && (
        <Button variant="primary" disabled={!check} onClick={() => setStep(2)}>
          Continuar
        </Button>
      )}
      {step === 2 && (
        <Button
          variant="primary"
          loading={importar.isPending}
          disabled={!check}
          onClick={() => {
            setError('');
            try {
              request(false);
            } catch (err) {
              return setError((err as Error).message);
            }
            importar.mutate();
          }}
        >
          Importar {vehiclesLabel}
        </Button>
      )}
    </>
  );

  return (
    <Modal open={open} wide title="Importar frota por planilha" onClose={close} footer={footer}>
      <ol className={styles.steps} aria-label="Etapas">
        {STEPS.map((label, index) => (
          <li
            key={label}
            className={`${styles.step} ${index === step ? styles.stepActive : ''} ${index < step ? styles.stepDone : ''}`}
            aria-current={index === step ? 'step' : undefined}
          >
            <button type="button" disabled={index >= step} onClick={() => setStep(index)}>
              <span className={styles.stepNumber}>{index < step ? '✓' : index + 1}</span>
              {label}
            </button>
          </li>
        ))}
      </ol>

      {!c ? (
        <Spinner label="Carregando" />
      ) : (
        <div className={pageStyles.form}>
          {error && <div className={pageStyles.note}>{error}</div>}

          {step === 0 && (
            <>
              <p className={pageStyles.description}>
                Uma linha por veículo, até {MAX_IMPORT_ROWS} por vez. Salve a planilha como CSV (no Excel: Salvar como →
                CSV). Só a coluna <strong>Nome</strong> é obrigatória; as outras podem ficar em branco:
              </p>
              <ul className={own.columns}>
                <li>
                  <strong>Placa</strong>, <strong>Tipo</strong> (carro ou moto), <strong>Marca</strong>,{' '}
                  <strong>Modelo</strong>, <strong>Ano</strong>, <strong>Cor</strong>
                </li>
                <li>
                  <strong>Grupo</strong>: o nome do grupo; os que a conta ainda não tem são criados.
                </li>
                <li>
                  {customLabels.length > 0 ? (
                    <>
                      {customLabels.map((label, i) => (
                        <span key={label}>
                          {i > 0 && ' e '}
                          <strong>{label}</strong>
                        </span>
                      ))}
                      : os campos personalizados da conta.
                    </>
                  ) : (
                    <>
                      <strong>Campo personalizado 1</strong> e <strong>2</strong>: os campos personalizados da conta.
                    </>
                  )}
                </li>
                <li>
                  <strong>IMEI do rastreador</strong>: se o aparelho já está com a central (cadastrado em
                  Rastreadores), o veículo já sai com ele. Em branco, o rastreador entra no envio ao cliente.
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
              {problemList}
            </>
          )}

          {step === 1 && sheet && (
            <>
              {problemList}
              {problems.length > 0 && (
                <div>
                  <Button variant="secondary" onClick={reset}>
                    Escolher outra planilha
                  </Button>
                </div>
              )}
              {check && (
                <div className={styles.orderSummary}>
                  <div className={styles.orderLine}>
                    <span>{fileName}</span>
                    <strong>{vehiclesLabel}</strong>
                  </div>
                  <div className={styles.orderLine}>
                    <span>Com o rastreador já vinculado</span>
                    <span>{check.installed}</span>
                  </div>
                  <div className={styles.orderLine}>
                    <span>Rastreador a enviar ao cliente</span>
                    <span>{check.vehicles - check.installed}</span>
                  </div>
                  {check.newGroups && check.newGroups.length > 0 && (
                    <div className={styles.orderLine}>
                      <span>Grupos novos</span>
                      <span>{check.newGroups.join(', ')}</span>
                    </div>
                  )}
                </div>
              )}
              <div className={pageStyles.tableWrap}>
                <table className={pageStyles.table}>
                  <thead>
                    <tr>
                      <th>Linha</th>
                      <th>Nome</th>
                      <th>Placa</th>
                      <th>Grupo</th>
                      <th>Rastreador</th>
                    </tr>
                  </thead>
                  <tbody>
                    {sheet.rows.slice(0, PREVIEW_ROWS).map((row, i) => (
                      <tr key={i}>
                        <td>{sheet.lines[i]}</td>
                        <td>{row.vehicle.name}</td>
                        <td>{row.vehicle.plate || '—'}</td>
                        <td>{row.group || '—'}</td>
                        <td>{row.imei || 'Enviar ao cliente'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {count > PREVIEW_ROWS && <p className={own.hint}>e mais {count - PREVIEW_ROWS} linhas.</p>}
            </>
          )}

          {step === 2 && check && planDraft && (
            <>
              {loan ? (
                <p className={pageStyles.note}>
                  Rastreadores em comodato (o plano do cliente): sem equipamento a cobrar
                  {admin.accountPlan!.installFeeCents > 0
                    ? `; a instalação (${formatMoney(admin.accountPlan!.installFeeCents)} cada) entra na fatura`
                    : ''}
                  .
                </p>
              ) : (
                <TextField
                  label="Equipamento (cada)"
                  hint={`Zero não cobra. Os aparelhos entram numa linha da fatura de ${formatDateOnly(chargeDate)}.`}
                  inputMode="decimal"
                  value={equipmentText}
                  onChange={(e) => setEquipment(e.target.value)}
                />
              )}
              <SubscriptionFields draft={planDraft} onChange={setPlan} lockedDueDay={admin.accountDueDay ?? null} />
              <div className={styles.orderSummary}>
                <div className={styles.orderLine}>
                  <span>Veículos</span>
                  <strong>{count}</strong>
                </div>
                <div className={styles.orderLine}>
                  <span>
                    Equipamento{equipmentCents ? ` (${count} × ${formatMoney(equipmentCents)})` : ''}
                    {equipmentCents ? `, na fatura de ${formatDateOnly(chargeDate)}` : ''}
                  </span>
                  <span>{equipmentCents ? formatMoney(equipmentCents * count) : 'sem cobrança'}</span>
                </div>
                <div className={styles.orderLine}>
                  <span>
                    Mensalidade de cada veículo, todo dia {planDraft.dueDay} (a 1ª na fatura de{' '}
                    {formatDateOnly(chargeDate)}, com o proporcional dos dias até lá)
                  </span>
                  <span>{parseMoney(planDraft.price) !== null ? formatMoney(parseMoney(planDraft.price)!) : '—'}</span>
                </div>
              </div>
            </>
          )}
        </div>
      )}
    </Modal>
  );
}

import { TextField } from '@/components/ui/Field';
import { centsToInput, formatMoney, parseMoney } from '@/services/format';
import type { Recurrence } from '@/types';

import pageStyles from '@/pages/Page.module.css';
import styles from './Billing.module.css';

/** O semestral e o anual na tela: ligado e os valores como digitados ("200,00"). */
export interface RecurrenceDraft {
  enabled: boolean;
  semiannual: string;
  annual: string;
}

/** O rascunho a partir do que está salvo (sem nada: desligado, R$ 200 e R$ 400). */
export function recurrenceDraft(r?: Recurrence | null): RecurrenceDraft {
  return {
    enabled: r?.enabled ?? false,
    semiannual: centsToInput(r?.semiannualCents ?? 20000),
    annual: centsToInput(r?.annualCents ?? 40000),
  };
}

/** O que vai para o servidor; inválido, a mensagem. */
export function recurrenceInput(d: RecurrenceDraft): Recurrence | string {
  const semiannual = d.semiannual.trim() ? parseMoney(d.semiannual) : 0;
  const annual = d.annual.trim() ? parseMoney(d.annual) : 0;
  if (semiannual === null || annual === null || Number.isNaN(semiannual) || Number.isNaN(annual)) {
    return 'Valor do semestral ou do anual inválido: use, por exemplo, 200,00.';
  }
  if (d.enabled && (!semiannual || !annual)) return 'Para ligar o semestral e o anual, informe os dois valores.';
  return { enabled: d.enabled, semiannualCents: semiannual, annualCents: annual };
}

/**
 * O semestral e o anual: pagos por período (6 ou 12 meses de uma vez), com o
 * rastreador incluso no valor (nada somado). Sem devolução: cancelado, fica
 * ativo até o fim do período pago e não renova.
 */
export function RecurrenceFields({
  draft,
  onChange,
  where,
}: {
  draft: RecurrenceDraft;
  onChange: (d: RecurrenceDraft) => void;
  /** Onde vale: "na contratação pelo site" ou "pelo link dele". */
  where: string;
}) {
  const per = (text: string, months: number) => {
    const cents = text.trim() ? parseMoney(text) : null;
    return cents ? `${formatMoney(Math.floor(cents / months))} por mês` : '';
  };
  return (
    <>
      <label className={styles.promoOption}>
        <input type="checkbox" checked={draft.enabled} onChange={(e) => onChange({ ...draft, enabled: e.target.checked })} />
        <span>
          <strong>Oferecer o semestral e o anual {where}</strong>
          <br />
          Pagos por período, de uma vez, com o rastreador incluso no valor (nada somado). Só com o rastreador incluso,
          não no comodato. Sem devolução: cancelado, segue ativo até o fim do período pago e não renova. Desligado, só o
          mensal.
        </span>
      </label>
      <div className={pageStyles.formRow}>
        <TextField
          label="Semestral (R$, a cada 6 meses)"
          hint={per(draft.semiannual, 6)}
          inputMode="decimal"
          value={draft.semiannual}
          onChange={(e) => onChange({ ...draft, semiannual: e.target.value })}
        />
        <TextField
          label="Anual (R$, a cada 12 meses)"
          hint={per(draft.annual, 12)}
          inputMode="decimal"
          value={draft.annual}
          onChange={(e) => onChange({ ...draft, annual: e.target.value })}
        />
      </div>
    </>
  );
}

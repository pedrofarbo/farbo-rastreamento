import { SelectField, TextField } from '@/components/ui/Field';
import { centsToInput, formatMoney, parseMoney } from '@/services/format';
import type { Tracker, TrackerRule } from '@/types';

import styles from '@/pages/Page.module.css';

/**
 * O rastreador incluso no pedido da central (a Farbo não vende o
 * rastreador): a parte dele nas primeiras mensalidades, somada ao plano
 * (abaixo do preço do site), já dentro do preço (no preço do site ou acima)
 * ou nenhuma (o aparelho é do cliente). Encerrada antes, o que falta da parte
 * vence numa fatura só.
 */
export type TrackerMode = 'added' | 'inside' | 'none';

export interface TrackerDraft {
  /** Nulo: pela regra (abaixo do preço do site, somada; senão, dentro). */
  mode: TrackerMode | null;
  cents: string;
  months: string;
}

export function trackerDraftFrom(rule: TrackerRule | Tracker): TrackerDraft {
  return { mode: null, cents: centsToInput(rule.cents), months: String(rule.months) };
}

/** O modo que vale: o escolhido, ou o da regra pelo preço do plano. */
export function trackerMode(draft: TrackerDraft, planCents: number | null, sitePlanCents: number): TrackerMode {
  if (draft.mode) return draft.mode;
  return planCents !== null && planCents < sitePlanCents ? 'added' : 'inside';
}

/** O rastreador do pedido, ou a mensagem do que está errado. */
export function trackerFromDraft(draft: TrackerDraft, mode: TrackerMode): Tracker | string {
  if (mode === 'none') return { cents: 0, months: 0, added: false };
  const cents = parseMoney(draft.cents);
  const months = Number(draft.months);
  if (cents === null || cents <= 0) return 'Parte do rastreador inválida. Use, por exemplo, 12,00.';
  if (!Number.isInteger(months) || months < 1 || months > 60) return 'O rastreador incluso vai de 1 a 60 mensalidades.';
  return { cents, months, added: mode === 'added' };
}

export function TrackerFields({
  draft,
  onChange,
  planCents,
  sitePlanCents,
}: {
  draft: TrackerDraft;
  onChange: (draft: TrackerDraft) => void;
  /** A mensalidade do plano escolhido (nula: inválida). */
  planCents: number | null;
  sitePlanCents: number;
}) {
  const mode = trackerMode(draft, planCents, sitePlanCents);
  const cents = parseMoney(draft.cents);
  const months = Number(draft.months);
  const hint =
    mode === 'none'
      ? 'Sem rastreador incluso: o aparelho já é do cliente. Nada a acertar no encerramento.'
      : mode === 'added'
        ? `Somada à mensalidade nas ${months || '—'} primeiras${
            planCents !== null && cents ? ` (${formatMoney(planCents + cents)}/mês, depois ${formatMoney(planCents)})` : ''
          }. Encerrada antes, o que falta vence numa fatura só.`
        : 'Já dentro do preço da mensalidade: só conta para o saldo de um encerramento antes da última.';
  return (
    <>
      <SelectField
        label="Rastreador incluso (fica com o cliente)"
        hint={`${hint} Pela regra: abaixo de ${formatMoney(sitePlanCents)} (o preço do site), somada; no preço do site ou acima, dentro dele.`}
        value={mode}
        onChange={(e) => onChange({ ...draft, mode: e.target.value as TrackerMode })}
      >
        <option value="added">Parte do rastreador somada nas primeiras mensalidades</option>
        <option value="inside">Parte do rastreador já no preço da mensalidade</option>
        <option value="none">Sem rastreador incluso (o aparelho é do cliente)</option>
      </SelectField>
      {mode !== 'none' && (
        <div className={styles.formRow}>
          <TextField
            label="Parte do rastreador por mensalidade (R$)"
            inputMode="decimal"
            value={draft.cents}
            onChange={(e) => onChange({ ...draft, cents: e.target.value })}
          />
          <TextField
            label="Em quantas mensalidades"
            inputMode="numeric"
            value={draft.months}
            onChange={(e) => onChange({ ...draft, months: e.target.value.replace(/\D/g, '').slice(0, 2) })}
          />
        </div>
      )}
    </>
  );
}

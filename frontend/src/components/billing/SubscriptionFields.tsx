import type { SubscriptionInput } from '@/api/resources';
import { SelectField, TextField } from '@/components/ui/Field';
import { centsToInput, parseMoney } from '@/services/format';

import styles from '@/pages/Page.module.css';

/** Planos da landing page; "personalizado" libera nome e valor. */
export const PLAN_PRESETS = [
  { id: 'mensal', planName: 'Plano Mensal', priceCents: 6990 },
  { id: 'insanos', planName: 'Especial Insanos MC', priceCents: 3990 },
] as const;

export interface SubscriptionDraft {
  preset: string;
  planName: string;
  price: string;
  dueDay: number;
}

export const DEFAULT_SUBSCRIPTION: SubscriptionDraft = {
  preset: PLAN_PRESETS[0].id,
  planName: PLAN_PRESETS[0].planName,
  price: centsToInput(PLAN_PRESETS[0].priceCents),
  dueDay: 10,
};

/** Converte o rascunho no corpo da API, ou devolve a mensagem de erro. */
export function subscriptionFromDraft(draft: SubscriptionDraft): SubscriptionInput | string {
  const priceCents = parseMoney(draft.price);
  if (!draft.planName.trim()) return 'Informe o nome do plano.';
  if (priceCents === null) return 'Valor mensal inválido. Use, por exemplo, 69,90.';
  return { planName: draft.planName.trim(), priceCents, dueDay: draft.dueDay };
}

const DUE_DAYS = Array.from({ length: 28 }, (_, i) => i + 1);

export function SubscriptionFields({
  draft,
  onChange,
  lockedDueDay = null,
  dueDayHint,
}: {
  draft: SubscriptionDraft;
  onChange: (draft: SubscriptionDraft) => void;
  /** O vencimento da conta, quando ela já tem um: o veículo novo vence nele. */
  lockedDueDay?: number | null;
  dueDayHint?: string;
}) {
  const custom = draft.preset === 'custom';

  return (
    <>
      <div className={styles.formRow}>
        <SelectField
          label="Plano"
          value={draft.preset}
          onChange={(event) => {
            const preset = PLAN_PRESETS.find((p) => p.id === event.target.value);
            onChange(
              preset
                ? { ...draft, preset: preset.id, planName: preset.planName, price: centsToInput(preset.priceCents) }
                : { ...draft, preset: 'custom' },
            );
          }}
        >
          {PLAN_PRESETS.map((preset) => (
            <option key={preset.id} value={preset.id}>
              {preset.planName} — R$ {centsToInput(preset.priceCents)}
            </option>
          ))}
          <option value="custom">Personalizado</option>
        </SelectField>
        <SelectField
          label="Vencimento"
          hint={
            lockedDueDay !== null
              ? 'O da conta: todos os veículos do cliente vencem no mesmo dia (para mudar, edite uma assinatura).'
              : (dueDayHint ?? 'Até o dia 28, para existir em todos os meses.')
          }
          value={lockedDueDay ?? draft.dueDay}
          disabled={lockedDueDay !== null}
          onChange={(event) => onChange({ ...draft, dueDay: Number(event.target.value) })}
        >
          {DUE_DAYS.map((day) => (
            <option key={day} value={day}>
              todo dia {day}
            </option>
          ))}
        </SelectField>
      </div>
      <div className={styles.formRow}>
        <TextField
          label="Nome do plano"
          value={draft.planName}
          disabled={!custom}
          onChange={(event) => onChange({ ...draft, planName: event.target.value })}
        />
        <TextField
          label="Valor mensal (R$)"
          inputMode="decimal"
          value={draft.price}
          onChange={(event) => onChange({ ...draft, preset: 'custom', price: event.target.value })}
        />
      </div>
    </>
  );
}

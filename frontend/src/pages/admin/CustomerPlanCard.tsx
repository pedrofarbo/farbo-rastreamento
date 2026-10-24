import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { customersApi } from '@/api/resources';
import {
  DEFAULT_SUBSCRIPTION,
  PLAN_PRESETS,
  SubscriptionFields,
  subscriptionFromDraft,
} from '@/components/billing/SubscriptionFields';
import type { SubscriptionDraft } from '@/components/billing/SubscriptionFields';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { Modal } from '@/components/ui/Modal';
import { useToast } from '@/components/ui/Toast';
import { centsToInput, formatMoney, parseMoney } from '@/services/format';
import type { AccountPlan, EquipmentTerms } from '@/types';

import org from '@/components/organization/Organization.module.css';
import { TextField } from '@/components/ui/Field';

import pageStyles from '../Page.module.css';

/** O rascunho do modal a partir do plano atual (ou do padrão), no vencimento da conta. */
function draftFrom(plan: AccountPlan | null, accountDueDay: number | null): SubscriptionDraft {
  const preset = plan && PLAN_PRESETS.find((p) => p.planName === plan.planName && p.priceCents === plan.priceCents);
  const draft = plan
    ? { preset: preset?.id ?? 'custom', planName: plan.planName, price: centsToInput(plan.priceCents), dueDay: plan.dueDay }
    : DEFAULT_SUBSCRIPTION;
  return accountDueDay ? { ...draft, dueDay: accountDueDay } : draft;
}

/** O rastreador e as taxas no rascunho (os valores como digitados). */
interface TermsDraft {
  mode: 'sale' | 'loan';
  install: string;
  uninstall: string;
}

function termsDraftFrom(plan: AccountPlan | null): TermsDraft {
  return {
    mode: plan?.equipmentMode ?? 'sale',
    install: plan?.installFeeCents ? centsToInput(plan.installFeeCents) : '',
    uninstall: plan?.uninstallFeeCents ? centsToInput(plan.uninstallFeeCents) : '',
  };
}

/** Os termos conferidos (vazio é zero), ou a mensagem do problema. */
export function termsFromDraft(d: TermsDraft): EquipmentTerms | string {
  if (d.mode === 'sale') return { equipmentMode: 'sale', installFeeCents: 0, uninstallFeeCents: 0 };
  const install = d.install.trim() ? parseMoney(d.install) : 0;
  const uninstall = d.uninstall.trim() ? parseMoney(d.uninstall) : 0;
  if (install === null || uninstall === null) return 'Confira as taxas de instalação e desinstalação (ex.: 80,00).';
  return { equipmentMode: 'loan', installFeeCents: install, uninstallFeeCents: uninstall };
}

/** O rastreador do plano, numa frase. */
export function termsLabel(plan: EquipmentTerms): string {
  if (plan.equipmentMode !== 'loan') return '';
  const fees = [
    plan.installFeeCents ? `instalação ${formatMoney(plan.installFeeCents)}` : '',
    plan.uninstallFeeCents ? `desinstalação ${formatMoney(plan.uninstallFeeCents)}` : '',
  ].filter(Boolean);
  return ` Rastreador em comodato${fees.length ? ` (${fees.join(', ')})` : ', sem taxas'}.`;
}

/**
 * O plano do cliente, definido pela central antes de ele cadastrar os
 * veículos (ex.: o Especial Insanos MC): todo veículo novo dele sai nesse
 * plano, pelo app ou pela central. O cliente não escolhe plano.
 */
export function CustomerPlanCard({
  customerId,
  plan,
  accountDueDay = null,
}: {
  customerId: string;
  plan: AccountPlan | null;
  /** O vencimento das assinaturas ativas (null sem nenhuma). */
  accountDueDay?: number | null;
}) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<SubscriptionDraft | null>(null);
  const [terms, setTerms] = useState<TermsDraft>(termsDraftFrom(plan));
  const [error, setError] = useState('');

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['customer', customerId] });
  const save = useMutation({
    mutationFn: () => {
      const input = subscriptionFromDraft(draft as SubscriptionDraft);
      if (typeof input === 'string') throw new Error(input);
      const equipment = termsFromDraft(terms);
      if (typeof equipment === 'string') throw new Error(equipment);
      return customersApi.setPlan(customerId, { ...input, ...equipment });
    },
    onSuccess: (saved) => {
      refresh();
      setDraft(null);
      const moved = accountDueDay !== null && saved.dueDay !== accountDueDay;
      notify({
        tone: 'success',
        title: 'Plano do cliente salvo',
        description: moved
          ? `${saved.planName}: vale para os veículos novos. A conta passou a vencer todo dia ${saved.dueDay}.`
          : `${saved.planName}: vale para os veículos novos.`,
      });
    },
    onError: (err: Error) => setError(err.message),
  });
  const clear = useMutation({
    mutationFn: () => customersApi.clearPlan(customerId),
    onSuccess: () => {
      refresh();
      notify({ tone: 'success', title: 'Plano padrão de volta' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível', description: err.message }),
  });

  return (
    <Card
      title="Plano do cliente"
      subtitle="Vale para todo veículo novo, pelo app ou pela central; o nome e o valor das assinaturas que já existem não mudam, e o vencimento é o da conta (um dia só para todas). O cliente não escolhe plano."
      actions={
        <div className={pageStyles.actions}>
          {plan && (
            <Button size="small" variant="ghost" loading={clear.isPending} onClick={() => clear.mutate()}>
              Voltar ao padrão
            </Button>
          )}
          <Button
            size="small"
            variant={plan ? 'secondary' : 'primary'}
            onClick={() => {
              setError('');
              setDraft(draftFrom(plan, accountDueDay));
              setTerms(termsDraftFrom(plan));
            }}
          >
            {plan ? 'Trocar o plano' : 'Definir o plano'}
          </Button>
        </div>
      }
    >
      <p className={pageStyles.description} style={{ margin: 0 }}>
        {plan
          ? `${plan.planName}: ${formatMoney(plan.priceCents)}/mês, vencimento todo dia ${plan.dueDay}.${termsLabel(plan)}`
          : 'Padrão: o plano da assinatura ativa do cliente ou, no primeiro veículo, o Plano Mensal.'}
      </p>

      <Modal
        open={draft !== null}
        title="Plano do cliente"
        onClose={() => setDraft(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setDraft(null)}>
              Cancelar
            </Button>
            <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
              Salvar
            </Button>
          </>
        }
      >
        {draft && (
          <div className={pageStyles.form}>
            <SubscriptionFields
              draft={draft}
              onChange={setDraft}
              dueDayHint={
                accountDueDay !== null
                  ? 'É o vencimento da conta: num dia diferente, todas as assinaturas ativas do cliente vão para ele.'
                  : undefined
              }
            />
            <p className={pageStyles.description} style={{ margin: 0 }}>
              Na promoção de pré-lançamento, o plano Especial Insanos MC tem a mensalidade promocional dos Insanos.
            </p>
            <div className={org.choices} role="radiogroup" aria-label="Rastreador">
              <label className={`${org.choice} ${terms.mode === 'sale' ? org.choiceActive : ''}`}>
                <input type="radio" name="plan-equipment" checked={terms.mode === 'sale'} onChange={() => setTerms({ ...terms, mode: 'sale' })} />
                Rastreador vendido
              </label>
              <label className={`${org.choice} ${terms.mode === 'loan' ? org.choiceActive : ''}`}>
                <input type="radio" name="plan-equipment" checked={terms.mode === 'loan'} onChange={() => setTerms({ ...terms, mode: 'loan' })} />
                Comodato (emprestado)
              </label>
            </div>
            {terms.mode === 'loan' && (
              <div className={pageStyles.formRow}>
                <TextField
                  label="Taxa de instalação (R$)"
                  inputMode="decimal"
                  placeholder="0,00"
                  hint="Na fatura da contratação."
                  value={terms.install}
                  onChange={(e) => setTerms({ ...terms, install: e.target.value })}
                />
                <TextField
                  label="Taxa de desinstalação (R$)"
                  inputMode="decimal"
                  placeholder="0,00"
                  hint="Lançada no encerramento."
                  value={terms.uninstall}
                  onChange={(e) => setTerms({ ...terms, uninstall: e.target.value })}
                />
              </div>
            )}
            {error && <div className={pageStyles.note}>{error}</div>}
          </div>
        )}
      </Modal>
    </Card>
  );
}

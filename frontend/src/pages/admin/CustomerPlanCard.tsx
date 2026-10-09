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
import { centsToInput, formatMoney } from '@/services/format';
import type { AccountPlan } from '@/types';

import pageStyles from '../Page.module.css';

/** O rascunho do modal a partir do plano atual (ou do padrão). */
function draftFrom(plan: AccountPlan | null): SubscriptionDraft {
  if (!plan) return DEFAULT_SUBSCRIPTION;
  const preset = PLAN_PRESETS.find((p) => p.planName === plan.planName && p.priceCents === plan.priceCents);
  return { preset: preset?.id ?? 'custom', planName: plan.planName, price: centsToInput(plan.priceCents), dueDay: plan.dueDay };
}

/**
 * O plano do cliente, definido pela central antes de ele cadastrar os
 * veículos (ex.: o Especial Insanos MC): todo veículo novo dele sai nesse
 * plano, pelo app ou pela central. O cliente não escolhe plano.
 */
export function CustomerPlanCard({ customerId, plan }: { customerId: string; plan: AccountPlan | null }) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<SubscriptionDraft | null>(null);
  const [error, setError] = useState('');

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['customer', customerId] });
  const save = useMutation({
    mutationFn: () => {
      const input = subscriptionFromDraft(draft as SubscriptionDraft);
      if (typeof input === 'string') throw new Error(input);
      return customersApi.setPlan(customerId, input);
    },
    onSuccess: (saved) => {
      refresh();
      setDraft(null);
      notify({ tone: 'success', title: 'Plano do cliente salvo', description: `${saved.planName}: vale para os veículos novos.` });
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
      subtitle="Vale para todo veículo novo, pelo app ou pela central; as assinaturas que já existem não mudam. O cliente não escolhe plano."
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
              setDraft(draftFrom(plan));
            }}
          >
            {plan ? 'Trocar o plano' : 'Definir o plano'}
          </Button>
        </div>
      }
    >
      <p className={pageStyles.description} style={{ margin: 0 }}>
        {plan
          ? `${plan.planName}: ${formatMoney(plan.priceCents)}/mês, vencimento todo dia ${plan.dueDay}.`
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
            <SubscriptionFields draft={draft} onChange={setDraft} />
            <p className={pageStyles.description} style={{ margin: 0 }}>
              Na promoção de pré-lançamento, o plano Especial Insanos MC tem a mensalidade promocional dos Insanos.
            </p>
            {error && <div className={pageStyles.note}>{error}</div>}
          </div>
        )}
      </Modal>
    </Card>
  );
}

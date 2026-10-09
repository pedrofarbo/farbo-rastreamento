import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { customersApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { useToast } from '@/components/ui/Toast';
import { formatMoney } from '@/services/format';
import type { PromoStatus } from '@/types';

import pageStyles from '../Page.module.css';

/** A chave da situação da promoção do cliente (a mesma do Novo veículo). */
export const launchPromoKey = (customerId: string) => ['customer', customerId, 'launch-promo'];

/** "rastreador por R$ 120,00 e mensalidade de R$ 34,90 nos 12 primeiros meses". */
function offerText(s: PromoStatus): string {
  return `rastreador por ${formatMoney(s.offer.equipmentCents)} e mensalidade de ${formatMoney(s.offer.monthlyCents)} nos ${s.offer.months} primeiros meses`;
}

/** O que a ficha diz da promoção do cliente. */
export function promoSummary(s: PromoStatus): string {
  if (s.claimed) return 'Já usada: o cliente contratou com a promoção (vale para 1 veículo).';
  const since = s.grantedAt ? new Date(s.grantedAt).toLocaleDateString('pt-BR') : '';
  if (s.eligible) {
    return s.onList
      ? 'Tem direito: o e-mail está na lista de lançamento.'
      : `Liberada pela central em ${since}: o cliente pode contratar com ela, pelo app ou pela central.`;
  }
  if (s.code === 'NOT_ON_LIST') {
    return 'Fora da lista de lançamento. Libere para ele contratar com a promoção, pelo app ou pela central (vale 1 das vagas).';
  }
  return s.grantedAt ? `Liberada pela central em ${since}, mas ${s.reason}.` : `Sem direito: ${s.reason}.`;
}

/**
 * A promoção de pré-lançamento na ficha do cliente: se ele tem direito (pela
 * lista de lançamento ou liberado pela central) e o botão para liberar a quem
 * não se inscreveu na lista — ou retirar a liberação, se ainda não usou.
 */
export function LaunchPromoCard({ customerId }: { customerId: string }) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const status = useQuery({ queryKey: launchPromoKey(customerId), queryFn: () => customersApi.launchPromo(customerId) });
  const change = useMutation({
    mutationFn: (grant: boolean) =>
      grant ? customersApi.grantLaunchPromo(customerId) : customersApi.revokeLaunchPromo(customerId),
    onSuccess: (next, grant) => {
      queryClient.setQueryData(launchPromoKey(customerId), next);
      notify({
        tone: 'success',
        title: grant ? 'Promoção de pré-lançamento liberada' : 'Liberação retirada',
        description: grant ? 'O cliente já pode contratar com ela, pelo app ou pela central.' : undefined,
      });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível', description: err.message }),
  });

  const s = status.data;
  if (!s) return null;
  const canGrant = s.code === 'NOT_ON_LIST';
  const canRevoke = s.grantedAt !== null && !s.claimed;
  return (
    <Card
      title="Promoção de pré-lançamento"
      subtitle={`Para quem está na lista de lançamento ou foi liberado pela central: ${offerText(s)}.`}
      actions={
        canGrant ? (
          <Button size="small" variant="primary" loading={change.isPending} onClick={() => change.mutate(true)}>
            Liberar a promoção
          </Button>
        ) : canRevoke ? (
          <Button size="small" variant="ghost" loading={change.isPending} onClick={() => change.mutate(false)}>
            Retirar a liberação
          </Button>
        ) : undefined
      }
    >
      <p className={pageStyles.description} style={{ margin: 0 }}>
        {promoSummary(s)}
      </p>
    </Card>
  );
}

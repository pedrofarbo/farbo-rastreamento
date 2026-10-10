import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { affiliatesApi, customersApi } from '@/api/resources';
import { Card } from '@/components/ui/Card';
import fieldStyles from '@/components/ui/Field.module.css';
import { useToast } from '@/components/ui/Toast';
import type { CustomerReferral } from '@/types';

const SOURCE: Record<CustomerReferral['source'], string> = {
  lead: 'pelo pré-cadastro feito no link',
  waitlist: 'pelo e-mail na lista de lançamento, feita no link',
  admin: 'informado pela central',
};

/**
 * Quem indicou o cliente (programa de afiliados). Vem sozinho quando o
 * cliente chegou pelo link; a central corrige ou informa aqui. A indicação
 * nova vale a partir do mês dela.
 */
export function CustomerReferralCard({ customerId, referral }: { customerId: string; referral: CustomerReferral | null }) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const affiliates = useQuery({ queryKey: ['affiliates'], queryFn: affiliatesApi.list });

  const save = useMutation({
    mutationFn: (affiliateId: string | null) => customersApi.setAffiliate(customerId, affiliateId),
    onSuccess: ({ affiliate }) => {
      queryClient.invalidateQueries({ queryKey: ['customer', customerId] });
      queryClient.invalidateQueries({ queryKey: ['affiliates'] });
      notify({ tone: 'success', title: affiliate ? `Indicado por ${affiliate.name}` : 'Indicação removida' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível salvar', description: err.message }),
  });

  const options = affiliates.data ?? [];
  // O afiliado atual aparece mesmo se a lista ainda não chegou.
  const missing = referral && !options.some((a) => a.id === referral.affiliateId);

  return (
    <Card
      title="Indicado por"
      subtitle={
        referral
          ? `${referral.handle ? `@${referral.handle}` : referral.name}, ${SOURCE[referral.source]} (desde ${new Date(referral.since).toLocaleDateString('pt-BR')}). Cada veículo do cliente, em cada mês pago, rende a comissão ao afiliado.`
          : 'Ninguém: o cliente não veio pelo link de um afiliado.'
      }
    >
      <select
        className={fieldStyles.select}
        aria-label="Afiliado que indicou o cliente"
        value={referral?.affiliateId ?? ''}
        disabled={save.isPending || affiliates.isLoading}
        onChange={(e) => save.mutate(e.target.value || null)}
      >
        <option value="">Ninguém</option>
        {missing && <option value={referral.affiliateId}>{referral.name}</option>}
        {options.map((a) => (
          <option key={a.id} value={a.id}>
            {a.name}
            {a.handle ? ` (@${a.handle})` : ''}
            {a.active ? '' : ' — inativo'}
          </option>
        ))}
      </select>
    </Card>
  );
}

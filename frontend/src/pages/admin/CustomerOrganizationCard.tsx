import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { customersApi, organizationsApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import fieldStyles from '@/components/ui/Field.module.css';
import { useToast } from '@/components/ui/Toast';
import { ORG_KIND_LABELS } from '@/services/roles';

import styles from '../Page.module.css';

/**
 * A associação ou seguradora que paga o rastreamento do cliente: o associado
 * não vê faturas nem pede veículo no app. Quem tem assinatura paga ativa
 * precisa encerrá-la antes (o servidor recusa).
 */
export function CustomerOrganizationCard({
  customerId,
  organizationId,
  organizationName,
}: {
  customerId: string;
  organizationId: string | null;
  organizationName: string;
}) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [choice, setChoice] = useState('');
  const [confirming, setConfirming] = useState(false);
  const orgs = useQuery({ queryKey: ['organizations'], queryFn: organizationsApi.list, enabled: !organizationId });

  const save = useMutation({
    mutationFn: (orgId: string | null) => customersApi.setOrganization(customerId, orgId),
    onSuccess: (res) => {
      setChoice('');
      setConfirming(false);
      void queryClient.invalidateQueries({ queryKey: ['customer', customerId] });
      void queryClient.invalidateQueries({ queryKey: ['customers'] });
      notify({
        tone: 'success',
        title: res.organization ? `Associado da ${res.organization.name}` : 'Cliente fora da organização',
        description: res.organization
          ? 'O rastreamento dele passa a entrar na fatura da organização.'
          : 'Ele volta a ser um cliente comum: para cobrar, contrate o rastreamento na ficha.',
      });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para alterar', description: err.message }),
  });

  return (
    <Card title="Associação ou seguradora" subtitle="Quem paga o rastreamento do cliente.">
      {organizationId ? (
        <div className={styles.form}>
          <span>
            Associado da <Link to={`/associacoes/${organizationId}`}>{organizationName}</Link>: o app dele não mostra
            faturas nem o pedido de veículo, e os veículos com rastreador entram na fatura mensal da organização.
          </span>
          {confirming ? (
            <div className={styles.actions} style={{ justifyContent: 'flex-start' }}>
              <span>Tirar da {organizationName}? Os veículos saem da próxima fatura dela.</span>
              <Button size="small" variant="danger" loading={save.isPending} onClick={() => save.mutate(null)}>
                Tirar
              </Button>
              <Button size="small" variant="ghost" onClick={() => setConfirming(false)}>
                Não
              </Button>
            </div>
          ) : (
            <div>
              <Button size="small" variant="ghost" onClick={() => setConfirming(true)}>
                Tirar da organização
              </Button>
            </div>
          )}
        </div>
      ) : (
        <div className={styles.form}>
          <span className={styles.description} style={{ marginTop: 0 }}>
            O cliente paga o próprio rastreamento. Para passar a cobrança a uma associação ou seguradora, escolha qual
            (com assinatura paga ativa, encerre-a antes).
          </span>
          <div className={styles.actions} style={{ justifyContent: 'flex-start' }}>
            <select
              className={fieldStyles.select}
              aria-label="Organização"
              value={choice}
              onChange={(e) => setChoice(e.target.value)}
              style={{ maxWidth: 320 }}
            >
              <option value="">Escolha a organização</option>
              {(orgs.data ?? [])
                .filter((o) => o.active)
                .map((o) => (
                  <option key={o.id} value={o.id}>
                    {o.name} ({ORG_KIND_LABELS[o.kind]})
                  </option>
                ))}
            </select>
            <Button size="small" variant="primary" disabled={!choice} loading={save.isPending} onClick={() => save.mutate(choice)}>
              Passar para a organização
            </Button>
          </div>
        </div>
      )}
    </Card>
  );
}

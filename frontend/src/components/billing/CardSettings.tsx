import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { meApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { useToast } from '@/components/ui/Toast';
import { formatDateOnly } from '@/services/format';
import type { CardView } from '@/types';

import styles from './Billing.module.css';

const cardKey = ['me', 'card'] as const;

/**
 * O cartão de crédito da conta, na tela de Faturas: o cartão salvo e o
 * débito automático (ligar pede a autorização; desligar e apagar, não), e o
 * aviso quando o banco recusou a última cobrança. Some quando o cartão não
 * está ligado.
 */
export function CardSettings() {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const card = useQuery({ queryKey: cardKey, queryFn: meApi.card });
  const [confirming, setConfirming] = useState(false);
  const done = (view: CardView | null, title: string) => {
    queryClient.setQueryData(cardKey, view ?? { ...card.data, saved: null });
    setConfirming(false);
    notify({ tone: 'success', title });
  };
  const autoDebit = useMutation({
    mutationFn: (enabled: boolean) => meApi.setAutoDebit(enabled),
    onSuccess: (view, enabled) => done(view, enabled ? 'Débito automático ligado' : 'Débito automático desligado'),
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para mudar o débito automático', description: err.message }),
  });
  const remove = useMutation({
    mutationFn: meApi.removeCard,
    onSuccess: () => done(null, 'Cartão apagado'),
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para apagar o cartão', description: err.message }),
  });

  if (!card.data?.enabled) return null;
  const saved = card.data.saved;
  if (!saved) {
    return (
      <Card title="Cartão de crédito" subtitle="Débito automático das faturas">
        <p className={styles.muted}>
          Pague uma fatura com cartão e marque o débito automático: as próximas passam a ser cobradas nele sozinhas, no
          vencimento.
        </p>
      </Card>
    );
  }
  return (
    <Card
      title="Cartão de crédito"
      subtitle={`${saved.brand} final ${saved.last4}${saved.autoDebit ? ' · débito automático ligado' : ''}`}
      actions={
        <div className={styles.cardActions}>
          {saved.autoDebit ? (
            <Button size="small" variant="ghost" loading={autoDebit.isPending} onClick={() => autoDebit.mutate(false)}>
              Desligar o débito
            </Button>
          ) : (
            !confirming && (
              <Button size="small" variant="secondary" onClick={() => setConfirming(true)}>
                Ligar o débito automático
              </Button>
            )
          )}
          <Button size="small" variant="ghost" loading={remove.isPending} onClick={() => remove.mutate()}>
            Apagar o cartão
          </Button>
        </div>
      }
    >
      <div className={styles.cardBody}>
        {saved.failedAt && (
          <div className={`${styles.banner} ${styles.bannerWarning}`} role="status">
            <span>
              <strong>O banco recusou a cobrança de {formatDateOnly(saved.failedAt.slice(0, 10))}.</strong> Pague a
              fatura em aberto por Pix ou boleto, ou pague com outro cartão (marcando o débito automático, ele passa a
              ser o da conta).
            </span>
          </div>
        )}
        {saved.autoDebit ? (
          <p className={styles.muted}>
            As faturas em aberto são cobradas neste cartão no dia do vencimento; se o banco recusar, tentamos de novo nos
            2 dias seguintes e avisamos por e-mail.
          </p>
        ) : confirming ? (
          <div className={styles.cardConfirm}>
            <p>
              Autorizo a Farbo a cobrar no cartão {saved.brand} final {saved.last4} as faturas da conta no dia do
              vencimento (se o banco recusar, nos 2 dias seguintes). Dá para desligar quando quiser.
            </p>
            <div className={styles.cardActions}>
              <Button size="small" variant="primary" loading={autoDebit.isPending} onClick={() => autoDebit.mutate(true)}>
                Autorizo
              </Button>
              <Button size="small" variant="ghost" onClick={() => setConfirming(false)}>
                Cancelar
              </Button>
            </div>
          </div>
        ) : (
          <p className={styles.muted}>O cartão está salvo para pagar as faturas aqui; o débito automático está desligado.</p>
        )}
      </div>
    </Card>
  );
}

import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';

import { ApiError } from '@/api/client';
import { fulfillmentsApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { useToast } from '@/components/ui/Toast';
import { errorCode } from '@/services/stepUp';
import type { Fulfillment } from '@/types';

import styles from './Fulfillment.module.css';

/**
 * A etiqueta comprada: imprimir (o link do Melhor Envios, em outra aba) e
 * baixar o PDF (com o veículo e o rastreio no nome do arquivo). Se o Melhor
 * Envios entregar a página de impressão em vez do PDF, aparece o link para
 * abri-la — aberto pelo clique, que o navegador não bloqueia.
 */
export function LabelActions({
  fulfillment,
  compact = false,
}: {
  fulfillment: Pick<Fulfillment, 'id' | 'labelUrl' | 'shippingOrderId'>;
  /** Na lista de pedidos: botões menores, sem o texto "etiqueta". */
  compact?: boolean;
}) {
  const { notify } = useToast();
  const [pageUrl, setPageUrl] = useState('');
  const download = useMutation({
    mutationFn: () => fulfillmentsApi.downloadLabel(fulfillment.id),
    onSuccess: () => setPageUrl(''),
    onError: (err: Error) => {
      if (errorCode(err) === 'LABEL_NOT_PDF') {
        const url = ((err as ApiError).body as { url?: string } | undefined)?.url ?? fulfillment.labelUrl;
        setPageUrl(url);
        notify({
          tone: 'info',
          title: 'A etiqueta veio como página para imprimir',
          description: 'Abra pelo link e, na impressão, escolha "Salvar como PDF".',
        });
        return;
      }
      notify({ tone: 'error', title: 'Não deu para baixar a etiqueta', description: err.message });
    },
  });

  if (!fulfillment.shippingOrderId && !fulfillment.labelUrl) return null;
  return (
    <>
      <div className={compact ? styles.labelCompact : styles.actionRow}>
        {fulfillment.labelUrl && (
          <Button
            size="small"
            variant={compact ? 'ghost' : 'primary'}
            onClick={() => window.open(fulfillment.labelUrl, '_blank', 'noopener,noreferrer')}
          >
            {compact ? 'Imprimir' : 'Imprimir etiqueta'}
          </Button>
        )}
        {fulfillment.shippingOrderId && (
          <Button size="small" variant={compact ? 'ghost' : 'secondary'} loading={download.isPending} onClick={() => download.mutate()}>
            {compact ? 'PDF' : 'Baixar PDF'}
          </Button>
        )}
      </div>
      {pageUrl && (
        <a className={styles.labelLink} href={pageUrl} target="_blank" rel="noreferrer noopener">
          Abrir a etiqueta para imprimir ou salvar em PDF
        </a>
      )}
    </>
  );
}

import { useToast } from '@/components/ui/Toast';
import type { FiscalDocument } from '@/types';

import styles from './Billing.module.css';

/** A situação da NFS-e, como aparece sob a fatura. */
export function fiscalLabel(doc: FiscalDocument): string {
  switch (doc.status) {
    case 'issued':
      return `NFS-e nº ${doc.number}`;
    case 'pending':
    case 'processing':
      return 'NFS-e em emissão';
    case 'error':
      return `NFS-e recusada: ${doc.error}`;
    case 'blocked':
      return 'NFS-e esperando créditos na Notaas: sai sozinha até 15 minutos depois de colocar créditos';
    case 'cancel_pending':
      return 'NFS-e em cancelamento (fatura estornada)';
    case 'cancelled':
      return doc.number ? `NFS-e nº ${doc.number} cancelada` : 'NFS-e cancelada';
  }
}

/**
 * A NFS-e de uma fatura paga, numa linha sob ela: o número e o PDF; a
 * recusada, com o motivo e, para a central (onRetry), o emitir de novo.
 */
export function FiscalDocumentLine({
  doc,
  onDownload,
  onRetry,
}: {
  doc: FiscalDocument;
  onDownload: (id: string) => Promise<void>;
  onRetry?: (id: string) => void;
}) {
  const { notify } = useToast();
  const failed = doc.status === 'error' || doc.status === 'blocked';
  return (
    <div className={failed ? styles.fiscalFailed : styles.muted}>
      {fiscalLabel(doc)}
      {doc.status === 'issued' && doc.hasPdf && (
        <>
          {' · '}
          <button
            type="button"
            className={styles.inlineLink}
            onClick={() =>
              void onDownload(doc.id).catch((err: Error) =>
                notify({ tone: 'error', title: 'Não deu para baixar a nota', description: err.message }),
              )
            }
          >
            Baixar o PDF
          </button>
        </>
      )}
      {failed && onRetry && (
        <>
          {' · '}
          <button type="button" className={styles.inlineLink} onClick={() => onRetry(doc.id)}>
            Emitir de novo
          </button>
        </>
      )}
    </div>
  );
}

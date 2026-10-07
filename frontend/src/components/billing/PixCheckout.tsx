import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';

import type { PixApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import fieldStyles from '@/components/ui/Field.module.css';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateTime, formatMoney } from '@/services/format';
import type { PixCharge } from '@/types';

import styles from './Billing.module.css';

/** De quanto em quanto tempo a tela pergunta se o Pix foi pago. */
const POLL_MS = 4000;

/**
 * O pagamento de uma fatura por Pix: gera (ou reaproveita) o Pix quando a
 * fatura chega, pergunta o status enquanto espera e avisa uma vez quando é
 * pago. Serve à janela do painel e à página do link de pagamento.
 */
export function usePixCheckout(invoiceId: string | null, api: PixApi, onPaid: () => void) {
  const { notify } = useToast();
  const [charge, setCharge] = useState<PixCharge | null>(null);
  const [error, setError] = useState('');
  const notified = useRef(false);

  const create = useMutation({
    mutationFn: api.invoicePix,
    onSuccess: (created) => setCharge(created),
    onError: (err: Error) => setError(err.message),
  });
  const start = () => {
    if (!invoiceId) return;
    setCharge(null);
    setError('');
    create.mutate(invoiceId);
  };

  useEffect(() => {
    notified.current = false;
    if (invoiceId) start();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [invoiceId]);

  const paid = Boolean(charge && (charge.status === 'PAID' || charge.invoiceStatus === 'PAID'));
  const ended = Boolean(charge && ['EXPIRED', 'CANCELLED', 'REFUNDED'].includes(charge.status));

  const status = useQuery({
    queryKey: ['pix-charge', charge?.id],
    queryFn: () => api.charge(charge!.id),
    enabled: Boolean(invoiceId && charge) && !paid && !ended,
    refetchInterval: POLL_MS,
  });
  useEffect(() => {
    if (status.data) setCharge(status.data);
  }, [status.data]);

  useEffect(() => {
    if (paid && !notified.current) {
      notified.current = true;
      onPaid();
    }
  }, [paid, onPaid]);

  const simulate = useMutation({
    mutationFn: () => api.simulateCharge(charge!.id),
    onSuccess: (updated) => setCharge(updated),
    onError: (err: Error) => notify({ tone: 'error', title: 'Simulação falhou', description: err.message }),
  });

  const copy = async () => {
    if (!charge) return;
    try {
      await navigator.clipboard.writeText(charge.brCode);
      notify({ tone: 'success', title: 'Código Pix copiado', description: 'Cole no app do seu banco para pagar.' });
    } catch {
      notify({ tone: 'error', title: 'Não foi possível copiar', description: 'Selecione o código e copie manualmente.' });
    }
  };

  return { charge, error, paid, ended, creating: create.isPending, start, simulate, copy };
}

export type PixCheckoutState = ReturnType<typeof usePixCheckout>;

/** O QR Code, o copia-e-cola e a confirmação (sem a moldura). */
export function PixCheckout({
  state,
  description,
  paidText,
}: {
  state: PixCheckoutState;
  description?: string;
  /** O texto do fim, quando o pagamento é confirmado. */
  paidText: string;
}) {
  const { charge, error, paid, ended, creating, start, simulate, copy } = state;
  return (
    <>
      {creating && <Spinner label="Gerando o Pix…" />}

      {error && (
        <div className={styles.pix}>
          <p>{error}</p>
          <Button variant="primary" onClick={start}>
            Tentar de novo
          </Button>
        </div>
      )}

      {charge && paid && (
        <div className={styles.pix} role="status">
          <div className={styles.pixDone} aria-hidden="true">
            ✓
          </div>
          <div className={styles.pixAmount}>{formatMoney(charge.amountCents)}</div>
          <p>{paidText}</p>
        </div>
      )}

      {charge && ended && !paid && (
        <div className={styles.pix}>
          <p>Este Pix expirou antes do pagamento. Gere um novo para pagar a fatura.</p>
          <Button variant="primary" onClick={start}>
            Gerar novo Pix
          </Button>
        </div>
      )}

      {charge && !paid && !ended && (
        <div className={styles.pix}>
          <div>
            <div className={styles.pixAmount}>{formatMoney(charge.amountCents)}</div>
            {description && <div className={styles.muted}>{description}</div>}
          </div>

          <img className={styles.pixQr} src={charge.qrCodeImage} alt="QR Code do Pix" />

          <div className={styles.pixCopy}>
            <input
              className={fieldStyles.input}
              readOnly
              value={charge.brCode}
              aria-label="Pix copia e cola"
              onFocus={(event) => event.target.select()}
            />
            <Button variant="primary" onClick={() => void copy()}>
              Copiar
            </Button>
          </div>

          <ol className={styles.pixSteps}>
            <li>Abra o app do seu banco e escolha pagar com Pix.</li>
            <li>Leia o QR Code ou cole o código copiado.</li>
            <li>Confirme o pagamento — a confirmação aparece aqui sozinha.</li>
          </ol>

          <div className={styles.pixWaiting}>
            <span className={styles.pixSpinner} aria-hidden="true" />
            Aguardando pagamento
            {charge.expiresAt && ` · válido até ${formatDateTime(charge.expiresAt).slice(0, 17)}`}
          </div>

          {charge.devMode && (
            <div className={styles.pixSandbox}>
              <span>
                <strong>Ambiente de testes.</strong> Este Pix é do sandbox da AbacatePay: nenhum
                dinheiro de verdade é movimentado.
              </span>
              <Button size="small" variant="secondary" loading={simulate.isPending} onClick={() => simulate.mutate()}>
                Simular pagamento
              </Button>
            </div>
          )}
        </div>
      )}
    </>
  );
}

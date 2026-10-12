import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';

import type { PixApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import buttonStyles from '@/components/ui/Button.module.css';
import fieldStyles from '@/components/ui/Field.module.css';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateOnly, formatDateTime, formatMoney } from '@/services/format';
import type { ChargeMethod, PixCharge } from '@/types';

import styles from './Billing.module.css';

/** De quanto em quanto tempo a tela pergunta se a cobrança foi paga. */
const POLL_MS = 4000;

const METHODS: { id: ChargeMethod; label: string; hint: string }[] = [
  { id: 'PIX', label: 'Pix', hint: 'confirma na hora' },
  { id: 'BOLETO', label: 'Boleto', hint: 'compensa em até 3 dias úteis' },
];

/** A linha digitável em blocos, como sai impressa: "00190.00009 02759.288002 19329.781701 8 78900000050000". */
export function formatBoletoLine(line: string): string {
  const d = (line ?? '').replace(/\D/g, '');
  if (d.length !== 47) return line;
  return `${d.slice(0, 5)}.${d.slice(5, 10)} ${d.slice(10, 15)}.${d.slice(15, 21)} ${d.slice(21, 26)}.${d.slice(26, 32)} ${d.slice(32, 33)} ${d.slice(33)}`;
}

const isPaid = (c: PixCharge) => c.status === 'PAID' || c.invoiceStatus === 'PAID';

/**
 * O pagamento de uma fatura por Pix ou boleto (que traz um Pix junto): gera
 * (ou reaproveita) o Pix quando a fatura chega e o boleto quando a pessoa
 * escolhe, pergunta o status do que está na tela enquanto espera e avisa uma
 * vez quando é pago. Serve à janela do painel e à página do link de
 * pagamento.
 */
export function usePixCheckout(invoiceId: string | null, api: PixApi, onPaid: () => void) {
  const { notify } = useToast();
  const [method, setMethod] = useState<ChargeMethod>('PIX');
  const [charges, setCharges] = useState<Partial<Record<ChargeMethod, PixCharge>>>({});
  const [errors, setErrors] = useState<Partial<Record<ChargeMethod, string>>>({});
  const notified = useRef(false);

  const keep = (updated: PixCharge) => setCharges((all) => ({ ...all, [updated.method || 'PIX']: updated }));
  const fail = (m: ChargeMethod) => (err: Error) => setErrors((all) => ({ ...all, [m]: err.message }));
  const creators = {
    PIX: useMutation({ mutationFn: api.invoicePix, onSuccess: keep, onError: fail('PIX') }),
    BOLETO: useMutation({ mutationFn: api.invoiceBoleto, onSuccess: keep, onError: fail('BOLETO') }),
  };
  const start = (m: ChargeMethod = method) => {
    if (!invoiceId) return;
    setCharges((all) => ({ ...all, [m]: undefined }));
    setErrors((all) => ({ ...all, [m]: '' }));
    creators[m].mutate(invoiceId);
  };
  /** Troca entre Pix e boleto; o que ainda não foi gerado, gera. */
  const choose = (m: ChargeMethod) => {
    setMethod(m);
    if (!charges[m] && !errors[m] && !creators[m].isPending) start(m);
  };

  useEffect(() => {
    notified.current = false;
    setMethod('PIX');
    setCharges({});
    setErrors({});
    if (invoiceId) start('PIX');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [invoiceId]);

  const charge = charges[method] ?? null;
  // Paga por qualquer um (o Pix, o boleto ou o Pix do boleto), a fatura quitou.
  const paidCharge = Object.values(charges).find((c): c is PixCharge => Boolean(c && isPaid(c))) ?? null;
  const paid = paidCharge !== null;
  const ended = Boolean(charge && ['EXPIRED', 'CANCELLED', 'REFUNDED'].includes(charge.status));

  const status = useQuery({
    queryKey: ['pix-charge', charge?.id],
    queryFn: () => api.charge(charge!.id),
    enabled: Boolean(invoiceId && charge) && !paid && !ended,
    refetchInterval: POLL_MS,
  });
  useEffect(() => {
    if (status.data) keep(status.data);
  }, [status.data]);

  useEffect(() => {
    if (paid && !notified.current) {
      notified.current = true;
      onPaid();
    }
  }, [paid, onPaid]);

  const simulate = useMutation({
    mutationFn: () => api.simulateCharge(charge!.id),
    onSuccess: keep,
    onError: (err: Error) => notify({ tone: 'error', title: 'Simulação falhou', description: err.message }),
  });

  const copy = async (text: string, title: string, description: string) => {
    try {
      await navigator.clipboard.writeText(text);
      notify({ tone: 'success', title, description });
    } catch {
      notify({ tone: 'error', title: 'Não foi possível copiar', description: 'Selecione o código e copie manualmente.' });
    }
  };

  return {
    method,
    choose,
    charge,
    error: errors[method] ?? '',
    paid,
    paidCharge,
    ended,
    creating: creators[method].isPending,
    start,
    simulate,
    copy,
  };
}

export type PixCheckoutState = ReturnType<typeof usePixCheckout>;

/** A escolha da forma, o Pix ou o boleto, e a confirmação (sem a moldura). */
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
  const { method, choose, charge, error, paid, paidCharge, ended, creating, start } = state;
  const boleto = method === 'BOLETO';
  return (
    <>
      {!paid && (
        <div className={styles.methods} role="radiogroup" aria-label="Forma de pagamento">
          {METHODS.map((m) => (
            <button
              key={m.id}
              type="button"
              role="radio"
              aria-checked={method === m.id}
              className={`${styles.method} ${method === m.id ? styles.methodOn : ''}`}
              onClick={() => choose(m.id)}
            >
              <strong>{m.label}</strong>
              <span>{m.hint}</span>
            </button>
          ))}
        </div>
      )}

      {creating && <Spinner label={boleto ? 'Gerando o boleto…' : 'Gerando o Pix…'} />}

      {error && !paid && (
        <div className={styles.pix}>
          <p>{error}</p>
          <div className={styles.pixActions}>
            <Button variant="primary" onClick={() => start()}>
              Tentar de novo
            </Button>
            {boleto && <Button onClick={() => choose('PIX')}>Pagar com Pix</Button>}
          </div>
        </div>
      )}

      {paidCharge && (
        <div className={styles.pix} role="status">
          <div className={styles.pixDone} aria-hidden="true">
            ✓
          </div>
          <div className={styles.pixAmount}>{formatMoney(paidCharge.amountCents)}</div>
          <p>{paidText}</p>
        </div>
      )}

      {charge && ended && !paid && (
        <div className={styles.pix}>
          <p>
            {boleto
              ? 'Este boleto venceu sem pagamento. Gere um novo para pagar a fatura.'
              : 'Este Pix expirou antes do pagamento. Gere um novo para pagar a fatura.'}
          </p>
          <Button variant="primary" onClick={() => start()}>
            {boleto ? 'Gerar novo boleto' : 'Gerar novo Pix'}
          </Button>
        </div>
      )}

      {charge && !paid && !ended && (boleto ? <BoletoView state={state} charge={charge} description={description} /> : <PixView state={state} charge={charge} description={description} />)}
    </>
  );
}

function PixView({ state, charge, description }: { state: PixCheckoutState; charge: PixCharge; description?: string }) {
  const { simulate, copy } = state;
  return (
    <div className={styles.pix}>
      <div>
        <div className={styles.pixAmount}>{formatMoney(charge.amountCents)}</div>
        {description && <div className={styles.muted}>{description}</div>}
      </div>

      <img className={styles.pixQr} src={charge.qrCodeImage} alt="QR Code do Pix" />

      <PixCode charge={charge} onCopy={() => void copy(charge.brCode, 'Código Pix copiado', 'Cole no app do seu banco para pagar.')} />

      <ol className={styles.pixSteps}>
        <li>Abra o app do seu banco e escolha pagar com Pix.</li>
        <li>Leia o QR Code ou cole o código copiado.</li>
        <li>Confirme o pagamento — a confirmação aparece aqui sozinha.</li>
      </ol>

      <Waiting until={charge.expiresAt ? `válido até ${formatDateTime(charge.expiresAt).slice(0, 17)}` : ''} />

      {charge.devMode && (
        <div className={styles.pixSandbox}>
          <span>
            <strong>Ambiente de testes.</strong> Este Pix é do sandbox da AbacatePay: nenhum dinheiro de verdade é
            movimentado.
          </span>
          <Button size="small" variant="secondary" loading={simulate.isPending} onClick={() => simulate.mutate()}>
            Simular pagamento
          </Button>
        </div>
      )}
    </div>
  );
}

function BoletoView({ state, charge, description }: { state: PixCheckoutState; charge: PixCharge; description?: string }) {
  const { copy } = state;
  // O boleto vence no fim do dia do vencimento (o expiresAt é desse dia, em UTC).
  const due = charge.expiresAt ? formatDateOnly(charge.expiresAt.slice(0, 10)) : '';
  return (
    <div className={styles.pix}>
      <div>
        <div className={styles.pixAmount}>{formatMoney(charge.amountCents)}</div>
        {description && <div className={styles.muted}>{description}</div>}
        {due && <div className={styles.muted}>Boleto com vencimento em {due}</div>}
      </div>

      <div className={styles.boletoLine}>
        <span className={styles.boletoLabel}>Linha digitável</span>
        <code>{formatBoletoLine(charge.boletoLine)}</code>
      </div>
      <div className={styles.pixActions}>
        <Button
          variant="primary"
          onClick={() =>
            void copy((charge.boletoLine ?? '').replace(/\D/g, ''), 'Linha digitável copiada', 'Cole no app do banco, em pagar boleto.')
          }
        >
          Copiar a linha digitável
        </Button>
        {charge.boletoUrl && (
          <a
            className={`${buttonStyles.button} ${buttonStyles.secondary}`}
            href={charge.boletoUrl}
            target="_blank"
            rel="noreferrer noopener"
          >
            Ver e imprimir o boleto
          </a>
        )}
      </div>
      <p className={styles.muted}>
        Pago pelo código de barras, o boleto leva até 3 dias úteis para compensar; a fatura é quitada sozinha quando o
        banco confirmar.
      </p>

      {charge.brCode && (
        <div className={styles.boletoPix}>
          <strong>Ou pague este boleto com Pix: a confirmação é na hora</strong>
          <img className={styles.pixQr} src={charge.qrCodeImage} alt="QR Code do Pix do boleto" />
          <PixCode charge={charge} onCopy={() => void copy(charge.brCode, 'Código Pix copiado', 'Cole no app do seu banco para pagar.')} />
        </div>
      )}

      <Waiting until={due ? `vence em ${due}` : ''} />

      {charge.devMode && (
        <div className={styles.pixSandbox}>
          <span>
            <strong>Ambiente de testes.</strong> Este boleto é do sandbox da AbacatePay: nenhum dinheiro de verdade é
            movimentado, e o sandbox não simula o pagamento de boleto.
          </span>
        </div>
      )}
    </div>
  );
}

function PixCode({ charge, onCopy }: { charge: PixCharge; onCopy: () => void }) {
  return (
    <div className={styles.pixCopy}>
      <input
        className={fieldStyles.input}
        readOnly
        value={charge.brCode}
        aria-label="Pix copia e cola"
        onFocus={(event) => event.target.select()}
      />
      <Button variant="primary" onClick={onCopy}>
        Copiar
      </Button>
    </div>
  );
}

function Waiting({ until }: { until: string }) {
  return (
    <div className={styles.pixWaiting}>
      <span className={styles.pixSpinner} aria-hidden="true" />
      Aguardando pagamento
      {until && ` · ${until}`}
    </div>
  );
}

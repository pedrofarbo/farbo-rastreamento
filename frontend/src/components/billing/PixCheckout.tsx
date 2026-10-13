import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';

import type { PixApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import buttonStyles from '@/components/ui/Button.module.css';
import fieldStyles from '@/components/ui/Field.module.css';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { cardErrorMessage, loadAbacatePay, type AbacateCheckout } from '@/services/abacatepayCard';
import { formatDateOnly, formatDateTime, formatMoney } from '@/services/format';
import type { CardPayInput, ChargeMethod, PixCharge } from '@/types';

import styles from './Billing.module.css';

/** De quanto em quanto tempo a tela pergunta se a cobrança foi paga. */
const POLL_MS = 4000;

const METHODS: { id: ChargeMethod; label: string; hint: string }[] = [
  { id: 'PIX', label: 'Pix', hint: 'confirma na hora' },
  { id: 'BOLETO', label: 'Boleto', hint: 'compensa em até 3 dias úteis' },
  { id: 'CARD', label: 'Cartão', hint: 'crédito, à vista' },
];

/** A linha digitável em blocos, como sai impressa: "00190.00009 02759.288002 19329.781701 8 78900000050000". */
export function formatBoletoLine(line: string): string {
  const d = (line ?? '').replace(/\D/g, '');
  if (d.length !== 47) return line;
  return `${d.slice(0, 5)}.${d.slice(5, 10)} ${d.slice(10, 15)}.${d.slice(15, 21)} ${d.slice(21, 26)}.${d.slice(26, 32)} ${d.slice(32, 33)} ${d.slice(33)}`;
}

// O cartão quita já aprovado (o PAID dele é o repasse da adquirente, depois).
const isPaid = (c: PixCharge) =>
  c.status === 'PAID' || (c.method === 'CARD' && c.status === 'APPROVED') || c.invoiceStatus === 'PAID';

export interface CheckoutOptions {
  /** O total da fatura (ou do grupo): o cartão só aparece sabendo o valor (o formulário cobra exatamente ele). */
  amountCents?: number;
  /** A forma que a janela abre (na contratação, a que a pessoa escolheu). */
  initialMethod?: ChargeMethod;
  /** A autorização do débito automático já vem marcada (quem escolheu cartão na contratação). */
  autoDebit?: boolean;
}

/**
 * O pagamento de uma fatura por Pix, boleto (que traz um Pix junto) ou
 * cartão: gera (ou reaproveita) o Pix quando a fatura chega e o boleto quando
 * a pessoa escolhe; o cartão é cobrado quando ela confirma o formulário.
 * Pergunta o status do que está na tela enquanto espera e avisa uma vez
 * quando é pago. Serve à janela do painel e à página do link de pagamento
 * (que não tem cartão: o formulário pede os dados do pagador).
 */
export function usePixCheckout(invoiceId: string | null, api: PixApi, onPaid: () => void, options: CheckoutOptions = {}) {
  const { notify } = useToast();
  const initial = options.initialMethod ?? 'PIX';
  const [method, setMethod] = useState<ChargeMethod>(initial);
  const [charges, setCharges] = useState<Partial<Record<ChargeMethod, PixCharge>>>({});
  const [errors, setErrors] = useState<Partial<Record<ChargeMethod, string>>>({});
  const notified = useRef(false);

  const keep = (updated: PixCharge) => setCharges((all) => ({ ...all, [updated.method || 'PIX']: updated }));
  const fail = (m: ChargeMethod) => (err: Error) => setErrors((all) => ({ ...all, [m]: err.message }));
  const creators = {
    PIX: useMutation({ mutationFn: api.invoicePix, onSuccess: keep, onError: fail('PIX') }),
    BOLETO: useMutation({ mutationFn: api.invoiceBoleto, onSuccess: keep, onError: fail('BOLETO') }),
  };
  const amount = options.amountCents ?? 0;
  const card = useQuery({
    queryKey: ['me', 'card'],
    queryFn: () => api.card!(),
    enabled: Boolean(invoiceId && api.card && api.invoiceCard && amount >= 100),
  });
  const cardOn = Boolean(card.data?.enabled && api.invoiceCard && amount >= 100);
  const payCard = useMutation({
    mutationFn: (input: CardPayInput) => api.invoiceCard!(invoiceId!, input),
    onSuccess: (charged) => {
      keep(charged);
      card.refetch();
    },
    onError: fail('CARD'),
  });

  const start = (m: ChargeMethod = method) => {
    if (!invoiceId) return;
    setCharges((all) => ({ ...all, [m]: undefined }));
    setErrors((all) => ({ ...all, [m]: '' }));
    if (m !== 'CARD') creators[m].mutate(invoiceId);
  };
  /** Troca de forma; o Pix e o boleto que ainda não foram gerados, gera (o cartão espera o formulário). */
  const choose = (m: ChargeMethod) => {
    setMethod(m);
    if (m !== 'CARD' && !charges[m] && !errors[m] && !creators[m].isPending) start(m);
  };

  useEffect(() => {
    notified.current = false;
    setMethod(initial);
    setCharges({});
    setErrors({});
    if (invoiceId && initial !== 'CARD') start(initial);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [invoiceId]);
  // Pediu cartão, mas ele não está disponível: volta ao Pix.
  useEffect(() => {
    if (method === 'CARD' && invoiceId && !cardOn && (card.isFetched || !api.card)) choose('PIX');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [method, cardOn, card.isFetched, invoiceId]);

  const charge = charges[method] ?? null;
  // Paga por qualquer um (o Pix, o boleto, o Pix do boleto ou o cartão), a fatura quitou.
  const paidCharge = Object.values(charges).find((c): c is PixCharge => Boolean(c && isPaid(c))) ?? null;
  const paid = paidCharge !== null;
  const ended = Boolean(charge && ['EXPIRED', 'CANCELLED', 'REFUNDED', 'FAILED'].includes(charge.status));

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
    methods: METHODS.filter((m) => m.id !== 'CARD' || cardOn),
    charge,
    error: errors[method] ?? '',
    paid,
    paidCharge,
    ended,
    creating: method === 'CARD' ? false : creators[method].isPending,
    start,
    simulate,
    copy,
    card: card.data ?? null,
    amountCents: amount,
    autoDebitDefault: Boolean(options.autoDebit),
    payCard,
  };
}

export type PixCheckoutState = ReturnType<typeof usePixCheckout>;

/** A escolha da forma, o Pix, o boleto ou o cartão, e a confirmação (sem a moldura). */
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
  const { method, methods, choose, charge, error, paid, paidCharge, ended, creating, start } = state;
  const boleto = method === 'BOLETO';
  const cardMethod = method === 'CARD';
  return (
    <>
      {!paid && (
        <div
          className={`${styles.methods} ${methods.length === 3 ? styles.methods3 : ''}`}
          role="radiogroup"
          aria-label="Forma de pagamento"
        >
          {methods.map((m) => (
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

      {error && !paid && !cardMethod && (
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

      {cardMethod && !paid && <CardPay state={state} description={description} />}

      {charge && ended && !paid && !cardMethod && (
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

      {charge && !paid && !ended && !cardMethod && (boleto ? <BoletoView state={state} charge={charge} description={description} /> : <PixView state={state} charge={charge} description={description} />)}
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

/**
 * O cartão: o salvo da conta ou o formulário da AbacatePay (o iframe dela,
 * montado aqui), e a autorização do débito automático. Sem resposta do banco
 * ainda, espera a confirmação, que aparece sozinha.
 */
function CardPay({ state, description }: { state: PixCheckoutState; description?: string }) {
  const { card, charge, error, payCard, amountCents, autoDebitDefault, start } = state;
  const saved = card?.saved ?? null;
  const [useSaved, setUseSaved] = useState(Boolean(saved));
  const [autoDebit, setAutoDebit] = useState(autoDebitDefault);
  const [formError, setFormError] = useState('');
  const [ready, setReady] = useState(false);
  const [tokenizing, setTokenizing] = useState(false);
  const host = useRef<HTMLDivElement>(null);
  const checkout = useRef<AbacateCheckout | null>(null);
  const waiting = Boolean(charge && (charge.status === 'CREATING' || charge.status === 'PENDING'));
  const showForm = Boolean(card?.payer) && !useSaved && !waiting;

  useEffect(() => {
    setUseSaved(Boolean(saved));
  }, [saved?.last4]); // eslint-disable-line react-hooks/exhaustive-deps

  // Monta o formulário (o iframe da AbacatePay) quando ele aparece.
  useEffect(() => {
    if (!showForm || !card || !host.current) return;
    let alive = true;
    const target = host.current;
    setReady(false);
    setFormError('');
    loadAbacatePay()
      .then((factory) => {
        if (!alive) return undefined;
        const instance = factory.create({ publicKey: card.publicKey, amount: amountCents, locale: 'pt', icons: true });
        checkout.current = instance;
        return instance.mount(target).then(() => {
          if (alive) setReady(true);
        });
      })
      .catch((err) => {
        if (alive) setFormError(cardErrorMessage(err));
      });
    return () => {
      alive = false;
      checkout.current?.destroy();
      checkout.current = null;
    };
  }, [showForm, card?.publicKey, amountCents]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!card) return <Spinner label="Carregando o cartão…" />;
  if (!card.payer) {
    return (
      <div className={styles.pix}>
        <p>
          Para pagar com cartão, o cadastro da conta precisa do CPF ou CNPJ, do e-mail e do celular. Complete em Conta,
          nos seus dados, ou pague com Pix ou boleto.
        </p>
      </div>
    );
  }
  if (waiting && charge) {
    return (
      <div className={styles.pix}>
        <div className={styles.pixAmount}>{formatMoney(charge.amountCents)}</div>
        <Waiting until="" label="Confirmando com o banco" />
        <p className={styles.muted}>Não precisa pagar de novo: a confirmação aparece aqui sozinha.</p>
      </div>
    );
  }

  const pay = async () => {
    setFormError('');
    start('CARD');
    if (useSaved) {
      payCard.mutate({ useSaved: true });
      return;
    }
    if (!checkout.current || !card.payer) return;
    setTokenizing(true);
    try {
      const { token } = await checkout.current.tokenize({
        name: card.payer.name,
        email: card.payer.email,
        taxId: card.payer.taxId,
        cellphone: card.payer.cellphone,
      });
      payCard.mutate({ token, save: autoDebit, autoDebit });
    } catch (err) {
      setFormError(cardErrorMessage(err));
    } finally {
      setTokenizing(false);
    }
  };
  const message =
    formError || error || (charge?.status === 'FAILED' ? 'O cartão foi recusado. Confira os dados ou use outro cartão.' : '');

  return (
    <div className={styles.pix}>
      <div>
        <div className={styles.pixAmount}>{formatMoney(amountCents)}</div>
        {description && <div className={styles.muted}>{description}</div>}
      </div>

      {saved && (
        <div className={styles.cardChoice} role="radiogroup" aria-label="Qual cartão">
          <label>
            <input type="radio" name="card-choice" checked={useSaved} onChange={() => setUseSaved(true)} />
            {saved.brand} final {saved.last4}
          </label>
          <label>
            <input type="radio" name="card-choice" checked={!useSaved} onChange={() => setUseSaved(false)} />
            Outro cartão
          </label>
        </div>
      )}

      {showForm && (
        <>
          <div ref={host} className={styles.cardForm} />
          {!ready && !formError && <Spinner label="Abrindo o formulário do cartão…" />}
          <label className={styles.cardConsent}>
            <input type="checkbox" checked={autoDebit} onChange={(e) => setAutoDebit(e.target.checked)} />
            <span>
              <strong>Débito automático:</strong> guardar este cartão e cobrar nele as próximas faturas da conta, no dia
              do vencimento (se o banco recusar, tenta de novo nos 2 dias seguintes). Dá para desligar quando quiser, em
              Faturas.
            </span>
          </label>
        </>
      )}

      {message && (
        <p className={styles.cardError} role="alert">
          {message}
        </p>
      )}
      <Button
        variant="primary"
        block
        loading={payCard.isPending || tokenizing}
        disabled={!useSaved && !ready}
        onClick={() => void pay()}
      >
        Pagar {formatMoney(amountCents)}
      </Button>
      <p className={styles.muted}>
        Os dados do cartão vão direto para a AbacatePay, num formulário seguro: não passam pela Farbo. Se o banco pedir
        confirmação, ela abre numa janela dele.
      </p>
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

function Waiting({ until, label = 'Aguardando pagamento' }: { until: string; label?: string }) {
  return (
    <div className={styles.pixWaiting}>
      <span className={styles.pixSpinner} aria-hidden="true" />
      {label}
      {until && ` · ${until}`}
    </div>
  );
}

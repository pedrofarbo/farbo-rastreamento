import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ApiError } from '@/api/client';
import { financeApi, shippingIntegrationApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Field, TextField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { useToast } from '@/components/ui/Toast';
import { centsToInput, formatMoney, parseMoney } from '@/services/format';
import { PixPayModal } from '@/pages/admin/company/PixModals';
import type { FinanceEntry, ShippingBalance, ShippingTopUp, ShippingTopUpMethod } from '@/types';

import styles from './Fulfillment.module.css';

export const walletKey = ['integration', 'melhorenvio', 'balance'] as const;

const PRESETS = [5000, 10000, 20000, 50000];
const MIN_CENTS = 100;
const MAX_CENTS = 1_000_000;
// Gerada a cobrança, a tela confere o saldo sozinha por um tempo.
const WATCH_EVERY_MS = 15_000;
const WATCH_FOR_MS = 15 * 60_000;

const codeOf = (err: unknown) =>
  err instanceof ApiError ? (err.body as { code?: string } | undefined)?.code : undefined;

/** Quanto pôr para cobrir o que falta: arredonda para cima, de R$ 10 em R$ 10. */
export function suggestTopUp(missingCents: number): number {
  if (missingCents <= 0) return 10000;
  return Math.max(1000, Math.ceil(missingCents / 1000) * 1000);
}

export function useShippingBalance(enabled = true) {
  return useQuery({
    queryKey: walletKey,
    queryFn: shippingIntegrationApi.balance,
    enabled,
    retry: false,
    staleTime: 30_000,
  });
}

/**
 * A carteira do Melhor Envios, de onde sai o pagamento das etiquetas: o
 * saldo e o "Adicionar saldo" (Pix ou boleto, pagos no Melhor Envios).
 * Com needCents (o preço da etiqueta escolhida), avisa quando não cobre.
 */
export function ShippingWallet({
  panelUrl,
  needCents,
  payWithAbacate = false,
}: {
  panelUrl: string;
  needCents?: number | null;
  /** A recarga por Pix pode sair do saldo da AbacatePay. */
  payWithAbacate?: boolean;
}) {
  const balance = useShippingBalance();
  const [adding, setAdding] = useState(false);
  const data = balance.data;
  const missing = data && needCents ? needCents - data.balanceCents : 0;

  return (
    <div className={styles.wallet}>
      <div className={styles.walletRow}>
        <div className={styles.walletInfo}>
          <span className={styles.actionTitle}>Saldo na carteira</span>
          {data ? (
            <>
              <strong className={styles.walletValue}>{formatMoney(data.balanceCents)}</strong>
              <span className={styles.hint}>{walletDetails(data)}</span>
            </>
          ) : (
            <span className={styles.hint}>{balance.isLoading ? 'Consultando o Melhor Envios…' : 'Indisponível agora'}</span>
          )}
        </div>
        <div className={styles.actionRow}>
          <Button size="small" variant="ghost" loading={balance.isFetching} onClick={() => void balance.refetch()}>
            Atualizar
          </Button>
          {!adding && (
            <Button size="small" variant={missing > 0 ? 'primary' : 'secondary'} onClick={() => setAdding(true)}>
              Adicionar saldo
            </Button>
          )}
        </div>
      </div>

      {balance.error &&
        (codeOf(balance.error) === 'BALANCE_FORBIDDEN' ? (
          <div className={styles.warning}>
            O Melhor Envios ainda não liberou o saldo para o sistema. Para liberar, desconecte e conecte o Melhor Envios
            de novo (no quadro do Melhor Envios, em Pedidos) e autorize. Enquanto isso, o saldo está no{' '}
            <a href={panelUrl} target="_blank" rel="noopener noreferrer" className={styles.labelLink}>
              painel do Melhor Envios
            </a>
            .
          </div>
        ) : (
          <div className={styles.warning}>Não deu para consultar o saldo: {(balance.error as Error).message}</div>
        ))}

      {missing > 0 && (
        <div className={styles.warning} role="status">
          Saldo insuficiente para esta etiqueta: faltam <strong>{formatMoney(missing)}</strong>.
        </div>
      )}

      {adding && (
        <TopUpForm
          panelUrl={panelUrl}
          payWithAbacate={payWithAbacate}
          suggestedCents={missing > 0 ? suggestTopUp(missing) : undefined}
          onClose={() => setAdding(false)}
        />
      )}
    </div>
  );
}

function walletDetails(b: ShippingBalance): string {
  const parts: string[] = [];
  if (b.reservedCents > 0) parts.push(`${formatMoney(b.reservedCents)} reservado`);
  if (b.debtsCents > 0) parts.push(`${formatMoney(b.debtsCents)} em débito`);
  const at = new Date(b.checkedAt).toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' });
  parts.push(`consultado às ${at}`);
  return parts.join(' · ');
}

/**
 * Gera o Pix ou o boleto da recarga. O Pix pode ser pago no Melhor Envios
 * (pelo banco) ou com o saldo da AbacatePay: a recarga vira uma conta a pagar
 * e sai pelo Pix dos fornecedores. Depois de gerado, confere o saldo de tempos
 * em tempos até o valor cair.
 */
function TopUpForm({
  panelUrl,
  payWithAbacate,
  suggestedCents,
  onClose,
}: {
  panelUrl: string;
  payWithAbacate: boolean;
  suggestedCents?: number;
  onClose: () => void;
}) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const [value, setValue] = useState(centsToInput(suggestedCents ?? 10000));
  const [method, setMethod] = useState<ShippingTopUpMethod>('pix');
  const [touched, setTouched] = useState(false);
  const [top, setTop] = useState<ShippingTopUp | null>(null);
  // O saldo de quando a cobrança foi gerada e a hora: base para ver o crédito.
  const [watchFrom, setWatchFrom] = useState<{ cents: number; at: number } | null>(null);
  const [credited, setCredited] = useState<number | null>(null);
  // Pela AbacatePay: o copia-e-cola colado (quando não veio), a conta a
  // pagar aberta na tela de Pix e o envio feito.
  const [pasted, setPasted] = useState('');
  const [bill, setBill] = useState<FinanceEntry | null>(null);
  const [sentByAbacate, setSentByAbacate] = useState(false);

  const cents = parseMoney(value);
  const invalid = cents === null || cents < MIN_CENTS || cents > MAX_CENTS;
  const presets = suggestedCents && !PRESETS.includes(suggestedCents) ? [suggestedCents, ...PRESETS] : PRESETS;

  const create = useMutation({
    mutationFn: () => shippingIntegrationApi.addBalance(cents as number, method),
    onSuccess: (result) => {
      setTop(result);
      setCredited(null);
      setPasted('');
      setSentByAbacate(false);
      const known = queryClient.getQueryData<ShippingBalance>(walletKey);
      setWatchFrom(known ? { cents: known.balanceCents, at: Date.now() } : null);
    },
  });
  const billing = useMutation({
    mutationFn: () => shippingIntegrationApi.topUpEntry((top as ShippingTopUp).id, pasted),
    onSuccess: setBill,
  });

  const watching = top !== null && watchFrom !== null && credited === null && Date.now() - watchFrom.at < WATCH_FOR_MS;
  const watch = useQuery({
    queryKey: walletKey,
    queryFn: shippingIntegrationApi.balance,
    enabled: watching,
    refetchInterval: watching ? WATCH_EVERY_MS : false,
    retry: false,
  });
  useEffect(() => {
    if (!watching || !watch.data || !watchFrom) return;
    if (watch.data.balanceCents > watchFrom.cents) {
      setCredited(watch.data.balanceCents);
      notify({ tone: 'success', title: 'Saldo creditado', description: `A carteira agora tem ${formatMoney(watch.data.balanceCents)}.` });
    }
  }, [watch.data, watching, watchFrom, notify]);

  const open = (link: string) => window.open(link, '_blank', 'noopener,noreferrer');
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      notify({ tone: 'success', title: 'Linha digitável copiada' });
    } catch {
      notify({ tone: 'error', title: 'Não foi possível copiar', description: 'Selecione a linha e copie manualmente.' });
    }
  };

  if (top) {
    const kind = top.method === 'pix' ? 'Pix' : 'Boleto';
    const abacate = payWithAbacate && top.method === 'pix';
    return (
      <div className={styles.topUp}>
        <span className={styles.actionTitle}>Adicionar saldo</span>
        <p className={styles.topUpDone}>
          <strong>
            {kind} de {formatMoney(top.valueCents)} gerado
          </strong>
          {top.protocol ? ` (${top.protocol})` : ''}.{' '}
          {abacate ? 'Pague com o saldo da AbacatePay ou pelo banco.' : 'O pagamento é feito no Melhor Envios.'}
        </p>
        {credited !== null ? (
          <div className={styles.credited} role="status">
            Saldo creditado: a carteira agora tem <strong>{formatMoney(credited)}</strong>.
          </div>
        ) : sentByAbacate ? (
          <div className={styles.credited} role="status">
            Pix enviado pela AbacatePay e lançado nas contas pagas (Frete e envio). O saldo cai em instantes; esta tela
            confere sozinha.
          </div>
        ) : (
          <>
            <div className={styles.actionRow}>
              {abacate && top.pixCode && (
                <Button size="small" variant="primary" loading={billing.isPending} onClick={() => billing.mutate()}>
                  Pagar com o saldo da AbacatePay
                </Button>
              )}
              {top.link && (
                <Button
                  size="small"
                  variant={abacate && top.pixCode ? 'secondary' : 'primary'}
                  onClick={() => open(top.link)}
                >
                  {top.method === 'pix' ? (abacate && top.pixCode ? 'Abrir o Pix' : 'Abrir o Pix para pagar') : 'Abrir o boleto'}
                </Button>
              )}
              {top.digitable && (
                <Button size="small" variant="secondary" onClick={() => void copy(top.digitable)}>
                  Copiar a linha digitável
                </Button>
              )}
              {!top.link && !top.digitable && (
                <a href={top.panelUrl || panelUrl} target="_blank" rel="noopener noreferrer" className={styles.labelLink}>
                  Pagar pela carteira no painel do Melhor Envios
                </a>
              )}
            </div>
            {top.digitable && <code className={styles.digitable}>{top.digitable}</code>}
            {abacate && !top.pixCode && (
              <div className={styles.paste}>
                <Field
                  label="Pagar com o saldo da AbacatePay"
                  hint="Abra o Pix, copie o código do Pix copia-e-cola e cole aqui."
                >
                  {(id) => (
                    <textarea
                      id={id}
                      className={fieldStyles.textarea}
                      rows={3}
                      placeholder="00020126…"
                      value={pasted}
                      onChange={(e) => setPasted(e.target.value)}
                    />
                  )}
                </Field>
                <div className={styles.actionRow}>
                  <Button
                    size="small"
                    variant="secondary"
                    loading={billing.isPending}
                    disabled={!pasted.trim()}
                    onClick={() => billing.mutate()}
                  >
                    Pagar com o saldo da AbacatePay
                  </Button>
                </div>
              </div>
            )}
            {billing.error && (
              <div className={styles.warning} role="alert">
                {(billing.error as Error).message}
              </div>
            )}
            <span className={styles.hint} role="status">
              {watching
                ? top.method === 'pix'
                  ? 'Pago o Pix, o saldo cai em instantes. Esta tela confere sozinha.'
                  : 'O boleto compensa em até 3 dias úteis; o saldo aparece aqui quando cair.'
                : 'Depois de pagar, clique em Atualizar para ver o saldo.'}
            </span>
          </>
        )}
        <div className={styles.actionRow}>
          <Button
            size="small"
            variant="ghost"
            onClick={() => {
              setTop(null);
              setCredited(null);
              setWatchFrom(null);
            }}
          >
            Gerar outra cobrança
          </Button>
          <Button size="small" variant="ghost" onClick={onClose}>
            Fechar
          </Button>
        </div>
        {bill && (
          <PixPayModal
            entry={bill}
            onClose={() => {
              setBill(null);
              // Desistiu sem enviar: a conta não fica sobrando em aberto. Com
              // um Pix em andamento ou sem resposta, o financeiro recusa, e
              // ela fica para conferir em Empresa.
              if (!sentByAbacate) void financeApi.removeEntry(bill.id).catch(() => undefined);
            }}
            onSent={() => {
              setSentByAbacate(true);
              void queryClient.invalidateQueries({ queryKey: walletKey });
            }}
          />
        )}
      </div>
    );
  }

  return (
    <form
      className={styles.topUp}
      onSubmit={(event) => {
        event.preventDefault();
        setTouched(true);
        if (!invalid) create.mutate();
      }}
    >
      <span className={styles.actionTitle}>Adicionar saldo</span>
      <div className={styles.presets} role="group" aria-label="Valores sugeridos">
        {presets.map((c) => (
          <button
            key={c}
            type="button"
            className={`${styles.preset} ${cents === c ? styles.presetActive : ''}`}
            aria-pressed={cents === c}
            onClick={() => setValue(centsToInput(c))}
          >
            {formatMoney(c)}
          </button>
        ))}
      </div>
      <TextField
        label="Valor"
        inputMode="decimal"
        value={value}
        error={touched && invalid ? 'Informe um valor entre R$ 1,00 e R$ 10.000,00.' : undefined}
        onChange={(e) => setValue(e.target.value)}
        onBlur={() => setTouched(true)}
      />
      <div className={styles.quotes} role="radiogroup" aria-label="Forma de pagamento">
        <label className={styles.quote}>
          <input type="radio" name="forma-saldo" checked={method === 'pix'} onChange={() => setMethod('pix')} />
          <span className={styles.quoteName}>
            <strong>Pix</strong>
            <br />
            <span className={styles.hint}>
              {payWithAbacate ? 'Pelo banco ou com o saldo da AbacatePay; o saldo cai em instantes.' : 'O saldo cai em instantes.'}
            </span>
          </span>
        </label>
        <label className={styles.quote}>
          <input type="radio" name="forma-saldo" checked={method === 'boleto'} onChange={() => setMethod('boleto')} />
          <span className={styles.quoteName}>
            <strong>Boleto</strong>
            <br />
            <span className={styles.hint}>Em nome da empresa (CNPJ); compensa em até 3 dias úteis.</span>
          </span>
        </label>
      </div>
      {create.error && (
        <div className={styles.warning} role="alert">
          Não deu para gerar a cobrança: {(create.error as Error).message}
        </div>
      )}
      <div className={styles.actionRow}>
        <Button type="submit" size="small" variant="primary" loading={create.isPending} disabled={touched && invalid}>
          {method === 'pix' ? 'Gerar Pix' : 'Gerar boleto'}
          {!invalid ? ` de ${formatMoney(cents)}` : ''}
        </Button>
        <Button type="button" size="small" variant="ghost" onClick={onClose}>
          Cancelar
        </Button>
      </div>
      <span className={styles.hint}>
        O pagamento é feito no Melhor Envios, e o valor fica na carteira para as próximas etiquetas.
      </span>
    </form>
  );
}

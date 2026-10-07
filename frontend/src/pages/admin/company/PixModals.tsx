import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';

import { financeApi } from '@/api/resources';
import { IdentityCheck } from '@/components/auth/IdentityCheck';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatMoney } from '@/services/format';
import type { FinanceEntry, PixKeyType, PixTransfer } from '@/types';

import { financeKey, useRefreshFinance } from './EntryModals';
import styles from './Company.module.css';

export const KEY_TYPE_LABELS: Record<PixKeyType, string> = {
  CPF: 'CPF',
  CNPJ: 'CNPJ',
  PHONE: 'Celular',
  EMAIL: 'E-mail',
  RANDOM: 'Chave aleatória',
  BR_CODE: 'Pix copia-e-cola',
};

/**
 * O tipo da chave pelo formato (o servidor faz igual). Vazio quando não dá
 * para saber: 11 dígitos soltos podem ser CPF ou celular.
 */
export function detectKeyType(key: string): '' | PixKeyType {
  const k = key.trim();
  const d = k.replace(/\D/g, '');
  if (!k) return '';
  if (k.includes('@')) return 'EMAIL';
  if (/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(k)) return 'RANDOM';
  if (/^\d{3}\.\d{3}\.\d{3}-\d{2}$/.test(k)) return 'CPF';
  if (/^\d{2}\.\d{3}\.\d{3}\/\d{4}-\d{2}$/.test(k) || (d === k && d.length === 14)) return 'CNPJ';
  if ((k.startsWith('+') || /[() ]/.test(k) || (d === k && d.length === 10)) && /^\+?[\d\s().-]+$/.test(k)) return 'PHONE';
  return '';
}


/** A chave como se confere a olho: o copia-e-cola fica curto. */
export function keyPreview(key: string, type: PixKeyType): string {
  if (type === 'BR_CODE') return `${key.slice(0, 24)}…${key.slice(-8)}`;
  if (type === 'CPF' && key.length === 11) return `${key.slice(0, 3)}.${key.slice(3, 6)}.${key.slice(6, 9)}-${key.slice(9)}`;
  if (type === 'CNPJ' && key.length === 14)
    return `${key.slice(0, 2)}.${key.slice(2, 5)}.${key.slice(5, 8)}/${key.slice(8, 12)}-${key.slice(12)}`;
  if (type === 'PHONE' && key.length >= 10) return `(${key.slice(0, 2)}) ${key.slice(2, -4)}-${key.slice(-4)}`;
  return key;
}

/**
 * Pagar a conta por Pix pela AbacatePay: confere o destino, o valor e o
 * saldo; confirma com a senha (ou a biometria) e envia. O dinheiro sai da
 * conta da AbacatePay na hora.
 */
export function PixPayModal({
  entry,
  onClose,
  onSent,
}: {
  entry: FinanceEntry;
  onClose: () => void;
  /** Avisado quando o Pix sai (quem abriu acompanha o que vem depois). */
  onSent?: (transfer: PixTransfer) => void;
}) {
  const { notify } = useToast();
  const refresh = useRefreshFinance();
  const [step, setStep] = useState<'review' | 'confirm' | 'done'>('review');
  const [error, setError] = useState('');
  const [result, setResult] = useState<PixTransfer | null>(null);
  const plan = useQuery({ queryKey: [...financeKey, 'pix-plan', entry.id], queryFn: () => financeApi.entryPix(entry.id) });
  const info = useQuery({ queryKey: [...financeKey, 'pix-info'], queryFn: financeApi.pixInfo });

  const send = useMutation({
    mutationFn: (token: string) => financeApi.sendPix(entry.id, token),
    onSuccess: (sent) => {
      refresh();
      setResult(sent);
      setStep('done');
      onSent?.(sent);
      notify({ tone: 'success', title: 'Pix enviado', description: `${formatMoney(sent.amountCents)} · ${entry.description}` });
    },
    onError: (err: Error) => {
      refresh();
      setError(err.message);
      setStep('review');
      void plan.refetch();
    },
  });

  const p = plan.data?.plan ?? null;
  const available = info.data?.availableCents ?? null;
  // O saldo que a API da AbacatePay informa pode vir atrasado (em produção,
  // ficou em zero com saldo no painel deles): só avisa. Sem saldo de verdade,
  // a AbacatePay recusa o envio e a conta continua em aberto.
  // A AbacatePay desconta a tarifa do valor enviado: o Pix leva a conta + a
  // tarifa, para o fornecedor receber a conta inteira.
  const short = p !== null && available !== null && available < p.sendCents;

  return (
    <Modal
      open
      title="Pagar por Pix"
      onClose={send.isPending ? () => undefined : onClose}
      footer={
        step === 'done' ? (
          <Button variant="primary" onClick={onClose}>
            Fechar
          </Button>
        ) : step === 'review' ? (
          <>
            <Button variant="ghost" onClick={onClose}>
              Cancelar
            </Button>
            <Button variant="primary" disabled={!p} onClick={() => setStep('confirm')}>
              Continuar
            </Button>
          </>
        ) : (
          <Button variant="ghost" disabled={send.isPending} onClick={() => setStep('review')}>
            Voltar
          </Button>
        )
      }
    >
      <div className={styles.pix}>
        {info.data?.devMode && (
          <p className={styles.pixDev}>Modo de testes da AbacatePay: o Pix é simulado, nenhum dinheiro sai.</p>
        )}
        {plan.isLoading ? (
          <Spinner label="Conferindo" />
        ) : step === 'done' && result ? (
          <div className={styles.pixDone} role="status">
            <strong>Pix enviado: {formatMoney(result.deliveredCents || result.amountCents)} para o fornecedor</strong>
            <span>
              A conta foi baixada como paga
              {result.feeCents > 0 ? ` e a tarifa de ${formatMoney(result.feeCents)} entrou nas contas pagas` : ''}.
            </span>
            {result.deliveredCents > 0 && result.deliveredCents < result.amountCents && (
              <span className={styles.warn}>
                A AbacatePay cobrou uma tarifa maior que a prevista: chegaram{' '}
                {formatMoney(result.deliveredCents)}, {formatMoney(result.amountCents - result.deliveredCents)} a menos
                que a conta. Lance a diferença como outra conta e pague-a.
              </span>
            )}
            {result.receiptUrl && (
              <a href={result.receiptUrl} target="_blank" rel="noopener noreferrer">
                Ver o comprovante
              </a>
            )}
          </div>
        ) : !p ? (
          <p className={styles.danger} role="alert">
            {plan.data?.problem || (plan.error as Error | null)?.message || 'Não dá para pagar esta conta por Pix.'}
          </p>
        ) : (
          <>
            <dl className={styles.pixFacts}>
              <dt>Conta</dt>
              <dd>{p.description}</dd>
              <dt>Para</dt>
              <dd>
                {p.source === 'code' ? p.recipient || p.supplierName || 'o recebedor do copia-e-cola' : p.supplierName}
              </dd>
              <dt>{KEY_TYPE_LABELS[p.keyType]}</dt>
              <dd className={styles.pixKey}>{keyPreview(p.key, p.keyType)}</dd>
              <dt>Recebe</dt>
              <dd>
                <strong>{formatMoney(p.amountCents)}</strong>
                <span className={styles.muted}> (o valor da conta, inteiro)</span>
              </dd>
              <dt>Tarifa</dt>
              <dd>
                {formatMoney(p.feeCents)}
                <span className={styles.muted}>
                  {' '}
                  da AbacatePay, paga por você ({p.feeCents > 80 ? 'a partir do 21º envio do mês' : 'até o 20º envio do mês'})
                </span>
              </dd>
              <dt>Sai do saldo</dt>
              <dd>
                <strong>{formatMoney(p.sendCents)}</strong>
              </dd>
              <dt>Saldo</dt>
              <dd className={short ? styles.warn : undefined}>
                {available !== null
                  ? short
                    ? `A API da AbacatePay informa ${formatMoney(available)}, mas pode estar atrasada: confira no painel dela. Sem saldo, o envio é recusado e a conta continua em aberto.`
                    : `${formatMoney(available)} disponível na AbacatePay`
                  : `não deu para consultar${info.data?.balanceError ? ` (${info.data.balanceError})` : ''}`}
              </dd>
            </dl>
            {step === 'review' && (
              <p className={styles.muted}>
                Confira a chave: o Pix sai na hora e não volta sozinho. Depois do envio, a conta é baixada como paga.
              </p>
            )}
            {error && (
              <p className={styles.danger} role="alert">
                {error}
              </p>
            )}
            {step === 'confirm' &&
              (send.isPending ? (
                <Spinner label="Enviando o Pix" />
              ) : (
                <IdentityCheck
                  purpose="supplier_pix"
                  action={`Para enviar ${formatMoney(p.amountCents)} por Pix`}
                  onGrant={(token) => {
                    setError('');
                    send.mutate(token);
                  }}
                />
              ))}
          </>
        )}
      </div>
    </Modal>
  );
}

/**
 * Um Pix sem resposta da AbacatePay: quem conferiu no painel dela diz se saiu
 * (a conta é baixada) ou não (dá para tentar de novo). Nunca é reenviado
 * sozinho, para não pagar duas vezes.
 */
export function PixResolveModal({ entry, transfer, onClose }: { entry: FinanceEntry; transfer: PixTransfer; onClose: () => void }) {
  const { notify } = useToast();
  const refresh = useRefreshFinance();
  const [providerId, setProviderId] = useState('');
  const resolve = useMutation({
    mutationFn: (sent: boolean) => financeApi.resolvePix(transfer.id, { sent, providerId: sent ? providerId.trim() : '' }),
    onSuccess: (done) => {
      refresh();
      notify({
        tone: 'success',
        title: done.status === 'COMPLETE' ? 'Pix confirmado: conta paga' : 'Pix marcado como não enviado',
        description: entry.description,
      });
      onClose();
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível', description: err.message }),
  });
  return (
    <Modal
      open
      title="Conferir o Pix"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Depois
          </Button>
          <Button variant="secondary" loading={resolve.isPending && resolve.variables === false} onClick={() => resolve.mutate(false)}>
            Não saiu
          </Button>
          <Button variant="primary" loading={resolve.isPending && resolve.variables === true} onClick={() => resolve.mutate(true)}>
            Saiu
          </Button>
        </>
      }
    >
      <div className={styles.pix}>
        <p>
          A AbacatePay não respondeu ao Pix de <strong>{formatMoney(transfer.amountCents)}</strong> para{' '}
          <strong>{keyPreview(transfer.key, transfer.keyType)}</strong> ({entry.description}). Abra o painel da AbacatePay
          e veja se ele aparece nas transações.
        </p>
        <TextField
          label="Id do envio na AbacatePay (opcional)"
          placeholder="tran_..."
          value={providerId}
          onChange={(e) => setProviderId(e.target.value)}
          hint="Com o id, o sistema confere o valor e guarda o comprovante."
        />
        {transfer.error && <p className={styles.muted}>{transfer.error}</p>}
      </div>
    </Modal>
  );
}

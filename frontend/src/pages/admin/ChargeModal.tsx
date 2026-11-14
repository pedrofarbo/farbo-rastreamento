import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';

import { customersApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { Button } from '@/components/ui/Button';
import { Field, fieldStyles } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { whatsappLink } from '@/config/contact';
import { formatMoney, todayISO } from '@/services/format';
import type { CustomerDetail } from '@/types';

import styles from '../Page.module.css';

/** As faturas em aberto do cliente (de qualquer vencimento) e quantas já venceram. */
export function openTotals(detail: Pick<CustomerDetail, 'invoices'>, today = todayISO()) {
  return detail.invoices.reduce(
    (acc, inv) =>
      inv.status === 'OPEN' && inv.amountCents > 0
        ? { count: acc.count + 1, cents: acc.cents + inv.amountCents, overdue: acc.overdue + (inv.dueDate < today ? 1 : 0) }
        : acc,
    { count: 0, cents: 0, overdue: 0 },
  );
}

/**
 * A mensagem pronta da cobrança, com o link de todas em aberto. Só o primeiro
 * nome; sem o cartão (cadastro incompleto), não promete o cartão.
 */
export function chargeMessage(detail: Pick<CustomerDetail, 'name' | 'invoices' | 'payAllUrl' | 'cardMissing'>, today = todayISO()): string {
  const open = openTotals(detail, today);
  const first = detail.name.trim().split(/\s+/)[0] ?? '';
  const card = !detail.cardMissing || detail.cardMissing.length === 0;
  const invoices = open.count === 1 ? '1 fatura em aberto' : `${open.count} faturas em aberto`;
  const overdue = open.overdue === 0 ? '' : open.overdue === 1 ? ' (1 vencida)' : ` (${open.overdue} vencidas)`;
  return (
    `Olá${first ? `, ${first}` : ''}! Aqui é da Farbo Rastreadores. Você tem ${invoices}, no total de ${formatMoney(open.cents)}${overdue}. ` +
    `Pelo link abaixo você paga tudo de uma vez, sem login${card ? ', por Pix, boleto ou cartão' : ''}:\n${detail.payAllUrl ?? ''}`
  );
}

/**
 * Cobrar o cliente: a mensagem pronta (dá para mudar) com o link que paga
 * todas as faturas em aberto, para mandar pelo WhatsApp ou copiar. Abre da
 * lista de clientes e da ficha.
 */
export function ChargeModal({ customerId, onClose }: { customerId: string | null; onClose: () => void }) {
  const detail = useQuery({
    queryKey: ['customer', customerId],
    queryFn: () => customersApi.get(customerId!),
    enabled: customerId !== null,
  });
  const data = customerId && detail.data?.id === customerId ? detail.data : null;
  const [message, setMessage] = useState('');
  const [copied, setCopied] = useState<'message' | 'link' | null>(null);
  const [copyFailed, setCopyFailed] = useState(false);
  useEffect(() => {
    setMessage(data ? chargeMessage(data) : '');
    setCopied(null);
    setCopyFailed(false);
    // A mensagem nasce quando a ficha chega; depois, é do que a central escrever.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data?.id, data?.payAllUrl]);

  const open = data ? openTotals(data) : { count: 0, cents: 0, overdue: 0 };
  const canCharge = !!data?.payAllUrl && open.count > 0;
  const canWhatsapp = !!data && data.phone.replace(/\D/g, '').length >= 10;
  const copy = async (what: 'message' | 'link') => {
    try {
      await navigator.clipboard.writeText(what === 'message' ? message : (data?.payAllUrl ?? ''));
      setCopied(what);
      setCopyFailed(false);
    } catch {
      setCopyFailed(true);
    }
  };

  return (
    <Modal
      open={customerId !== null}
      title={data ? `Cobrar ${data.name}` : 'Cobrar'}
      wide
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Fechar
          </Button>
          {canCharge && (
            <>
              {canWhatsapp && (
                <Button
                  variant="secondary"
                  onClick={() => window.open(whatsappLink(data!.phone, message), '_blank', 'noopener,noreferrer')}
                >
                  Enviar pelo WhatsApp
                </Button>
              )}
              <Button variant="primary" onClick={() => void copy('message')}>
                {copied === 'message' ? 'Mensagem copiada' : 'Copiar mensagem'}
              </Button>
            </>
          )}
        </>
      }
    >
      {!data ? (
        detail.isError ? (
          <p className={billing.muted}>Não deu para carregar o cliente. Feche e tente de novo.</p>
        ) : (
          <Spinner label="Carregando as faturas" />
        )
      ) : !canCharge ? (
        <p className={billing.muted}>{data.name} não tem fatura em aberto.</p>
      ) : (
        <div className={styles.form}>
          <p className={billing.muted}>
            {open.count === 1 ? '1 fatura em aberto' : `${open.count} faturas em aberto`}, {formatMoney(open.cents)}
            {open.overdue > 0 && ` (${open.overdue} vencida${open.overdue > 1 ? 's' : ''})`}. O link paga todas de uma vez e
            continua valendo: mostra o que estiver em aberto quando for aberto.
          </p>
          {data.cardMissing && data.cardMissing.length > 0 && (
            <div className={`${billing.banner} ${billing.bannerWarning}`} role="status">
              <span>
                O cartão não aparece no link: falta {data.cardMissing.join(', ')} no cadastro. O Pix continua valendo.
              </span>
            </div>
          )}
          <Field
            label="Mensagem"
            hint={
              copyFailed
                ? 'Não deu para copiar: selecione o texto e copie à mão.'
                : canWhatsapp
                  ? `Enviar pelo WhatsApp abre a conversa com ${data.phone}, com a mensagem escrita.`
                  : 'Sem celular com DDD no cadastro: copie e mande por onde preferir.'
            }
          >
            {(id) => (
              <textarea
                id={id}
                className={fieldStyles.textarea}
                rows={6}
                // A mensagem é texto de conversa: na letra do resto, não na de código.
                style={{ fontFamily: 'var(--font-sans)', lineHeight: 1.5 }}
                value={message}
                onChange={(event) => setMessage(event.target.value)}
              />
            )}
          </Field>
          <div>
            <Button size="small" variant="ghost" onClick={() => void copy('link')}>
              {copied === 'link' ? 'Link copiado' : 'Copiar só o link'}
            </Button>
          </div>
        </div>
      )}
    </Modal>
  );
}

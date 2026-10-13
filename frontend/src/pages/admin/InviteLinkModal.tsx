import { useState } from 'react';

import billing from '@/components/billing/Billing.module.css';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { whatsappLink } from '@/config/contact';
import { formatDateTime } from '@/services/format';

import styles from '../Page.module.css';

/**
 * O link do convite, para a central mandar por onde quiser (WhatsApp, por
 * exemplo): abrindo, o cliente cria a senha e entra. Um link novo, ou o
 * convite por e-mail, derruba este.
 */
export function InviteLinkModal({
  link,
  name,
  phone,
  onClose,
}: {
  link: { url: string; expiresAt: string } | null;
  name: string;
  phone: string;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const [copyFailed, setCopyFailed] = useState(false);
  const first = name.trim().split(/\s+/)[0] ?? '';
  const message = link
    ? `Olá${first ? `, ${first}` : ''}! Este é o link para você criar a sua senha e entrar na Farbo Rastreadores: ${link.url}`
    : '';
  // Dá para chamar: o número tem DDD.
  const canWhatsapp = phone.replace(/\D/g, '').length >= 10;

  const close = () => {
    setCopied(false);
    setCopyFailed(false);
    onClose();
  };
  const copy = async () => {
    if (!link) return;
    try {
      await navigator.clipboard.writeText(link.url);
      setCopied(true);
      setCopyFailed(false);
    } catch {
      setCopyFailed(true);
    }
  };

  return (
    <Modal
      open={link !== null}
      title="Link do convite"
      onClose={close}
      footer={
        <>
          <Button variant="ghost" onClick={close}>
            Fechar
          </Button>
          {canWhatsapp && (
            <Button onClick={() => window.open(whatsappLink(phone, message), '_blank', 'noopener,noreferrer')}>
              Enviar pelo WhatsApp
            </Button>
          )}
          <Button variant="primary" onClick={() => void copy()}>
            {copied ? 'Copiado' : 'Copiar link'}
          </Button>
        </>
      }
    >
      {link && (
        <div className={styles.form}>
          <p className={billing.muted}>
            Mande para {name} pelo WhatsApp ou como preferir: abrindo o link, o cliente cria a senha e já entra.
          </p>
          <TextField
            label="Link"
            value={link.url}
            readOnly
            onFocus={(e) => e.target.select()}
            hint={copyFailed ? 'Não deu para copiar: selecione o link e copie à mão.' : undefined}
          />
          <p className={billing.muted}>
            Vale até {formatDateTime(link.expiresAt)}. O link anterior, inclusive o do e-mail, deixou de valer; gerar
            outro (ou reenviar o convite por e-mail) derruba este.
          </p>
        </div>
      )}
    </Modal>
  );
}

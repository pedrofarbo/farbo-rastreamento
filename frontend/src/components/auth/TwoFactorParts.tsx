import { useState } from 'react';

import { Button } from '@/components/ui/Button';
import { useToast } from '@/components/ui/Toast';
import type { TwoFactorEnrollment } from '@/types';

import styles from './TwoFactor.module.css';

/** Só os dígitos (até 6): o campo do código. */
export const digitsOnly = (value: string) => value.replace(/\D/g, '').slice(0, 6);

/** O segredo em grupos de 4, para digitar no app sem se perder. */
export const groupSecret = (secret: string) => secret.replace(/(.{4})/g, '$1 ').trim();

async function copyText(text: string, notify: ReturnType<typeof useToast>['notify'], title: string) {
  try {
    await navigator.clipboard.writeText(text);
    notify({ tone: 'success', title });
  } catch {
    notify({ tone: 'error', title: 'Não foi possível copiar', description: 'Selecione o texto e copie manualmente.' });
  }
}

/**
 * Cadastrar no app autenticador: o QR Code (no computador), o botão que abre
 * o app (no mesmo celular) e a chave para digitar.
 */
export function EnrollApp({ enrollment }: { enrollment: TwoFactorEnrollment }) {
  const { notify } = useToast();
  return (
    <>
      <ol className={styles.apps}>
        <li>
          Abra um app autenticador: Google Authenticator, Microsoft Authenticator, Authy ou o do gerenciador de
          senhas.
        </li>
        <li>Escaneie o QR Code (ou, neste celular, toque em “Abrir no app”).</li>
        <li>Digite abaixo o código de 6 dígitos que aparecer.</li>
      </ol>
      <div className={styles.qr}>
        <img src={`data:image/svg+xml;utf8,${encodeURIComponent(enrollment.qrCode)}`} alt="QR Code para o app autenticador" />
      </div>
      <div className={styles.secret}>
        <code aria-label="Chave para digitar no app">{groupSecret(enrollment.secret)}</code>
        <button
          type="button"
          className={styles.linkButton}
          onClick={() => void copyText(enrollment.secret, notify, 'Chave copiada')}
        >
          Copiar a chave
        </button>
        <a className={styles.linkButton} href={enrollment.otpauthUrl}>
          Abrir no app
        </a>
      </div>
    </>
  );
}

/**
 * Os códigos de recuperação: aparecem uma vez só. Copiar, baixar e só
 * continuar depois de confirmar que guardou.
 */
export function RecoveryCodes({
  codes,
  onDone,
  doneLabel = 'Continuar',
}: {
  codes: string[];
  onDone: () => void;
  doneLabel?: string;
}) {
  const { notify } = useToast();
  const [saved, setSaved] = useState(false);
  const text = `Farbo Rastreadores — códigos de recuperação\nCada código vale uma vez.\n\n${codes.join('\n')}\n`;

  const download = () => {
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = 'farbo-codigos-de-recuperacao.txt';
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <>
      <p className={styles.notice}>
        Guarde estes códigos num lugar seguro (gerenciador de senhas, papel guardado). Se você perder o celular, cada
        um abre a conta uma vez. <strong>Eles não aparecem de novo.</strong>
      </p>
      <ul className={styles.codes} aria-label="Códigos de recuperação">
        {codes.map((code) => (
          <li key={code}>{code}</li>
        ))}
      </ul>
      <div className={styles.actions}>
        <Button variant="secondary" onClick={() => void copyText(codes.join('\n'), notify, 'Códigos copiados')}>
          Copiar
        </Button>
        <Button variant="secondary" onClick={download}>
          Baixar .txt
        </Button>
      </div>
      <label className={styles.check}>
        <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} />
        <span>Guardei os códigos de recuperação.</span>
      </label>
      <Button variant="primary" size="large" block disabled={!saved} onClick={onDone}>
        {doneLabel}
      </Button>
    </>
  );
}

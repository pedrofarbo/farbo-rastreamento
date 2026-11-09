import { useMutation } from '@tanstack/react-query';

import { Button } from '@/components/ui/Button';
import { useToast } from '@/components/ui/Toast';
import { installerPinMessage } from '@/services/installations';

import styles from './Appointments.module.css';

/**
 * A senha do link do instalador, na central: o número, a mensagem à parte
 * pelo WhatsApp (o link repassado sozinho não abre a tela), copiar e gerar
 * outra — a antiga para de valer e o link travado por senha errada destrava.
 */
export function LinkPin<T extends { pin?: string }>({
  pin,
  whatsappFor,
  renew,
  onRenewed,
  compact = false,
}: {
  pin: string;
  /** O WhatsApp do instalador com a mensagem. */
  whatsappFor: (message: string) => string;
  renew: () => Promise<T>;
  onRenewed?: (renewed: T) => void;
  /** Na linha da tabela: sem o copiar (o número está à vista), o resto mais curto. */
  compact?: boolean;
}) {
  const { notify } = useToast();
  const renewing = useMutation({
    mutationFn: renew,
    onSuccess: (renewed) => {
      onRenewed?.(renewed);
      notify({ tone: 'success', title: `Senha nova: ${renewed.pin ?? ''}`, description: 'A antiga não abre mais o link. Mande a nova ao instalador.' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para trocar a senha', description: err.message }),
  });
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(pin);
      notify({ tone: 'success', title: 'Senha copiada' });
    } catch {
      /* sem permissão: a pessoa copia na mão */
    }
  };
  return (
    <div className={`${styles.pin} ${compact ? styles.pinCompact : ''}`}>
      <span className={styles.pinValue}>
        Senha <strong>{pin}</strong>
      </span>
      <a className={styles.waLink} href={whatsappFor(installerPinMessage(pin))} target="_blank" rel="noreferrer noopener">
        Senha pelo WhatsApp
      </a>
      {!compact && (
        <Button size="small" variant="ghost" onClick={() => void copy()}>
          Copiar senha
        </Button>
      )}
      <Button size="small" variant="ghost" loading={renewing.isPending} onClick={() => renewing.mutate()}>
        {compact ? 'Outra senha' : 'Gerar outra senha'}
      </Button>
    </div>
  );
}

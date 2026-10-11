import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { meApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import fieldStyles from '@/components/ui/Field.module.css';
import { useToast } from '@/components/ui/Toast';
import type { AlertSettings } from '@/types';

import styles from './ExtraRecipients.module.css';

const EMAIL = /^[^\s@<>",;]+@[^\s@<>",;]+\.[^\s@<>",;]+$/;

/**
 * Também avisar: e-mails sem login (o sócio, a central de operações da
 * frota) que recebem os mesmos alertas que o titular escolheu, só por e-mail.
 * O titular e o Gestor mudam; o Visualizador só vê.
 */
export function ExtraRecipients({ settings, queryKey }: { settings: AlertSettings; queryKey: readonly unknown[] }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [draft, setDraft] = useState('');
  const [error, setError] = useState('');
  const list = settings.extraRecipients ?? [];
  const max = settings.maxExtraRecipients ?? 5;

  const save = useMutation({
    mutationFn: (emails: string[]) => meApi.saveAlertRecipients(emails),
    onSuccess: (data) => {
      queryClient.setQueryData(queryKey, data);
      setDraft('');
      notify({ tone: 'success', title: 'Destinatários salvos' });
    },
    onError: (err: Error) => setError(err.message),
  });

  const add = () => {
    const email = draft.trim().toLowerCase();
    if (!EMAIL.test(email)) return setError('Informe um e-mail válido.');
    if (list.includes(email)) return setError('Esse e-mail já está na lista.');
    setError('');
    save.mutate([...list, email]);
  };

  if (!settings.canEditRecipients && list.length === 0) return null;
  return (
    <div className={styles.box}>
      <p className={styles.intro}>
        Até {max} e-mails recebem os mesmos alertas que o titular da conta escolheu, só por e-mail (sem login no app). Bom
        para um sócio ou a central de operações da frota.
      </p>
      {list.length > 0 && (
        <ul className={styles.list}>
          {list.map((email) => (
            <li key={email}>
              <span>{email}</span>
              {settings.canEditRecipients && (
                <Button
                  size="small"
                  variant="ghost"
                  disabled={save.isPending}
                  onClick={() => save.mutate(list.filter((e) => e !== email))}
                >
                  Tirar
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {settings.canEditRecipients && list.length < max && (
        <div className={styles.add}>
          <input
            className={fieldStyles.input}
            type="email"
            inputMode="email"
            placeholder="email@empresa.com.br"
            aria-label="E-mail para também avisar"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && add()}
          />
          <Button variant="secondary" loading={save.isPending} onClick={add}>
            Incluir
          </Button>
        </div>
      )}
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
    </div>
  );
}

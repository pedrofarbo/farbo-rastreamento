import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { customersApi } from '@/api/resources';
import { IdentityCheck } from '@/components/auth/IdentityCheck';
import { SessionList } from '@/components/auth/Sessions';
import billing from '@/components/billing/Billing.module.css';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';

import styles from '../Page.module.css';

const sessionsKey = (id: string) => ['customer', id, 'sessions'] as const;

/** Uma senha de 12 letras e números, sem os que se confundem (0/O, 1/l/I). */
export function randomPassword(length = 12): string {
  const alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789';
  const bytes = new Uint32Array(length);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => alphabet[b % alphabet.length]).join('');
}

/**
 * As sessões do cliente (os aparelhos com a conta dele aberta) e
 * "desconectar todas": caem na hora, e ele entra de novo com a senha.
 */
export function CustomerSessionsModal({
  customerId,
  name,
  open,
  onClose,
}: {
  customerId: string;
  name: string;
  open: boolean;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const sessions = useQuery({ queryKey: sessionsKey(customerId), queryFn: () => customersApi.sessions(customerId), enabled: open });
  const signOut = useMutation({
    mutationFn: () => customersApi.signOut(customerId),
    onSuccess: ({ signedOut }) => {
      void queryClient.invalidateQueries({ queryKey: sessionsKey(customerId) });
      notify({
        tone: 'success',
        title: signedOut === 1 ? '1 sessão desconectada' : `${signedOut} sessões desconectadas`,
        description: `${name} entra de novo com a senha.`,
      });
    },
    onError: (error: Error) => notify({ tone: 'error', title: 'Não foi possível desconectar', description: error.message }),
  });
  const list = sessions.data?.sessions ?? [];

  return (
    <Modal
      open={open}
      title={`Sessões de ${name}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Fechar
          </Button>
          <Button
            variant="danger"
            disabled={list.length === 0}
            loading={signOut.isPending}
            onClick={() => {
              if (window.confirm(`Desconectar todas as sessões de ${name}? Os aparelhos saem da conta na hora.`)) signOut.mutate();
            }}
          >
            Desconectar todas
          </Button>
        </>
      }
    >
      <div className={styles.form}>
        <p className={billing.muted}>Os aparelhos com a conta aberta, do usado por último ao mais antigo.</p>
        {sessions.isLoading ? (
          <Spinner label="Carregando as sessões" />
        ) : sessions.isError ? (
          <p className={billing.muted}>Não foi possível carregar as sessões.</p>
        ) : list.length === 0 ? (
          <p className={billing.muted}>Nenhuma sessão aberta: o cliente entra de novo com a senha.</p>
        ) : (
          <SessionList sessions={list} />
        )}
      </div>
    </Modal>
  );
}

/**
 * A senha nova do cliente, definida pela central: confirmada com a senha (ou
 * a biometria) de quem faz. As sessões dele caem, e ele recebe o aviso de
 * senha alterada por e-mail.
 */
export function CustomerPasswordModal({
  customerId,
  name,
  open,
  onClose,
}: {
  customerId: string;
  name: string;
  open: boolean;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [password, setPassword] = useState('');
  const [step, setStep] = useState<'form' | 'confirm' | 'done'>('form');
  const [error, setError] = useState('');
  const save = useMutation({
    mutationFn: (token: string) => customersApi.setPassword(customerId, password, token),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: sessionsKey(customerId) });
      setStep('done');
      notify({ tone: 'success', title: 'Senha definida', description: `As sessões de ${name} foram desconectadas.` });
    },
    onError: (err: Error) => {
      setError(err.message);
      setStep('form');
    },
  });
  const close = () => {
    setPassword('');
    setError('');
    setStep('form');
    onClose();
  };
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(password);
      notify({ tone: 'success', title: 'Senha copiada' });
    } catch {
      notify({ tone: 'error', title: 'Não deu para copiar', description: password });
    }
  };
  const short = password.length < 10;

  return (
    <Modal
      open={open}
      title={`Senha nova para ${name}`}
      onClose={save.isPending ? () => undefined : close}
      footer={
        step === 'done' ? (
          <Button variant="primary" onClick={close}>
            Fechar
          </Button>
        ) : step === 'form' ? (
          <>
            <Button variant="ghost" onClick={close}>
              Cancelar
            </Button>
            <Button variant="primary" disabled={short} onClick={() => setStep('confirm')}>
              Continuar
            </Button>
          </>
        ) : (
          <Button variant="ghost" disabled={save.isPending} onClick={() => setStep('form')}>
            Voltar
          </Button>
        )
      }
    >
      <div className={styles.form}>
        {step === 'done' ? (
          <>
            <p>
              A senha de <strong>{name}</strong> agora é:
            </p>
            <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-2)' }}>
              <code style={{ fontSize: 'var(--text-lg)', fontWeight: 700 }}>{password}</code>
              <Button size="small" variant="secondary" onClick={() => void copy()}>
                Copiar
              </Button>
            </div>
            <p className={billing.muted}>
              As sessões abertas dele foram desconectadas, e ele recebeu o aviso de senha alterada por e-mail. Passe a senha
              por um canal seguro.
            </p>
          </>
        ) : (
          <>
            <p className={billing.muted}>
              A senha antiga deixa de valer e todas as sessões de {name} são desconectadas na hora. Ele recebe o aviso de
              senha alterada por e-mail.
            </p>
            {error && <div className={styles.note}>{error}</div>}
            <TextField
              label="Senha nova"
              type="password"
              value={password}
              autoComplete="new-password"
              disabled={step === 'confirm'}
              hint="Pelo menos 10 caracteres."
              onChange={(e) => setPassword(e.target.value)}
            />
            {step === 'form' && (
              <Button variant="secondary" size="small" onClick={() => setPassword(randomPassword())}>
                Gerar senha
              </Button>
            )}
            {step === 'confirm' &&
              (save.isPending ? (
                <Spinner label="Definindo a senha" />
              ) : (
                <IdentityCheck
                  purpose="customer_password"
                  action={`Para definir a senha de ${name}`}
                  onGrant={(token) => {
                    setError('');
                    save.mutate(token);
                  }}
                />
              ))}
          </>
        )}
      </div>
    </Modal>
  );
}

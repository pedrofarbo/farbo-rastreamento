import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';

import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { useToast } from '@/components/ui/Toast';
import {
  biometricAvailable,
  biometricLabels,
  deviceCredential,
  enrollBiometric,
  errorCode,
  listBiometrics,
  removeBiometric,
} from '@/services/stepUp';
import type { BiometricDevice } from '@/services/stepUp';
import { useAuth } from '@/stores/AuthContext';

import styles from './screens/Screen.module.css';

const key = ['step-up', 'biometrics'] as const;

/** Conta → Bloqueio do motor: ativar ou desativar a biometria deste aparelho. */
export function BiometricCard() {
  const { user } = useAuth();
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const devices = useQuery({ queryKey: key, queryFn: listBiometrics });
  const [supported, setSupported] = useState<boolean | null>(null);
  const [local, setLocal] = useState(() => (user ? deviceCredential(user.id) : null));
  const [asking, setAsking] = useState(false);
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    void biometricAvailable().then(setSupported);
  }, []);

  if (!user) return null;
  const biometric = biometricLabels();
  const list = devices.data?.credentials ?? [];
  const here = list.find((device) => device.credentialId === local);
  const others = list.filter((device) => device !== here);

  const refresh = () => {
    setLocal(deviceCredential(user.id));
    return queryClient.invalidateQueries({ queryKey: key });
  };

  const enroll = async (event: FormEvent) => {
    event.preventDefault();
    if (!password) {
      setError('Digite a sua senha.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await enrollBiometric(user, password);
      setAsking(false);
      setPassword('');
      notify({ tone: 'success', title: `${biometric.enabled} neste aparelho` });
      await refresh();
    } catch (err) {
      setError(
        errorCode(err) === 'WRONG_PASSWORD'
          ? 'Senha incorreta.'
          : err instanceof Error && err.name !== 'BiometricError'
            ? err.message
            : `Não foi possível ativar ${biometric.withArticle}. Tente de novo.`,
      );
    } finally {
      setBusy(false);
    }
  };

  const remove = async (device: BiometricDevice) => {
    setBusy(true);
    try {
      await removeBiometric(user.id, device);
      notify({ tone: 'success', title: device === here ? `${biometric.disabled} neste aparelho` : 'Aparelho removido' });
      await refresh();
    } catch (err) {
      notify({ tone: 'error', title: 'Não foi possível remover', description: err instanceof Error ? err.message : undefined });
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className={styles.section} aria-label={biometric.name}>
      <h2 className={styles.sectionTitle}>{biometric.name}</h2>
      <p className={styles.muted}>
        Com {biometric.withArticle} deste aparelho, você entra no app e confirma o bloqueio do motor sem digitar a
        senha. Se não der, o app pede a senha.
      </p>

      {here ? (
        <div className={styles.row}>
          <p className={styles.good}>{biometric.enabled} neste aparelho.</p>
          <Button variant="ghost" onClick={() => void remove(here)} disabled={busy}>
            Desativar
          </Button>
        </div>
      ) : supported === false ? (
        <p className={styles.muted}>Este aparelho não oferece biometria para o app: o bloqueio pede a sua senha.</p>
      ) : asking ? (
        <form className={styles.form} onSubmit={enroll}>
          <TextField
            label="Sua senha"
            type="password"
            autoComplete="current-password"
            autoFocus
            value={password}
            error={error || undefined}
            onChange={(event) => setPassword(event.target.value)}
          />
          <div className={styles.row}>
            <Button type="button" variant="ghost" onClick={() => setAsking(false)} disabled={busy}>
              Cancelar
            </Button>
            <Button type="submit" variant="primary" loading={busy}>
              Continuar
            </Button>
          </div>
        </form>
      ) : (
        <Button variant="secondary" onClick={() => setAsking(true)} disabled={supported === null} block>
          Ativar {biometric.withArticle}
        </Button>
      )}

      {others.length > 0 && (
        <ul className={styles.list} aria-label="Outros aparelhos com biometria">
          {others.map((device) => (
            <li key={device.id} className={styles.listItem}>
              <span>
                {device.name}
                <span className={styles.muted}> · desde {new Date(device.createdAt).toLocaleDateString('pt-BR')}</span>
              </span>
              <Button variant="ghost" size="small" onClick={() => void remove(device)} disabled={busy}>
                Remover
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

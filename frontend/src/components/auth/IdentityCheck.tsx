import { useCallback, useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';

import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import {
  biometricAvailable,
  biometricGrant,
  biometricLabels,
  deviceCredential,
  errorCode,
  passwordGrant,
} from '@/services/stepUp';
import type { StepUpPurpose } from '@/services/stepUp';
import { useAuth } from '@/stores/AuthContext';

import styles from './IdentityCheck.module.css';

interface IdentityCheckProps {
  purpose: StepUpPurpose;
  /** O que a confirmação libera, no pedido da senha: "Para dar o acesso, ...". */
  action: string;
  /** Recebe o comprovante (uso único, vale poucos minutos). */
  onGrant: (token: string) => void;
}

/**
 * "Confirme que é você": a biometria deste aparelho, se cadastrada, e, se
 * ela falhar ou não houver, a senha da conta. O servidor exige o comprovante
 * para a ação (purpose).
 */
export function IdentityCheck({ purpose, action, onGrant }: IdentityCheckProps) {
  const { user } = useAuth();
  const biometric = biometricLabels();
  const [mode, setMode] = useState<'biometric' | 'password'>('biometric');
  const [biometricFailed, setBiometricFailed] = useState(false);
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [checking, setChecking] = useState(false);
  // Cada tentativa ganha um número: desmontar ou tentar de novo descarta a anterior.
  const attempt = useRef(0);
  // A biometria é pedida uma vez ao abrir, mesmo que quem usa passe uma
  // função nova a cada renderização.
  const grantTo = useRef(onGrant);
  grantTo.current = onGrant;

  const tryBiometric = useCallback(async () => {
    const mine = ++attempt.current;
    if (!user || !deviceCredential(user.id) || !(await biometricAvailable())) {
      if (attempt.current === mine) setMode('password');
      return;
    }
    setMode('biometric');
    try {
      const grant = await biometricGrant(user.id, purpose);
      if (attempt.current === mine) grantTo.current(grant.token);
    } catch {
      if (attempt.current !== mine) return;
      setBiometricFailed(true);
      setMode('password');
    }
  }, [user, purpose]);

  useEffect(() => {
    void tryBiometric();
    return () => {
      attempt.current += 1;
    };
  }, [tryBiometric]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!password) {
      setError('Digite a sua senha.');
      return;
    }
    setChecking(true);
    setError('');
    try {
      const grant = await passwordGrant(purpose, password);
      setPassword('');
      grantTo.current(grant.token);
    } catch (err) {
      setError(
        errorCode(err) === 'WRONG_PASSWORD'
          ? 'Senha incorreta.'
          : err instanceof Error
            ? err.message
            : 'Não foi possível conferir a senha.',
      );
    } finally {
      setChecking(false);
    }
  };

  if (mode === 'biometric') {
    return (
      <div className={styles.waiting} role="status">
        <span className={styles.spinner} aria-hidden="true" />
        Confirme com {biometric.withArticle}…
      </div>
    );
  }

  return (
    <form className={styles.form} onSubmit={submit}>
      <p className={styles.lead}>
        {biometricFailed
          ? `Não deu para confirmar com ${biometric.withArticle}. Use a senha da sua conta.`
          : `${action}, confirme que é você com a senha da sua conta.`}
      </p>
      <TextField
        label="Sua senha"
        type="password"
        name="identity-password"
        autoComplete="current-password"
        autoFocus
        value={password}
        error={error || undefined}
        onChange={(event) => setPassword(event.target.value)}
      />
      <div className={styles.actions}>
        <Button type="submit" variant="primary" loading={checking}>
          Confirmar
        </Button>
        {biometricFailed && (
          <Button type="button" variant="ghost" size="small" onClick={() => void tryBiometric()}>
            Tentar {biometric.withArticle} de novo
          </Button>
        )}
      </div>
    </form>
  );
}

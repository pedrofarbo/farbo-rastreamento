import { useState } from 'react';
import type { FormEvent } from 'react';
import { Link, Navigate, useLocation } from 'react-router-dom';

import { TwoFactorLogin } from '@/components/auth/TwoFactorLogin';
import { AuthLayout, authStyles as styles } from '@/components/layout/AuthLayout';
import { PRIVACY_PATH, TERMS_PATH } from '@/config/legal';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useAuth } from '@/stores/AuthContext';
import type { TwoFactorChallenge } from '@/types';

/** Estado de navegação que a tela de redefinição deixa ao mandar para cá. */
export interface LoginLocationState {
  passwordReset?: boolean;
}

export function LoginPage() {
  const { user, loading, login, completeLogin } = useAuth();
  const location = useLocation();
  const passwordReset = (location.state as LoginLocationState | null)?.passwordReset === true;

  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  // Depois da senha: o segundo fator (ou a ativação, a equipe).
  const [challenge, setChallenge] = useState<TwoFactorChallenge | null>(null);

  if (loading) {
    return <Spinner label="Verificando sessão" />;
  }
  if (user) {
    return <Navigate to="/dashboard" replace />;
  }

  const onSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setError('');
    setSubmitting(true);
    try {
      const outcome = await login(email, password);
      if (outcome.challenge) {
        setChallenge(outcome.challenge);
        setPassword('');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'não foi possível entrar');
    } finally {
      setSubmitting(false);
    }
  };

  if (challenge) {
    const setup = challenge.kind === 'setup';
    return (
      <AuthLayout
        tag="Segurança da conta"
        title={setup ? 'Proteja o seu acesso' : 'Confirme que é você'}
        subtitle={setup ? undefined : 'Falta só o segundo passo.'}
      >
        <TwoFactorLogin
          challenge={challenge}
          showTitle={false}
          onDone={(result) => completeLogin(result, email)}
          onCancel={(message) => {
            setChallenge(null);
            setError(message ?? '');
          }}
        />
      </AuthLayout>
    );
  }

  return (
    <AuthLayout
      tag="Área do cliente"
      title="Acesse seu painel"
      subtitle="Acompanhe seus veículos em tempo real."
    >
      <form className={styles.form} onSubmit={onSubmit}>
        {passwordReset && !error && (
          <div className={styles.notice} role="status">
            Senha alterada. Entre com a nova senha.
          </div>
        )}
        {error && <div className={styles.error}>{error}</div>}

        <TextField
          label="E-mail"
          type="email"
          autoComplete="username"
          required
          value={email}
          onChange={(event) => setEmail(event.target.value)}
        />

        <TextField
          label="Senha"
          type="password"
          autoComplete="current-password"
          required
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />

        <Link to="/esqueci-senha" className={styles.inlineLink}>
          Esqueci minha senha
        </Link>

        <Button type="submit" variant="primary" size="large" block loading={submitting}>
          Entrar
        </Button>

        <p className={styles.legal}>
          Ao entrar, você concorda com os <Link to={TERMS_PATH}>Termos de Uso</Link> e a{' '}
          <Link to={PRIVACY_PATH}>Política de Privacidade</Link>.
        </p>
      </form>
    </AuthLayout>
  );
}

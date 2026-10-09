import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { authApi } from '@/api/resources';
import {
  AlertIcon,
  AuthLayout,
  CheckIcon,
  authStyles as styles,
} from '@/components/layout/AuthLayout';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useAuth } from '@/stores/AuthContext';

import type { LoginLocationState } from './LoginPage';

type Stage = 'checking' | 'invalid' | 'form' | 'done';

const MIN_PASSWORD_LENGTH = 10;

/**
 * O link do e-mail traz o token no fragmento: /redefinir-senha#token=…
 * O convite de cliente novo acrescenta &boasvindas=1, que só muda os textos.
 */
function readLinkFromHash(): { token: string; welcome: boolean } {
  const params = new URLSearchParams(window.location.hash.slice(1));
  return { token: params.get('token')?.trim() ?? '', welcome: params.get('boasvindas') === '1' };
}

/**
 * Tela aberta pelo link do e-mail: confere o link e grava a senha nova.
 */
export function ResetPasswordPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const { user, logout } = useAuth();

  const [{ token, welcome }] = useState(readLinkFromHash);
  const tag = welcome ? 'Bem-vindo' : 'Área do cliente';
  const [stage, setStage] = useState<Stage>(token ? 'checking' : 'invalid');
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [fieldError, setFieldError] = useState<{ password?: string; confirmation?: string }>({});
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  // Tira o token da barra de endereço (e do histórico) assim que ele é lido.
  useEffect(() => {
    if (location.hash) {
      navigate({ pathname: location.pathname, search: location.search }, { replace: true });
    }
  }, [location.hash, location.pathname, location.search, navigate]);

  // Confere o link antes de pedir a senha nova, para não fazer a pessoa
  // digitar à toa num link vencido.
  useEffect(() => {
    if (!token) return;
    let active = true;
    authApi
      .validateResetToken(token)
      .then(() => {
        if (active) setStage('form');
      })
      .catch((err) => {
        if (!active) return;
        // 410 é link inválido. Qualquer outra falha (rede, limite) deixa
        // seguir: o envio confere o link de novo.
        setStage(err instanceof ApiError && err.status === 410 ? 'invalid' : 'form');
      });
    return () => {
      active = false;
    };
  }, [token]);

  const onSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setError('');

    const errors: typeof fieldError = {};
    if (password.length < MIN_PASSWORD_LENGTH) {
      errors.password = `Use ao menos ${MIN_PASSWORD_LENGTH} caracteres.`;
    }
    if (confirmation !== password) {
      errors.confirmation = 'As senhas não conferem.';
    }
    setFieldError(errors);
    if (errors.password || errors.confirmation) return;

    setSubmitting(true);
    try {
      await authApi.resetPassword(token, password);
      // O backend encerrou todas as sessões; se havia uma aberta neste
      // navegador, ela também sai, para o login pedir a senha nova.
      if (user) await logout();
      setStage('done');
    } catch (err) {
      if (err instanceof ApiError && err.status === 410) {
        setStage('invalid');
      } else {
        setError(err instanceof Error ? err.message : 'não foi possível alterar a senha');
      }
    } finally {
      setSubmitting(false);
    }
  };

  const loginFooter = (
    <Link to="/login" className={styles.back}>
      ← Voltar para o login
    </Link>
  );

  if (stage === 'checking') {
    return (
      <AuthLayout tag={tag} title={welcome ? 'Crie sua senha' : 'Redefinir senha'} footer={loginFooter}>
        <Spinner label="Conferindo o link…" />
      </AuthLayout>
    );
  }

  if (stage === 'invalid') {
    // Já conectado (o link é velho, de um convite que ele mesmo já usou):
    // não há nada a fazer aqui, é só abrir o painel.
    if (user) {
      return (
        <AuthLayout tag={tag} title="Você já está conectado" icon={<CheckIcon />}>
          <div className={styles.stack}>
            <p className={styles.text}>
              Este link já foi usado, mas não precisa dele: você já entrou na sua conta neste aparelho.
            </p>
            <Button variant="primary" size="large" block onClick={() => navigate('/dashboard', { replace: true })}>
              Abrir o painel
            </Button>
          </div>
        </AuthLayout>
      );
    }
    // O link de boas-vindas serve uma vez, para criar a senha. Quem volta a
    // ele depois de criar a senha só precisa entrar.
    if (welcome) {
      return (
        <AuthLayout
          tag={tag}
          title="Este convite já foi usado"
          icon={<AlertIcon />}
          footer={loginFooter}
        >
          <div className={styles.stack}>
            <p className={styles.text}>
              O link de boas-vindas serve uma vez só, para criar a senha. Se você já criou a sua, é só entrar com o
              seu e-mail e a senha. Se o convite venceu antes disso, peça um link novo.
            </p>
            <Button variant="primary" size="large" block onClick={() => navigate('/login', { replace: true })}>
              Entrar
            </Button>
            <Button variant="ghost" size="large" block onClick={() => navigate('/esqueci-senha')}>
              Pedir um novo link
            </Button>
          </div>
        </AuthLayout>
      );
    }
    return (
      <AuthLayout
        tag={tag}
        title="Link inválido ou expirado"
        icon={<AlertIcon />}
        iconTone="danger"
        footer={loginFooter}
      >
        <div className={styles.stack}>
          <p className={styles.text}>
            Os links de redefinição valem por tempo limitado e só podem ser usados uma vez. Se você
            pediu mais de uma vez, só o e-mail mais recente vale.
          </p>
          <Button variant="primary" size="large" block onClick={() => navigate('/esqueci-senha')}>
            Pedir um novo link
          </Button>
        </div>
      </AuthLayout>
    );
  }

  if (stage === 'done') {
    const state: LoginLocationState = { passwordReset: true };
    return (
      <AuthLayout tag={tag} title={welcome ? 'Senha criada!' : 'Senha alterada!'} icon={<CheckIcon />}>
        <div className={styles.stack}>
          <p className={styles.text}>
            {welcome
              ? 'Tudo pronto. Entre com o seu e-mail e a senha que você acabou de criar.'
              : 'Sua nova senha já está valendo. Por segurança, encerramos as sessões abertas em outros aparelhos.'}
          </p>
          <Button
            variant="primary"
            size="large"
            block
            onClick={() => navigate('/login', { state, replace: true })}
          >
            Entrar no painel
          </Button>
        </div>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout
      tag={tag}
      title={welcome ? 'Crie sua senha de acesso' : 'Crie uma nova senha'}
      subtitle={
        welcome
          ? 'Sua conta no painel da Farbo Rastreadores está pronta. Escolha uma senha que você não usa em outros sites.'
          : 'Escolha uma senha que você não usa em outros sites.'
      }
      footer={loginFooter}
    >
      <form className={styles.form} onSubmit={onSubmit} noValidate>
        {error && <div className={styles.error}>{error}</div>}

        <TextField
          label="Nova senha"
          type="password"
          autoComplete="new-password"
          autoFocus
          required
          minLength={MIN_PASSWORD_LENGTH}
          hint={fieldError.password ? undefined : `Mínimo de ${MIN_PASSWORD_LENGTH} caracteres.`}
          error={fieldError.password}
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />

        <TextField
          label="Confirme a nova senha"
          type="password"
          autoComplete="new-password"
          required
          error={fieldError.confirmation}
          value={confirmation}
          onChange={(event) => setConfirmation(event.target.value)}
        />

        <Button type="submit" variant="primary" size="large" block loading={submitting}>
          Salvar nova senha
        </Button>
      </form>
    </AuthLayout>
  );
}

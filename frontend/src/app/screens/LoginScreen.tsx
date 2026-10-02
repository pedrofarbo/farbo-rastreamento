import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Navigate, useNavigate } from 'react-router-dom';

import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import {
  BiometricError,
  biometricAvailable,
  biometricLabels,
  biometricLoginAccount,
  biometricOfferDismissed,
  deviceCredential,
  dismissBiometricOffer,
  enrollBiometric,
} from '@/services/stepUp';
import { useAuth } from '@/stores/AuthContext';
import type { User } from '@/types';

import { FaceIdIcon, FingerprintIcon } from '../icons';
import styles from './Screen.module.css';

/**
 * Entrar no app: a mesma conta do painel. Com a biometria ativada neste
 * aparelho (Face ID no iPhone, digital ou rosto no Android), basta tocar em
 * "Entrar com o Face ID"; e-mail e senha continuam como alternativa.
 */
export function LoginScreen() {
  const { user, login, loginWithBiometric } = useAuth();
  const navigate = useNavigate();
  const biometric = biometricLabels();
  const Icon = biometric.name === 'Face ID' ? FaceIdIcon : FingerprintIcon;

  const [account, setAccount] = useState(biometricLoginAccount);
  const [supported, setSupported] = useState<boolean | null>(null);
  const [useForm, setUseForm] = useState(false);
  const [email, setEmail] = useState(() => biometricLoginAccount()?.email ?? '');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  // Depois de entrar com a senha, segura a ida para o mapa enquanto oferece
  // a biometria.
  const [holding, setHolding] = useState(false);
  const [offer, setOffer] = useState<User | null>(null);
  const typedPassword = useRef('');

  useEffect(() => {
    void biometricAvailable().then(setSupported);
  }, []);

  if (user && !holding) return <Navigate to="/mapa" replace />;

  const goToMap = () => navigate('/mapa', { replace: true });

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError('');
    setBusy(true);
    setHolding(true);
    try {
      const who = await login(email.trim(), password);
      typedPassword.current = password;
      setPassword('');
      // Pergunta na hora (não pelo estado): quem entra rápido, com o
      // preenchimento automático, chega aqui antes da checagem inicial.
      if (!deviceCredential(who.id) && !biometricOfferDismissed(who.id) && (await biometricAvailable())) {
        setOffer(who);
      } else {
        goToMap();
      }
    } catch (err) {
      setHolding(false);
      setError(err instanceof Error ? err.message : 'Não foi possível entrar.');
    } finally {
      setBusy(false);
    }
  };

  const enterWithBiometric = async () => {
    setError('');
    setBusy(true);
    try {
      await loginWithBiometric();
      goToMap();
    } catch (err) {
      if (err instanceof BiometricError && err.reason === 'not-enrolled') {
        setAccount(null);
        setError(`${biometric.disabled} neste aparelho. Entre com a senha.`);
      } else {
        setError(`Não deu para entrar com ${biometric.withArticle}. Tente de novo ou use a senha.`);
      }
    } finally {
      setBusy(false);
    }
  };

  const enable = async () => {
    if (!offer) return;
    setError('');
    setBusy(true);
    try {
      await enrollBiometric(offer, typedPassword.current);
      typedPassword.current = '';
      goToMap();
    } catch (err) {
      setError(
        err instanceof Error && err.name !== 'BiometricError'
          ? err.message
          : `Não foi possível ativar ${biometric.withArticle}. Você pode tentar de novo em Conta.`,
      );
    } finally {
      setBusy(false);
    }
  };

  const notNow = () => {
    if (offer) dismissBiometricOffer(offer.id);
    typedPassword.current = '';
    goToMap();
  };

  // Logo depois de entrar com a senha: oferecer a biometria.
  if (offer) {
    return (
      <div className={styles.login}>
        <div className={styles.loginBadge} aria-hidden="true">
          <Icon size={44} />
        </div>
        <div className={styles.loginIntro}>
          <h1 className={styles.title}>Entrar com {biometric.withArticle}?</h1>
          <p className={styles.lead}>
            Da próxima vez, é só {biometric.name === 'Face ID' ? 'olhar para o iPhone' : 'usar a digital ou o rosto'}. {biometric.name === 'Face ID' ? 'O Face ID' : 'A biometria'} também
            confirma o bloqueio do motor.
          </p>
        </div>
        {error && <p className={styles.error} role="alert">{error}</p>}
        <div className={styles.form}>
          <Button variant="primary" size="large" block loading={busy} onClick={() => void enable()} icon={<Icon size={20} />}>
            Ativar {biometric.withArticle}
          </Button>
          <Button variant="ghost" block onClick={notNow} disabled={busy}>
            Agora não
          </Button>
        </div>
      </div>
    );
  }

  const quick = Boolean(account && supported) && !useForm;

  return (
    <div className={styles.login}>
      <img src="/assets/logo-header.png" alt="Farbo Rastreadores" className={styles.loginLogo} />

      {quick && account ? (
        <>
          <div className={styles.loginIntro}>
            <h1 className={styles.title}>Olá, {account.name.split(' ')[0]}</h1>
            <p className={styles.lead}>{account.email}</p>
          </div>
          {error && <p className={styles.error} role="alert">{error}</p>}
          <div className={styles.form}>
            <Button
              variant="primary"
              size="large"
              block
              loading={busy}
              onClick={() => void enterWithBiometric()}
              icon={<Icon size={20} />}
            >
              Entrar com {biometric.withArticle}
            </Button>
            <Button
              variant="ghost"
              block
              disabled={busy}
              onClick={() => {
                setError('');
                setUseForm(true);
              }}
            >
              Entrar com e-mail e senha
            </Button>
          </div>
        </>
      ) : (
        <>
          <div className={styles.loginIntro}>
            <h1 className={styles.title}>Seus veículos na palma da mão</h1>
            <p className={styles.lead}>Entre com o e-mail e a senha da sua conta Farbo.</p>
          </div>
          <form className={styles.form} onSubmit={submit}>
            <TextField
              label="E-mail"
              type="email"
              name="email"
              autoComplete="username"
              inputMode="email"
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
              enterKeyHint="next"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
            <TextField
              label="Senha"
              type="password"
              name="password"
              autoComplete="current-password"
              enterKeyHint="go"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            {error && <p className={styles.error} role="alert">{error}</p>}
            <Button type="submit" variant="primary" size="large" block loading={busy}>
              Entrar
            </Button>
            {account && supported && (
              <Button
                type="button"
                variant="ghost"
                block
                disabled={busy}
                onClick={() => {
                  setError('');
                  setUseForm(false);
                }}
                icon={<Icon size={20} />}
              >
                Entrar com {biometric.withArticle}
              </Button>
            )}
          </form>
        </>
      )}

      <div className={styles.loginFooter}>
        <a className={styles.link} href="/esqueci-senha">
          Esqueci minha senha
        </a>
      </div>
    </div>
  );
}

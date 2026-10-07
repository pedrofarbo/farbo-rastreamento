import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';

import { twoFactorApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { useToast } from '@/components/ui/Toast';
import { errorCode } from '@/services/stepUp';
import type { LoginResponse, TwoFactorChallenge, TwoFactorEnrollment } from '@/types';

import styles from './TwoFactor.module.css';
import { EnrollApp, RecoveryCodes, digitsOnly } from './TwoFactorParts';

const RESEND_SECONDS = 30;

const messageOf = (err: unknown) => (err instanceof Error ? err.message : 'Não foi possível conferir o código.');

/**
 * Depois da senha: o código do app autenticador ou do e-mail (ou um de
 * recuperação), com "confiar neste aparelho". Para a equipe sem a
 * verificação, a ativação guiada: o código do e-mail, o QR Code do app e os
 * códigos de recuperação. onDone recebe a sessão; onCancel volta para a
 * senha (com o motivo, quando expirou).
 */
export function TwoFactorLogin({
  challenge,
  trustByDefault = false,
  showTitle = true,
  onDone,
  onCancel,
}: {
  challenge: TwoFactorChallenge;
  trustByDefault?: boolean;
  /** Sem o título (a moldura da tela já tem). */
  showTitle?: boolean;
  onDone: (result: LoginResponse) => void;
  onCancel: (message?: string) => void;
}) {
  const { notify } = useToast();
  const setup = challenge.kind === 'setup';
  const [step, setStep] = useState<'code' | 'app' | 'codes'>('code');
  const [code, setCode] = useState('');
  const [recovery, setRecovery] = useState(false);
  const [trust, setTrust] = useState(trustByDefault);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [enrollment, setEnrollment] = useState<TwoFactorEnrollment | null>(null);
  const [session, setSession] = useState<LoginResponse | null>(null);
  // O reenvio do código por e-mail espera 30 s.
  const emailCode = setup ? step === 'code' : challenge.method === 'email';
  const [cooldown, setCooldown] = useState(emailCode ? RESEND_SECONDS : 0);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (cooldown <= 0) return;
    const timer = setTimeout(() => setCooldown((s) => s - 1), 1000);
    return () => clearTimeout(timer);
  }, [cooldown]);

  useEffect(() => {
    input.current?.focus();
  }, [step, recovery]);

  const fail = (err: unknown) => {
    if (errorCode(err) === 'TWO_FACTOR_EXPIRED') {
      onCancel(messageOf(err));
      return;
    }
    setError(messageOf(err));
    setCode('');
  };

  const submit = async (value = code) => {
    setError('');
    setBusy(true);
    try {
      if (!setup) {
        onDone(await twoFactorApi.verify(challenge.challenge, value, trust));
      } else if (step === 'code') {
        setEnrollment(await twoFactorApi.setupEmail(challenge.challenge, value));
        setCode('');
        setStep('app');
      } else {
        const result = await twoFactorApi.setupConfirm(challenge.challenge, value, trust);
        setSession(result);
        setStep('codes');
      }
    } catch (err) {
      fail(err);
    } finally {
      setBusy(false);
    }
  };

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    if (code) void submit();
  };

  const type = (value: string) => {
    const next = recovery ? value.slice(0, 12) : digitsOnly(value);
    setCode(next);
    // Seis dígitos: confere sozinho (o app do celular cola o código inteiro).
    if (!recovery && next.length === 6 && !busy) void submit(next);
  };

  const resend = async () => {
    setError('');
    try {
      await twoFactorApi.resend(challenge.challenge);
      setCooldown(RESEND_SECONDS);
      notify({ tone: 'success', title: 'Código reenviado', description: `Confira o e-mail ${challenge.emailHint}.` });
    } catch (err) {
      fail(err);
    }
  };

  if (step === 'codes' && session?.recoveryCodes) {
    return (
      <div className={styles.root}>
        <div className={styles.head}>
          <span className={styles.step}>Passo 3 de 3</span>
          <h2 className={styles.title}>Verificação ativada</h2>
          <p className={styles.lead}>Por último, os códigos de recuperação.</p>
        </div>
        <RecoveryCodes codes={session.recoveryCodes} onDone={() => onDone(session)} doneLabel="Entrar no painel" />
      </div>
    );
  }

  const head = setup ? (
    step === 'code' ? (
      <>
        <span className={styles.step}>Passo 1 de 3</span>
        <h2 className={styles.title}>Ative a verificação em duas etapas</h2>
        <p className={styles.lead}>
          A equipe da central entra com a senha e um código do app autenticador. Primeiro, confirme que é você: digite
          o código que mandamos para <strong>{challenge.emailHint}</strong>.
        </p>
      </>
    ) : (
      <>
        <span className={styles.step}>Passo 2 de 3</span>
        <h2 className={styles.title}>Cadastre no app autenticador</h2>
      </>
    )
  ) : (
    <>
      {showTitle && <h2 className={styles.title}>Verificação em duas etapas</h2>}
      <p className={styles.lead}>
        {recovery ? (
          'Digite um dos seus códigos de recuperação (cada um vale uma vez).'
        ) : challenge.method === 'email' ? (
          <>
            Mandamos um código de 6 dígitos para <strong>{challenge.emailHint}</strong>.
          </>
        ) : (
          'Digite o código de 6 dígitos do seu app autenticador.'
        )}
      </p>
    </>
  );

  return (
    <form className={styles.root} onSubmit={onSubmit}>
      <div className={styles.head}>{head}</div>
      {setup && step === 'app' && enrollment && <EnrollApp enrollment={enrollment} />}

      <input
        ref={input}
        className={`${styles.code} ${recovery ? styles.codeRecovery : ''}`}
        aria-label={recovery ? 'Código de recuperação' : 'Código de 6 dígitos'}
        inputMode={recovery ? 'text' : 'numeric'}
        autoComplete="one-time-code"
        autoCapitalize="none"
        spellCheck={false}
        placeholder={recovery ? 'abcd-efgh' : '000000'}
        value={code}
        onChange={(e) => type(e.target.value)}
      />
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
      {(!setup || step === 'app') && (
        <label className={styles.check}>
          <input type="checkbox" checked={trust} onChange={(e) => setTrust(e.target.checked)} />
          <span>Confiar neste aparelho por 30 dias (não use em computador compartilhado).</span>
        </label>
      )}
      <Button type="submit" variant="primary" size="large" block loading={busy} disabled={!code}>
        {setup ? (step === 'code' ? 'Continuar' : 'Ativar') : 'Entrar'}
      </Button>

      <div className={styles.links}>
        {emailCode && (
          <button type="button" className={styles.linkButton} disabled={cooldown > 0} onClick={() => void resend()}>
            {cooldown > 0 ? `Reenviar o código (${cooldown} s)` : 'Reenviar o código'}
          </button>
        )}
        {!setup && (
          <button
            type="button"
            className={styles.linkButton}
            onClick={() => {
              setRecovery(!recovery);
              setCode('');
              setError('');
            }}
          >
            {recovery ? 'Usar o código de 6 dígitos' : 'Usar um código de recuperação'}
          </button>
        )}
        <button type="button" className={styles.linkButton} onClick={() => onCancel()}>
          Voltar
        </button>
      </div>
    </form>
  );
}

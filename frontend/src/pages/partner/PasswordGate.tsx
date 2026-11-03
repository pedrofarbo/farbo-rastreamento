import { useState } from 'react';
import type { FormEvent } from 'react';

import { ApiError } from '@/api/client';
import { publicApi } from '@/api/resources';
import { savePartnerSession } from '@/services/partnerSession';

import base from '../PreLaunch.module.css';

/**
 * A senha da página do afiliado (quando a central pôs uma): a entrada vale
 * 30 dias neste aparelho. Esqueceu? A central troca no cadastro dele.
 */
export function PasswordGate({ token, onUnlocked }: { token: string; onUnlocked: () => void }) {
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!password || busy) return;
    setBusy(true);
    setError('');
    try {
      const { session, expiresAt } = await publicApi.partnerLogin(token, password);
      savePartnerSession(token, session, expiresAt);
      onUnlocked();
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 401
          ? 'Senha incorreta.'
          : err instanceof ApiError && err.status === 429
            ? 'Muitas tentativas. Espere um minuto e tente de novo.'
            : 'Não deu para entrar agora. Tente de novo em instantes.',
      );
      setBusy(false);
    }
  };

  return (
    <>
      <section className={base.intro}>
        <h1 className={base.title}>Sua página de parceiro</h1>
        <p className={base.lead}>Esta página pede senha. Digite a senha que a Farbo combinou com você.</p>
      </section>
      <form className={base.form} onSubmit={submit} noValidate>
        <label className={base.field}>
          <span>Senha</span>
          <input
            type="password"
            autoComplete="current-password"
            autoFocus
            maxLength={72}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        {error && (
          <p className={base.error} role="alert">
            {error}
          </p>
        )}
        <button type="submit" className={base.submit} disabled={!password || busy}>
          {busy ? 'Entrando…' : 'Entrar'}
        </button>
        <p className={base.small}>Esqueceu a senha? Fale com a Farbo para trocar.</p>
      </form>
    </>
  );
}

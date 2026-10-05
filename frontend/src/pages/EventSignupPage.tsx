import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Link, useParams } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { publicApi } from '@/api/resources';
import { INSTAGRAM_HANDLE, INSTAGRAM_URL } from '@/config/contact';
import { INSANOS_MONTHLY, LAUNCH_OFFER, eventSlug } from '@/config/landing';
import { PRIVACY_PATH } from '@/config/legal';
import { track, trackPageview } from '@/services/analytics';
import { formatPhoneInput, isPhoneComplete } from '@/services/format';

import styles from './EventSignup.module.css';

/**
 * Cadastro no pré-lançamento em eventos: a tela que o QR Code do estande abre
 * no celular (/evento/<nome-do-evento>). Poucos campos e grandes, a promoção
 * em destaque e, depois do cadastro, o botão para cadastrar a próxima pessoa
 * (quando alguém da equipe cadastra no tablet). A inscrição vai para a lista
 * de lançamento com o nome do evento.
 */
export function EventSignupPage() {
  const { evento = '' } = useParams();
  const event = eventSlug(evento);
  const [name, setName] = useState('');
  const [phone, setPhone] = useState('');
  const [email, setEmail] = useState('');
  const [consent, setConsent] = useState(false);
  const [website, setWebsite] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState<{ name: string; email: string } | null>(null);
  const first = useRef<HTMLInputElement>(null);

  useEffect(() => {
    document.title = 'Pré-lançamento · Farbo Rastreadores';
    trackPageview({ source: 'evento', campaign: event });
  }, [event]);

  const ready = name.trim() !== '' && isPhoneComplete(phone) && email.trim() !== '' && consent;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!ready || sending) return;
    setError('');
    setSending(true);
    try {
      await publicApi.joinLaunch({ name, email, phone, consent, website, event });
      track('waitlist_submit', event || 'evento');
      setDone({ name: name.trim().split(' ')[0], email: email.trim() });
      window.scrollTo({ top: 0 });
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 429
          ? 'Muita gente se cadastrando agora. Espere uns segundos e toque de novo.'
          : err instanceof Error && err.message
            ? err.message
            : 'Não deu para enviar agora. Tente de novo.',
      );
    } finally {
      setSending(false);
    }
  };

  const next = () => {
    setName('');
    setPhone('');
    setEmail('');
    setConsent(false);
    setDone(null);
    setError('');
    window.scrollTo({ top: 0 });
    setTimeout(() => first.current?.focus(), 50);
  };

  return (
    <main className={styles.page}>
      <header className={styles.header}>
        <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={styles.logo} />
        <span className={styles.badge}>Pré-lançamento</span>
      </header>

      {done ? (
        <section className={styles.done} role="status" aria-live="polite">
          <div className={styles.doneIcon} aria-hidden="true">
            ✓
          </div>
          <h1 className={styles.title}>Pronto, {done.name}!</h1>
          <p className={styles.lead}>
            Você está na lista do pré-lançamento. Vamos avisar no seu WhatsApp e no e-mail{' '}
            <strong>{done.email}</strong> assim que lançarmos.
          </p>
          <p className={styles.hint}>
            Na hora de contratar, use este mesmo e-mail: é por ele que você garante o preço de pré-lançamento.
          </p>
          <div className={styles.actions}>
            <a className={styles.primary} href={INSTAGRAM_URL} target="_blank" rel="noopener noreferrer">
              Seguir @{INSTAGRAM_HANDLE}
            </a>
            <Link className={styles.secondary} to="/">
              Conhecer a Farbo
            </Link>
            <button type="button" className={styles.ghost} onClick={next}>
              Cadastrar outra pessoa
            </button>
          </div>
        </section>
      ) : (
        <>
          <section className={styles.intro}>
            <h1 className={styles.title}>Garanta o preço de pré-lançamento</h1>
            <p className={styles.lead}>
              Rastreador para moto e carro, com app, alertas e bloqueio pelo celular. Cadastre-se e seja avisado no
              lançamento.
            </p>
            <div className={styles.offer}>
              <div className={styles.offerRow}>
                <span>Rastreador</span>
                <span>
                  <s>{LAUNCH_OFFER.equipmentRegular}</s> <strong>{LAUNCH_OFFER.equipment}</strong>
                </span>
              </div>
              <div className={styles.offerRow}>
                <span>Mensalidade por {LAUNCH_OFFER.months} meses</span>
                <span>
                  <s>{LAUNCH_OFFER.monthlyRegular}</s> <strong>{LAUNCH_OFFER.monthly}</strong>
                </span>
              </div>
              <p className={styles.offerNote}>
                Insanos MC: {LAUNCH_OFFER.insanosMonthly} por mês nos {LAUNCH_OFFER.months} meses (depois,{' '}
                {INSANOS_MONTHLY.label}). Para os {LAUNCH_OFFER.slots} primeiros da lista, 1 veículo por pessoa.
              </p>
            </div>
          </section>

          <form className={styles.form} onSubmit={submit} noValidate>
            <label className={styles.field}>
              <span>Nome</span>
              <input
                ref={first}
                type="text"
                autoComplete="name"
                autoCapitalize="words"
                enterKeyHint="next"
                maxLength={120}
                placeholder="Como podemos te chamar"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </label>
            <label className={styles.field}>
              <span>WhatsApp</span>
              <input
                type="tel"
                inputMode="tel"
                autoComplete="tel-national"
                enterKeyHint="next"
                maxLength={16}
                placeholder="(11) 99999-9999"
                value={phone}
                onChange={(e) => setPhone(formatPhoneInput(e.target.value, phone))}
              />
            </label>
            <label className={styles.field}>
              <span>E-mail</span>
              <input
                type="email"
                inputMode="email"
                autoComplete="email"
                autoCapitalize="none"
                enterKeyHint="done"
                maxLength={254}
                placeholder="voce@email.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </label>

            {/* Isca para robôs: fora da tela e fora do Tab. */}
            <div className={styles.honeypot} aria-hidden="true">
              <label>
                Site
                <input type="text" name="website" tabIndex={-1} autoComplete="off" value={website} onChange={(e) => setWebsite(e.target.value)} />
              </label>
            </div>

            <label className={styles.consent}>
              <input type="checkbox" checked={consent} onChange={(e) => setConsent(e.target.checked)} />
              <span>
                Aceito receber o aviso do lançamento da Farbo Rastreadores por WhatsApp e e-mail (veja a{' '}
                <a href={PRIVACY_PATH} target="_blank" rel="noopener noreferrer">
                  Política de Privacidade
                </a>
                ).
              </span>
            </label>

            {error && (
              <p className={styles.error} role="alert">
                {error}
              </p>
            )}

            <button type="submit" className={styles.submit} disabled={!ready || sending}>
              {sending ? 'Enviando…' : 'Garantir meu preço'}
            </button>
            <p className={styles.small}>Sem compromisso e sem cobrança: é só o aviso do lançamento.</p>
          </form>
        </>
      )}
    </main>
  );
}

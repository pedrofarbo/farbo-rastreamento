import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Link } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { publicApi } from '@/api/resources';
import { INSTAGRAM_HANDLE, INSTAGRAM_URL } from '@/config/contact';
import { checkoutPath, loanOnly, priceLabel, priceParts, trackerAdded } from '@/config/landing';
import { PRIVACY_PATH } from '@/config/legal';
import { useCheckoutOffer } from '@/hooks/useCheckoutOffer';
import { track, trackPageview } from '@/services/analytics';
import { headlineMonthly, kindPricesLabel } from '@/services/kindPrices';
import { formatPhoneInput, isPhoneComplete } from '@/services/format';
import type { CheckoutOffer, PublicAffiliate } from '@/types';

import styles from './PreLaunch.module.css';

interface CheckoutInviteProps {
  /** A origem nas visitas (evento ou indicacao) e a campanha (o evento ou o código do link). */
  source: 'evento' | 'indicacao';
  campaign: string;
  /** O evento da tela do QR Code (vai na contratação e no contato). */
  event?: string;
  /** O código do link de indicação (vai no contato; a contratação o lê do aparelho). */
  referral?: string;
  /** Quem indica: o selo "Indicado por @fulano". */
  referrer?: PublicAffiliate | null;
  /** Depois do contato, o botão para cadastrar a próxima pessoa (o tablet do estande). */
  allowNext?: boolean;
}

/**
 * A chamada para contratar das telas fora da landing: a do QR Code dos
 * eventos e a do link de indicação dos afiliados. O preço em destaque (da
 * oferta do servidor, o mesmo do funil) e o botão que abre a contratação
 * pelo site; quem prefere conversar deixa o contato, que vai para a lista
 * da central com o evento ou o afiliado.
 */
export function CheckoutInvite({
  source,
  campaign,
  event = '',
  referral = '',
  referrer,
  allowNext,
}: CheckoutInviteProps) {
  // O plano do Insanos MC fica fora: só a central coloca o cliente nele. Pelo
  // link do afiliado, os preços dele; do evento, sem o link guardado no
  // aparelho (pode ser o tablet da equipe).
  const { offer, open } = useCheckoutOffer(source === 'evento' ? '' : referral);
  // A contratação fechada na central: o contato é o caminho.
  const [contact, setContact] = useState(false);
  const [name, setName] = useState('');
  const [phone, setPhone] = useState('');
  const [city, setCity] = useState('');
  const [email, setEmail] = useState('');
  const [consent, setConsent] = useState(false);
  const [website, setWebsite] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState<{ name: string; email: string } | null>(null);
  const first = useRef<HTMLInputElement>(null);

  useEffect(() => {
    document.title = 'Contrate o seu rastreador · Farbo Rastreadores';
    trackPageview({ source, campaign });
  }, [source, campaign]);

  const showForm = contact || !open;
  const ready = name.trim() !== '' && isPhoneComplete(phone) && city.trim() !== '' && email.trim() !== '' && consent;
  const contractUrl = checkoutPath({ event });

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!ready || sending) return;
    setError('');
    setSending(true);
    try {
      await publicApi.joinLaunch({
        name, email, phone, consent, website, event, city, ...(referral ? { ref: referral } : {}),
      });
      track('waitlist_submit', campaign || source);
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
    setCity('');
    setEmail('');
    setConsent(false);
    setDone(null);
    setError('');
    setContact(true);
    window.scrollTo({ top: 0 });
    setTimeout(() => first.current?.focus(), 50);
  };

  const openContact = () => {
    setContact(true);
    setTimeout(() => first.current?.focus(), 50);
  };

  const referrerLabel = referrer ? (referrer.handle ? `@${referrer.handle}` : referrer.name) : '';

  return (
    <main className={styles.page}>
      <header className={styles.header}>
        <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={styles.logo} />
        <span className={styles.badge}>Pix na hora</span>
      </header>

      {done ? (
        <section className={styles.done} role="status" aria-live="polite">
          <div className={styles.doneIcon} aria-hidden="true">
            ✓
          </div>
          <h1 className={styles.title}>Pronto, {done.name}!</h1>
          <p className={styles.lead}>
            A nossa equipe vai falar com você no WhatsApp e no e-mail <strong>{done.email}</strong>.
          </p>
          {open && <p className={styles.hint}>Se preferir não esperar, contrate agora pelo site e pague com Pix.</p>}
          <div className={styles.actions}>
            {open && (
              <Link className={styles.primary} to={contractUrl}>
                Contratar agora
              </Link>
            )}
            <a className={styles.secondary} href={INSTAGRAM_URL} target="_blank" rel="noopener noreferrer">
              Seguir @{INSTAGRAM_HANDLE}
            </a>
            {allowNext && (
              <button type="button" className={styles.ghost} onClick={next}>
                Cadastrar outra pessoa
              </button>
            )}
          </div>
        </section>
      ) : (
        <>
          <section className={styles.intro}>
            {referrerLabel && (
              <p className={styles.referral}>
                <span className={styles.referralAvatar} aria-hidden="true">
                  {(referrer?.handle || referrer?.name || '?').charAt(0)}
                </span>
                <span>
                  Indicado por <strong>{referrerLabel}</strong>
                </span>
              </p>
            )}
            <h1 className={styles.title}>Contrate o seu rastreador</h1>
            <p className={styles.lead}>
              Para moto e carro, com app, alertas e bloqueio pelo celular. Escolha o plano, pague com Pix e receba em
              casa.
            </p>
            <Highlight offer={offer} open={open} />
          </section>

          {!showForm && (
            <div className={styles.actions}>
              <Link className={styles.primary} to={contractUrl} data-analytics="convite-contratar">
                Contratar agora
              </Link>
              <button type="button" className={styles.secondary} onClick={openContact}>
                Prefiro que a Farbo fale comigo
              </button>
            </div>
          )}

          {showForm && (
            <form className={styles.form} onSubmit={submit} noValidate>
              <p className={styles.formTitle}>Deixe o seu contato que a gente fala com você</p>
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
                <span>Cidade da instalação</span>
                <input
                  type="text"
                  autoComplete="address-level2"
                  autoCapitalize="words"
                  enterKeyHint="next"
                  maxLength={120}
                  placeholder="Ex.: São Paulo - SP"
                  value={city}
                  onChange={(e) => setCity(e.target.value)}
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
                  Aceito que a Farbo Rastreadores fale comigo por WhatsApp e e-mail (veja a{' '}
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
                {sending ? 'Enviando…' : 'Quero que me chamem'}
              </button>
              {open ? (
                <Link className={styles.ghost} to={contractUrl}>
                  Prefiro contratar agora
                </Link>
              ) : (
                <p className={styles.small}>Sem compromisso: a nossa equipe fala com você.</p>
              )}
            </form>
          )}
        </>
      )}
    </main>
  );
}

/** O preço em destaque e o rastreador incluso, da oferta do servidor (carregando, o lugar deles). */
function Highlight({ offer, open }: { offer: CheckoutOffer | undefined; open: boolean }) {
  // Pelo link de um afiliado com tabela: a menor mensalidade e as três.
  const headline = offer && headlineMonthly(offer, open);
  const table = headline?.table ?? null;
  const monthly = headline?.cents;
  const regular = offer && open ? offer.planRegularCents : undefined;
  // Pela tabela do afiliado, a parte do rastreador incluso soma nas primeiras.
  const added = trackerAdded(offer, open);
  return (
    <div className={styles.insanos} aria-busy={!offer}>
      <span className={styles.insanosTag}>
        {table ? 'A partir de' : regular !== undefined && monthly !== undefined && regular > monthly ? 'Preço promocional' : 'Plano mensal'}
      </span>
      <BigPrice cents={monthly} />
      {offer && (
        <>
          {table && <p className={styles.insanosEquipment}>{kindPricesLabel(table, priceLabel)}</p>}
          <p className={styles.insanosNote}>
            por veículo{open && ', enquanto a assinatura durar'}
            {regular !== undefined && monthly !== undefined && regular > monthly && (
              <>
                {' '}
                · o normal é <s className={styles.nowrap}>{priceLabel(regular)}</s>
              </>
            )}
          </p>
          {loanOnly(offer, open) ? (
            <p className={styles.insanosEquipment}>Rastreador em comodato</p>
          ) : (
            <p className={styles.insanosEquipment}>
              <strong className={styles.nowrap}>Rastreador incluso</strong>, que fica com você: só o frete à parte
              {added && (
                <span className={styles.installments}>
                  + {priceLabel(added.cents)}/mês nas {added.months} primeiras mensalidades
                </span>
              )}
              {open && offer.loan && <span className={styles.installments}>ou em comodato</span>}
            </p>
          )}
        </>
      )}
    </div>
  );
}

function BigPrice({ cents }: { cents: number | undefined }) {
  if (cents === undefined) {
    return <p className={`${styles.insanosPrice} ${styles.pricePending}`}>R$ —</p>;
  }
  const parts = priceParts(cents);
  return (
    <p className={styles.insanosPrice}>
      <span className={styles.insanosCurrency}>R$</span>
      <span className={styles.insanosReais}>{parts.reais}</span>
      <span className={styles.insanosCents}>
        {parts.cents}
        <small>/mês</small>
      </span>
    </p>
  );
}

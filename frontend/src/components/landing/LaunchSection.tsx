import React, { useState } from 'react';

import { publicApi } from '@/api/resources';
import { PRIVACY_PATH } from '@/config/legal';
import { track } from '@/services/analytics';
import { currentReferral } from '@/services/referral';
import { CONTACT_EMAIL } from '@/config/contact';
import { INSANOS_MONTHLY, LAUNCH_OFFER } from '@/config/landing';
import { formatPhoneInput, isPhoneComplete } from '@/services/format';

import styles from './LaunchSection.module.css';

const PERKS = [
  { title: 'Aviso em primeira mão', text: 'Você fica sabendo assim que os rastreadores estiverem disponíveis.' },
  { title: 'Sem compromisso', text: 'Só o aviso do lançamento: nada de spam nem de cobrança.' },
  { title: 'Sai quando quiser', text: `Para deixar a lista, é só pedir por e-mail em ${CONTACT_EMAIL}.` },
];

/**
 * Pré-lançamento: quem tem interesse deixa o e-mail para ser avisado quando a
 * Farbo lançar. Fica no lugar dos depoimentos até haver clientes ativos.
 */
export const LaunchSection: React.FC = () => {
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [phone, setPhone] = useState('');
  const [consent, setConsent] = useState(false);
  const [website, setWebsite] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState('');
  const [joined, setJoined] = useState(false);

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    setError('');
    setSending(true);
    try {
      await publicApi.joinLaunch({ name, email, phone, consent, website, ref: currentReferral() });
      setJoined(true);
      track('waitlist_submit');
    } catch (err) {
      setError(err instanceof Error && err.message ? err.message : 'Não deu para enviar agora. Tente de novo.');
    } finally {
      setSending(false);
    }
  };

  return (
    <section className={styles.section} id="lancamento" aria-labelledby="lancamento-titulo">
      <div className={styles.container}>
        <div className={styles.card}>
          <div className={styles.intro}>
            <span className={styles.badge}>Pré-lançamento</span>
            <h2 className={styles.title} id="lancamento-titulo">
              Seja avisado no lançamento
            </h2>
            <p className={styles.subtitle}>
              A Farbo Rastreadores está chegando. Entre na lista para ser avisado do lançamento e garantir o preço
              de pré-lançamento para o seu carro ou a sua moto.
            </p>

            <div className={styles.offer}>
              <span className={styles.offerTag}>Promoção de pré-lançamento · só para quem está na lista</span>
              <div className={styles.offerPrices}>
                <div className={styles.offerItem}>
                  <span className={styles.offerLabel}>Rastreador</span>
                  <span>
                    <s className={styles.offerOld}>{LAUNCH_OFFER.equipmentRegular}</s>{' '}
                    <strong className={styles.offerPrice}>{LAUNCH_OFFER.equipment}</strong>
                  </span>
                </div>
                <div className={styles.offerItem}>
                  <span className={styles.offerLabel}>Mensalidade por {LAUNCH_OFFER.months} meses</span>
                  <span>
                    <s className={styles.offerOld}>{LAUNCH_OFFER.monthlyRegular}</s>{' '}
                    <strong className={styles.offerPrice}>{LAUNCH_OFFER.monthly}</strong>
                  </span>
                </div>
              </div>
              <p className={styles.offerNote}>
                Limitado aos {LAUNCH_OFFER.slots} primeiros clientes da lista, 1 veículo por cliente. Integrantes do
                Insanos MC pagam <span className={styles.nowrap}>{LAUNCH_OFFER.insanosMonthly}</span> nos{' '}
                {LAUNCH_OFFER.months} meses. Depois deles, a mensalidade passa a{' '}
                <span className={styles.nowrap}>{LAUNCH_OFFER.monthlyRegular}</span> (
                <span className={styles.nowrap}>{INSANOS_MONTHLY.label}</span> para o Insanos MC).
              </p>
            </div>
            <ul className={styles.perks}>
              {PERKS.map((perk) => (
                <li key={perk.title}>
                  <span className={styles.check} aria-hidden="true">
                    ✓
                  </span>
                  <span>
                    <strong>{perk.title}</strong>
                    <br />
                    {perk.text}
                  </span>
                </li>
              ))}
            </ul>
          </div>

          {joined ? (
            <div className={styles.done} role="status">
              <div className={styles.doneIcon} aria-hidden="true">
                ✓
              </div>
              <h3>Você está na lista!</h3>
              <p>
                Vamos avisar no e-mail <strong>{email}</strong> assim que lançarmos.
              </p>
              <p className={styles.doneHint}>
                Na hora de contratar, use este mesmo e-mail: é por ele que garantimos o preço de pré-lançamento.
              </p>
            </div>
          ) : (
            <form className={styles.form} onSubmit={submit} noValidate>
              <label className={styles.field}>
                <span>
                  Seu nome <span className={styles.optional}>(opcional)</span>
                </span>
                <input
                  type="text"
                  autoComplete="name"
                  autoCapitalize="words"
                  maxLength={120}
                  placeholder="Como podemos te chamar"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </label>
              <div className={styles.row}>
                <label className={styles.field}>
                  <span>E-mail</span>
                  <input
                    type="email"
                    autoComplete="email"
                    inputMode="email"
                    autoCapitalize="none"
                    maxLength={254}
                    placeholder="voce@email.com"
                    required
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                  />
                </label>
                <label className={styles.field}>
                  <span>WhatsApp</span>
                  <input
                    type="tel"
                    inputMode="tel"
                    autoComplete="tel-national"
                    maxLength={16}
                    placeholder="(11) 99999-9999"
                    required
                    value={phone}
                    onChange={(e) => setPhone(formatPhoneInput(e.target.value, phone))}
                  />
                </label>
              </div>

              {/* Isca para robôs: fora da tela e fora do Tab. */}
              <div className={styles.honeypot} aria-hidden="true">
                <label>
                  Site
                  <input
                    type="text"
                    name="website"
                    tabIndex={-1}
                    autoComplete="off"
                    value={website}
                    onChange={(e) => setWebsite(e.target.value)}
                  />
                </label>
              </div>

              <label className={styles.consent}>
                <input type="checkbox" checked={consent} onChange={(e) => setConsent(e.target.checked)} />
                <span>
                  Aceito receber o aviso do lançamento da Farbo Rastreadores por e-mail e WhatsApp (veja a{' '}
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

              <button type="submit" className={styles.submit} disabled={sending || !consent || !email.trim() || !isPhoneComplete(phone)}>
                {sending ? 'Enviando…' : 'Quero entrar na lista'}
              </button>
            </form>
          )}
        </div>
      </div>
    </section>
  );
};

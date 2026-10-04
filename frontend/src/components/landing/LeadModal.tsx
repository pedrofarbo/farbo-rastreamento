import React, { useEffect, useState } from 'react';

import { publicApi } from '@/api/resources';
import { CONTACT_EMAIL } from '@/config/contact';
import { LAUNCH_OFFER, PRE_LAUNCH } from '@/config/landing';
import { track } from '@/services/analytics';
import { formatPhoneInput, isPhoneComplete } from '@/services/format';
import type { LeadVehicleType } from '@/types';

import base from './ContactModal.module.css';
import styles from './LeadModal.module.css';
import { useModalBehavior } from './useModalBehavior';

const PLANS = [
  { value: 'Plano Mensal - R$ 69,90', label: 'Plano Mensal (R$ 69,90/mês)' },
  { value: 'Preço Especial Insanos MC - R$ 39,90', label: 'Insanos MC (R$ 39,90/mês)' },
  { value: 'Apenas Equipamento - R$ 150,00', label: 'Apenas Equipamento (R$ 150,00)' },
];

/** No plano do Insanos MC, a promoção de pré-lançamento tem a mensalidade deles. */
const INSANOS_PLAN = PLANS[1].value;

const VEHICLE_TYPES = [
  ['moto', '🏍 Moto'],
  ['carro', '🚗 Carro'],
  ['frota', '🚚 Frota'],
] as const;

/** Frota grande cabe aqui; o servidor aceita até 500. */
const MAX_VEHICLES = 500;

interface LeadModalProps {
  isOpen: boolean;
  onClose: () => void;
  defaultPlan?: string;
}

/**
 * Cadastro de interesse: enquanto não há WhatsApp oficial, quem quer o
 * rastreador deixa os dados e vira pré-cliente; a equipe responde por e-mail.
 *
 * Compacto de propósito: no computador os campos ficam lado a lado e o modal
 * cabe numa tela de notebook; no celular, a folha que sobe de baixo cabe sem
 * rolar na maioria dos aparelhos (e rola quando não cabe).
 */
export const LeadModal: React.FC<LeadModalProps> = ({ isOpen, onClose, defaultPlan }) => {
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [phone, setPhone] = useState('');
  const [city, setCity] = useState('');
  const [plan, setPlan] = useState(PLANS[0].value);
  const [vehicleType, setVehicleType] = useState<LeadVehicleType>('moto');
  const [vehicleCount, setVehicleCount] = useState(1);
  const [message, setMessage] = useState('');
  // A mensagem é rara: fica atrás de um link até alguém querer escrever.
  const [showMessage, setShowMessage] = useState(false);
  const [consent, setConsent] = useState(false);
  // No pré-lançamento, entrar também na lista (aviso e promoção) vem marcado.
  const [joinLaunch, setJoinLaunch] = useState(PRE_LAUNCH);
  const [website, setWebsite] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState('');
  const [sent, setSent] = useState(false);

  const close = () => {
    if (sent) {
      // Enviado: a próxima abertura começa do zero.
      setName('');
      setEmail('');
      setPhone('');
      setCity('');
      setVehicleCount(1);
      setMessage('');
      setShowMessage(false);
      setConsent(false);
      setJoinLaunch(PRE_LAUNCH);
      setSent(false);
    }
    setError('');
    onClose();
  };
  useModalBehavior(isOpen, close);

  // O botão que abriu (o card do plano) escolhe o plano.
  useEffect(() => {
    if (isOpen) setPlan(PLANS.find((p) => p.value === defaultPlan)?.value ?? PLANS[0].value);
  }, [isOpen, defaultPlan]);

  if (!isOpen) return null;

  const setCount = (n: number) => setVehicleCount(Math.min(MAX_VEHICLES, Math.max(1, Number.isFinite(n) ? n : 1)));

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    setError('');
    setSending(true);
    try {
      await publicApi.createLead({
        name, email, phone, city, plan, vehicleType, vehicleCount, message, consent, website,
        joinLaunch: PRE_LAUNCH && joinLaunch,
      });
      setSent(true);
      track('lead_submit', plan);
    } catch (err) {
      setError(
        err instanceof Error && err.message
          ? err.message
          : `Não deu para enviar agora. Tente de novo ou escreva para ${CONTACT_EMAIL}.`,
      );
    } finally {
      setSending(false);
    }
  };

  return (
    <div className={base.overlay} onClick={close} id="lead-modal-overlay">
      <div
        className={`${base.modal} ${styles.modal}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby="lead-modal-title"
        onClick={(e) => e.stopPropagation()}
      >
        <button type="button" className={base.closeBtn} onClick={close} aria-label="Fechar">
          ✕
        </button>

        {sent ? (
          <div className={styles.done} role="status">
            <div className={styles.doneIcon} aria-hidden="true">
              ✓
            </div>
            <h2 id="lead-modal-title">Recebemos seu interesse!</h2>
            <p>
              Vamos responder no e-mail <strong>{email}</strong> com os próximos passos para proteger o seu veículo.
            </p>
            {PRE_LAUNCH && joinLaunch && (
              <p>
                Você também entrou na lista de pré-lançamento. Na hora de contratar, use este mesmo e-mail para
                garantir a promoção.
              </p>
            )}
            <p className={styles.muted}>
              Ficou alguma dúvida? Escreva para <a href={`mailto:${CONTACT_EMAIL}`}>{CONTACT_EMAIL}</a>.
            </p>
            <button type="button" className={base.submitBtn} onClick={close}>
              Fechar
            </button>
          </div>
        ) : (
          <>
            <div className={styles.head}>
              <div className={base.badge}>Pré-cadastro</div>
              <h2 id="lead-modal-title">Quero meu rastreador</h2>
              <p>Deixe seus dados e a nossa equipe responde por e-mail.</p>
            </div>

            <form onSubmit={submit} className={styles.form} noValidate>
              <div className={`${styles.row} ${styles.rowKeep}`}>
                <div className={styles.field}>
                  <label htmlFor="lead-name">Seu nome</label>
                  <input
                    id="lead-name"
                    className={styles.input}
                    type="text"
                    autoComplete="name"
                    autoCapitalize="words"
                    enterKeyHint="next"
                    placeholder="Nome completo"
                    maxLength={120}
                    required
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                  />
                </div>
                <div className={styles.field}>
                  <label htmlFor="lead-email">E-mail</label>
                  <input
                    id="lead-email"
                    className={styles.input}
                    type="email"
                    autoComplete="email"
                    inputMode="email"
                    autoCapitalize="none"
                    enterKeyHint="next"
                    placeholder="voce@email.com"
                    maxLength={254}
                    required
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                  />
                </div>
              </div>

              <div className={`${styles.row} ${styles.rowKeep}`}>
                <div className={styles.field}>
                  <label htmlFor="lead-phone">WhatsApp</label>
                  <input
                    id="lead-phone"
                    className={styles.input}
                    type="tel"
                    inputMode="tel"
                    autoComplete="tel-national"
                    enterKeyHint="next"
                    placeholder="(11) 99999-9999"
                    maxLength={16}
                    required
                    value={phone}
                    onChange={(e) => setPhone(formatPhoneInput(e.target.value, phone))}
                  />
                </div>
                <div className={styles.field}>
                  <label htmlFor="lead-city">
                    Cidade <span className={styles.optional}>(opcional)</span>
                  </label>
                  <input
                    id="lead-city"
                    className={styles.input}
                    type="text"
                    autoComplete="address-level2"
                    enterKeyHint="next"
                    placeholder="Ex.: Campinas"
                    maxLength={80}
                    value={city}
                    onChange={(e) => setCity(e.target.value)}
                  />
                </div>
              </div>

              <div className={styles.field}>
                <label htmlFor="lead-plan">Plano</label>
                <select id="lead-plan" className={styles.input} value={plan} onChange={(e) => setPlan(e.target.value)}>
                  {PLANS.map((p) => (
                    <option key={p.value} value={p.value}>
                      {p.label}
                    </option>
                  ))}
                </select>
              </div>

              <div className={styles.inline}>
                <div className={styles.field}>
                  <span className={styles.label} id="lead-type-label">
                    Tipo de veículo
                  </span>
                  <div className={`${base.typeSelector} ${styles.types}`} role="radiogroup" aria-labelledby="lead-type-label">
                    {VEHICLE_TYPES.map(([value, label]) => (
                      <button
                        key={value}
                        type="button"
                        role="radio"
                        aria-checked={vehicleType === value}
                        className={vehicleType === value ? base.activeType : ''}
                        onClick={() => setVehicleType(value)}
                      >
                        {label}
                      </button>
                    ))}
                  </div>
                </div>
                <div className={styles.field}>
                  <label htmlFor="lead-count">Quantidade</label>
                  <div className={styles.stepper}>
                    <button
                      type="button"
                      aria-label="Um veículo a menos"
                      disabled={vehicleCount <= 1}
                      onClick={() => setCount(vehicleCount - 1)}
                    >
                      −
                    </button>
                    <input
                      id="lead-count"
                      type="number"
                      inputMode="numeric"
                      min={1}
                      max={MAX_VEHICLES}
                      value={vehicleCount}
                      onChange={(e) => setCount(parseInt(e.target.value, 10))}
                    />
                    <button
                      type="button"
                      aria-label="Um veículo a mais"
                      disabled={vehicleCount >= MAX_VEHICLES}
                      onClick={() => setCount(vehicleCount + 1)}
                    >
                      +
                    </button>
                  </div>
                </div>
              </div>

              {showMessage ? (
                <div className={styles.field}>
                  <label htmlFor="lead-message">
                    Mensagem <span className={styles.optional}>(opcional)</span>
                  </label>
                  <textarea
                    id="lead-message"
                    className={styles.input}
                    rows={2}
                    maxLength={1000}
                    autoFocus
                    placeholder="Modelo do veículo, dúvidas…"
                    value={message}
                    onChange={(e) => setMessage(e.target.value)}
                  />
                </div>
              ) : (
                <button type="button" className={styles.linkButton} onClick={() => setShowMessage(true)}>
                  + Escrever uma mensagem (opcional)
                </button>
              )}

              {/* Isca para robôs: fora da tela e fora do Tab. */}
              <div className={styles.honeypot} aria-hidden="true">
                <label htmlFor="lead-website">Site</label>
                <input
                  id="lead-website"
                  type="text"
                  tabIndex={-1}
                  autoComplete="off"
                  value={website}
                  onChange={(e) => setWebsite(e.target.value)}
                />
              </div>

              {PRE_LAUNCH && (
                <label className={`${styles.consent} ${styles.launchOption}`}>
                  <input
                    id="lead-launch"
                    type="checkbox"
                    checked={joinLaunch}
                    onChange={(e) => setJoinLaunch(e.target.checked)}
                  />
                  <span>
                    <strong>Entrar também na lista de pré-lançamento</strong>: promoção para os {LAUNCH_OFFER.slots}{' '}
                    primeiros — rastreador por {LAUNCH_OFFER.equipment} e{' '}
                    {plan === INSANOS_PLAN ? LAUNCH_OFFER.insanosMonthly : LAUNCH_OFFER.monthly}/mês por{' '}
                    {LAUNCH_OFFER.months} meses.
                  </span>
                </label>
              )}

              <label className={styles.consent}>
                <input
                  id="lead-consent"
                  type="checkbox"
                  checked={consent}
                  onChange={(e) => setConsent(e.target.checked)}
                />
                <span>Aceito ser contatado pela Farbo Rastreadores sobre a contratação.</span>
              </label>

              {error && (
                <p className={styles.error} role="alert">
                  {error}
                </p>
              )}

              <button
                type="submit"
                className={`${base.submitBtn} ${styles.submit}`}
                disabled={sending || !consent || !name.trim() || !email.trim() || !isPhoneComplete(phone)}
              >
                <span>{sending ? 'Enviando…' : 'Enviar meu interesse'}</span>
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="M5 12h14M12 5l7 7-7 7" />
                </svg>
              </button>
            </form>
          </>
        )}
      </div>
    </div>
  );
};

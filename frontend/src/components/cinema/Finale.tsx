import { Link } from 'react-router-dom';

import { WHATSAPP_GREETING, WHATSAPP_NUMBER, whatsappUrl } from '@/config/contact';
import { CHECKOUT_PATH } from '@/config/landing';
import { useCheckoutOffer } from '@/hooks/useCheckoutOffer';

import { Icon, WhatsAppGlyph } from './icons';
import cine from './cine.module.css';
import styles from './Finale.module.css';

/**
 * As rotas que chegam dos cantos (viewBox 1000×600) e param na borda do bloco
 * de texto, sem passar por cima dele; o ponto de chegada acende no fim.
 */
const ROUTES: { d: string; end: [number, number] }[] = [
  { d: 'M-20 90H150V210H240', end: [240, 210] },
  { d: 'M1020 70H850V190H760', end: [760, 190] },
  { d: 'M-20 530H190V410H240', end: [240, 410] },
  { d: 'M1020 550H820V430H760', end: [760, 430] },
  { d: 'M500 -20V70', end: [500, 70] },
  { d: 'M500 620V560', end: [500, 560] },
];

interface FinaleProps {
  onOpenModal: (plan?: string) => void;
}

/**
 * O fim da landing: as rotas neon chegam dos cantos e se encontram no símbolo
 * da Farbo conforme a rolagem (--p), com a frase da marca e a chamada final.
 * Aberta a contratação pelo site, o botão leva a ela; fechada, abre o
 * pré-cadastro. O WhatsApp fica ao lado, quando ligado.
 */
export function Finale({ onOpenModal }: FinaleProps) {
  const { open } = useCheckoutOffer();
  return (
    <section id="contato" className={styles.scene} data-scene="view" aria-labelledby="final-title">
      <svg className={styles.routes} viewBox="0 0 1000 600" preserveAspectRatio="xMidYMid slice" aria-hidden="true">
        {ROUTES.map(({ d, end }) => (
          <g key={d}>
            <path className={styles.glow} d={d} pathLength={1} />
            <path className={styles.line} d={d} pathLength={1} />
            <circle className={styles.end} cx={end[0]} cy={end[1]} r="6" />
          </g>
        ))}
      </svg>
      <div className={styles.pulse} aria-hidden="true" />

      <div className={styles.inner}>
        <img src="/assets/logo-mark.png" alt="" width={175} height={128} className={styles.mark} loading="lazy" />
        <p className={styles.script}>Liberdade com mais segurança</p>
        <h2 id="final-title" className={`${cine.h2} ${styles.title}`}>
          Proteja seu veículo <em>hoje.</em>
        </h2>
        <p className={`${cine.lead} ${styles.lead}`}>
          Bloqueio pelo celular, localização em tempo real e alertas no app, com o rastreador e o chip inclusos.
        </p>
        <div className={styles.ctas}>
          {open ? (
            <Link className={`${cine.btn} ${cine.primary}`} to={CHECKOUT_PATH} data-analytics="final-contratar">
              Quero proteger meu veículo
              <Icon name="arrowRight" size={18} strokeWidth={2.4} />
            </Link>
          ) : (
            <button
              type="button"
              className={`${cine.btn} ${cine.primary}`}
              onClick={() => onOpenModal('Quero meu rastreador')}
              data-analytics="final-contratar"
            >
              Quero proteger meu veículo
              <Icon name="arrowRight" size={18} strokeWidth={2.4} />
            </button>
          )}
          {WHATSAPP_NUMBER && (
            <a
              className={`${cine.btn} ${cine.ghost}`}
              href={whatsappUrl(WHATSAPP_GREETING)}
              target="_blank"
              rel="noopener noreferrer"
              data-analytics="final-whatsapp"
            >
              <WhatsAppGlyph size={18} />
              Falar no WhatsApp
            </a>
          )}
        </div>
        <ul className={styles.trust}>
          <li>
            <Icon name="shield" size={15} />
            Mais segurança
          </li>
          <li>
            <Icon name="pin" size={15} />
            Mais controle
          </li>
          <li>
            <Icon name="lock" size={15} />
            Mais tranquilidade
          </li>
        </ul>
      </div>
    </section>
  );
}

/** A abertura dos planos: o título antes dos cartões (os preços vêm da oferta, na seção de planos). */
export function PlansIntro() {
  return (
    <div className={styles.plansIntro} data-cine-reveal>
      <p className={cine.eyebrow}>Planos</p>
      <h2 className={cine.h2}>
        Um plano. <em>Tudo incluso.</em>
      </h2>
      <p className={cine.lead}>
        Rastreador, chip M2M, app e sistema web numa mensalidade só. Para frotas, um preço especial pelo volume.
      </p>
    </div>
  );
}

/** A barra de progresso da página, no alto da tela (--progress, do useScenes). */
export function ScrollProgress() {
  return <div className={styles.progress} aria-hidden="true" />;
}

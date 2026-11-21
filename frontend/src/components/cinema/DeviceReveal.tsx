import { ANATEL_HOMOLOGATION, ANATEL_LOOKUP_URL, loanOnly } from '@/config/landing';
import { useCheckoutOffer } from '@/hooks/useCheckoutOffer';

import { Icon, type IconName } from './icons';
import cine from './cine.module.css';
import styles from './DeviceReveal.module.css';

/**
 * "O rastreador J16", presa: a foto do rastreador cresce e gira para a frente
 * com o brilho neon, e as qualidades entram uma a uma, ligadas a ele por um
 * traço (data-steps; no computador ficam todas, no celular uma por vez): a
 * homologação da Anatel (com o número), o chip M2M, o relé de bloqueio no kit
 * e o rastreador incluso que fica com você (pelo link só de comodato, o
 * comodato). Sem movimento, tudo já aparece.
 */
export function DeviceReveal() {
  const { offer, open } = useCheckoutOffer();
  const loan = loanOnly(offer, open);
  const callouts: { icon: IconName; title: string; text: string; link?: { href: string; label: string } }[] = [
    {
      icon: 'shield',
      title: 'Homologado pela Anatel',
      text: `Certificado nº ${ANATEL_HOMOLOGATION}`,
      link: { href: ANATEL_LOOKUP_URL, label: 'Conferir na Anatel' },
    },
    { icon: 'chip', title: 'Chip M2M incluso', text: 'Multi operadora, com 20 MB/mês no plano' },
    { icon: 'lock', title: 'Relé de bloqueio no kit', text: 'Para bloquear e desbloquear o motor pelo app' },
    loan
      ? { icon: 'wrench', title: 'Em comodato', text: 'Emprestado e instalado pelos nossos técnicos' }
      : { icon: 'key', title: 'Fica com você', text: 'Incluso na mensalidade: só o frete à parte' },
  ];

  return (
    <section
      id="rastreador"
      className={`${cine.pin} ${styles.scene}`}
      data-scene="pin"
      data-steps={callouts.length}
      aria-labelledby="rastreador-title"
    >
      <div className={`${cine.stage} ${styles.stage}`}>
        <header className={styles.head}>
          <p className={cine.eyebrow}>O rastreador</p>
          <h2 id="rastreador-title" className={`${cine.h2} ${styles.title}`}>
            Chega pronto. <em>É só instalar.</em>
          </h2>
          <p className={`${cine.lead} ${styles.lead}`}>
            O {offer?.equipmentName ?? 'rastreador J16'} sai configurado da nossa base, com o chip M2M e o relé de
            bloqueio no kit.
          </p>
        </header>

        <div className={styles.stageArea}>
          <div className={styles.device} aria-hidden="true">
            <div className={styles.halo} />
            <div className={styles.ring} />
            <img
              src="/assets/rastreador-j16-farbo-640.webp"
              alt=""
              width={640}
              height={850}
              className={styles.img}
              loading="lazy"
              decoding="async"
            />
            <div className={styles.scan} />
          </div>

          <ul className={styles.callouts}>
            {callouts.map((callout, i) => (
              <li key={callout.title} className={`${styles.callout} ${styles[`c${i}`]}`}>
                <span className={styles.calloutIcon}>
                  <Icon name={callout.icon} size={18} />
                </span>
                <span className={styles.calloutText}>
                  <strong>{callout.title}</strong>
                  <small>{callout.text}</small>
                  {callout.link && (
                    <a href={callout.link.href} target="_blank" rel="noopener noreferrer" className={styles.calloutLink}>
                      {callout.link.label}
                    </a>
                  )}
                </span>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </section>
  );
}

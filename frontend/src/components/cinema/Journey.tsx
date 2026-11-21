import type { CSSProperties } from 'react';

import { ACTOR_LABEL, howItWorksSteps, type Step } from '@/components/landing/HowItWorksSection';
import { useCheckoutOffer } from '@/hooks/useCheckoutOffer';

import { Icon, type IconName } from './icons';
import cine from './cine.module.css';
import styles from './Journey.module.css';

const ICONS: Record<Step['art'], IconName> = {
  pedido: 'cart',
  interesse: 'phone',
  whatsapp: 'phone',
  preparo: 'box',
  envio: 'truck',
  instalacao: 'wrench',
  acompanhamento: 'pin',
};

/**
 * A estrada em zigue-zague com trechos iguais: o veículo (animateMotion, em
 * velocidade constante) chega ao marco i em i/(n-1) do tempo, o mesmo ponto
 * em que o --ps do data-snap para.
 */
function road(n: number, vertical: boolean) {
  const points = Array.from({ length: n }, (_, i) => {
    const along = 60 + (i * 880) / Math.max(1, n - 1);
    const across = i % 2 === 0 ? 70 : 230;
    return vertical ? { x: across / 3 + 10, y: along } : { x: along, y: across };
  });
  return { points, d: `M${points.map((p) => `${p.x} ${p.y}`).join('L')}` };
}

/**
 * "Como funciona", presa: os passos de verdade, do pedido ao mapa (os mesmos
 * da seção da landing clássica, que dependem da contratação pelo site estar
 * aberta), numa estrada em que o veículo para em cada marco (data-snap +
 * data-smil). O cartão do passo atual aparece embaixo (no celular, ao lado da
 * estrada, que fica na vertical). Sem movimento: os passos em lista.
 */
export function Journey() {
  const { open } = useCheckoutOffer();
  const steps = howItWorksSteps(open);
  const wide = road(steps.length, false);
  const tall = road(steps.length, true);
  const dur = steps.length - 1;

  return (
    <section
      id="como-funciona"
      className={`${cine.pin} ${styles.scene}`}
      data-scene="pin"
      data-steps={steps.length}
      data-snap
      aria-labelledby="como-funciona-title"
      style={{ '--n': steps.length } as CSSProperties}
    >
      <div className={`${cine.stage} ${styles.stage}`}>
        <header className={styles.head}>
          <p className={cine.eyebrow}>Passo a passo</p>
          <h2 id="como-funciona-title" className={`${cine.h2} ${styles.title}`}>
            Do pedido <em>ao mapa no celular.</em>
          </h2>
          <p className={`${cine.lead} ${styles.lead}`}>
            {open
              ? 'Do pedido no site ao mapa no celular, você acompanha cada etapa pelo app.'
              : 'Do primeiro contato à instalação, você acompanha cada etapa pelo painel.'}
          </p>
        </header>

        <div className={styles.body}>
          <svg
            className={`${styles.road} ${styles.wide}`}
            viewBox="0 0 1000 300"
            aria-hidden="true"
            data-smil={dur}
          >
            <Road d={wide.d} points={wide.points} dur={dur} />
          </svg>
          <svg
            className={`${styles.road} ${styles.tall}`}
            viewBox="0 0 100 1000"
            aria-hidden="true"
            data-smil={dur}
          >
            <Road d={tall.d} points={tall.points} dur={dur} small />
          </svg>

          <ol className={styles.cards}>
            {steps.map((step, i) => (
              <li key={step.title} className={styles.card} data-i={i}>
                <div className={styles.cardTop}>
                  <span className={styles.num}>{String(i + 1).padStart(2, '0')}</span>
                  <span className={`${styles.actor} ${styles[`actor_${step.actor}`]}`}>{ACTOR_LABEL[step.actor]}</span>
                </div>
                <h3 className={styles.cardTitle}>
                  <Icon name={ICONS[step.art]} size={20} />
                  {step.title}
                </h3>
                <p className={styles.cardText}>{step.description}</p>
              </li>
            ))}
          </ol>
        </div>
      </div>
    </section>
  );
}

function Road({ d, points, dur, small = false }: { d: string; points: { x: number; y: number }[]; dur: number; small?: boolean }) {
  return (
    <>
      <path className={styles.track} d={d} />
      <path className={styles.trackDash} d={d} />
      <path className={styles.done} d={d} pathLength={1} />
      {points.map((p, i) => (
        <g key={i} className={styles.milestone} data-i={i} transform={`translate(${p.x} ${p.y})`}>
          <circle r={small ? 11 : 16} className={styles.mileHalo} />
          <circle r={small ? 6 : 9} className={styles.mileDot} />
          {!small && (
            <text y={i % 2 === 0 ? -30 : 44} textAnchor="middle" className={styles.mileNum}>
              {String(i + 1).padStart(2, '0')}
            </text>
          )}
        </g>
      ))}
      <g className={styles.car}>
        <circle r={small ? 15 : 22} className={styles.carHalo} />
        <circle r={small ? 8 : 12} className={styles.carRing} />
        <circle r={small ? 3.5 : 5} className={styles.carDot} />
        <animateMotion dur={`${dur}s`} fill="freeze" path={d} />
      </g>
    </>
  );
}

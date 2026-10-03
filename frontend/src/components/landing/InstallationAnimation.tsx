import React from 'react';

import styles from './InstallationAnimation.module.css';

const STEPS = ['Fixação no veículo', 'Ligação elétrica', 'Rastreador online'];

/**
 * A instalação em três etapas, num ciclo de 8 s: o rastreador é fixado
 * atrás do painel, os fios vão à bateria, ao terra e à ignição, e ele fica
 * online (LED verde, sinal ao satélite, localização no mapa). As etapas
 * embaixo acendem junto.
 *
 * O desenho sem animação é a cena final: é o que aparece com "reduzir
 * movimento" ligado no aparelho.
 */
export const InstallationAnimation: React.FC = () => (
  <figure className={styles.figure}>
    <svg
      className={styles.scene}
      viewBox="0 0 600 290"
      role="img"
      aria-labelledby="instalacao-titulo instalacao-desc"
    >
      <title id="instalacao-titulo">Instalação profissional do rastreador</title>
      <desc id="instalacao-desc">
        O rastreador é fixado atrás do painel do carro, ligado à bateria, ao terra e à ignição, e fica online:
        o sinal chega ao satélite e a localização aparece no mapa.
      </desc>

      {/* Chão */}
      <line className={styles.ground} x1="30" y1="246" x2="570" y2="246" />

      {/* Carro (frente à direita) */}
      <g className={styles.car}>
        <path
          className={styles.body}
          d="M60,215 L102,215 A38,38 0 0 1 178,215 L382,215 A38,38 0 0 1 458,215 L505,215 L505,182
             Q503,166 485,162 L415,150 L362,112 Q350,104 334,104 L222,104 Q206,104 196,112 L150,150
             L82,160 Q62,164 60,182 Z"
        />
        <path className={styles.window} d="M207,146 L229,117 L282,117 L282,146 Z" />
        <path className={styles.window} d="M292,146 L292,117 L327,117 Q335,117 341,124 L357,146 Z" />
        <line className={styles.door} x1="287" y1="117" x2="287" y2="206" />
        <circle className={styles.wheel} cx="140" cy="215" r="27" />
        <circle className={styles.hub} cx="140" cy="215" r="9" />
        <circle className={styles.wheel} cx="420" cy="215" r="27" />
        <circle className={styles.hub} cx="420" cy="215" r="9" />
      </g>

      {/* Bateria, embaixo do capô */}
      <g className={styles.battery}>
        <rect x="446" y="166" width="38" height="24" rx="3" />
        <rect className={styles.terminal} x="452" y="161" width="7" height="5" rx="1" />
        <rect className={styles.terminal} x="471" y="161" width="7" height="5" rx="1" />
        <text x="455.5" y="182" textAnchor="middle">+</text>
        <text x="474.5" y="182" textAnchor="middle">−</text>
      </g>

      {/* Chave na ignição, no painel */}
      <g className={styles.ignition}>
        <circle cx="371" cy="140" r="5.5" />
        <path d="M376.5,140 H392 M386,140 V145 M391,140 V144" />
      </g>

      {/* Fios: positivo (bateria), terra (chassi) e ignição */}
      <path className={`${styles.wire} ${styles.wireRed}`} pathLength={1} d="M346,174 C384,166 420,150 455,163" />
      <path className={`${styles.wire} ${styles.wireGray}`} pathLength={1} d="M346,182 C366,204 392,206 402,198" />
      <path className={`${styles.wire} ${styles.wireYellow}`} pathLength={1} d="M334,168 C334,150 350,141 364,140" />
      <g className={styles.groundMark}>
        <line x1="402" y1="198" x2="402" y2="204" />
        <line x1="395" y1="204" x2="409" y2="204" />
        <line x1="398" y1="208" x2="406" y2="208" />
      </g>

      {/* Sinal: ondas saindo do rastreador e o feixe até o satélite */}
      <circle className={`${styles.ring} ${styles.ring1}`} cx="332" cy="176" r="14" />
      <circle className={`${styles.ring} ${styles.ring2}`} cx="332" cy="176" r="14" />
      <path className={styles.beam} pathLength={1} d="M332,165 Q392,48 526,50" />

      {/* Satélite */}
      {/* A posição fica no grupo de fora: a animação (flutuar) mexe no de dentro. */}
      <g transform="translate(540 44) rotate(-30)">
        <g className={styles.satellite}>
          <rect x="-26" y="-6" width="16" height="12" rx="1.5" className={styles.panel} />
          <rect x="10" y="-6" width="16" height="12" rx="1.5" className={styles.panel} />
          <rect x="-8" y="-9" width="16" height="18" rx="3" className={styles.satBody} />
          <line x1="-10" y1="0" x2="-8" y2="0" />
          <line x1="8" y1="0" x2="10" y2="0" />
        </g>
      </g>

      {/* Rastreador: desce até o lugar dele, atrás do painel */}
      <g className={styles.tracker}>
        <rect x="318" y="167" width="28" height="18" rx="4" className={styles.trackerBox} />
        <circle cx="339" cy="176" r="3" className={styles.led} />
        <line x1="323" y1="172" x2="331" y2="172" className={styles.trackerLine} />
        <line x1="323" y1="176" x2="329" y2="176" className={styles.trackerLine} />
      </g>

      {/* Chave de boca: aparece enquanto o rastreador é fixado */}
      <g className={styles.wrench}>
        <path d="M300,150 L314,164 M296,146 a6,6 0 1 1 8,-8 l-4,4 l3,3 l4,-4 a6,6 0 0 1 -8,8" />
      </g>

      {/* Localização no mapa e o aviso de online */}
      <g transform="translate(20 0)">
        <g className={styles.pin}>
          <path d="M280,58 C268,58 260,67 260,77 C260,92 280,108 280,108 C280,108 300,92 300,77 C300,67 292,58 280,58 Z" />
          <circle cx="280" cy="77" r="7" className={styles.pinDot} />
        </g>
      </g>
      {/* O de fora aumenta o aviso no celular; o de dentro anima. */}
      <g className={styles.badgeWrap}>
        <g className={styles.badge}>
          <rect x="24" y="14" width="176" height="36" rx="18" />
          <circle cx="46" cy="32" r="9" className={styles.badgeCheck} />
          <path d="M41.5,32 l3,3 l6,-6" className={styles.badgeTick} />
          <text x="64" y="37">Rastreador online</text>
        </g>
      </g>
    </svg>

    <figcaption>
      <ol className={styles.steps}>
        {STEPS.map((label, i) => (
          <li key={label} className={`${styles.step} ${styles[`step${i + 1}`]}`}>
            <span className={styles.stepNumber}>{i + 1}</span>
            {label}
          </li>
        ))}
      </ol>
    </figcaption>
  </figure>
);

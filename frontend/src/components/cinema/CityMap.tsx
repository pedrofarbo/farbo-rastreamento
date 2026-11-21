import type { ReactNode, SVGProps } from 'react';

import styles from './CityMap.module.css';

/** O tamanho do desenho (viewBox) e o passo da grade de ruas. */
export const MAP_W = 1000;
export const MAP_H = 700;
export const BLOCK = 70;

const lines = (() => {
  const out: { d: string; avenue: boolean }[] = [];
  // Mais larga que o desenho: girada, a grade ainda cobre os cantos.
  for (let i = -6, n = 0; i <= 20; i++, n++) {
    const v = i * BLOCK;
    out.push({ d: `M${v} -500V1200`, avenue: n % 4 === 0 });
    out.push({ d: `M-500 ${v}H1500`, avenue: n % 4 === 2 });
  }
  return out;
})();

interface CityMapProps extends Omit<SVGProps<SVGSVGElement>, 'children'> {
  /** O que vai por cima das ruas, no mesmo sistema da grade (rotas, veículos). */
  children?: ReactNode;
  className?: string;
}

/**
 * Um mapa noturno estilizado (ilustração, não um lugar de verdade): a grade
 * de ruas inclinada, algumas avenidas, um parque e um rio, no verde-escuro da
 * marca. O que vem em children usa as coordenadas da grade (múltiplos de
 * BLOCK caem em cima das ruas). Os data-* (data-smil, data-from...) passam
 * para o <svg>: o useScenes controla as animações SMIL dele.
 */
export function CityMap({ children, className = '', ...svg }: CityMapProps) {
  return (
    <svg
      viewBox={`0 0 ${MAP_W} ${MAP_H}`}
      preserveAspectRatio="xMidYMid slice"
      className={`${styles.map} ${className}`}
      aria-hidden="true"
      focusable="false"
      {...svg}
    >
      <g transform={`rotate(-16 ${MAP_W / 2} ${MAP_H / 2})`}>
        <path className={styles.river} d="M-300 905C80 830 260 760 520 790S900 860 1300 760" />
        <rect className={styles.park} x={BLOCK * 9 + 8} y={BLOCK * 1 + 8} width={BLOCK * 2 - 16} height={BLOCK * 2 - 16} rx="14" />
        <rect className={styles.park} x={BLOCK * 2 + 8} y={BLOCK * 6 + 8} width={BLOCK - 16} height={BLOCK * 2 - 16} rx="12" />
        {lines.map((line) => (
          <path key={line.d} d={line.d} className={line.avenue ? styles.avenue : styles.street} />
        ))}
        <path className={styles.avenue} d="M-200 980L1200 -120" />
        {children}
      </g>
    </svg>
  );
}

/**
 * Uma rota em neon: o brilho é um traço largo e translúcido por baixo (sem
 * filtro: o Safari não aplica filtro CSS em elementos de dentro do SVG). Com
 * pathLength=1, o CSS desenha a rota com stroke-dashoffset.
 */
export function Route({ d, className = '' }: { d: string; className?: string }) {
  return (
    <g className={`${styles.route} ${className}`}>
      <path className={styles.routeGlow} d={d} pathLength={1} />
      <path className={styles.routeLine} d={d} pathLength={1} />
    </g>
  );
}

/** O veículo no mapa: o halo pulsando, o anel e o ponto, centrados em (0, 0). */
export function VehicleMarker({ className = '', children }: { className?: string; children?: ReactNode }) {
  return (
    <g className={`${styles.vehicle} ${className}`}>
      <circle className={styles.halo} r="30" />
      <circle className={styles.ring} r="15" />
      <circle className={styles.dot} r="6" />
      {children}
    </g>
  );
}

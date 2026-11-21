import type { CSSProperties, ReactNode } from 'react';

import { CityMap, Route, VehicleMarker } from './CityMap';
import { Icon, type IconName } from './icons';
import cine from './cine.module.css';
import styles from './Features.module.css';

const LIVE_ROUTE = 'M140 560V350H420V210H700V70H980';

/** Os alertas que o app manda de verdade (os mesmos do passo a passo). */
const ALERTS: { icon: IconName; text: string; tone?: 'bad' }[] = [
  { icon: 'alert', text: 'SOS acionado', tone: 'bad' },
  { icon: 'fence', text: 'Saiu da cerca: Casa' },
  { icon: 'key', text: 'Ignição ligada de madrugada' },
  { icon: 'alert', text: 'Bateria desconectada', tone: 'bad' },
  { icon: 'route', text: 'Movimento com a ignição desligada' },
];

interface CardProps {
  icon: IconName;
  title: string;
  text: string;
  className?: string;
  index: number;
  children: ReactNode;
}

function Card({ icon, title, text, className = '', index, children }: CardProps) {
  return (
    <li className={`${styles.card} ${className}`} data-cine-reveal data-tilt style={{ '--i': index } as CSSProperties}>
      <div className={styles.art} aria-hidden="true">
        {children}
      </div>
      <div className={styles.text}>
        <h3>
          <span className={styles.icon}>
            <Icon name={icon} size={18} />
          </span>
          {title}
        </h3>
        <p>{text}</p>
      </div>
    </li>
  );
}

/**
 * "Tudo incluso no seu plano": os recursos em grade (bento), cada cartão com
 * uma animação curta do que ele faz: o mapa ao vivo, o trajeto se desenhando,
 * os alertas chegando, a cerca virtual, o link ao vivo, o app e o sistema web,
 * o chip M2M e o suporte. Ilustrações, sem dados de verdade.
 */
export function Features() {
  return (
    <section id="recursos" className={`${cine.section} ${styles.section}`} aria-labelledby="recursos-title">
      <div className={cine.container}>
        <header className={styles.head} data-cine-reveal>
          <p className={cine.eyebrow}>Tudo incluso no seu plano</p>
          <h2 id="recursos-title" className={cine.h2}>
            Seu veículo na palma da mão. <em>De dia e de madrugada.</em>
          </h2>
          <p className={cine.lead}>
            O app no celular e o sistema web no computador vêm no plano, com o chip M2M multi operadora já incluso.
          </p>
        </header>

        <ul className={styles.grid}>
          <Card
            index={0}
            className={styles.live}
            icon="pin"
            title="Localização em tempo real"
            text="Veja agora onde o seu carro ou a sua moto está, no app e no computador."
          >
            <CityMap className={styles.liveMap} data-still="3">
              <Route d={LIVE_ROUTE} className={styles.liveRoute} />
              <g>
                <VehicleMarker />
                <animateMotion dur="12s" repeatCount="indefinite" path={LIVE_ROUTE} />
              </g>
            </CityMap>
            <span className={styles.liveChip}>
              <i />
              Ao vivo
            </span>
          </Card>

          <Card
            index={1}
            className={styles.alerts}
            icon="bell"
            title="Alertas inteligentes"
            text="No celular e por e-mail: SOS, bateria desconectada, cerca, ignição de madrugada e mais."
          >
            <div className={styles.feed}>
              <ul className={styles.feedRoll}>
                {[...ALERTS, ...ALERTS].map((alert, i) => (
                  <li key={i} className={alert.tone === 'bad' ? styles.bad : ''} aria-hidden={i >= ALERTS.length || undefined}>
                    <Icon name={alert.icon} size={15} strokeWidth={2.2} />
                    {alert.text}
                    <small>agora</small>
                  </li>
                ))}
              </ul>
            </div>
          </Card>

          <Card
            index={2}
            className={styles.history}
            icon="route"
            title="Histórico de trajetos"
            text="Reveja por onde ele passou, dia a dia."
          >
            <svg viewBox="0 0 240 130" className={styles.historyArt}>
              <path className={styles.historyBase} d="M20 105C60 105 50 40 95 40S140 92 170 80 205 25 222 22" pathLength={1} />
              <path className={styles.historyLine} d="M20 105C60 105 50 40 95 40S140 92 170 80 205 25 222 22" pathLength={1} />
              <circle className={styles.historyStart} cx="20" cy="105" r="6" />
              <circle className={styles.historyEnd} cx="222" cy="22" r="7" />
              <text x="20" y="126" className={styles.historyTime}>08:12</text>
              <text x="222" y="48" textAnchor="end" className={styles.historyTime}>08:47</text>
            </svg>
          </Card>

          <Card
            index={3}
            className={styles.fence}
            icon="fence"
            title="Cercas virtuais"
            text="Um círculo em volta de casa, do trabalho ou da escola: aviso quando ele entrar ou sair."
          >
            <svg viewBox="0 0 240 130" className={styles.fenceArt}>
              <circle className={styles.fenceArea} cx="110" cy="65" r="46" />
              <circle className={styles.fenceRing} cx="110" cy="65" r="46" />
              <g className={styles.fenceCar}>
                <circle r="13" className={styles.fenceHalo} />
                <circle r="7" className={styles.fenceDot} />
              </g>
            </svg>
          </Card>

          <Card
            index={4}
            className={styles.share}
            icon="share"
            title="Localização ao vivo por link"
            text="Mande um link para alguém acompanhar o veículo em tempo real."
          >
            <div className={styles.shareArt}>
              <span className={styles.link}>
                <Icon name="share" size={14} />
                Acompanhe Honda CG 160 ao vivo
              </span>
              <span className={styles.copied}>
                <Icon name="check" size={13} strokeWidth={2.6} />
                Link enviado
              </span>
            </div>
          </Card>

          <Card
            index={5}
            className={styles.apps}
            icon="monitor"
            title="App e sistema web"
            text="No celular (é só adicionar à tela inicial) e no computador, com o mesmo login."
          >
            <div className={styles.appsArt}>
              <span className={styles.laptop}>
                <i />
              </span>
              <span className={styles.mobile}>
                <i />
              </span>
            </div>
          </Card>

          <Card
            index={6}
            className={styles.chip}
            icon="chip"
            title="Chip M2M incluso"
            text="Multi operadora, com 20 MB/mês: o rastreador conversa com o app sem você pagar linha."
          >
            <div className={styles.signal}>
              <i />
              <i />
              <i />
              <i />
              <i />
            </div>
          </Card>

          <Card
            index={7}
            className={styles.support}
            icon="headset"
            title="Suporte especializado"
            text="Gente que entende de rastreamento para ajudar na instalação e no dia a dia."
          >
            <div className={styles.chat}>
              <span className={styles.bubbleIn}>O app mostra a moto parada?</span>
              <span className={styles.bubbleOut}>
                <i />
                <i />
                <i />
              </span>
            </div>
          </Card>
        </ul>
      </div>
    </section>
  );
}

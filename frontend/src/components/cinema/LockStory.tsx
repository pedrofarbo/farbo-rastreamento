import { CityMap, Route, VehicleMarker } from './CityMap';
import { Icon } from './icons';
import cine from './cine.module.css';
import styles from './LockStory.module.css';

/**
 * O trajeto de quem levou a moto, na grade do mapa: do alto à esquerda do
 * painel para a direita, longe do celular (computador) e da faixa de status
 * (celular).
 */
const ROUTE = 'M490 210V280H630V420H770';

const BEATS = [
  {
    time: '02:14',
    title: 'Tudo parado na garagem.',
    text: 'O app mostra onde a moto está, com a ignição desligada e o motor liberado.',
  },
  {
    time: '02:19',
    title: 'Ignição ligada de madrugada?',
    text: 'O alerta chega na hora, no celular e por e-mail. E o mapa já mostra a moto andando, ao vivo.',
  },
  {
    time: '02:20',
    title: 'Um toque: bloquear.',
    text: 'Você envia o bloqueio pelo app. Por segurança, ele age assim que o veículo para.',
  },
  {
    time: '02:24',
    title: 'Parou. Não liga mais.',
    text: 'Motor bloqueado. Você vê onde ela está e manda a localização ao vivo para quem vai ajudar.',
  },
];

/** O que cada quadrinho do app mostra em cada passo (0 a 3). */
const TILES: { label: string; values: string[]; tone?: string[] }[] = [
  { label: 'Ignição', values: ['Desligada', 'Ligada', 'Ligada', 'Ligada'], tone: ['', 'warn', 'warn', ''] },
  { label: 'Velocidade', values: ['0 km/h', '38 km/h', '12 km/h', '0 km/h'] },
  { label: 'Motor', values: ['Liberado', 'Liberado', 'Bloqueando…', 'Bloqueado'], tone: ['', '', 'warn', 'bad'] },
];

const PUSH = [
  null,
  { icon: 'alert' as const, title: 'Ignição ligada de madrugada', text: 'Honda CG 160 · agora' },
  { icon: 'alert' as const, title: 'Ignição ligada de madrugada', text: 'Honda CG 160 · há 1 min' },
  { icon: 'lock' as const, title: 'Motor bloqueado', text: 'Comando confirmado pelo rastreador' },
];

/** Os passos em que cada versão aparece (data-at, lido pelo CSS). */
const at = (...steps: number[]) => steps.join(' ');

/**
 * "Do alerta ao motor bloqueado", presa: a história em quatro tempos conforme
 * a rolagem (data-steps): a moto parada, a ignição ligada de madrugada (o
 * alerta e a moto andando no mapa), o bloqueio enviado pelo app (que age com o
 * veículo parado, como dizem os Termos) e o motor bloqueado, com a
 * localização ao vivo para compartilhar e o desbloqueio pelo app. O veículo
 * anda pela rota com animateMotion, no tempo da rolagem (data-smil). Sem
 * movimento, os quatro tempos viram uma lista e o app mostra o fim.
 */
export function LockStory() {
  return (
    <section
      id="bloqueio"
      className={`${cine.pin} ${styles.scene}`}
      data-scene="pin"
      data-steps={BEATS.length}
      aria-labelledby="bloqueio-title"
    >
      <div className={`${cine.stage} ${styles.stage}`}>
        <div className={styles.layout}>
          <div className={styles.copy}>
            <p className={cine.eyebrow}>Na prática</p>
            <h2 id="bloqueio-title" className={`${cine.h2} ${styles.title}`}>
              Do alerta ao <em>motor bloqueado.</em>
            </h2>

            <ol className={styles.beats}>
              {BEATS.map((beat, i) => (
                <li key={beat.time} className={styles.beat} data-at={at(i)}>
                  <span className={styles.time}>{beat.time}</span>
                  <strong>{beat.title}</strong>
                  <span className={styles.beatText}>{beat.text}</span>
                </li>
              ))}
            </ol>

            <div className={styles.dots} aria-hidden="true">
              {BEATS.map((beat, i) => (
                <span key={beat.time} className={styles.dot}>
                  <i data-at={at(...BEATS.map((_, j) => j).filter((j) => j >= i))} />
                </span>
              ))}
            </div>
          </div>

          <div className={styles.visual} aria-hidden="true">
            <div className={styles.panel}>
              <CityMap className={styles.map} data-smil="6" data-from="0.27" data-to="0.72">
                <Route d={ROUTE} className={styles.route} />
                <g>
                  <g className={styles.waves}>
                    <circle r="34" />
                    <circle r="34" />
                  </g>
                  <VehicleMarker className={styles.car}>
                    <g className={styles.lockBadge} transform="rotate(16) translate(16 -32)">
                      <rect x="-12" y="-12" width="24" height="24" rx="7" />
                      <path d="M-4.5 -1v-2.5a4.5 4.5 0 0 1 9 0V-1M-6.5 -1h13v8h-13z" />
                    </g>
                  </VehicleMarker>
                  <animateMotion dur="6s" fill="freeze" path={ROUTE} />
                </g>
              </CityMap>

              <div className={styles.mapChips}>
                <span className={`${styles.mapChip} ${styles.chipWarn}`} data-at={at(2)}>
                  <Icon name="lock" size={14} strokeWidth={2.4} />
                  Bloqueio enviado · age quando ela parar
                </span>
                <span className={`${styles.mapChip} ${styles.chipBad}`} data-at={at(3)}>
                  <Icon name="lock" size={14} strokeWidth={2.4} />
                  Motor bloqueado
                </span>
              </div>

              {/* No celular, o app vira uma faixa embaixo do mapa (o alerta já está no texto do passo). */}
              <div className={styles.mini}>
                <div className={styles.miniBar}>
                  <strong>Honda CG 160</strong>
                  <Stack className={styles.miniState} values={['Motor liberado', 'Andando · 38 km/h', 'Bloqueando…', 'Motor bloqueado']} tone={['', 'warn', 'warn', 'bad']} />
                </div>
              </div>
            </div>

            <div className={styles.phone}>
              <div className={styles.screen}>
                <div className={styles.statusBar}>
                  <Stack values={BEATS.map((b) => b.time)} />
                  <span className={styles.bars}>
                    <i />
                    <i />
                    <i />
                  </span>
                </div>
                <Push />
                <div className={styles.appHead}>
                  <span className={styles.vehicle}>
                    <strong>Honda CG 160</strong>
                    <small>FRB2C24</small>
                  </span>
                  <span className={styles.online}>
                    <i />
                    Online
                  </span>
                </div>
                <div className={styles.tiles}>
                  {TILES.map((tile) => (
                    <div key={tile.label} className={styles.tile}>
                      <small>{tile.label}</small>
                      <Stack values={tile.values} tone={tile.tone} />
                    </div>
                  ))}
                </div>
                <div className={styles.action}>
                  <Stack
                    className={styles.actionLabel}
                    values={['Bloquear motor', 'Bloquear motor', 'Aguardando o veículo parar…', 'Desbloquear motor']}
                  />
                </div>
                <div className={styles.share} data-at={at(3)}>
                  <Icon name="share" size={15} />
                  Compartilhar localização ao vivo
                </div>
              </div>
            </div>
          </div>
        </div>

        <p className={`${cine.note} ${styles.note}`}>
          Ilustração com dados de demonstração. O bloqueio exige o relé instalado e é feito com o veículo parado; o
          desbloqueio é pelo app.
        </p>
      </div>
    </section>
  );
}

/** Um texto que muda a cada passo: as versões empilhadas, uma visível por vez. */
function Stack({ values, tone, className = '' }: { values: string[]; tone?: string[]; className?: string }) {
  return (
    <span className={`${styles.stack} ${className}`}>
      {values.map((value, i) => (
        <span key={i} data-at={at(i)} data-tone={tone?.[i] || undefined}>
          {value}
        </span>
      ))}
    </span>
  );
}

/** A notificação do celular (cada passo com a sua; o primeiro, sem nenhuma). */
function Push() {
  return (
    <div className={styles.push}>
      {PUSH.map((push, i) =>
        push ? (
          <div key={i} className={`${styles.pushCard} ${push.icon === 'lock' ? styles.pushBad : ''}`} data-at={at(i)}>
            <span className={styles.pushIcon}>
              <Icon name={push.icon} size={15} strokeWidth={2.4} />
            </span>
            <span>
              <strong>{push.title}</strong>
              <small>{push.text}</small>
            </span>
          </div>
        ) : null,
      )}
    </div>
  );
}

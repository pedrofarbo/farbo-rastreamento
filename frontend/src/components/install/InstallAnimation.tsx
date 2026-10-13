import { useEffect, useRef, useState } from 'react';

import styles from './InstallAnimation.module.css';

/** Uma etapa da instalação: o que a cena mostra, a legenda e os fios dela. */
interface Step {
  title: string;
  text: string;
  /** Quanto a etapa fica na tela, tocando sozinha. */
  ms: number;
  /** As cores dos fios da etapa (as bolinhas da legenda). */
  wires: { name: string; color: string }[];
  /** A seção da página com os detalhes. */
  anchor: string;
}

const RED = '#dc2626';
const BLACK = '#111827';
const ORANGE = '#ea580c';
const YELLOW = '#ca8a04';
const WHITE = '#f8fafc';
const GREEN = '#16a34a';

export const STEPS: Step[] = [
  {
    title: 'Desligue o veículo',
    text: 'Antes de mexer na parte elétrica, desligue a ignição e tire a chave.',
    ms: 4500,
    wires: [],
    anchor: 'antes',
  },
  {
    title: 'Escolha o local',
    text: 'Embaixo do painel, na parte de plástico, com a face de cima do rastreador para cima e sem metal por cima: o sinal do GPS precisa chegar até ele.',
    ms: 6500,
    wires: [],
    anchor: 'local',
  },
  {
    title: 'Preto no terra',
    text: 'O fio preto vai no negativo da bateria ou num ponto do chassi sem tinta.',
    ms: 5000,
    wires: [{ name: 'preto', color: BLACK }],
    anchor: 'chicote',
  },
  {
    title: 'Vermelho no +12V permanente',
    text: 'O fio vermelho vai num ponto que tem energia mesmo com a chave desligada, como o positivo da bateria.',
    ms: 5000,
    wires: [{ name: 'vermelho', color: RED }],
    anchor: 'chicote',
  },
  {
    title: 'Laranja no pós-chave',
    text: 'O fio laranja vai num fio que só tem energia com a ignição ligada: é por ele que o app sabe quando o veículo está ligado.',
    ms: 6500,
    wires: [{ name: 'laranja', color: ORANGE }],
    anchor: 'chicote',
  },
  {
    title: 'O relê: amarelo e branco',
    text: 'O amarelo do relê vai no amarelo do rastreador. O branco vai no mesmo +12V permanente do vermelho.',
    ms: 6500,
    wires: [
      { name: 'amarelo', color: YELLOW },
      { name: 'branco', color: WHITE },
    ],
    anchor: 'rele',
  },
  {
    title: 'O relê: verdes no fio da bomba',
    text: 'Corte o fio de alimentação da bomba de combustível e ligue um verde em cada ponta. O combustível continua passando: o relê só abre quando você bloqueia pelo app.',
    ms: 8000,
    wires: [{ name: 'dois verdes', color: GREEN }],
    anchor: 'rele',
  },
  {
    title: 'Teste pelo app',
    text: 'Com a ignição ligada e a céu aberto, o veículo aparece online. Parado, toque em Desligar motor (ele não pega) e depois em Liberar motor.',
    ms: 10000,
    wires: [],
    anchor: 'teste',
  },
];

function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined' && Boolean(window.matchMedia?.('(prefers-reduced-motion: reduce)').matches);
}

/**
 * A instalação do J16 inteira, animada e por etapas: o esquema vai sendo
 * montado (o rastreador no lugar, cada fio desenhado na hora em que entra, o
 * relê no fio da bomba) e termina no teste pelo app, com o bloqueio. Toca
 * sozinha quando aparece na tela; dá para pausar, voltar e ir direto a uma
 * etapa, e a legenda leva aos detalhes de cada uma na página.
 *
 * Parada (pausada, fora da tela ou com "reduzir movimento" no aparelho), cada
 * etapa aparece pronta, sem animação, para olhar com calma; tocar de novo
 * recomeça a etapa do início.
 */
export function InstallAnimation() {
  const [step, setStep] = useState(0);
  const [playing, setPlaying] = useState(() => !prefersReducedMotion());
  const [visible, setVisible] = useState(true);
  // Cada vez que volta a tocar, a etapa recomeça (a cena é remontada).
  const [round, setRound] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  const running = playing && visible;
  const current = STEPS[step];

  // Só toca enquanto está na tela.
  useEffect(() => {
    const el = root.current;
    if (!el || typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver(([entry]) => setVisible(entry.isIntersecting), { threshold: 0.25 });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (running) setRound((r) => r + 1);
  }, [running]);

  useEffect(() => {
    if (!running) return;
    const timer = window.setTimeout(() => setStep((s) => (s + 1) % STEPS.length), current.ms);
    return () => window.clearTimeout(timer);
  }, [running, step, round, current.ms]);

  const go = (next: number) => setStep((next + STEPS.length) % STEPS.length);

  return (
    <div ref={root} className={`${styles.player} ${running ? '' : styles.still}`}>
      <div className={styles.stage}>
        {/* A cena é remontada a cada etapa: as animações recomeçam do início. */}
        <Scene key={`${step}-${round}`} step={step} />
        {running && (
          <div className={styles.progress} aria-hidden="true">
            <span key={`${step}-${round}`} style={{ animationDuration: `${current.ms}ms` }} />
          </div>
        )}
      </div>

      <div className={styles.caption} aria-live={running ? 'off' : 'polite'}>
        <p className={styles.counter}>
          Etapa {step + 1} de {STEPS.length}
        </p>
        <h3 className={styles.captionTitle}>{current.title}</h3>
        <p className={styles.captionText}>{current.text}</p>
        {current.wires.length > 0 && (
          <p className={styles.chips}>
            {current.wires.map((w) => (
              <span key={w.name}>
                <i style={{ background: w.color }} />
                {w.name}
              </span>
            ))}
          </p>
        )}
        <a className={styles.details} href={`#${current.anchor}`}>
          Ver os detalhes desta etapa
        </a>
      </div>

      <div className={styles.controls}>
        <button type="button" className={styles.control} onClick={() => go(step - 1)} aria-label="Etapa anterior">
          ‹
        </button>
        <button
          type="button"
          className={`${styles.control} ${styles.play}`}
          onClick={() => setPlaying(!playing)}
          aria-label={playing ? 'Pausar a animação' : 'Tocar a animação'}
        >
          {playing ? '❚❚' : '▶'}
        </button>
        <button type="button" className={styles.control} onClick={() => go(step + 1)} aria-label="Próxima etapa">
          ›
        </button>
      </div>
      <div className={styles.dots} role="group" aria-label="Etapas da instalação">
        {STEPS.map((s, i) => (
          <button
            key={s.title}
            type="button"
            className={i === step ? styles.dotOn : styles.dot}
            aria-label={`Etapa ${i + 1}: ${s.title}`}
            aria-current={i === step ? 'step' : undefined}
            onClick={() => go(i)}
          />
        ))}
      </div>
    </div>
  );
}

/**
 * A cena de uma etapa. Tudo o que já foi feito aparece pronto; o que é desta
 * etapa entra animado (os fios são "desenhados" do rastreador até o destino).
 */
function Scene({ step }: { step: number }) {
  const done = (s: number) => step > s;
  const now = (s: number) => step === s;
  const wire = (s: number) => (now(s) ? styles.drawing : done(s) ? '' : styles.hidden);
  const lit = (...steps: number[]) => (steps.includes(step) ? styles.lit : '');
  const relay = step >= 5;
  const cut = step >= 6;

  return (
    <svg className={styles.scene} viewBox="0 0 400 300" role="img" aria-label={`Etapa ${step + 1}: ${STEPS[step].title}`}>
      {/* O painel (de plástico) e o satélite do GPS. */}
      <rect x="132" y="104" width="136" height="20" rx="6" className={styles.dash} />
      <text x="200" y="118" textAnchor="middle" className={styles.small}>
        painel (plástico)
      </text>
      {/* O satélite do GPS: o sinal precisa chegar ao rastreador (só no local). */}
      {step === 1 && (
        <g>
          <rect x="162" y="30" width="26" height="12" rx="3" fill="#475569" />
          <rect x="146" y="33" width="14" height="6" fill="#64748b" />
          <rect x="190" y="33" width="14" height="6" fill="#64748b" />
          <text x="175" y="24" textAnchor="middle" className={styles.label}>
            GPS
          </text>
          <g className={styles.waves}>
            <path d="M 178 56 Q 192 66 206 56" />
            <path d="M 172 72 Q 194 86 214 72" />
            <path d="M 168 88 Q 196 106 224 88" />
          </g>
        </g>
      )}

      {/* A bateria. */}
      <g className={lit(0, 3, 5)}>
        <rect x="16" y="22" width="84" height="50" rx="7" fill="#1f2937" />
        <rect x="28" y="13" width="16" height="10" rx="2" fill={RED} />
        <rect x="72" y="13" width="16" height="10" rx="2" fill="#64748b" />
        <text x="36" y="35" textAnchor="middle" className={styles.light}>
          +
        </text>
        <text x="80" y="35" textAnchor="middle" className={styles.light}>
          −
        </text>
        <text x="58" y="56" textAnchor="middle" className={styles.lightBold}>
          BATERIA
        </text>
      </g>

      {/* A ignição, com a chave. */}
      <g className={lit(0, 4)}>
        <circle cx="252" cy="58" r="22" fill="#1f2937" />
        <g className={`${styles.key} ${step === 0 ? styles.keyOff : step === 4 ? styles.keyBlink : step === 7 ? styles.keyOn : ''}`}>
          <rect x="248" y="42" width="8" height="26" rx="3" fill="#cbd5e1" />
          <circle cx="252" cy="40" r="7" fill="#cbd5e1" />
        </g>
        <text x="282" y="56" className={styles.label}>
          ignição
        </text>
        <text x="282" y="68" className={styles.small}>
          {step === 7 ? 'ligada' : step === 0 ? 'desligue' : 'desligada'}
        </text>
      </g>

      {/* O terra (chassi). */}
      <g className={lit(2)}>
        <line x1="48" y1="250" x2="72" y2="250" stroke={BLACK} strokeWidth="3" />
        <line x1="52" y1="256" x2="68" y2="256" stroke={BLACK} strokeWidth="3" />
        <line x1="56" y1="262" x2="64" y2="262" stroke={BLACK} strokeWidth="3" />
        <text x="60" y="280" textAnchor="middle" className={styles.label}>
          terra
        </text>
      </g>

      {/* O fio da bomba de combustível: inteiro até o corte; depois, o relê no meio. */}
      <g className={lit(6)}>
        <path d="M 172 262 H 296" className={styles.pumpWire} />
        <path d="M 350 262 H 366" className={styles.pumpWire} />
        {!cut && <path d="M 296 262 H 350" className={styles.pumpWire} />}
        {now(6) && <path d="M 296 262 H 350" className={`${styles.pumpWire} ${styles.cutAway}`} />}
        <rect x="366" y="246" width="22" height="32" rx="5" fill="#334155" />
        <path d="M 377 254 C 372 262 372 268 377 270 C 382 268 382 262 377 254 Z" fill="#fbbf24" />
        <text x="236" y="284" textAnchor="middle" className={styles.small}>
          fio da bomba de combustível
        </text>
        <text x="374" y="292" textAnchor="middle" className={styles.small}>
          bomba
        </text>
        {now(6) && (
          <text x="323" y="266" textAnchor="middle" className={styles.scissors}>
            ✂
          </text>
        )}
      </g>

      {/* O combustível passando pelo relê (depois dos verdes). */}
      {cut && (
        <path
          d="M 172 262 H 296 V 228 H 318 V 204 M 350 204 V 262 H 366"
          className={`${styles.flow} ${now(6) ? styles.flowLate : ''} ${now(7) ? styles.flowBlocked : ''}`}
        />
      )}

      {/* Os fios do chicote, do rastreador até cada ponto. */}
      <path d="M 160 178 H 60 V 250" pathLength={1} className={`${styles.wire} ${wire(2)}`} stroke={BLACK} />
      <path d="M 160 158 H 120 V 6 H 36 V 13" pathLength={1} className={`${styles.wire} ${wire(3)}`} stroke={RED} />
      <path d="M 240 158 H 252 V 80" pathLength={1} className={`${styles.wire} ${wire(4)} ${now(4) || now(7) ? styles.energized : ''}`} stroke={ORANGE} />

      {/* O relê: o amarelo no rastreador, o branco no +12V, os verdes no fio da bomba. */}
      {relay && (
        <>
          <path d="M 240 178 H 304" pathLength={1} className={`${styles.wire} ${wire(5)}`} stroke={YELLOW} />
          <path
            d="M 334 150 V 20 H 120"
            pathLength={1}
            className={`${styles.wire} ${styles.whiteWire} ${wire(5)} ${now(5) ? styles.later : ''}`}
            stroke={WHITE}
          />
          <circle cx="120" cy="20" r="4" className={`${styles.splice} ${now(5) ? styles.spliceLate : ''}`} />
          <g className={lit(5, 6)}>
            <g className={now(5) ? styles.popIn : ''}>
              <rect x="304" y="150" width="60" height="54" rx="8" fill="#0d130e" />
              <text x="334" y="168" textAnchor="middle" className={styles.relayLabel}>
                RELÊ
              </text>
              <circle cx="322" cy="190" r="3" fill="#cbd5e1" />
              <circle cx="346" cy="190" r="3" fill="#cbd5e1" />
              <line x1="322" y1="190" x2="346" y2="188" className={`${styles.lever} ${now(7) ? styles.leverBlock : ''}`} />
            </g>
          </g>
          <path
            d="M 318 204 V 228 H 296 V 262"
            pathLength={1}
            className={`${styles.wire} ${cut ? wire(6) : styles.hidden} ${now(6) ? styles.afterCut : ''}`}
            stroke={GREEN}
          />
          <path
            d="M 350 204 V 262"
            pathLength={1}
            className={`${styles.wire} ${cut ? wire(6) : styles.hidden} ${now(6) ? styles.afterCut : ''}`}
            stroke={GREEN}
          />
        </>
      )}

      {/* O rastreador (entra no lugar na etapa 2). */}
      {step >= 1 && (
        <g className={lit(1)}>
          <g className={now(1) ? styles.dropIn : ''}>
            <rect x="160" y="146" width="80" height="44" rx="8" fill="#0d130e" />
            <text x="200" y="172" textAnchor="middle" className={styles.trackerLabel}>
              J16
            </text>
            <circle cx="228" cy="156" r="3" className={step === 7 ? styles.ledOn : styles.led} />
          </g>
          {now(1) && (
            <g className={styles.faceUp}>
              <path d="M 200 142 V 130 M 194 136 L 200 130 L 206 136" />
            </g>
          )}
        </g>
      )}

      {/* O teste: o app mostra o veículo online e bloqueia e libera o motor. */}
      {now(7) && (
        <g className={styles.phone}>
          <rect x="146" y="12" width="104" height="132" rx="14" fill="#0d130e" />
          <rect x="152" y="24" width="92" height="104" rx="6" fill="#e2e8f0" />
          <path d="M 152 70 H 244 M 190 24 V 128 M 152 104 L 244 50" stroke="#cbd5e1" strokeWidth="4" />
          <g className={styles.online}>
            <rect x="160" y="30" width="44" height="14" rx="7" fill={GREEN} />
            <text x="182" y="40" textAnchor="middle" className={styles.pill}>
              online
            </text>
          </g>
          <g className={styles.pin}>
            <path d="M 198 58 C 190 58 186 64 186 69 C 186 77 198 88 198 88 C 198 88 210 77 210 69 C 210 64 206 58 198 58 Z" fill={RED} />
            <circle cx="198" cy="69" r="4" fill="#ffffff" />
          </g>
          <g className={styles.blockButton}>
            <rect x="160" y="106" width="76" height="16" rx="8" fill={RED} />
            <text x="198" y="117" textAnchor="middle" className={styles.pill}>
              Desligar motor
            </text>
          </g>
          <g className={styles.releaseButton}>
            <rect x="160" y="106" width="76" height="16" rx="8" fill={GREEN} />
            <text x="198" y="117" textAnchor="middle" className={styles.pill}>
              Liberar motor
            </text>
          </g>
          <circle cx="198" cy="114" r="9" className={styles.tap} />
        </g>
      )}
      {now(7) && (
        <text x="244" y="244" textAnchor="middle" className={styles.blockedNote}>
          motor bloqueado
        </text>
      )}
    </svg>
  );
}

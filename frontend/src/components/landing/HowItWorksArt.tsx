import React from 'react';

import styles from './HowItWorksArt.module.css';

/** Qual desenho vai em cada etapa do passo a passo. */
export type StepArtKind =
  | 'interesse'
  | 'whatsapp'
  | 'pedido'
  | 'preparo'
  | 'envio'
  | 'instalacao'
  | 'acompanhamento';

const cx = (...names: string[]) => names.map((name) => styles[name]).join(' ');

/** O e-mail com o convite para definir a senha (a chave). */
const InviteMail: React.FC<{ from: string }> = ({ from }) => (
  <>
    <path className={styles.send} pathLength={1} d={from} />
    <g className={cx('pop', 'in58')}>
      <rect className={styles.panelStrong} x="166" y="40" width="56" height="38" rx="5" />
      <path className={styles.line} d="M168.5,43 L194,62 L219.5,43" />
    </g>
    <g className={cx('pop', 'in70')}>
      <circle className={styles.greenFill} cx="221" cy="40" r="11" />
      <circle className={styles.inkStroke} cx="216.5" cy="40" r="3.2" />
      <path className={styles.inkStroke} d="M219.7,40 H226.5 M224,40 V43.5" />
    </g>
  </>
);

/** 1 · Pré-cadastro: os campos se preenchem, o envio vira o convite por e-mail. */
const InterestArt = () => (
  <>
    <rect className={styles.panel} x="24" y="10" width="104" height="100" rx="10" />
    <rect className={styles.field} x="36" y="22" width="80" height="13" rx="3" />
    <rect className={styles.field} x="36" y="41" width="80" height="13" rx="3" />
    <rect className={styles.field} x="36" y="60" width="80" height="13" rx="3" />
    <rect className={cx('bar', 'type', 'typeA')} x="41" y="26.5" width="50" height="4" rx="2" />
    <rect className={cx('bar', 'type', 'typeB')} x="41" y="45.5" width="62" height="4" rx="2" />
    <rect className={cx('bar', 'type', 'typeC')} x="41" y="64.5" width="40" height="4" rx="2" />
    <rect className={styles.field} x="36" y="80" width="9" height="9" rx="2" />
    <path className={cx('greenStroke', 'pop', 'in40')} d="M38.3,84.6 l2.1,2.1 l3.7,-4.3" />
    <rect className={styles.barMuted} x="50" y="82.5" width="50" height="4" rx="2" />
    <rect className={cx('greenFill', 'press')} x="36" y="95" width="80" height="9" rx="4.5" />
    <InviteMail from="M118,99.5 C146,99.5 146,59 164,59" />
  </>
);

/** 1 · WhatsApp (quando houver o número oficial): a conversa vira o convite. */
const WhatsappArt = () => (
  <>
    <rect className={styles.panel} x="36" y="6" width="76" height="108" rx="12" />
    <path className={styles.lineMuted} d="M66,13 H82" />
    <g className={cx('pop', 'in10')}>
      <rect className={styles.bubbleMine} x="58" y="22" width="46" height="16" rx="7" />
      <rect className={styles.bar} x="64" y="28" width="34" height="4" rx="2" />
    </g>
    <g className={cx('pop', 'in22')}>
      <rect className={styles.bubbleTheirs} x="44" y="44" width="54" height="24" rx="7" />
      <rect className={styles.bar} x="50" y="50" width="42" height="4" rx="2" />
      <rect className={styles.barMuted} x="50" y="58" width="28" height="4" rx="2" />
    </g>
    <g className={cx('pop', 'in34')}>
      <rect className={styles.bubbleMine} x="66" y="74" width="38" height="16" rx="7" />
      <rect className={styles.bar} x="72" y="80" width="26" height="4" rx="2" />
    </g>
    <InviteMail from="M114,86 C144,86 146,59 164,59" />
  </>
);

/** 2 · Pedido no painel: endereço, veículo, rastreador + assinatura, Pix pago. */
const OrderArt = () => (
  <>
    <rect className={styles.panel} x="14" y="10" width="212" height="100" rx="9" />
    <circle className={styles.dot} cx="25" cy="19" r="2" />
    <circle className={styles.dot} cx="32" cy="19" r="2" />
    <circle className={styles.dot} cx="39" cy="19" r="2" />
    <path className={styles.divider} d="M14,27 H226" />

    <g className={cx('pop', 'in04')}>
      <path
        className={styles.greenFill}
        d="M30,35 c-3.6,0 -6.5,2.8 -6.5,6.2 c0,4.6 6.5,10.3 6.5,10.3 s6.5,-5.7 6.5,-10.3 c0,-3.4 -2.9,-6.2 -6.5,-6.2 z"
      />
      <circle className={styles.inkFill} cx="30" cy="41.2" r="2.2" />
      <rect className={styles.bar} x="44" y="38" width="72" height="4" rx="2" />
      <rect className={styles.barMuted} x="44" y="46" width="46" height="4" rx="2" />
    </g>
    <g className={cx('pop', 'in16')}>
      <path className={styles.line} d="M21,70 v-4.5 l3.5,-1.2 l3,-4.3 h7.5 l3,4.3 l3.5,1.2 v4.5 z" />
      <circle className={styles.wheel} cx="26" cy="70.5" r="2.4" />
      <circle className={styles.wheel} cx="35" cy="70.5" r="2.4" />
      <rect className={styles.bar} x="44" y="60" width="64" height="4" rx="2" />
      <rect className={styles.barMuted} x="44" y="68" width="38" height="4" rx="2" />
    </g>
    <g className={cx('pop', 'in28')}>
      <rect className={styles.greenStroke} x="22.5" y="81" width="15" height="10" rx="2.5" />
      <circle className={styles.greenFill} cx="33.5" cy="86" r="1.6" />
      <rect className={styles.bar} x="44" y="82" width="56" height="4" rx="2" />
      <rect className={styles.barMuted} x="44" y="90" width="50" height="4" rx="2" />
    </g>

    <g className={cx('pop', 'in40')}>
      <rect className={styles.qrBack} x="152" y="32" width="56" height="56" rx="4" />
      <g className={styles.qr}>
        <rect x="156" y="36" width="12" height="12" rx="1.5" className={styles.qrFinder} />
        <rect x="192" y="36" width="12" height="12" rx="1.5" className={styles.qrFinder} />
        <rect x="156" y="72" width="12" height="12" rx="1.5" className={styles.qrFinder} />
        <rect x="159.5" y="39.5" width="5" height="5" />
        <rect x="195.5" y="39.5" width="5" height="5" />
        <rect x="159.5" y="75.5" width="5" height="5" />
        <rect x="172" y="37" width="4" height="4" />
        <rect x="180" y="41" width="4" height="4" />
        <rect x="174" y="49" width="4" height="4" />
        <rect x="184" y="51" width="4" height="4" />
        <rect x="158" y="56" width="4" height="4" />
        <rect x="166" y="60" width="4" height="4" />
        <rect x="176" y="59" width="4" height="4" />
        <rect x="188" y="61" width="4" height="4" />
        <rect x="198" y="54" width="4" height="4" />
        <rect x="196" y="66" width="4" height="4" />
        <rect x="174" y="70" width="4" height="4" />
        <rect x="186" y="73" width="4" height="4" />
        <rect x="178" y="79" width="4" height="4" />
        <rect x="194" y="78" width="4" height="4" />
      </g>
      <text className={styles.label} x="180" y="101" textAnchor="middle">
        Pix
      </text>
    </g>
    <path className={styles.scan} d="M150,34 H210" />
    <g className={cx('pop', 'in70')}>
      <circle className={styles.greenFill} cx="207" cy="85" r="9" />
      <path className={styles.inkStroke} d="M202.5,85 l3,3 l5.5,-6" />
    </g>
  </>
);

/** 3 · Na base: o chip M2M entra, o rastreador é configurado, as duas etapas ficam prontas. */
const PrepareArt = () => (
  <>
    {/* O chip vem antes do corpo: a parte de baixo fica "dentro" do rastreador. */}
    <g className={styles.chip}>
      <path className={styles.chipBody} d="M77,25 h14 l6,6 v23 h-20 z" />
      <rect className={styles.chipGold} x="81" y="33" width="12" height="10" rx="1.5" />
      <path className={styles.chipLines} d="M87,33 V43 M81,38 H93" />
    </g>
    <rect className={styles.trackerBody} x="50" y="46" width="76" height="42" rx="8" />
    <rect className={styles.slot} x="75" y="44.5" width="24" height="3.5" rx="1.5" />
    <rect className={styles.barMuted} x="60" y="60" width="28" height="4" rx="2" />
    <rect className={styles.barMuted} x="60" y="69" width="18" height="4" rx="2" />
    <circle className={styles.ledBoot} cx="114" cy="58" r="3.5" />

    <g className={styles.gear}>
      <circle className={styles.gearTeeth} cx="126" cy="46" r="10" />
      <circle className={styles.gearBody} cx="126" cy="46" r="7" />
      <circle className={styles.gearHole} cx="126" cy="46" r="2.6" />
    </g>

    <rect className={styles.track} x="50" y="98" width="76" height="6" rx="3" />
    <rect className={cx('greenFill', 'progress')} x="50" y="98" width="76" height="6" rx="3" />

    <circle className={styles.todo} cx="164" cy="52" r="7" />
    <g className={cx('pop', 'in22')}>
      <circle className={styles.greenFill} cx="164" cy="52" r="7" />
      <path className={styles.inkStroke} d="M160.6,52 l2.3,2.3 l4.3,-4.8" />
    </g>
    <rect className={styles.bar} x="177" y="46" width="38" height="4" rx="2" />
    <rect className={styles.barMuted} x="177" y="54" width="26" height="4" rx="2" />

    <circle className={styles.todo} cx="164" cy="80" r="7" />
    <g className={cx('pop', 'in64')}>
      <circle className={styles.greenFill} cx="164" cy="80" r="7" />
      <path className={styles.inkStroke} d="M160.6,80 l2.3,2.3 l4.3,-4.8" />
    </g>
    <rect className={styles.bar} x="177" y="74" width="46" height="4" rx="2" />
    <rect className={styles.barMuted} x="177" y="82" width="30" height="4" rx="2" />
  </>
);

/** Um envelope pequeno, para os avisos por e-mail. */
const Mail: React.FC<{ x: number; y: number }> = ({ x, y }) => (
  <>
    <rect className={styles.panelStrong} x={x} y={y} width="28" height="19" rx="3" />
    <path className={styles.line} d={`M${x + 1.5},${y + 1.5} L${x + 14},${y + 11} L${x + 26.5},${y + 1.5}`} />
  </>
);

/** 4 · Envio: o caminhão sai da base até a casa; um e-mail na saída e outro na chegada. */
const ShipArt = () => (
  <>
    <path className={styles.road} d="M10,92 H230" />
    <path className={styles.trail} d="M80,92 H158" />

    {/* Base */}
    <path className={styles.line} d="M14,62 L38,47 L62,62" />
    <rect className={styles.panelStrong} x="18" y="62" width="40" height="30" />
    <path className={styles.lineMuted} d="M31,76 H45 M31,81 H45 M31,86 H45" />

    {/* Casa */}
    <path className={styles.line} d="M180,64 L203,47 L226,64" />
    <rect className={styles.panelStrong} x="184" y="64" width="38" height="28" />
    <rect className={styles.window} x="189" y="70" width="8" height="8" rx="1" />
    <rect className={styles.door} x="203" y="76" width="10" height="16" rx="1" />

    <g className={cx('pop', 'in10')}>
      <Mail x={24} y={16} />
    </g>
    <g className={cx('pop', 'in70')}>
      <Mail x={189} y={16} />
      <circle className={styles.greenFill} cx="217" cy="16" r="7" />
      <path className={styles.inkStroke} d="M213.8,16 l2.1,2.1 l4,-4.5" />
    </g>

    {/* O caminhão está desenhado na chegada; a animação o traz desde a base. */}
    <g className={styles.truck}>
      <rect className={styles.panelStrong} x="140" y="70" width="25" height="18" rx="2" />
      <rect className={styles.greenFill} x="144" y="77" width="17" height="3" rx="1.5" />
      <path className={styles.line} d="M165,74 h7 l5,6 v8 h-12 z" />
      <path className={styles.window} d="M167,76.5 h4 l3,3.5 h-7 z" />
      <circle className={styles.wheel} cx="149" cy="89" r="3.5" />
      <circle className={styles.wheel} cx="170" cy="89" r="3.5" />
    </g>
  </>
);

/** 5 · Instalação: os parceiros no painel, um é escolhido, horário e valor pelo WhatsApp. */
const InstallerRow: React.FC<{ y: number; at: string }> = ({ y, at }) => (
  <g className={cx('pop', at)}>
    <circle className={styles.avatar} cx="31" cy={y + 12} r="8" />
    <circle className={styles.lightFill} cx="31" cy={y + 10} r="2.8" />
    <path className={styles.lightFill} d={`M25.5,${y + 17.5} a5.5,4.5 0 0 1 11,0 z`} />
    <rect className={styles.bar} x="45" y={y + 6} width="50" height="4" rx="2" />
    {[0, 1, 2, 3, 4].map((i) => (
      <circle key={i} className={styles.amberFill} cx={47 + i * 6} cy={y + 16.5} r="1.7" />
    ))}
  </g>
);

const InstallArt = () => (
  <>
    <rect className={styles.panel} x="14" y="10" width="110" height="100" rx="10" />
    <rect className={cx('picked', 'pop', 'in22')} x="18" y="44" width="102" height="28" rx="7" />
    <InstallerRow y={18} at="in04" />
    <InstallerRow y={46} at="in10" />
    <InstallerRow y={74} at="in16" />

    <g className={cx('pop', 'in34')}>
      <rect className={styles.bubbleTheirs} x="168" y="14" width="58" height="22" rx="8" />
      <circle className={styles.line} cx="181" cy="25" r="5.5" />
      <path className={styles.line} d="M181,22 V25 L183.5,26.5" />
      <rect className={styles.bar} x="192" y="23" width="26" height="4" rx="2" />
    </g>
    <g className={cx('pop', 'in46')}>
      <rect className={styles.bubbleAmber} x="134" y="42" width="62" height="22" rx="8" />
      <text className={styles.labelAmber} x="141" y="56.5">
        R$
      </text>
      <rect className={styles.barAmber} x="158" y="51" width="30" height="4" rx="2" />
    </g>
    <g className={cx('pop', 'in58')}>
      <rect className={styles.panelStrong} x="166" y="74" width="44" height="34" rx="5" />
      <rect className={styles.amberFill} x="166.75" y="74.75" width="42.5" height="8" rx="4" />
      <path className={styles.line} d="M176,70.5 V77 M200,70.5 V77" />
    </g>
    <path className={cx('checkDraw', 'pop', 'in64')} d="M180,95 l5,5 l10,-11" />
  </>
);

/** 6 · Tempo real: o trajeto no mapa do celular, os alertas e o bloqueio. */
const TrackArt = () => (
  <>
    <rect className={styles.phone} x="18" y="6" width="66" height="108" rx="11" />
    <rect className={styles.screen} x="24" y="16" width="54" height="88" rx="4" />
    <path className={styles.lineMuted} d="M45,11 H57" />
    <path className={styles.street} d="M24,40 H78 M24,70 H78 M40,16 V104 M62,16 V104" />
    <circle className={styles.origin} cx="40" cy="98" r="3" />
    <path className={styles.route} pathLength={1} d="M40,98 V70 H62 V40 H74" />
    <circle className={styles.pulse} cx="74" cy="40" r="4.5" />
    <circle className={styles.car} cx="74" cy="40" r="4.5" />

    <g className={cx('pop', 'in58')}>
      <rect className={styles.panelStrong} x="94" y="12" width="134" height="27" rx="8" />
      <circle className={styles.alertFill} cx="108" cy="25.5" r="7" />
      <path className={styles.whiteStroke} d="M108,21.8 V26.2 M108,29.3 V29.4" />
      <rect className={styles.bar} x="121" y="19.5" width="72" height="4" rx="2" />
      <rect className={styles.barMuted} x="121" y="27.5" width="46" height="4" rx="2" />
    </g>
    <g className={cx('pop', 'in64')}>
      <rect className={styles.panelStrong} x="94" y="45" width="134" height="27" rx="8" />
      <rect className={styles.greenStroke} x="101" y="53.5" width="14" height="10" rx="1.5" />
      <path className={styles.greenStroke} d="M102,54.5 L108,59 L114,54.5" />
      <rect className={styles.bar} x="121" y="52.5" width="64" height="4" rx="2" />
      <rect className={styles.barMuted} x="121" y="60.5" width="40" height="4" rx="2" />
    </g>
    <g className={cx('pop', 'in70')}>
      <rect className={styles.panelStrong} x="94" y="78" width="134" height="30" rx="8" />
      <path className={cx('line', 'shackle')} d="M104.5,90 v-3.5 a3.5,3.5 0 0 1 7,0 v3.5" />
      <rect className={styles.lightFill} x="102" y="89.5" width="12" height="10" rx="2" />
      <rect className={styles.bar} x="121" y="88" width="52" height="4" rx="2" />
      <rect className={styles.toggleTrack} x="196" y="86.5" width="24" height="13" rx="6.5" />
      <circle className={styles.toggleKnob} cx="213.5" cy="93" r="4.5" />
    </g>
  </>
);

const ART: Record<StepArtKind, React.FC> = {
  interesse: InterestArt,
  whatsapp: WhatsappArt,
  pedido: OrderArt,
  preparo: PrepareArt,
  envio: ShipArt,
  instalacao: InstallArt,
  acompanhamento: TrackArt,
};

/**
 * A ilustração animada de uma etapa do passo a passo, num ciclo de 7 s. É
 * decorativa: o texto do card já diz tudo, então fica fora do leitor de tela.
 *
 * O desenho sem animação é a cena final: é o que aparece com "reduzir
 * movimento" ligado no aparelho.
 */
export const HowItWorksArt: React.FC<{ kind: StepArtKind }> = ({ kind }) => {
  const Art = ART[kind];
  return (
    <svg className={styles.scene} viewBox="0 0 240 120" aria-hidden="true" focusable="false" data-art={kind}>
      <Art />
    </svg>
  );
};

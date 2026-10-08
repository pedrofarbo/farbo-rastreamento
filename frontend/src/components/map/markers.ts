import L from 'leaflet';

/**
 * Ícones do mapa.
 *
 * O marcador é um SVG inline em vez de imagem: assim ele acompanha os tokens
 * de cor do tema e gira conforme o rumo sem precisar de sprite por ângulo.
 *
 * O SVG é montado nó a nó (createElementNS + setAttribute), nunca como texto
 * HTML: o rumo e os demais valores vêm do rastreador pelo WebSocket, e texto
 * interpolado num innerHTML viraria injeção de marcação. Com setAttribute um
 * valor estranho fica, no máximo, um atributo inválido — nunca um nó novo.
 */

const SVG_NS = 'http://www.w3.org/2000/svg';

/** Verde = ignição ligada, vermelho = desligada, cinza = ainda sem leitura. */
const IGNITION_ON = '#3be558';
const IGNITION_OFF = '#ef5b52';
const IGNITION_UNKNOWN = '#64748b';

/** Contorno dos ícones: o fundo da marca, para destacá-los sobre o mapa. */
const OUTLINE = '#060907';

/** Cor do selo de bloqueio, deliberadamente distinta do vermelho de ignição
 * desligada: as duas coisas podem ocorrer juntas e precisam ser distinguíveis
 * à primeira vista (ver docs sobre ACC × relé serem sinais independentes). */
const BLOCKED_BADGE = '#b3261e';

/** Altura reservada ao selo de bloqueio, na mesma unidade do desenho do
 * veículo (que usa um viewBox fixo de 32 unidades de largura). */
const BADGE_BAND = 14;

interface VehicleIconOptions {
  /**
   * Estado da ignição (ACC), lido do próprio rastreador.
   * true = ligada (verde) · false = desligada (vermelho) · null = sem leitura
   * ainda (cinza).
   */
  ignition: boolean | null;
  heading: number | null;
  /** O veículo andando ganha as ondas (e, selecionado, a velocidade). */
  moving: boolean;
  /** Carro ou moto, vistos de cima e virados para o rumo (sem tipo: carro). */
  kind?: VehicleKind | string | null;
  selected: boolean;
  /**
   * Relé de corte acionado. Mostra um selo de cadeado acima do marcador — não
   * muda a cor do marcador em si, porque ignição e bloqueio são dois sinais
   * independentes do rastreador (o motor pode estar bloqueado com a ignição
   * ligada ou desligada).
   */
  blocked: boolean;
  /**
   * Dispositivo sem comunicação recente (STALE/OFFLINE): o ícone fica
   * esmaecido para avisar que a cor pode não refletir o estado atual.
   */
  online?: boolean;
  /** A velocidade, na etiqueta do veículo selecionado em movimento. */
  speedKmh?: number | null;
}

/** A velocidade da etiqueta: um número de 0 a 400; qualquer outra coisa, nada. */
export function safeSpeed(speed: unknown): number | null {
  return typeof speed === 'number' && Number.isFinite(speed) && speed >= 0 && speed <= 400 ? Math.round(speed) : null;
}

export type VehicleKind = 'CAR' | 'MOTORCYCLE';

/** Rumo em graus, sempre um número de 0 a 360. Qualquer outra coisa (texto,
 * objeto, NaN, fora da faixa) vira 0: a seta aponta para o norte. */
export function safeHeading(heading: unknown): number {
  return typeof heading === 'number' && Number.isFinite(heading) && heading >= 0 && heading <= 360
    ? heading
    : 0;
}

type Attributes = Record<string, string | number>;

function svgNode<K extends keyof SVGElementTagNameMap>(
  tag: K,
  attributes: Attributes,
  ...children: SVGElement[]
): SVGElementTagNameMap[K] {
  const node = document.createElementNS(SVG_NS, tag);
  for (const [name, value] of Object.entries(attributes)) {
    node.setAttribute(name, String(value));
  }
  node.append(...children);
  return node;
}

/** O Leaflet aceita qualquer Element em `html` (confere com instanceof
 * Element e usa appendChild); a tipagem dele é que só fala em HTMLElement. */
function asIconHtml(node: SVGSVGElement): HTMLElement {
  return node as unknown as HTMLElement;
}

export function vehicleIcon({
  ignition,
  heading,
  moving,
  selected,
  blocked,
  online = true,
  speedKmh = null,
  kind = 'CAR',
}: VehicleIconOptions): L.DivIcon {
  const color = ignition === true ? IGNITION_ON : ignition === false ? IGNITION_OFF : IGNITION_UNKNOWN;
  const rotation = safeHeading(heading);
  const size = selected ? 42 : 36;

  // O veículo é sempre desenhado no mesmo espaço de 32x32 unidades; quando há
  // selo de bloqueio, o viewBox cresce para cima e o grupo do veículo desce
  // pelo tamanho da faixa reservada — o desenho do veículo em si não muda.
  const bandUnits = blocked ? BADGE_BAND : 0;
  const totalUnits = 32 + bandUnits;
  const scale = size / 32;
  const pixelWidth = size;
  const pixelHeight = scale * totalUnits;

  const vehicle = svgNode('g', { transform: `translate(0, ${bandUnits})` });
  // Andando (e com sinal): ondas saindo do veículo, na cor da ignição, e um
  // halo por baixo da seta. As ondas são CSS (TrackerMap.module.css) e param
  // para quem pede menos movimento.
  const live = moving && online;
  if (live) {
    const ring = { cx: 16, cy: 16, r: 9, fill: color, 'fill-opacity': 0.12, stroke: color, 'stroke-width': 2 };
    vehicle.append(
      svgNode('circle', { class: 'vehicle-pulse', ...ring }),
      svgNode('circle', { class: 'vehicle-pulse vehicle-pulse-late', ...ring }),
      svgNode('circle', { cx: 16, cy: 16, r: 12, fill: color, opacity: 0.2 }),
    );
  }
  if (selected) {
    vehicle.append(
      svgNode('circle', { cx: 16, cy: 16, r: 15, fill: 'none', stroke: color, 'stroke-width': 1.5, opacity: 0.5 }),
    );
  }
  // O carro ou a moto, de cima, com a frente para o rumo.
  vehicle.append(
    svgNode(
      'g',
      { class: 'vehicle-body', 'data-kind': kind === 'MOTORCYCLE' ? 'motorcycle' : 'car', transform: `rotate(${rotation} 16 16)` },
      ...(kind === 'MOTORCYCLE' ? motorcycle(color) : car(color)),
    ),
  );

  // A etiqueta da velocidade, logo abaixo do veículo selecionado andando.
  const speed = live && selected ? safeSpeed(speedKmh) : null;
  if (speed !== null) vehicle.append(...speedTag(speed));

  const svg = svgNode('svg', { width: pixelWidth, height: pixelHeight, viewBox: `0 0 32 ${totalUnits}` });
  if (!online) svg.setAttribute('opacity', '0.55');
  if (blocked) svg.append(...blockedBadge(16, BADGE_BAND / 2));
  svg.append(vehicle);

  // O ponto de ancoragem fica sempre no centro do veículo (nunca no selo),
  // para o marcador continuar exatamente sobre a coordenada do GPS.
  const anchorY = scale * (16 + bandUnits);

  return L.divIcon({
    className: live ? 'vehicle-marker vehicle-marker-moving' : 'vehicle-marker',
    html: asIconHtml(svg),
    iconSize: [pixelWidth, pixelHeight],
    iconAnchor: [pixelWidth / 2, anchorY],
    // Abre acima de tudo o que está desenhado no topo do ícone — o selo,
    // quando existe, ou o próprio veículo quando não há bloqueio.
    popupAnchor: [0, -anchorY],
  });
}

/** O carro visto de cima, frente para cima: carroceria na cor da ignição,
 * para-brisa e vidro traseiro escuros, teto claro, retrovisores e faróis. */
function car(color: string): SVGElement[] {
  const glass = { fill: OUTLINE, opacity: 0.8 };
  return [
    svgNode('ellipse', { cx: 8.3, cy: 11.2, rx: 1.5, ry: 1, fill: color, stroke: OUTLINE, 'stroke-width': 0.9 }),
    svgNode('ellipse', { cx: 23.7, cy: 11.2, rx: 1.5, ry: 1, fill: color, stroke: OUTLINE, 'stroke-width': 0.9 }),
    svgNode('path', {
      d: 'M16 2.4 C20.4 2.4 22.7 4.3 23 7.6 L23.3 25.4 C23.3 28.2 21.5 29.7 18.7 29.7 L13.3 29.7 C10.5 29.7 8.7 28.2 8.7 25.4 L9 7.6 C9.3 4.3 11.6 2.4 16 2.4 Z',
      fill: color,
      stroke: OUTLINE,
      'stroke-width': 1.4,
      'stroke-linejoin': 'round',
    }),
    svgNode('path', { d: 'M10.5 10.4 C12.8 9 19.2 9 21.5 10.4 L20.3 14.4 L11.7 14.4 Z', ...glass }),
    svgNode('rect', { x: 11.7, y: 14.4, width: 8.6, height: 7.2, rx: 1, fill: '#ffffff', opacity: 0.22 }),
    svgNode('path', { d: 'M11.7 21.6 L20.3 21.6 L21.2 25 C19 26 13 26 10.8 25 Z', ...glass }),
    svgNode('ellipse', { cx: 12.1, cy: 4.4, rx: 1.5, ry: 0.8, fill: '#fff6c2' }),
    svgNode('ellipse', { cx: 19.9, cy: 4.4, rx: 1.5, ry: 0.8, fill: '#fff6c2' }),
  ];
}

/** A moto vista de cima, frente para cima: rodas e guidão escuros, tanque e
 * banco na cor da ignição, o capacete do piloto e o farol. */
function motorcycle(color: string): SVGElement[] {
  const dark = { fill: OUTLINE };
  return [
    svgNode('rect', { x: 14.1, y: 21.4, width: 3.8, height: 8.6, rx: 1.9, ...dark }),
    svgNode('rect', { x: 14.1, y: 1.8, width: 3.8, height: 7.6, rx: 1.9, ...dark }),
    svgNode('rect', { x: 8.2, y: 8.3, width: 15.6, height: 2.2, rx: 1.1, ...dark }),
    svgNode('circle', { cx: 8.6, cy: 9.4, r: 1.5, fill: color, stroke: OUTLINE, 'stroke-width': 0.9 }),
    svgNode('circle', { cx: 23.4, cy: 9.4, r: 1.5, fill: color, stroke: OUTLINE, 'stroke-width': 0.9 }),
    svgNode('path', {
      d: 'M16 6.4 C18.7 6.4 20.1 8.4 20.1 11 L19.3 21.8 C19.1 24.5 17.8 25.8 16 25.8 C14.2 25.8 12.9 24.5 12.7 21.8 L11.9 11 C11.9 8.4 13.3 6.4 16 6.4 Z',
      fill: color,
      stroke: OUTLINE,
      'stroke-width': 1.3,
      'stroke-linejoin': 'round',
    }),
    svgNode('rect', { x: 13.9, y: 17.4, width: 4.2, height: 6.4, rx: 2.1, fill: OUTLINE, opacity: 0.55 }),
    svgNode('circle', { cx: 16, cy: 14.6, r: 3.3, fill: OUTLINE, stroke: color, 'stroke-width': 0.9 }),
    svgNode('ellipse', { cx: 15.1, cy: 13.6, rx: 1.1, ry: 0.7, fill: '#ffffff', opacity: 0.55 }),
    svgNode('ellipse', { cx: 16, cy: 6.9, rx: 1.4, ry: 0.7, fill: '#fff6c2' }),
  ];
}

/** "62 km/h" numa pílula escura abaixo do veículo (fora da área do ícone: o
 * SVG mostra o que passa da borda). */
function speedTag(speed: number): SVGElement[] {
  const text = `${speed} km/h`;
  const width = 8 + text.length * 3.9;
  const label = svgNode('text', {
    x: 16, y: 39.6, 'font-size': 6.5, 'font-weight': 700, 'text-anchor': 'middle', fill: '#ffffff',
    'font-family': 'system-ui, sans-serif',
  });
  label.textContent = text;
  return [
    svgNode('rect', { x: 16 - width / 2, y: 33, width, height: 9, rx: 4.5, fill: OUTLINE, opacity: 0.85 }),
    label,
  ];
}

/** Selo de motor bloqueado: halo + círculo + cadeado, para chamar atenção
 * mesmo num ícone pequeno no mapa. */
function blockedBadge(cx: number, cy: number): SVGElement[] {
  const lock = svgNode('text', { x: cx, y: cy + 2.5, 'font-size': 7, 'text-anchor': 'middle' });
  lock.textContent = '🔒';
  return [
    svgNode('circle', { cx, cy, r: 8, fill: BLOCKED_BADGE, opacity: 0.25 }),
    svgNode('circle', { cx, cy, r: 6, fill: BLOCKED_BADGE, stroke: '#ffffff', 'stroke-width': 1.25 }),
    lock,
  ];
}

function dotIcon(className: string, size: number, radius: number, color: string, strokeWidth: number): L.DivIcon {
  const center = size / 2;
  return L.divIcon({
    className,
    html: asIconHtml(
      svgNode(
        'svg',
        { width: size, height: size, viewBox: `0 0 ${size} ${size}` },
        svgNode('circle', { cx: center, cy: center, r: radius, fill: color, stroke: OUTLINE, 'stroke-width': strokeWidth }),
      ),
    ),
    iconSize: [size, size],
    iconAnchor: [center, center],
  });
}

/** Marcador pequeno para os pontos do histórico. */
export function historyIcon(color = IGNITION_ON): L.DivIcon {
  return dotIcon('history-marker', 10, 3.5, color, 1.5);
}

/** Bandeiras de início e fim de um trajeto. */
export function endpointIcon(kind: 'start' | 'end'): L.DivIcon {
  return dotIcon('endpoint-marker', 18, 7, kind === 'start' ? IGNITION_ON : IGNITION_OFF, 2);
}

import { LAUNCH_OFFER, PRE_LAUNCH, installmentsOf, priceParts } from '@/config/landing';

/**
 * O flyer do afiliado, desenhado num canvas: a oferta do site (os mesmos
 * preços da landing), os destaques do plano e o QR Code do link de indicação
 * dele. Três formatos: A5 para a gráfica (PDF ou PNG, 300 dpi), stories e
 * post. Tudo é desenhado numa largura de 1080 "unidades" e escalado para os
 * pixels do formato.
 */

export type FlyerFormat = 'a5' | 'stories' | 'post';

export interface FormatSpec {
  label: string;
  hint: string;
  width: number;
  height: number;
  /** Sai também em PDF (o tamanho da página, em pontos). */
  pdf?: { width: number; height: number };
}

/** A5 (148 × 210 mm) a 300 dpi; stories e post, nos tamanhos do Instagram. */
export const FLYER_FORMATS: Record<FlyerFormat, FormatSpec> = {
  a5: { label: 'Flyer A5', hint: 'Para imprimir: 14,8 × 21 cm', width: 1748, height: 2480, pdf: { width: 419.53, height: 595.28 } },
  stories: { label: 'Stories', hint: 'Instagram e WhatsApp: 1080 × 1920', width: 1080, height: 1920 },
  post: { label: 'Post', hint: 'Feed do Instagram: 1080 × 1350', width: 1080, height: 1350 },
};

/** O que muda de um afiliado para outro. */
export interface FlyerContent {
  /** O link de indicação inteiro (https://…/indicacao/<código>). */
  link: string;
  /** "Indicado por …" ("" não mostra). */
  referrer: string;
}

export interface FlyerAssets {
  logo: CanvasImageSource;
  qr: CanvasImageSource;
}

/** Um pedaço de texto de uma linha com estilos diferentes. */
interface Segment {
  text: string;
  color?: string;
  weight?: number;
  strike?: boolean;
}

export interface FlyerOffer {
  tag: string;
  monthlyCents: number;
  /** A linha embaixo do preço. */
  note: Segment[];
  /** O rastreador: preço e parcelamento. */
  equipment: Segment[];
  /** O convite ao lado do QR. */
  cta: string;
  /** As letras miúdas do rodapé. */
  fine: string;
}

const GREEN = '#3be558';
const WHITE = '#ffffff';
const MUTED = '#94a3b8';
const SOFT = '#cbd5e1';
const FONT = 'Inter, system-ui, -apple-system, "Segoe UI", sans-serif';

/** A oferta do flyer: no pré-lançamento, a promoção (os valores da landing). */
export function flyerOffer(preLaunch = PRE_LAUNCH): FlyerOffer {
  if (preLaunch) {
    return {
      tag: 'PREÇO DE PRÉ-LANÇAMENTO',
      monthlyCents: LAUNCH_OFFER.monthlyCents,
      note: [
        { text: `nos ${LAUNCH_OFFER.months} primeiros meses · o normal é ` },
        { text: LAUNCH_OFFER.monthlyRegular, strike: true },
      ],
      equipment: [
        { text: 'Rastreador ' },
        { text: LAUNCH_OFFER.equipmentRegular, strike: true },
        { text: ` ${LAUNCH_OFFER.equipment}`, color: WHITE, weight: 800 },
        { text: ` ou ${installmentsOf(LAUNCH_OFFER.equipmentCents)} sem juros no Pix` },
      ],
      cta: 'e garanta o seu preço',
      fine: `Promoção para os ${LAUNCH_OFFER.slots} primeiros da lista, 1 veículo por pessoa.`,
    };
  }
  return {
    tag: 'PLANO MENSAL',
    monthlyCents: LAUNCH_OFFER.monthlyRegularCents,
    note: [{ text: 'chip M2M, app e suporte inclusos' }],
    equipment: [
      { text: 'Rastreador ' },
      { text: LAUNCH_OFFER.equipmentRegular, color: WHITE, weight: 800 },
      { text: ` ou ${installmentsOf(LAUNCH_OFFER.equipmentRegularCents)} sem juros no Pix` },
    ],
    cta: 'e contrate o seu',
    fine: '',
  };
}

const FEATURES = ['Localização em tempo real', 'Alertas inteligentes', 'Histórico de rotas', 'Chip M2M incluso'];

/** Os blocos do flyer, de cima para baixo, com a altura de cada um (em unidades). */
type Block = 'logo' | 'tag' | 'headline' | 'sub' | 'price' | 'features' | 'qr' | 'footer';

const HEIGHTS: Record<Block, number> = {
  logo: 92,
  tag: 48,
  headline: 300,
  sub: 92,
  price: 340,
  features: 128,
  qr: 340,
  footer: 76,
};

const BLOCKS: Record<FlyerFormat, Block[]> = {
  a5: ['logo', 'tag', 'headline', 'price', 'features', 'qr', 'footer'],
  stories: ['logo', 'tag', 'headline', 'sub', 'price', 'features', 'qr', 'footer'],
  post: ['logo', 'headline', 'price', 'qr', 'footer'],
};

/** A largura do desenho, em unidades. */
const UNITS = 1080;

export interface Layout {
  /** A escala dos blocos (1: o tamanho de projeto; menos, se não couber). */
  scale: number;
  /** Onde começa cada bloco (y em unidades, já com a escala). */
  blocks: { block: Block; y: number; height: number }[];
  /** A altura do formato em unidades. */
  height: number;
}

/**
 * Distribui os blocos na altura do formato: margem em cima e embaixo,
 * espaço igual entre eles (sem exagero; o que sobra vai para as margens) e,
 * se não couber, tudo um pouco menor.
 */
export function flyerLayout(format: FlyerFormat): Layout {
  const spec = FLYER_FORMATS[format];
  const height = (spec.height * UNITS) / spec.width;
  // No stories, o Instagram cobre o topo (perfil) e o pé (responder): tudo
  // fica na faixa do meio.
  const margin = format === 'stories' ? 210 : Math.round(height * 0.05);
  const blocks = BLOCKS[format];
  const content = blocks.reduce((sum, b) => sum + HEIGHTS[b], 0);
  const minGap = 28;
  const maxGap = 72;
  const available = height - 2 * margin;
  const gaps = blocks.length - 1;
  const scale = Math.min(1, available / (content + minGap * gaps));
  const gap = Math.min(maxGap, (available - content * scale) / gaps);
  let y = margin + (available - content * scale - gap * gaps) / 2;
  const out = blocks.map((block) => {
    const h = HEIGHTS[block] * scale;
    const item = { block, y, height: h };
    y += h + gap;
    return item;
  });
  return { scale, blocks: out, height };
}

/** O nome do arquivo: flyer-farbo-joao-moto-a5.pdf. */
export function flyerFileName(code: string, format: FlyerFormat, ext: 'png' | 'pdf'): string {
  return `flyer-farbo-${code}-${format}.${ext}`;
}

// ---------------------------------------------------------------------------
// Desenho
// ---------------------------------------------------------------------------

/** Desenha o flyer no canvas (que já tem o tamanho do formato). */
export function drawFlyer(
  ctx: CanvasRenderingContext2D,
  format: FlyerFormat,
  content: FlyerContent,
  assets: FlyerAssets,
  offer: FlyerOffer = flyerOffer(),
) {
  const spec = FLYER_FORMATS[format];
  const unit = spec.width / UNITS;
  const layout = flyerLayout(format);

  ctx.save();
  ctx.scale(unit, unit);
  background(ctx, layout.height);
  for (const { block, y } of layout.blocks) {
    ctx.save();
    // Cada bloco é desenhado no tamanho de projeto, a partir do topo dele,
    // e encolhe pelo centro quando o formato pede.
    ctx.translate(UNITS / 2, y);
    ctx.scale(layout.scale, layout.scale);
    ctx.translate(-UNITS / 2, 0);
    DRAW[block](ctx, content, assets, offer);
    ctx.restore();
  }
  ctx.restore();
}

type Draw = (ctx: CanvasRenderingContext2D, content: FlyerContent, assets: FlyerAssets, offer: FlyerOffer) => void;

const DRAW: Record<Block, Draw> = {
  logo: (ctx, _c, assets) => {
    const w = 500;
    ctx.drawImage(assets.logo, (UNITS - w) / 2, 0, w, HEIGHTS.logo);
  },
  tag: (ctx) => {
    const text = 'RASTREAMENTO VEICULAR INTELIGENTE';
    setFont(ctx, 800, 24, 3);
    const w = ctx.measureText(text).width + 56;
    pill(ctx, (UNITS - w) / 2, 0, w, HEIGHTS.tag);
    ctx.fillStyle = GREEN;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText(text, UNITS / 2 + 1.5, HEIGHTS.tag / 2 + 1);
  },
  headline: (ctx) => {
    const lines: [string, string][] = [
      ['Seu veículo', WHITE],
      ['sempre mais', GREEN],
      ['seguro', GREEN],
    ];
    ctx.textAlign = 'center';
    ctx.textBaseline = 'alphabetic';
    lines.forEach(([text, color], i) => {
      setFont(ctx, 900, 96, -1);
      fitFont(ctx, text, 940, 900, 96, -1);
      ctx.save();
      if (color === GREEN) {
        ctx.shadowColor = 'rgba(59, 229, 88, 0.45)';
        ctx.shadowBlur = 36;
      }
      ctx.fillStyle = color;
      ctx.fillText(text, UNITS / 2, 84 + i * 100);
      ctx.restore();
    });
  },
  sub: (ctx) => {
    setFont(ctx, 500, 34);
    ctx.fillStyle = SOFT;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'alphabetic';
    wrap(ctx, 'Rastreador para moto e carro, com app, alertas e bloqueio pelo celular.', 860).forEach((line, i) => {
      ctx.fillText(line, UNITS / 2, 36 + i * 46);
    });
  },
  price: (ctx, _c, _a, offer) => {
    const x = 80;
    const w = UNITS - 2 * x;
    const h = HEIGHTS.price;
    ctx.save();
    ctx.shadowColor = 'rgba(59, 229, 88, 0.28)';
    ctx.shadowBlur = 48;
    roundRect(ctx, x, 0, w, h, 40);
    ctx.fillStyle = '#0d130e';
    ctx.fill();
    ctx.restore();
    roundRect(ctx, x, 0, w, h, 40);
    ctx.lineWidth = 3;
    ctx.strokeStyle = GREEN;
    ctx.stroke();

    ctx.textAlign = 'center';
    ctx.textBaseline = 'alphabetic';
    setFont(ctx, 800, 26, 2.5);
    ctx.fillStyle = GREEN;
    ctx.fillText(offer.tag, UNITS / 2, 62);

    // R$ 34,90/mês, com o 34 grande.
    const { reais, cents } = priceParts(offer.monthlyCents);
    const parts: { text: string; size: number; weight: number; color: string }[] = [
      { text: 'R$ ', size: 46, weight: 800, color: WHITE },
      { text: reais, size: 150, weight: 900, color: GREEN },
      { text: cents, size: 60, weight: 900, color: GREEN },
      { text: '/mês', size: 34, weight: 600, color: MUTED },
    ];
    const widths = parts.map((p) => {
      setFont(ctx, p.weight, p.size, p.size > 100 ? -3 : 0);
      return ctx.measureText(p.text).width;
    });
    let px = UNITS / 2 - widths.reduce((a, b) => a + b, 0) / 2;
    ctx.textAlign = 'left';
    parts.forEach((p, i) => {
      setFont(ctx, p.weight, p.size, p.size > 100 ? -3 : 0);
      ctx.save();
      if (p.color === GREEN) {
        ctx.shadowColor = 'rgba(59, 229, 88, 0.4)';
        ctx.shadowBlur = 28;
      }
      ctx.fillStyle = p.color;
      ctx.fillText(p.text, px, 206);
      ctx.restore();
      px += widths[i];
    });

    segments(ctx, offer.note, UNITS / 2, 256, w - 80, 28, 500, MUTED);
    segments(ctx, offer.equipment, UNITS / 2, 306, w - 80, 28, 600, SOFT);
  },
  features: (ctx) => {
    const cols = [130, 580];
    FEATURES.forEach((text, i) => {
      const cx = cols[i % 2];
      const cy = 32 + Math.floor(i / 2) * 64;
      check(ctx, cx, cy);
      setFont(ctx, 600, 30);
      fitFont(ctx, text, 380, 600, 30);
      ctx.fillStyle = WHITE;
      ctx.textAlign = 'left';
      ctx.textBaseline = 'middle';
      ctx.fillText(text, cx + 30, cy + 1);
    });
  },
  qr: (ctx, content, assets, offer) => {
    const size = HEIGHTS.qr;
    const x = 80;
    // O QR no branco, com a borda (a margem do próprio PNG) que a câmera precisa.
    roundRect(ctx, x, 0, size, size, 28);
    ctx.fillStyle = WHITE;
    ctx.fill();
    ctx.save();
    ctx.imageSmoothingEnabled = false;
    ctx.drawImage(assets.qr, x + 10, 10, size - 20, size - 20);
    ctx.restore();

    const tx = x + size + 44;
    const tw = UNITS - 80 - tx;
    ctx.textAlign = 'left';
    ctx.textBaseline = 'alphabetic';
    setFont(ctx, 900, 50, -0.5);
    fitFont(ctx, 'Aponte a câmera', tw, 900, 50, -0.5);
    ctx.fillStyle = WHITE;
    ctx.fillText('Aponte a câmera', tx, 70);
    setFont(ctx, 800, 38);
    fitFont(ctx, offer.cta, tw, 800, 38);
    ctx.fillStyle = GREEN;
    ctx.fillText(offer.cta, tx, 122);

    // O link, quebrado na barra: o site numa linha, o caminho na outra.
    const shown = content.link.replace(/^https?:\/\//, '');
    const cut = shown.indexOf('/');
    const lines = cut > 0 ? [shown.slice(0, cut + 1), shown.slice(cut + 1)] : [shown];
    const longest = lines.reduce((a, b) => (a.length > b.length ? a : b));
    setFont(ctx, 600, 26);
    fitFont(ctx, longest, tw, 600, 26);
    ctx.fillStyle = SOFT;
    lines.forEach((line, i) => ctx.fillText(line, tx, 186 + i * 34));

    if (content.referrer) {
      const label = `Indicado por ${content.referrer}`;
      setFont(ctx, 700, 26);
      fitFont(ctx, label, tw - 40, 700, 26);
      const w = Math.min(tw, ctx.measureText(label).width + 40);
      pill(ctx, tx, size - 58, w, 52);
      ctx.fillStyle = WHITE;
      ctx.textBaseline = 'middle';
      ctx.fillText(label, tx + 20, size - 31);
    }
  },
  footer: (ctx, _c, _a, offer) => {
    ctx.textAlign = 'center';
    ctx.textBaseline = 'alphabetic';
    if (offer.fine) {
      setFont(ctx, 500, 24);
      fitFont(ctx, offer.fine, 920, 500, 24);
      ctx.fillStyle = MUTED;
      ctx.fillText(offer.fine, UNITS / 2, 26);
    }
    setFont(ctx, 800, 28, 1);
    ctx.fillStyle = GREEN;
    ctx.fillText('farborastreadores.com.br', UNITS / 2, offer.fine ? 70 : 48);
  },
};

function background(ctx: CanvasRenderingContext2D, height: number) {
  ctx.fillStyle = '#060907';
  ctx.fillRect(0, 0, UNITS, height);
  const top = ctx.createRadialGradient(UNITS / 2, -height * 0.05, 0, UNITS / 2, -height * 0.05, UNITS * 0.95);
  top.addColorStop(0, 'rgba(59, 229, 88, 0.26)');
  top.addColorStop(1, 'rgba(59, 229, 88, 0)');
  ctx.fillStyle = top;
  ctx.fillRect(0, 0, UNITS, height);
  const bottom = ctx.createRadialGradient(UNITS, height, 0, UNITS, height, UNITS * 0.8);
  bottom.addColorStop(0, 'rgba(59, 229, 88, 0.12)');
  bottom.addColorStop(1, 'rgba(59, 229, 88, 0)');
  ctx.fillStyle = bottom;
  ctx.fillRect(0, 0, UNITS, height);
}

function setFont(ctx: CanvasRenderingContext2D, weight: number, size: number, spacing = 0) {
  ctx.font = `${weight} ${size}px ${FONT}`;
  // letterSpacing ainda não existe em todo navegador: sem ele, só fica mais junto.
  (ctx as CanvasRenderingContext2D & { letterSpacing?: string }).letterSpacing = `${spacing}px`;
}

/** Diminui a fonte até o texto caber na largura. */
function fitFont(ctx: CanvasRenderingContext2D, text: string, max: number, weight: number, size: number, spacing = 0) {
  let s = size;
  while (s > 12 && ctx.measureText(text).width > max) {
    s -= 1;
    setFont(ctx, weight, s, spacing);
  }
  return s;
}

/** Quebra o texto em linhas que caibam na largura. */
function wrap(ctx: CanvasRenderingContext2D, text: string, max: number): string[] {
  const lines: string[] = [];
  let line = '';
  for (const word of text.split(' ')) {
    const next = line ? `${line} ${word}` : word;
    if (line && ctx.measureText(next).width > max) {
      lines.push(line);
      line = word;
    } else {
      line = next;
    }
  }
  if (line) lines.push(line);
  return lines;
}

/** Uma linha centralizada com pedaços de estilos diferentes (e riscados). */
function segments(
  ctx: CanvasRenderingContext2D,
  parts: Segment[],
  cx: number,
  y: number,
  max: number,
  size: number,
  weight: number,
  color: string,
) {
  let s = size;
  const measure = () =>
    parts.map((p) => {
      setFont(ctx, p.weight ?? weight, s);
      return ctx.measureText(p.text).width;
    });
  let widths = measure();
  while (s > 14 && widths.reduce((a, b) => a + b, 0) > max) {
    s -= 1;
    widths = measure();
  }
  let x = cx - widths.reduce((a, b) => a + b, 0) / 2;
  ctx.textAlign = 'left';
  ctx.textBaseline = 'alphabetic';
  parts.forEach((p, i) => {
    setFont(ctx, p.weight ?? weight, s);
    ctx.fillStyle = p.color ?? color;
    ctx.fillText(p.text, x, y);
    if (p.strike) {
      ctx.fillRect(x, y - s * 0.32, widths[i], Math.max(2, s * 0.08));
    }
    x += widths[i];
  });
}

function roundRect(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number) {
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.arcTo(x + w, y, x + w, y + h, r);
  ctx.arcTo(x + w, y + h, x, y + h, r);
  ctx.arcTo(x, y + h, x, y, r);
  ctx.arcTo(x, y, x + w, y, r);
  ctx.closePath();
}

function pill(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number) {
  roundRect(ctx, x, y, w, h, h / 2);
  ctx.fillStyle = 'rgba(59, 229, 88, 0.1)';
  ctx.fill();
  ctx.lineWidth = 2;
  ctx.strokeStyle = 'rgba(59, 229, 88, 0.55)';
  ctx.stroke();
}

/** O visto verde dos destaques. */
function check(ctx: CanvasRenderingContext2D, cx: number, cy: number) {
  ctx.beginPath();
  ctx.arc(cx, cy, 17, 0, Math.PI * 2);
  ctx.fillStyle = GREEN;
  ctx.fill();
  ctx.beginPath();
  ctx.moveTo(cx - 8, cy + 1);
  ctx.lineTo(cx - 2, cy + 7);
  ctx.lineTo(cx + 9, cy - 6);
  ctx.lineWidth = 4;
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';
  ctx.strokeStyle = '#060907';
  ctx.stroke();
}

// ---------------------------------------------------------------------------
// PDF
// ---------------------------------------------------------------------------

/**
 * O PDF de uma página com a imagem (JPEG) ocupando a página inteira: o
 * formato que a gráfica pede, sem biblioteca. width e height são o tamanho
 * da página em pontos (1/72 de polegada).
 */
export function jpegToPdf(jpeg: Uint8Array, pixelWidth: number, pixelHeight: number, width: number, height: number): Uint8Array {
  const encoder = new TextEncoder();
  const chunks: Uint8Array[] = [];
  const offsets: number[] = [];
  let length = 0;
  const push = (chunk: Uint8Array | string) => {
    const bytes = typeof chunk === 'string' ? encoder.encode(chunk) : chunk;
    chunks.push(bytes);
    length += bytes.length;
  };
  const object = (n: number, ...body: (Uint8Array | string)[]) => {
    offsets[n] = length;
    push(`${n} 0 obj\n`);
    body.forEach(push);
    push('\nendobj\n');
  };
  const w = width.toFixed(2);
  const h = height.toFixed(2);
  const drawing = `q ${w} 0 0 ${h} 0 0 cm /Im0 Do Q`;

  push('%PDF-1.4\n%âãÏÓ\n');
  object(1, '<< /Type /Catalog /Pages 2 0 R >>');
  object(2, '<< /Type /Pages /Kids [3 0 R] /Count 1 >>');
  object(
    3,
    `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${w} ${h}] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>`,
  );
  object(
    4,
    `<< /Type /XObject /Subtype /Image /Width ${pixelWidth} /Height ${pixelHeight} /ColorSpace /DeviceRGB ` +
      `/BitsPerComponent 8 /Filter /DCTDecode /Length ${jpeg.length} >>\nstream\n`,
    jpeg,
    '\nendstream',
  );
  object(5, `<< /Length ${drawing.length} >>\nstream\n${drawing}\nendstream`);
  object(6, '<< /Title (Flyer Farbo Rastreadores) /Producer (Farbo Rastreadores) >>');
  const xref = length;
  push(`xref\n0 7\n0000000000 65535 f \n${offsets.slice(1).map((o) => `${String(o).padStart(10, '0')} 00000 n \n`).join('')}`);
  push(`trailer\n<< /Size 7 /Root 1 0 R /Info 6 0 R >>\nstartxref\n${xref}\n%%EOF\n`);

  const out = new Uint8Array(length);
  let at = 0;
  for (const chunk of chunks) {
    out.set(chunk, at);
    at += chunk.length;
  }
  return out;
}

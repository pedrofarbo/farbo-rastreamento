import { MAX_SIDE, fitSize } from './photos';

/**
 * As fotos da vistoria: tiradas da câmera ao vivo (não da galeria), com a
 * marca embaixo — a data e a hora, a placa e o lugar —, reduzidas a
 * MAX_SIDE e em JPEG. A marca é para quem olha a foto depois; a conferência
 * de verdade é a do servidor (a hora do envio, a posição, o hash).
 */

/** A posição do celular, com a precisão em metros e quando chegou. */
export interface PositionFix {
  latitude: number;
  longitude: number;
  accuracy: number | null;
  at: number;
}

/** Até quanto tempo a última posição vale para a foto. */
export const FRESH_MS = 2 * 60_000;

/** A última posição, se ainda for de agora. */
export function freshFix(fix: PositionFix | null, now = Date.now()): PositionFix | null {
  return fix && now - fix.at <= FRESH_MS ? fix : null;
}

/** As linhas da marca: "15/11/2026 14:05 · ABC1D23" e o lugar (ou "sem localização"). */
export function watermarkLines(when: Date, plate: string, fix: PositionFix | null): string[] {
  const pad = (n: number) => String(n).padStart(2, '0');
  const date = `${pad(when.getDate())}/${pad(when.getMonth() + 1)}/${when.getFullYear()} ${pad(when.getHours())}:${pad(when.getMinutes())}`;
  const place = fix
    ? `${fix.latitude.toFixed(5)}, ${fix.longitude.toFixed(5)}${fix.accuracy !== null ? ` (±${Math.round(fix.accuracy)} m)` : ''}`
    : 'sem localização';
  return [[date, plate].filter(Boolean).join(' · '), `${place} · vistoria Farbo`];
}

/** O tamanho da letra e da faixa da marca para a largura da foto. */
export function stampLayout(width: number, lines: number): { font: number; pad: number; band: number } {
  const font = Math.max(14, Math.round(width / 45));
  const pad = Math.round(font * 0.6);
  return { font, pad, band: lines * (font + Math.round(pad / 2)) + pad };
}

/** A imagem reduzida, com a marca na faixa de baixo, em JPEG. */
export async function stampedJpeg(source: CanvasImageSource, width: number, height: number, lines: string[]): Promise<Blob> {
  const size = fitSize(width, height, MAX_SIDE);
  const canvas = document.createElement('canvas');
  canvas.width = size.width;
  canvas.height = size.height;
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('O navegador não conseguiu montar a foto.');
  ctx.drawImage(source, 0, 0, size.width, size.height);
  const { font, pad, band } = stampLayout(size.width, lines.length);
  ctx.fillStyle = 'rgba(0, 0, 0, 0.55)';
  ctx.fillRect(0, size.height - band, size.width, band);
  ctx.fillStyle = '#ffffff';
  ctx.font = `600 ${font}px sans-serif`;
  ctx.textBaseline = 'top';
  lines.forEach((line, i) => {
    ctx.fillText(line, pad, size.height - band + Math.round(pad / 2) + i * (font + Math.round(pad / 2)), size.width - 2 * pad);
  });
  const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/jpeg', 0.85));
  if (!blob) throw new Error('O navegador não conseguiu montar a foto.');
  return blob;
}

/** A foto escolhida do aparelho (o navegador sem a câmera), com a marca. */
export async function stampFile(file: File, lines: string[]): Promise<Blob> {
  if (typeof createImageBitmap === 'undefined') return file;
  try {
    const bitmap = await createImageBitmap(file);
    try {
      return await stampedJpeg(bitmap, bitmap.width, bitmap.height, lines);
    } finally {
      bitmap.close();
    }
  } catch {
    return file;
  }
}

/** O que dizer quando a câmera não abre. */
export function cameraProblem(error: unknown): string {
  const name = error instanceof DOMException ? error.name : '';
  if (name === 'NotAllowedError' || name === 'SecurityError') {
    return 'A câmera está bloqueada para este site. Permita a câmera nas configurações do navegador e tente de novo.';
  }
  if (name === 'NotFoundError' || name === 'OverconstrainedError') return 'Não achamos uma câmera neste aparelho.';
  if (name === 'NotReadableError') return 'A câmera está sendo usada por outro aplicativo. Feche-o e tente de novo.';
  return 'Este navegador não abre a câmera por aqui. Abra o link no Chrome ou no Safari do celular.';
}

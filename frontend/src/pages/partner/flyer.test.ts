import { describe, expect, it } from 'vitest';

import { FLYER_FORMATS, flyerFileName, flyerLayout, flyerOffer, jpegToPdf } from './flyer';
import type { FlyerFormat } from './flyer';

const ascii = (bytes: Uint8Array) => Array.from(bytes, (b) => String.fromCharCode(b)).join('');

describe('flyer do afiliado', () => {
  it('os blocos cabem em cada formato, em ordem e sem encostar na borda', () => {
    for (const format of Object.keys(FLYER_FORMATS) as FlyerFormat[]) {
      const layout = flyerLayout(format);
      expect(layout.scale).toBeGreaterThan(0.85);
      expect(layout.scale).toBeLessThanOrEqual(1);
      const first = layout.blocks[0];
      const last = layout.blocks[layout.blocks.length - 1];
      expect(first.y).toBeGreaterThan(40);
      expect(last.y + last.height).toBeLessThan(layout.height - 40);
      layout.blocks.slice(1).forEach((b, i) => expect(b.y).toBeGreaterThan(layout.blocks[i].y + layout.blocks[i].height));
      // O QR sempre vai, e o logo abre.
      expect(first.block).toBe('logo');
      expect(layout.blocks.some((b) => b.block === 'qr')).toBe(true);
    }
    // A5 na proporção da folha (148 × 210 mm) a 300 dpi.
    expect(FLYER_FORMATS.a5.width / FLYER_FORMATS.a5.height).toBeCloseTo(148 / 210, 2);
  });

  it('a oferta segue a landing: no pré-lançamento, a promoção', () => {
    const launch = flyerOffer(true);
    expect(launch.tag).toBe('PREÇO DE PRÉ-LANÇAMENTO');
    expect(launch.monthlyCents).toBe(3490);
    expect(launch.equipment.map((s) => s.text).join('')).toBe('Rastreador R$ 150 R$ 120 ou 10x de R$ 12,00 sem juros no Pix');
    expect(launch.fine).toContain('500 primeiros');
    const regular = flyerOffer(false);
    expect(regular.monthlyCents).toBe(6990);
    expect(regular.equipment.map((s) => s.text).join('')).toBe('Rastreador R$ 150 ou 10x de R$ 15,00 sem juros no Pix');
  });

  it('o nome do arquivo', () => {
    expect(flyerFileName('joao-moto', 'a5', 'pdf')).toBe('flyer-farbo-joao-moto-a5.pdf');
    expect(flyerFileName('joao-moto', 'stories', 'png')).toBe('flyer-farbo-joao-moto-stories.png');
  });

  it('o PDF tem uma página A5 com a imagem e a tabela de posições certa', () => {
    const jpeg = new Uint8Array([0xff, 0xd8, 0xff, 0xe0, 1, 2, 3, 0xff, 0xd9]);
    const pdf = jpegToPdf(jpeg, 1748, 2480, 419.53, 595.28);
    const text = ascii(pdf);
    expect(text.startsWith('%PDF-1.4\n')).toBe(true);
    expect(text.trimEnd().endsWith('%%EOF')).toBe(true);
    expect(text).toContain('/MediaBox [0 0 419.53 595.28]');
    expect(text).toContain('/Width 1748 /Height 2480');
    expect(text).toContain(`/Filter /DCTDecode /Length ${jpeg.length}`);
    // Os bytes da imagem vão inteiros dentro do stream.
    const start = text.indexOf('stream\n', text.indexOf('/DCTDecode')) + 'stream\n'.length;
    expect(Array.from(pdf.slice(start, start + jpeg.length))).toEqual(Array.from(jpeg));
    // Cada entrada do xref aponta para o começo do objeto, e o startxref para o xref.
    const startxref = Number(/startxref\n(\d+)/.exec(text)?.[1]);
    expect(text.slice(startxref, startxref + 4)).toBe('xref');
    const entries = text.slice(startxref).split('\n').slice(3, 9);
    entries.forEach((line, i) => {
      const offset = Number(line.slice(0, 10));
      expect(text.slice(offset, offset + `${i + 1} 0 obj`.length)).toBe(`${i + 1} 0 obj`);
    });
  });
});

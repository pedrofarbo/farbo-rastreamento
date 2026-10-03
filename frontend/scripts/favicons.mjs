// Gera o favicon do site (landing e painel) a partir da marca, no mesmo
// desenho do ícone do app: a logo sobre o fundo escuro, aqui com os cantos
// arredondados para a aba do navegador. Rode de novo se a logo mudar:
// node scripts/favicons.mjs
import { writeFileSync } from 'node:fs';

import sharp from 'sharp';

const SOURCE = 'public/assets/logo-mark.png';
const BACKGROUND = '#060907'; // o fundo da marca

/** A logo ocupando `ratio` do quadrado, sobre o fundo, com cantos `radius`. */
async function icon(size, { ratio = 0.78, radius = 0.22 } = {}) {
  const inner = Math.round(size * ratio);
  const logo = await sharp(SOURCE).resize(inner, inner, { fit: 'inside' }).toBuffer();
  const r = Math.round(size * radius);
  const square = Buffer.from(
    `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}">` +
      `<rect width="${size}" height="${size}" rx="${r}" ry="${r}" fill="${BACKGROUND}"/></svg>`,
  );
  return sharp(square).composite([{ input: logo, gravity: 'center' }]).png({ compressionLevel: 9 }).toBuffer();
}

/** .ico com PNGs dentro (aceito por todos os navegadores atuais). */
function ico(images) {
  const header = Buffer.alloc(6 + 16 * images.length);
  header.writeUInt16LE(0, 0); // reservado
  header.writeUInt16LE(1, 2); // tipo: ícone
  header.writeUInt16LE(images.length, 4);
  let offset = header.length;
  images.forEach(({ size, data }, i) => {
    const entry = 6 + 16 * i;
    header.writeUInt8(size >= 256 ? 0 : size, entry); // largura
    header.writeUInt8(size >= 256 ? 0 : size, entry + 1); // altura
    header.writeUInt8(0, entry + 2); // paleta
    header.writeUInt8(0, entry + 3); // reservado
    header.writeUInt16LE(1, entry + 4); // planos
    header.writeUInt16LE(32, entry + 6); // bits por pixel
    header.writeUInt32LE(data.length, entry + 8);
    header.writeUInt32LE(offset, entry + 12);
    offset += data.length;
  });
  return Buffer.concat([header, ...images.map((img) => img.data)]);
}

const sizes = [16, 32, 48];
// Em 16 px os detalhes somem: a logo ocupa mais do quadrado.
const images = await Promise.all(sizes.map(async (size) => ({ size, data: await icon(size, { ratio: size <= 16 ? 0.9 : 0.8 }) })));
writeFileSync('public/favicon.ico', ico(images));
writeFileSync('public/favicon-32.png', images.find((img) => img.size === 32).data);
// iPhone (favorito na Tela de Início): quadrado sem transparência, o iOS
// arredonda sozinho.
await sharp(await icon(180, { ratio: 0.7, radius: 0 })).toFile('public/apple-touch-icon.png');

console.log('favicon gerado: public/favicon.ico, public/favicon-32.png, public/apple-touch-icon.png');

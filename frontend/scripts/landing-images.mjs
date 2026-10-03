// Gera as imagens da landing a partir das originais em assets-src/ (fora do
// site). Rode de novo se trocar alguma: node scripts/landing-images.mjs
import sharp from 'sharp';

const SRC = 'assets-src';
const OUT = 'public/assets';

// Rastreador J16 com a marca: a original já vem sem fundo. Aqui só saem as
// margens transparentes, e fica com 300 px de largura — o dobro do que o
// cartão do equipamento mostra (150 px), para telas de alta densidade.
const trackerInfo = await sharp(`${SRC}/j16-rastreador-farbo.webp`)
  .trim()
  .resize({ width: 300 })
  .webp({ quality: 85, alphaQuality: 90, effort: 6 })
  .toFile(`${OUT}/rastreador-j16-farbo.webp`);

// Escudo do Insanos MC (já vem sem fundo).
const shieldInfo = await sharp(`${SRC}/escudo-insanos.png`)
  .trim()
  .resize({ height: 176 })
  .webp({ quality: 85, alphaQuality: 90, effort: 6 })
  .toFile(`${OUT}/insanos-escudo.webp`);

// Logo da Anatel para o selo de homologação: recortada do exemplo de selo que
// a Anatel publica (gov.br/anatel/pt-br/assuntos/celular-legal/selo-anatel),
// só o símbolo e o "ANATEL" — sem a linha "Agência Nacional de
// Telecomunicações" nem o número de exemplo. Fundo branco: vai num selo
// branco, como na etiqueta do produto.
const anatelInfo = await sharp(`${SRC}/anatel-selo-exemplo.png`)
  .extract({ left: 44, top: 9, width: 172, height: 46 })
  .trim({ background: '#ffffff', threshold: 20 })
  .png({ compressionLevel: 9 })
  .toFile(`${OUT}/anatel-logo.png`);

console.log(`rastreador-j16-farbo.webp ${trackerInfo.width}x${trackerInfo.height} (${trackerInfo.size} bytes)`);
console.log(`anatel-logo.png ${anatelInfo.width}x${anatelInfo.height} (${anatelInfo.size} bytes)`);
console.log(`insanos-escudo.webp ${shieldInfo.width}x${shieldInfo.height} (${shieldInfo.size} bytes)`);

import { createWorker, OEM, PSM } from 'tesseract.js';
import type { Worker } from 'tesseract.js';
import coreUrl from 'tesseract.js-core/tesseract-core-lstm.wasm.js?url';
import coreSimdUrl from 'tesseract.js-core/tesseract-core-simd-lstm.wasm.js?url';
import workerUrl from 'tesseract.js/dist/worker.min.js?url';
import { prepareZXingModule, readBarcodes } from 'zxing-wasm/reader';
import zxingWasmUrl from 'zxing-wasm/reader/zxing_reader.wasm?url';

/*
 * As leituras da etiqueta pela câmera. Este módulo só é carregado quando o
 * leitor abre: o de código de barras tem ~1 MB e o de texto (OCR) ~7 MB. Tudo
 * vem do próprio site, sem CDN (a CSP só deixa buscar no próprio domínio), e
 * roda em WebAssembly (por isso o 'wasm-unsafe-eval' na CSP).
 */

prepareZXingModule({
  overrides: { locateFile: (path: string, prefix: string) => (path.endsWith('.wasm') ? zxingWasmUrl : prefix + path) },
});

// Os formatos das etiquetas de rastreador e de chip. Fora ficam os de
// produto (EAN, UPC), que não carregam IMEI nem ICCID e só atrasam a busca.
const FORMATS = ['Code128', 'Code39', 'Code93', 'ITF', 'Codabar', 'DataMatrix', 'QRCode', 'PDF417', 'Aztec'] as const;

/** O conteúdo de cada código de barras ou QR code que aparece na imagem. */
export async function readBarcodeTexts(image: ImageData): Promise<string[]> {
  const results = await readBarcodes(image, { formats: [...FORMATS] });
  return results.filter((r) => r.isValid).map((r) => r.text);
}

// Um módulo WebAssembly mínimo com instrução SIMD (o teste do
// wasm-feature-detect): com SIMD, o OCR roda umas duas vezes mais rápido.
const SIMD_PROBE = new Uint8Array([0, 97, 115, 109, 1, 0, 0, 0, 1, 5, 1, 96, 0, 1, 123, 3, 2, 1, 0, 10, 10, 1, 8, 0, 65, 0, 253, 15, 253, 98, 11]);

// Onde o build põe o modelo de inglês do OCR (plugin ocrLanguage do
// vite.config.ts). O inglês basta: o que interessa são os dígitos.
const OCR_LANG_PATH = '/assets/ocr/4.0.0_best_int';

let textWorker: Promise<Worker> | null = null;

function startTextWorker(): Promise<Worker> {
  if (textWorker) return textWorker;
  const starting = (async () => {
    const worker = await createWorker('eng', OEM.LSTM_ONLY, {
      workerPath: workerUrl,
      langPath: OCR_LANG_PATH,
      // O padrão cria o worker a partir de um blob:, que a CSP recusa.
      workerBlobURL: false,
      corePath: WebAssembly.validate(SIMD_PROBE) ? coreSimdUrl : coreUrl,
      // O navegador já guarda os arquivos (nomes com hash, cache longo).
      cacheMethod: 'none',
    });
    // Etiqueta não é página: o texto vem solto, em blocos espalhados. Quadro
    // de câmera não traz DPI: sem um valor, o OCR estima e avisa no console.
    await worker.setParameters({ tessedit_pageseg_mode: PSM.SPARSE_TEXT, user_defined_dpi: '300' });
    return worker;
  })();
  textWorker = starting;
  // Se falhou (rede, por exemplo), a próxima leitura tenta de novo.
  starting.catch(() => {
    if (textWorker === starting) textWorker = null;
  });
  return starting;
}

/** O texto que o OCR acha na imagem. */
export async function readText(image: HTMLCanvasElement): Promise<string> {
  const worker = await startTextWorker();
  const { data } = await worker.recognize(image);
  return data.text;
}

/** Encerra o OCR (libera a memória dele) até a próxima leitura. */
export async function stopTextReader(): Promise<void> {
  const current = textWorker;
  textWorker = null;
  const worker = await current?.catch(() => null);
  await worker?.terminate();
}

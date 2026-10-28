import { iccidProblem } from './chipImport';
import { luhnValid } from './imei';

/** O que a câmera procura na etiqueta: o IMEI do rastreador ou o ICCID do chip. */
export type ScanKind = 'imei' | 'iccid';

/** De onde veio o número: do código de barras ou do texto impresso. */
export type ScanSource = 'barcode' | 'text';

export const SCAN_LABEL: Record<ScanKind, string> = { imei: 'IMEI', iccid: 'ICCID' };

// Letras que o OCR confunde com número, trocadas só quando estão entre dois
// dígitos ("86OO2" é "86002"; o "IMEI" do rótulo fica como está).
const LOOKALIKE: Record<string, string> = {
  O: '0', o: '0', Q: '0', D: '0', I: '1', l: '1', '|': '1', '!': '1', S: '5', s: '5', B: '8', Z: '2', z: '2',
};

function valid(digits: string, kind: ScanKind): boolean {
  if (kind === 'imei') return digits.length === 15 && luhnValid(digits);
  return iccidProblem(digits).problem === '';
}

/**
 * Os IMEIs (15 dígitos com o verificador certo) ou ICCIDs (19 ou 20, começando
 * com 89, verificador certo) que aparecem num texto: o conteúdo de um código de
 * barras ou o que o OCR leu da etiqueta. O número pode vir em grupos ("8955
 * 3202 1000…") ou colado em outro ("861234567890123 2024"): vale qualquer
 * sequência de grupos vizinhos da mesma linha que forme um número válido. Sem
 * repetidos, na ordem em que aparecem.
 */
export function findCodes(text: string, kind: ScanKind): string[] {
  const found: string[] = [];
  const fixed = text.replace(/(?<=\d)[OoQDIl|!SsBZz]+(?=\d)/g, (run) => [...run].map((c) => LOOKALIKE[c]).join(''));
  for (const line of fixed.split(/\r?\n/)) {
    // Grupos de dígitos separados por um espaço, hífen ou ponto formam uma
    // corrente; qualquer outra coisa entre eles começa outra.
    for (const chain of line.match(/\d+(?:[ .-]\d+)*/g) ?? []) {
      const groups = chain.split(/[ .-]/);
      for (let i = 0; i < groups.length; i++) {
        let digits = '';
        for (let j = i; j < groups.length && digits.length < 20; j++) {
          digits += groups[j];
          if (valid(digits, kind) && !found.includes(digits)) found.push(digits);
        }
      }
    }
  }
  return found;
}

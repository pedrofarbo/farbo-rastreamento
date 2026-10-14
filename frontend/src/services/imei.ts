/**
 * O que está errado num IMEI de rastreador (a mesma regra do servidor): 16
 * dígitos (o IMEI tem 15) ou 15 com o dígito verificador que não bate. Com o
 * IMEI errado, o rastreador pega a configuração por SMS, conecta e é recusado
 * — e os SMS se perdem. Vazio se está certo, ou se não tem cara de IMEI (o
 * H02, por exemplo, usa um código de 10 dígitos).
 */
export function imeiProblem(imei: string): string {
  const value = imei.trim();
  if (!/^\d+$/.test(value)) return '';
  if (value.length === 16) return 'tem 16 dígitos, e o IMEI tem 15 (provavelmente sobrou um número)';
  if (value.length === 15 && !luhnValid(value)) {
    return 'não confere: o último dígito (o verificador) não bate com os outros 14 (provavelmente faltou, sobrou ou trocou um número)';
  }
  return '';
}

/** O dígito verificador (Luhn), o último do IMEI. */
export function luhnValid(digits: string): boolean {
  let sum = 0;
  for (let i = 0; i < digits.length; i++) {
    let d = Number(digits[digits.length - 1 - i]);
    if (i % 2 === 1) {
      d *= 2;
      if (d > 9) d -= 9;
    }
    sum += d;
  }
  return sum % 10 === 0;
}

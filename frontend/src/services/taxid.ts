/**
 * CPF e CNPJ: só os números, a máscara enquanto a pessoa digita e os dígitos
 * verificadores (a mesma conta do servidor).
 */

export const taxIdDigits = (value: string) => value.replace(/\D/g, '');

/** 52998224725 → 529.982.247-25; com mais de 11 dígitos, a máscara do CNPJ. */
export function formatTaxId(value: string): string {
  const d = taxIdDigits(value).slice(0, 14);
  if (d.length <= 11) {
    return d
      .replace(/^(\d{3})(\d)/, '$1.$2')
      .replace(/^(\d{3})\.(\d{3})(\d)/, '$1.$2.$3')
      .replace(/^(\d{3})\.(\d{3})\.(\d{3})(\d)/, '$1.$2.$3-$4');
  }
  return d
    .replace(/^(\d{2})(\d)/, '$1.$2')
    .replace(/^(\d{2})\.(\d{3})(\d)/, '$1.$2.$3')
    .replace(/^(\d{2})\.(\d{3})\.(\d{3})(\d)/, '$1.$2.$3/$4')
    .replace(/^(\d{2})\.(\d{3})\.(\d{3})\/(\d{4})(\d)/, '$1.$2.$3/$4-$5');
}

const allSame = (d: string) => d.split('').every((c) => c === d[0]);

function validCPF(d: string): boolean {
  if (d.length !== 11 || allSame(d)) return false;
  for (const size of [9, 10]) {
    let sum = 0;
    for (let i = 0; i < size; i++) sum += Number(d[i]) * (size + 1 - i);
    const check = ((sum * 10) % 11) % 10;
    if (check !== Number(d[size])) return false;
  }
  return true;
}

function validCNPJ(d: string): boolean {
  if (d.length !== 14 || allSame(d)) return false;
  const weights = [6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2];
  for (const size of [12, 13]) {
    let sum = 0;
    for (let i = 0; i < size; i++) sum += Number(d[i]) * weights[weights.length - size + i];
    const rest = sum % 11;
    if ((rest < 2 ? 0 : 11 - rest) !== Number(d[size])) return false;
  }
  return true;
}

/** CPF ou CNPJ com os dígitos verificadores corretos. */
export function validTaxId(value: string): boolean {
  const d = taxIdDigits(value);
  return d.length === 11 ? validCPF(d) : d.length === 14 ? validCNPJ(d) : false;
}

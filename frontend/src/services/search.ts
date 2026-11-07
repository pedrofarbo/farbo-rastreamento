import type { Chip, Device } from '@/types';

/**
 * O texto como a busca compara: sem acento, minúsculo e sem pontuação nem
 * espaço. Assim "(34) 99999-0000", "8955 3202…" e "abc-1d23" casam do jeito
 * que forem digitados ou lidos da etiqueta.
 */
export function compactSearch(text: string | null | undefined): string {
  return (text ?? '')
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]/g, '');
}

/**
 * Se o que foi digitado aparece, inteiro, em algum dos campos (sem a
 * pontuação nem os espaços: "chip 1" acha "Chip 10", mas não o "1" de um
 * ICCID). Em branco, tudo aparece.
 */
export function matchesSearch(term: string, fields: (string | null | undefined)[]): boolean {
  const t = compactSearch(term);
  return !t || fields.some((f) => compactSearch(f).includes(t));
}

/** Se o digitado é, inteiro, um destes códigos (o IMEI, o ICCID). */
export function isExactCode(term: string, codes: (string | null | undefined)[]): boolean {
  const t = compactSearch(term);
  return t.length >= 14 && codes.some((c) => compactSearch(c) === t);
}

/** Onde a busca procura o rastreador: o IMEI, o chip, o modelo e as notas. */
export function deviceSearchFields(d: Pick<Device, 'imei' | 'iccid' | 'phoneNumber' | 'model' | 'manufacturer' | 'protocol' | 'notes'>) {
  return [d.imei, d.iccid, d.phoneNumber, d.model, d.manufacturer, d.protocol, d.notes];
}

/** Onde a busca procura o chip: o ICCID, a linha, a operadora, o APN e o IMEI de quem está com ele. */
export function chipSearchFields(c: Pick<Chip, 'iccid' | 'phoneNumber' | 'carrier' | 'apn' | 'notes' | 'device'>) {
  return [c.iccid, c.phoneNumber, c.carrier, c.apn, c.notes, c.device?.imei];
}

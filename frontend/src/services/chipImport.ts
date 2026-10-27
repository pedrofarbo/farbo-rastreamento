import type { ChipInput } from '@/api/resources';
import { normalizeHeader, parseCsv } from '@/services/fleetImport';
import { formatPhoneInput } from '@/services/format';
import { luhnValid } from '@/services/imei';

/** O que vale para todos os chips do lote quando a linha deixa em branco. */
export interface ChipDefaults {
  carrier: string;
  apn: string;
  apnUser: string;
  apnPassword: string;
  notes: string;
}

export const EMPTY_CHIP_DEFAULTS: ChipDefaults = { carrier: '', apn: '', apnUser: '', apnPassword: '', notes: '' };

export interface ParsedChipRow {
  lineNumber: number;
  iccid: string;
  phoneNumber: string;
  carrier: string;
  apn: string;
  status: 'valid' | 'duplicate' | 'error';
  message: string;
  input: ChipInput;
}

export interface ParsedChipBatch {
  rows: ParsedChipRow[];
  validCount: number;
  errorCount: number;
  duplicateCount: number;
}

type Column = 'iccid' | 'phoneNumber' | 'carrier' | 'apn' | 'apnUser' | 'apnPassword' | 'notes';

const HEADERS: Record<Column, string[]> = {
  iccid: ['iccid', 'iccid do chip', 'simcard', 'sim card', 'chip'],
  phoneNumber: ['linha', 'telefone', 'numero', 'linha do chip', 'celular', 'msisdn', 'phone'],
  carrier: ['operadora', 'carrier'],
  apn: ['apn'],
  apnUser: ['usuario', 'usuario apn', 'apn usuario', 'user'],
  apnPassword: ['senha', 'senha apn', 'apn senha', 'password'],
  notes: ['observacoes', 'observacao', 'obs', 'lote', 'notas'],
};
// Sem cabeçalho, as colunas vão nesta ordem (um ICCID por linha também vale).
const ORDER: Column[] = ['iccid', 'phoneNumber', 'carrier', 'apn', 'apnUser', 'apnPassword', 'notes'];

/** Modelo de planilha para baixar, com o cabeçalho e dois exemplos. */
export function templateChipCsv(): string {
  const rows = [
    ['ICCID', 'Linha', 'Operadora', 'APN', 'Usuário', 'Senha', 'Observações'],
    ['89553202100093795330', '(34) 99999-0001', 'Algar', 'ss.algar.br', 'algar', 'algar', 'Lote 12'],
    ['89553202100093795348', '', '', '', '', '', ''],
  ];
  return '﻿' + rows.map((r) => r.join(';')).join('\r\n') + '\r\n';
}

/**
 * O ICCID só com os números (o "SP" da etiqueta e os espaços saem) e o que há
 * de errado nele: 19 ou 20 dígitos, começando com 89, com o último dígito
 * (o verificador) batendo — a mesma conferência do servidor.
 */
export function iccidProblem(raw: string): { iccid: string; problem: string } {
  const iccid = raw.trim().replace(/[a-z]{1,3}$/i, '').replace(/\D/g, '');
  if (!iccid) return { iccid, problem: 'ICCID em branco' };
  if (iccid.length !== 19 && iccid.length !== 20) return { iccid, problem: `ICCID tem 19 ou 20 dígitos (este tem ${iccid.length})` };
  if (!iccid.startsWith('89')) return { iccid, problem: 'ICCID começa com 89' };
  if (!luhnValid(iccid)) return { iccid, problem: 'O último dígito do ICCID não confere: confira na etiqueta' };
  return { iccid, problem: '' };
}

/**
 * Lê a planilha (ou o texto colado) do lote de chips. O que a linha deixa em
 * branco vem de defaults; repetidos na lista ou já no estoque não entram.
 */
export function parseChipBatch(text: string, existingIccids: string[] = [], defaults: ChipDefaults = EMPTY_CHIP_DEFAULTS): ParsedChipBatch {
  const out: ParsedChipBatch = { rows: [], validCount: 0, errorCount: 0, duplicateCount: 0 };
  const records = text.trim() ? parseCsv(text.trim()) : [];
  if (records.length === 0) return out;

  const columns: Partial<Record<Column, number>> = {};
  records[0].forEach((cell, i) => {
    const name = normalizeHeader(cell);
    for (const [key, aliases] of Object.entries(HEADERS) as [Column, string[]][]) {
      if (aliases.includes(name) && columns[key] === undefined) columns[key] = i;
    }
  });
  const hasHeader = columns.iccid !== undefined;
  if (!hasHeader) ORDER.forEach((key, i) => (columns[key] = i));

  const inStock = new Set(existingIccids);
  const seen = new Set<string>();
  const data = hasHeader ? records.slice(1) : records;
  data.forEach((record, r) => {
    if (record.every((cell) => cell.trim() === '')) return;
    const cell = (key: Column) => (columns[key] !== undefined ? (record[columns[key]!] ?? '').trim() : '');
    const { iccid, problem } = iccidProblem(cell('iccid'));
    const phone = cell('phoneNumber');
    const input: ChipInput = {
      iccid,
      phoneNumber: phone ? formatPhoneInput(phone) : '',
      carrier: cell('carrier') || defaults.carrier.trim(),
      apn: cell('apn') || defaults.apn.trim(),
      apnUser: cell('apnUser') || defaults.apnUser.trim(),
      apnPassword: cell('apnPassword') || defaults.apnPassword,
      notes: cell('notes') || defaults.notes.trim(),
    };
    let status: ParsedChipRow['status'] = 'valid';
    let message = problem;
    if (problem) {
      status = 'error';
    } else if (/[,#]/.test(input.apn + input.apnUser + input.apnPassword)) {
      status = 'error';
      message = 'O APN, o usuário e a senha não podem ter vírgula nem #';
    } else if (seen.has(iccid)) {
      status = 'duplicate';
      message = 'ICCID repetido nesta lista';
    } else if (inStock.has(iccid)) {
      status = 'duplicate';
      message = 'Já está no estoque';
    }
    if (status === 'valid') seen.add(iccid);
    out[status === 'valid' ? 'validCount' : status === 'duplicate' ? 'duplicateCount' : 'errorCount']++;
    out.rows.push({
      lineNumber: (hasHeader ? 2 : 1) + r,
      iccid,
      phoneNumber: input.phoneNumber,
      carrier: input.carrier,
      apn: input.apn,
      status,
      message,
      input,
    });
  });
  return out;
}

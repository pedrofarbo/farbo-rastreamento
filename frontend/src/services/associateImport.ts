import type { VehicleInput } from '@/api/resources';
import type { VehicleKind } from '@/types';

import { normalizeHeader, parseCsv } from './fleetImport';
import type { SheetProblem } from './fleetImport';

/** O máximo de linhas de uma planilha (o mesmo limite do servidor). */
export const MAX_ASSOCIATE_ROWS = 500;

/** Uma linha da planilha, como a API recebe. */
export interface AssociateImportRow {
  /** A linha na planilha (as mensagens do servidor citam). */
  line: number;
  name: string;
  email: string;
  phone: string;
  document: string;
  vehicle?: VehicleInput;
}

export interface ParsedAssociates {
  rows: AssociateImportRow[];
  problems: SheetProblem[];
  ignored: string[];
  /** Quantos associados (e-mails diferentes) e veículos. */
  associates: number;
  vehicles: number;
}

type ColumnKey = 'name' | 'email' | 'phone' | 'document' | 'vehicle' | 'plate' | 'kind' | 'brand' | 'model' | 'year' | 'color';

/** Os nomes aceitos de cada coluna (o primeiro é o do modelo). */
const COLUMNS: Record<ColumnKey, string[]> = {
  name: ['Nome', 'Associado', 'Nome do associado', 'Segurado', 'Nome do segurado'],
  email: ['E-mail', 'Email'],
  phone: ['Telefone', 'Celular', 'WhatsApp'],
  document: ['CPF', 'CPF ou CNPJ', 'CNPJ', 'Documento'],
  vehicle: ['Veículo', 'Nome do veículo'],
  plate: ['Placa'],
  kind: ['Tipo'],
  brand: ['Marca'],
  model: ['Modelo'],
  year: ['Ano'],
  color: ['Cor'],
};

const KINDS: Record<string, VehicleKind> = {
  carro: 'CAR',
  car: 'CAR',
  automovel: 'CAR',
  moto: 'MOTORCYCLE',
  motocicleta: 'MOTORCYCLE',
};

/** O modelo para baixar: só o cabeçalho, com ";" e BOM (o Excel em português abre direto). */
export function associateTemplateCsv(): string {
  const order: ColumnKey[] = ['name', 'email', 'phone', 'document', 'vehicle', 'plate', 'kind', 'brand', 'model', 'year', 'color'];
  return '﻿' + order.map((key) => COLUMNS[key][0]).join(';') + '\r\n';
}

/**
 * Lê a planilha de associados: uma linha por veículo (ou por associado, sem
 * veículo); as linhas com o mesmo e-mail são o mesmo associado. Nome e
 * e-mail são obrigatórios. O CPF, as placas que já existem e os e-mails já
 * cadastrados o servidor confere.
 */
export function parseAssociateSheet(text: string): ParsedAssociates {
  const records = parseCsv(text);
  const result: ParsedAssociates = { rows: [], problems: [], ignored: [], associates: 0, vehicles: 0 };
  const headerIndex = records.findIndex((r) => r.some((cell) => cell.trim() !== ''));
  if (headerIndex < 0) {
    result.problems.push({ line: 0, message: 'A planilha está vazia.' });
    return result;
  }
  const lookup = new Map<string, ColumnKey>();
  for (const key of Object.keys(COLUMNS) as ColumnKey[]) {
    for (const name of COLUMNS[key]) {
      const normalized = normalizeHeader(name);
      if (!lookup.has(normalized)) lookup.set(normalized, key);
    }
  }
  const columns = new Map<ColumnKey, number>();
  records[headerIndex].forEach((header, index) => {
    const key = lookup.get(normalizeHeader(header));
    if (key && !columns.has(key)) columns.set(key, index);
    else if (header.trim()) result.ignored.push(header.trim());
  });
  for (const [key, label] of [
    ['name', 'Nome'],
    ['email', 'E-mail'],
  ] as const) {
    if (!columns.has(key)) {
      result.problems.push({ line: headerIndex + 1, message: `Falta a coluna "${label}" no cabeçalho (a primeira linha).` });
    }
  }
  if (result.problems.length > 0) return result;

  const emails = new Set<string>();
  const maxYear = new Date().getFullYear() + 1;
  for (let i = headerIndex + 1; i < records.length; i++) {
    const record = records[i];
    if (record.every((cell) => cell.trim() === '')) continue;
    const line = i + 1;
    const cell = (key: ColumnKey) => {
      const index = columns.get(key);
      return index === undefined ? '' : (record[index] ?? '').trim();
    };
    const problem = (message: string) => result.problems.push({ line, message });

    const row: AssociateImportRow = {
      line,
      name: cell('name'),
      email: cell('email').toLowerCase(),
      phone: cell('phone'),
      document: cell('document'),
    };
    if (!row.name) problem('Falta o nome do associado.');
    if (!/^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/.test(row.email)) problem(`E-mail "${cell('email')}" inválido.`);

    const vehicleName = cell('vehicle');
    const plate = cell('plate').toUpperCase();
    if (vehicleName || plate) {
      const vehicle: VehicleInput = { name: vehicleName || plate };
      if (plate) vehicle.plate = plate;
      const kind = cell('kind');
      if (kind) {
        const mapped = KINDS[normalizeHeader(kind)];
        if (mapped) vehicle.kind = mapped;
        else problem(`Tipo "${kind}": use carro ou moto.`);
      }
      for (const key of ['brand', 'model', 'color'] as const) {
        const value = cell(key);
        if (value) vehicle[key] = value;
      }
      const year = cell('year');
      if (year) {
        const n = Number(year);
        if (Number.isInteger(n) && n >= 1900 && n <= maxYear) vehicle.year = n;
        else problem(`Ano "${year}": use de 1900 a ${maxYear}.`);
      }
      row.vehicle = vehicle;
      result.vehicles++;
    }
    if (row.email) emails.add(row.email);
    result.rows.push(row);
  }
  result.associates = emails.size;

  if (result.rows.length === 0 && result.problems.length === 0) {
    result.problems.push({ line: 0, message: 'A planilha só tem o cabeçalho: nenhum associado para importar.' });
  }
  if (result.rows.length > MAX_ASSOCIATE_ROWS) {
    result.problems.push({
      line: 0,
      message: `A planilha tem ${result.rows.length} linhas; importe no máximo ${MAX_ASSOCIATE_ROWS} por vez.`,
    });
  }
  return result;
}

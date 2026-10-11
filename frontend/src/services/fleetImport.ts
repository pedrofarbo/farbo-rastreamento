import type { VehicleInput } from '@/api/resources';
import type { FleetSettings, VehicleKind } from '@/types';

/** O máximo de veículos de uma planilha (o mesmo limite do servidor). */
export const MAX_IMPORT_ROWS = 1000;

/** Uma linha da planilha, como a API recebe. */
export interface FleetImportRow {
  vehicle: VehicleInput;
  /** O nome do grupo; o servidor cria os que a conta ainda não tem. */
  group: string;
  /** O IMEI do rastreador já com a central (vazio: enviar ao cliente). */
  imei: string;
}

export interface SheetProblem {
  /** A linha na planilha (1 é o cabeçalho); 0 vale para a planilha inteira. */
  line: number;
  message: string;
}

export interface ParsedSheet {
  rows: FleetImportRow[];
  /** A linha da planilha de cada veículo (para mostrar os problemas). */
  lines: number[];
  problems: SheetProblem[];
  /** Os cabeçalhos que não viraram coluna nenhuma (ignorados). */
  ignored: string[];
}

type ColumnKey =
  | 'name'
  | 'plate'
  | 'kind'
  | 'brand'
  | 'model'
  | 'year'
  | 'color'
  | 'group'
  | 'custom1'
  | 'custom2'
  | 'imei'
  | 'speedLimit';

/** Tira acento, caixa e pontuação: "Nº do Contrato" e "n do contrato" são a mesma coluna. */
export function normalizeHeader(text: string): string {
  return text
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[º°ª]/g, '')
    .replace(/[^a-z0-9]+/g, ' ')
    .trim();
}

/** Os nomes aceitos de cada coluna (o primeiro é o do modelo). */
function columnNames(fleet?: FleetSettings): Record<ColumnKey, string[]> {
  const label1 = fleet?.labels.label1.trim();
  const label2 = fleet?.labels.label2.trim();
  return {
    name: ['Nome', 'Veículo', 'Nome do veículo', 'Identificação'],
    plate: ['Placa'],
    kind: ['Tipo'],
    brand: ['Marca'],
    model: ['Modelo'],
    year: ['Ano'],
    color: ['Cor'],
    group: ['Grupo'],
    custom1: [label1 || 'Campo personalizado 1', 'Campo personalizado 1', 'Campo 1'],
    custom2: [label2 || 'Campo personalizado 2', 'Campo personalizado 2', 'Campo 2'],
    imei: ['IMEI do rastreador', 'IMEI', 'Rastreador'],
    speedLimit: ['Limite de velocidade', 'Limite', 'Velocidade máxima'],
  };
}

/** O modelo para baixar: só o cabeçalho, com ";" e BOM (o Excel em português abre direto). */
export function templateCsv(fleet?: FleetSettings): string {
  const names = columnNames(fleet);
  const order: ColumnKey[] = ['name', 'plate', 'kind', 'brand', 'model', 'year', 'color', 'group', 'custom1', 'custom2', 'imei'];
  return '﻿' + order.map((key) => names[key][0]).join(';') + '\r\n';
}

/** O separador da planilha: o que mais aparece no cabeçalho (fora das aspas). */
function detectDelimiter(text: string): string {
  const firstLine = text.slice(0, text.search(/\r?\n|$/));
  let best = ';';
  let bestCount = 0;
  for (const candidate of [';', ',', '\t']) {
    let count = 0;
    let quoted = false;
    for (const ch of firstLine) {
      if (ch === '"') quoted = !quoted;
      else if (ch === candidate && !quoted) count++;
    }
    if (count > bestCount) {
      best = candidate;
      bestCount = count;
    }
  }
  return best;
}

/** Lê o CSV em registros (aspas, aspas dobradas e quebras de linha dentro das aspas). */
export function parseCsv(input: string): string[][] {
  const text = input.replace(/^﻿/, '');
  const delimiter = detectDelimiter(text);
  const records: string[][] = [];
  let record: string[] = [];
  let field = '';
  let quoted = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (quoted) {
      if (ch === '"' && text[i + 1] === '"') {
        field += '"';
        i++;
      } else if (ch === '"') {
        quoted = false;
      } else {
        field += ch;
      }
    } else if (ch === '"' && field === '') {
      quoted = true;
    } else if (ch === delimiter) {
      record.push(field);
      field = '';
    } else if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && text[i + 1] === '\n') i++;
      record.push(field);
      records.push(record);
      record = [];
      field = '';
    } else {
      field += ch;
    }
  }
  if (field !== '' || record.length > 0) {
    record.push(field);
    records.push(record);
  }
  return records;
}

const KINDS: Record<string, VehicleKind> = {
  carro: 'CAR',
  car: 'CAR',
  automovel: 'CAR',
  moto: 'MOTORCYCLE',
  motocicleta: 'MOTORCYCLE',
  motorcycle: 'MOTORCYCLE',
};

/**
 * Lê a planilha da frota: o cabeçalho diz qual coluna é qual (só "Nome" é
 * obrigatória), linhas em branco são puladas, e cada linha com dado errado
 * vira um problema. A conferência de placas, rastreadores e grupos é do
 * servidor.
 */
export function parseFleetSheet(text: string, fleet?: FleetSettings): ParsedSheet {
  const records = parseCsv(text);
  const result: ParsedSheet = { rows: [], lines: [], problems: [], ignored: [] };
  const headerIndex = records.findIndex((r) => r.some((cell) => cell.trim() !== ''));
  if (headerIndex < 0) {
    result.problems.push({ line: 0, message: 'A planilha está vazia.' });
    return result;
  }

  const names = columnNames(fleet);
  const lookup = new Map<string, ColumnKey>();
  for (const key of Object.keys(names) as ColumnKey[]) {
    for (const name of names[key]) {
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
  if (!columns.has('name')) {
    result.problems.push({ line: headerIndex + 1, message: 'Falta a coluna "Nome" no cabeçalho (a primeira linha).' });
    return result;
  }

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

    const vehicle: VehicleInput = { name: cell('name') };
    if (!vehicle.name) problem('Falta o nome do veículo.');
    const plate = cell('plate');
    if (plate) vehicle.plate = plate.toUpperCase();
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
    const limit = cell('speedLimit');
    if (limit) {
      const n = Number(limit.replace(',', '.'));
      if (Number.isFinite(n) && n > 0 && n <= 300) vehicle.speedLimitKmh = n;
      else problem(`Limite de velocidade "${limit}": use de 1 a 300 km/h.`);
    }
    for (const key of ['custom1', 'custom2'] as const) {
      const value = cell(key);
      if (value) vehicle[key] = value;
    }
    const imei = cell('imei').replace(/\s+/g, '');
    if (imei && !/^\d{8,20}$/.test(imei)) problem(`IMEI "${cell('imei')}": só números.`);

    result.rows.push({ vehicle, group: cell('group'), imei });
    result.lines.push(line);
  }

  if (result.rows.length === 0 && result.problems.length === 0) {
    result.problems.push({ line: 0, message: 'A planilha só tem o cabeçalho: nenhum veículo para importar.' });
  }
  if (result.rows.length > MAX_IMPORT_ROWS) {
    result.problems.push({
      line: 0,
      message: `A planilha tem ${result.rows.length} veículos; importe no máximo ${MAX_IMPORT_ROWS} por vez.`,
    });
  }
  return result;
}

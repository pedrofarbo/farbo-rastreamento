import type { BulkSmsSetupResponse, DeviceInput } from '@/api/resources';
import { normalizeHeader, parseCsv } from '@/services/fleetImport';
import { formatPhoneInput } from '@/services/format';

export interface ParsedTrackerRow {
  lineNumber: number;
  raw: string;
  imei: string;
  phoneNumber: string;
  model: string;
  iccid: string;
  protocol: string;
  status: 'valid' | 'duplicate' | 'error';
  message?: string;
  input: DeviceInput;
}

export interface ParsedTrackerBatch {
  rows: ParsedTrackerRow[];
  validCount: number;
  errorCount: number;
  duplicateCount: number;
}

type TrackerColumnKey = 'imei' | 'phoneNumber' | 'model' | 'iccid' | 'protocol';

const HEADER_MAP: Record<TrackerColumnKey, string[]> = {
  imei: ['imei', 'rastreador', 'dispositivo', 'aparelho'],
  phoneNumber: ['linha', 'telefone', 'numero', 'linha do chip', 'celular', 'chip', 'phone', 'sim'],
  model: ['modelo', 'model', 'equipamento'],
  iccid: ['iccid', 'iccid do chip', 'simcard'],
  protocol: ['protocolo', 'protocol'],
};

/** Modelo de CSV para download com cabeçalho e exemplos. */
export function templateTrackerCsv(): string {
  const headers = ['IMEI', 'Linha', 'Modelo', 'ICCID', 'Protocolo'];
  const row1 = ['869247061230001', '(34) 99999-0001', 'TK910', '89553202100093795330', ''];
  const row2 = ['869247061230002', '(34) 99999-0002', 'TK910', '', 'gt06'];
  return '\uFEFF' + [headers.join(';'), row1.join(';'), row2.join(';')].join('\r\n') + '\r\n';
}

/** Limpa caracteres do ICCID, removendo sufixo SP se houver. */
export function cleanIccid(raw: string): string {
  return raw.replace(/sp$/i, '').replace(/\D/g, '');
}

/** Limpa o IMEI mantendo apenas números. */
export function cleanImei(raw: string): string {
  return raw.replace(/\D/g, '');
}

/**
 * Lê uma planilha CSV ou texto colado com rastreadores, validando cada linha.
 */
export function parseTrackerBatch(
  text: string,
  existingDevices: { imei: string; iccid?: string }[] = [],
): ParsedTrackerBatch {
  const trimmed = text.trim();
  if (!trimmed) {
    return { rows: [], validCount: 0, errorCount: 0, duplicateCount: 0 };
  }

  const rawRecords = parseCsv(trimmed);
  if (rawRecords.length === 0) {
    return { rows: [], validCount: 0, errorCount: 0, duplicateCount: 0 };
  }

  // Detecta se a primeira linha é cabeçalho
  let hasHeader = false;
  const colMap: Partial<Record<TrackerColumnKey, number>> = {};

  const firstRow = rawRecords[0];
  const normalizedFirstRow = firstRow.map((c) => normalizeHeader(c));

  // Checa se algum termo bate com as colunas esperadas
  for (let i = 0; i < normalizedFirstRow.length; i++) {
    const val = normalizedFirstRow[i];
    for (const [key, aliases] of Object.entries(HEADER_MAP) as [TrackerColumnKey, string[]][]) {
      if (aliases.includes(val)) {
        colMap[key] = i;
        hasHeader = true;
      }
    }
  }

  // Se não teve cabeçalho nomeado, usa a ordem padrão por posição
  if (!hasHeader) {
    colMap.imei = 0;
    colMap.phoneNumber = 1;
    colMap.model = 2;
    colMap.iccid = 3;
    colMap.protocol = 4;
  }

  const dataRows = hasHeader ? rawRecords.slice(1) : rawRecords;
  const startLine = hasHeader ? 2 : 1;

  const existingImeis = new Set(existingDevices.map((d) => d.imei.trim()));
  const existingIccids = new Set(
    existingDevices.map((d) => cleanIccid(d.iccid || '')).filter(Boolean),
  );

  const seenImeisInBatch = new Set<string>();
  const seenIccidsInBatch = new Set<string>();

  const rows: ParsedTrackerRow[] = [];
  let validCount = 0;
  let errorCount = 0;
  let duplicateCount = 0;

  for (let r = 0; r < dataRows.length; r++) {
    const record = dataRows[r];
    // Se a linha for toda em branco, ignora
    if (record.every((cell) => cell.trim() === '')) {
      continue;
    }

    const lineNumber = startLine + r;
    const rawImei = colMap.imei !== undefined && record[colMap.imei] ? record[colMap.imei].trim() : '';
    const imei = cleanImei(rawImei);

    const rawPhone =
      colMap.phoneNumber !== undefined && record[colMap.phoneNumber]
        ? record[colMap.phoneNumber].trim()
        : '';
    const phoneNumber = rawPhone ? formatPhoneInput(rawPhone) : '';

    const model =
      colMap.model !== undefined && record[colMap.model] ? record[colMap.model].trim() : '';

    const rawIccid =
      colMap.iccid !== undefined && record[colMap.iccid] ? record[colMap.iccid].trim() : '';
    const iccid = rawIccid ? cleanIccid(rawIccid) : '';

    const rawProtocol =
      colMap.protocol !== undefined && record[colMap.protocol]
        ? record[colMap.protocol].trim()
        : '';
    const protocol = rawProtocol.toLowerCase();

    const input: DeviceInput = {
      imei,
      phoneNumber,
      model,
      iccid,
      protocol,
      manufacturer: 'TKSTAR',
      apn: '',
      apnUser: '',
      apnPassword: '',
      serverHost: '',
      serverPort: null,
      commandPassword: '',
    };

    let status: 'valid' | 'duplicate' | 'error' = 'valid';
    let message = '';

    if (!rawImei) {
      status = 'error';
      message = 'IMEI em branco';
    } else if (imei.length !== 15) {
      status = 'error';
      message = `IMEI deve ter 15 dígitos (encontrado: ${imei.length || rawImei.length})`;
    } else if (seenImeisInBatch.has(imei)) {
      status = 'duplicate';
      message = 'IMEI repetido nesta lista';
    } else if (existingImeis.has(imei)) {
      status = 'duplicate';
      message = 'IMEI já cadastrado no sistema';
    } else if (iccid) {
      if (iccid.length !== 19 && iccid.length !== 20) {
        status = 'error';
        message = `ICCID deve ter 19 ou 20 dígitos (encontrado: ${iccid.length})`;
      } else if (seenIccidsInBatch.has(iccid)) {
        status = 'duplicate';
        message = 'ICCID repetido nesta lista';
      } else if (existingIccids.has(iccid)) {
        status = 'duplicate';
        message = 'ICCID já cadastrado em outro rastreador';
      }
    }

    if (status === 'valid') {
      seenImeisInBatch.add(imei);
      if (iccid) seenIccidsInBatch.add(iccid);
      validCount++;
    } else if (status === 'duplicate') {
      duplicateCount++;
    } else {
      errorCount++;
    }

    rows.push({
      lineNumber,
      raw: record.join(';'),
      imei,
      phoneNumber,
      model,
      iccid,
      protocol,
      status,
      message,
      input,
    });
  }

  return { rows, validCount, errorCount, duplicateCount };
}

/**
 * Determina se o rastreador já está ativado e comunicando com o servidor
 * (online, pacote recente ou último contato registrado).
 */
export function isDeviceActivated(device: { status?: string | null; lastSeenAt?: string | null }): boolean {
  return Boolean(
    device.status === 'ONLINE' ||
    device.status === 'STALE' ||
    (device.lastSeenAt && device.lastSeenAt.trim().length > 0),
  );
}

/** Quantos rastreadores o servidor aceita por lote de ativação por SMS. */
export const SMS_BULK_LIMIT = 100;

/** Divide a lista em lotes de até `size`. */
export function chunk<T>(list: T[], size: number): T[][] {
  const out: T[][] = [];
  for (let i = 0; i < list.length; i += size) out.push(list.slice(i, i + size));
  return out;
}

/** Junta as respostas dos lotes numa só; as que falharam vêm primeiro (são as que pedem reenvio). */
export function mergeBulkResults(parts: BulkSmsSetupResponse[]): BulkSmsSetupResponse {
  const merged = parts.reduce<BulkSmsSetupResponse>(
    (all, part) => ({
      total: all.total + part.total,
      started: all.started + part.started,
      failed: all.failed + part.failed,
      results: [...all.results, ...part.results],
    }),
    { total: 0, started: 0, failed: 0, results: [] },
  );
  return {
    ...merged,
    results: [...merged.results.filter((r) => !r.success), ...merged.results.filter((r) => r.success)],
  };
}

/** Um lote que não chegou ao servidor: todos os rastreadores dele com o erro. */
export function failedBatch(
  deviceIds: string[],
  message: string,
  imeiOf: (id: string) => string | undefined,
): BulkSmsSetupResponse {
  return {
    total: deviceIds.length,
    started: 0,
    failed: deviceIds.length,
    results: deviceIds.map((deviceId) => ({ deviceId, imei: imeiOf(deviceId), success: false, error: message })),
  };
}

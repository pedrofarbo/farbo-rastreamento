import { describe, expect, it } from 'vitest';
import { cleanIccid, cleanImei, isDeviceActivated, parseTrackerBatch, templateTrackerCsv } from './deviceBulk';

describe('deviceBulk', () => {
  it('templateTrackerCsv returns CSV with BOM and headers', () => {
    const csv = templateTrackerCsv();
    expect(csv.startsWith('\uFEFF')).toBe(true);
    expect(csv).toContain('IMEI;Linha;Modelo;ICCID;Protocolo');
    expect(csv).toContain('869247061230001');
  });

  it('cleanImei strips non-digit characters', () => {
    expect(cleanImei('869247-06-1230001')).toBe('869247061230001');
    expect(cleanImei('  869247061230001  ')).toBe('869247061230001');
  });

  it('cleanIccid removes SP suffix and non-digits', () => {
    expect(cleanIccid('89553202100093795330SP')).toBe('89553202100093795330');
    expect(cleanIccid('8955 3202 1000 9379 5330')).toBe('89553202100093795330');
  });

  it('isDeviceActivated correctly identifies active vs pending devices', () => {
    expect(isDeviceActivated({ status: 'ONLINE', lastSeenAt: null })).toBe(true);
    expect(isDeviceActivated({ status: 'STALE', lastSeenAt: null })).toBe(true);
    expect(isDeviceActivated({ status: 'OFFLINE', lastSeenAt: '2026-10-10T12:00:00Z' })).toBe(true);
    expect(isDeviceActivated({ status: 'OFFLINE', lastSeenAt: null })).toBe(false);
    expect(isDeviceActivated({ status: undefined, lastSeenAt: undefined })).toBe(false);
  });

  it('parses raw text with one IMEI per line without headers', () => {
    const text = `
      869247061230001
      869247061230002
      869247061230003
    `;
    const res = parseTrackerBatch(text);
    expect(res.rows.length).toBe(3);
    expect(res.validCount).toBe(3);
    expect(res.errorCount).toBe(0);
    expect(res.duplicateCount).toBe(0);
    expect(res.rows[0].imei).toBe('869247061230001');
    expect(res.rows[0].status).toBe('valid');
  });

  it('parses CSV with headers, phone formatting and model', () => {
    const csv = `
IMEI;Linha;Modelo;ICCID;Protocolo
869247061230001;34999990001;TK910;89553202100093795330;gt06
869247061230002;1133334444;Coban;;
    `.trim();

    const res = parseTrackerBatch(csv);
    expect(res.rows.length).toBe(2);
    expect(res.validCount).toBe(2);
    expect(res.rows[0].imei).toBe('869247061230001');
    expect(res.rows[0].phoneNumber).toBe('(34) 9-9999-0001');
    expect(res.rows[0].model).toBe('TK910');
    expect(res.rows[0].iccid).toBe('89553202100093795330');
    expect(res.rows[0].protocol).toBe('gt06');

    expect(res.rows[1].imei).toBe('869247061230002');
    expect(res.rows[1].phoneNumber).toBe('(11) 3333-4444');
    expect(res.rows[1].model).toBe('Coban');
  });

  it('detects invalid IMEI and duplicate IMEIs', () => {
    const csv = `
869247061230001
12345
869247061230001
869247061230099
    `.trim();

    const existing = [{ imei: '869247061230099' }];
    const res = parseTrackerBatch(csv, existing);

    expect(res.rows.length).toBe(4);
    expect(res.validCount).toBe(1); // 1st is valid
    expect(res.errorCount).toBe(1); // 2nd is invalid (< 15 digits)
    expect(res.duplicateCount).toBe(2); // 3rd duplicated in batch, 4th already exists in system

    expect(res.rows[0].status).toBe('valid');
    expect(res.rows[1].status).toBe('error');
    expect(res.rows[1].message).toContain('15 dígitos');
    expect(res.rows[2].status).toBe('duplicate');
    expect(res.rows[2].message).toContain('repetido nesta lista');
    expect(res.rows[3].status).toBe('duplicate');
    expect(res.rows[3].message).toContain('já cadastrado no sistema');
  });

  it('detects invalid and duplicate ICCID', () => {
    const csv = `
IMEI;ICCID
869247061230001;123
869247061230002;89553202100093795330
869247061230003;89553202100093795330
869247061230004;89553202100093795399
    `.trim();

    const existing = [{ imei: '869247061230088', iccid: '89553202100093795399' }];
    const res = parseTrackerBatch(csv, existing);

    expect(res.rows[0].status).toBe('error');
    expect(res.rows[0].message).toContain('ICCID deve ter 19 ou 20 dígitos');

    expect(res.rows[1].status).toBe('valid');

    expect(res.rows[2].status).toBe('duplicate');
    expect(res.rows[2].message).toContain('ICCID repetido nesta lista');

    expect(res.rows[3].status).toBe('duplicate');
    expect(res.rows[3].message).toContain('ICCID já cadastrado em outro rastreador');
  });
});

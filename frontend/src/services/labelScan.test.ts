import { describe, expect, it } from 'vitest';

import { findCodes } from './labelScan';

// Números válidos (dígito verificador certo).
const IMEI = '869247061230055';
const IMEI2 = '861234567890127';
const ICCID = '89553202100093795348';

describe('findCodes', () => {
  it('acha o IMEI puro, o de código de barras com prefixo e o de QR code com vários campos', () => {
    expect(findCodes(IMEI, 'imei')).toEqual([IMEI]);
    expect(findCodes(`IMEI:${IMEI}`, 'imei')).toEqual([IMEI]);
    expect(findCodes(`MODEL:TK910;IMEI:${IMEI};SN:2024101`, 'imei')).toEqual([IMEI]);
  });

  it('junta os grupos do número impresso e separa o que vem colado na mesma linha', () => {
    expect(findCodes('IMEI 86 924706 123005 5', 'imei')).toEqual([IMEI]);
    expect(findCodes(`IMEI ${IMEI} 2024`, 'imei')).toEqual([IMEI]);
    expect(findCodes('ICCID 8955 3202 1000 9379 5348', 'iccid')).toEqual([ICCID]);
  });

  it('recusa número com o verificador errado, com dígito a mais ou de outro tipo', () => {
    expect(findCodes('869247061230056', 'imei')).toEqual([]);
    expect(findCodes('8692470612300551', 'imei')).toEqual([]);
    expect(findCodes(ICCID, 'imei')).toEqual([]);
    expect(findCodes(IMEI, 'iccid')).toEqual([]);
    expect(findCodes('Data 2024-10-27 · Lote 12', 'imei')).toEqual([]);
  });

  it('corrige letra que o OCR confunde com número só no meio dos dígitos', () => {
    expect(findCodes('IMEI: 8692470612300S5', 'imei')).toEqual([IMEI]);
    expect(findCodes('IMEI 86924706l23OO55', 'imei')).toEqual([IMEI]);
  });

  it('tira o SP da etiqueta do chip', () => {
    expect(findCodes(`${ICCID}SP`, 'iccid')).toEqual([ICCID]);
  });

  it('devolve cada número uma vez, na ordem, linha a linha', () => {
    expect(findCodes(`IMEI1 ${IMEI}\nIMEI2 ${IMEI2}\n${IMEI}`, 'imei')).toEqual([IMEI, IMEI2]);
  });
});

// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';

import { deliveryLine } from '../IntegrationsPage';
import { licenseDays, parseDecimal } from './FleetOpsPage';

describe('frota e integrações', () => {
  it('lê os números como a pessoa digita', () => {
    expect(parseDecimal('10.500,5')).toBe(10500.5);
    expect(parseDecimal('42,5')).toBe(42.5);
    expect(parseDecimal('')).toBeNull();
    expect(parseDecimal('abc')).toBeNaN();
  });

  it('conta os dias até a CNH vencer', () => {
    expect(licenseDays('2026-11-23', '2026-10-24')).toBe(30);
    expect(licenseDays('2026-10-20', '2026-10-24')).toBe(-4);
    expect(licenseDays(null)).toBeNull();
  });

  it('escreve a última entrega do webhook', () => {
    expect(deliveryLine({ lastStatus: null, lastError: '', lastSentAt: null, failures: 0 })).toBe('nenhum envio ainda');
    const at = new Date().toISOString();
    expect(deliveryLine({ lastStatus: 204, lastError: '', lastSentAt: at, failures: 0 })).toMatch(/: 204$/);
    expect(deliveryLine({ lastStatus: 500, lastError: 'resposta 500', lastSentAt: at, failures: 3 })).toMatch(/resposta 500 \(3 falhas seguidas\)$/);
  });
});

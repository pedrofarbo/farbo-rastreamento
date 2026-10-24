import { describe, expect, it } from 'vitest';

import { maintenanceInput } from './MaintenanceModals';

const draft = {
  kind: 'document' as const,
  name: ' IPVA ',
  dueDate: '2027-02-10',
  everyKm: '',
  everyMonths: '12',
  everyHours: '',
  lastDate: '2026-10-23',
  lastKm: '',
  lastHours: '',
};

describe('o pedido de um documento', () => {
  it('vai só com a renovação e o próximo vencimento', () => {
    expect(maintenanceInput(draft, true)).toEqual({
      kind: 'document', name: 'IPVA', everyKm: null, everyMonths: 12, everyHours: null, dueDate: '2027-02-10',
    });
  });

  it('pede o que falta', () => {
    expect(maintenanceInput({ ...draft, name: ' ' }, true)).toContain('nome ao documento');
    expect(maintenanceInput({ ...draft, everyMonths: '' }, true)).toContain('quantos em quantos meses');
    expect(maintenanceInput({ ...draft, everyMonths: '0' }, true)).toContain('quantos em quantos meses');
    expect(maintenanceInput({ ...draft, dueDate: '' }, true)).toContain('próximo vencimento');
  });

  it('a manutenção segue como antes', () => {
    const input = maintenanceInput({ ...draft, kind: 'service', name: 'Troca de óleo', everyKm: '10.000', everyMonths: '6' }, true);
    expect(input).toMatchObject({ name: 'Troca de óleo', everyKm: 10000, everyMonths: 6, lastDate: '2026-10-23' });
  });
});

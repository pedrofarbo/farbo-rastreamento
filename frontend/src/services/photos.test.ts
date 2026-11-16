import { describe, expect, it } from 'vitest';

import { fitSize } from './photos';
import {
  CLAIM_STATUS,
  claimEditable,
  claimKindLabel,
  inspectionExpired,
  inspectionMessage,
  missingSlots,
  photosLabel,
  whatsappLink,
} from './occurrences';

describe('fotos e ocorrências', () => {
  it('reduz a foto pelo lado maior, mantendo a proporção', () => {
    expect(fitSize(4000, 3000)).toEqual({ width: 1600, height: 1200 });
    expect(fitSize(3000, 4000)).toEqual({ width: 1200, height: 1600 });
    expect(fitSize(800, 600)).toEqual({ width: 800, height: 600 });
  });

  it('escreve as situações e os ângulos', () => {
    expect(claimKindLabel('collision')).toBe('Colisão');
    expect(CLAIM_STATUS.analyzing.label).toBe('Em análise');
    expect(claimEditable('open')).toBe(true);
    expect(claimEditable('closed')).toBe(false);
    const slots = [
      { id: 'front', label: 'Frente', hint: '' },
      { id: 'rear', label: 'Traseira', hint: '' },
      { id: 'damage_extra', label: 'Outra avaria', hint: '', optional: true },
    ];
    expect(photosLabel(slots, [{ slot: 'front' }, { slot: 'damage_extra' }])).toBe('1 de 2 fotos');
    expect(missingSlots(slots, [{ slot: 'front' }]).map((s) => s.id)).toEqual(['rear']);
  });

  it('o link da vistoria: o vencimento e a mensagem do WhatsApp', () => {
    const now = new Date('2026-11-15T12:00:00Z');
    expect(inspectionExpired({ status: 'pending', expiresAt: '2026-11-15T11:00:00Z' }, now)).toBe(true);
    expect(inspectionExpired({ status: 'pending', expiresAt: '2026-11-20T11:00:00Z' }, now)).toBe(false);
    expect(inspectionExpired({ status: 'submitted', expiresAt: '2026-11-15T11:00:00Z' }, now)).toBe(false);
    const i = { associateName: 'Carla Nova Silva', organizationName: 'Rota Segura', plate: 'ABC1D23', vehicleName: 'Gol', mode: 'self' as const, kind: 'entry' as const };
    const text = inspectionMessage(i, 'https://painel/vistoria/x');
    expect(text).toContain('Olá, Carla!');
    expect(text).toContain('vistoria de adesão do veículo ABC1D23');
    expect(text).not.toContain('Silva');
    expect(inspectionMessage({ ...i, mode: 'technician' }, 'L')).toContain('(Carla)');
    expect(whatsappLink('(34) 9-8888-1234', 'oi')).toBe('https://wa.me/5534988881234?text=oi');
    expect(whatsappLink('', 'oi')).toBe('https://wa.me/?text=oi');
  });
});

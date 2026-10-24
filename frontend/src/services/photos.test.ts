import { describe, expect, it } from 'vitest';

import { fitSize } from './photos';
import { CLAIM_STATUS, INSPECTION_SLOTS, claimEditable, claimKindLabel, photosLabel } from './occurrences';

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
    expect(INSPECTION_SLOTS).toHaveLength(6);
    expect(photosLabel(3)).toBe('3 de 6 fotos');
  });
});

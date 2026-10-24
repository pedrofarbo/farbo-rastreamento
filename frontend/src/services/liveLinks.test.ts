import { describe, expect, it } from 'vitest';

import { durationLabel, liveShareText, untilLabel, viewsLabel } from '@/services/liveLinks';

describe('localização ao vivo', () => {
  it('diz até quando vale e quantas vezes abriram', () => {
    const now = new Date(2026, 9, 23, 15, 0);
    expect(untilLabel(new Date(2026, 9, 23, 17, 40).toISOString(), now)).toBe('até 17:40');
    expect(untilLabel(new Date(2026, 9, 24, 9, 15).toISOString(), now)).toBe('até 24/10 às 09:15');
    expect(viewsLabel(0)).toBe('ainda não aberto');
    expect(viewsLabel(1)).toBe('aberto 1 vez');
    expect(viewsLabel(3)).toBe('aberto 3 vezes');
    expect(durationLabel(24)).toBe('24 h');
  });

  it('a mensagem leva o nome do veículo e o link', () => {
    const expiresAt = new Date(Date.now() + 60 * 60 * 1000).toISOString();
    const text = liveShareText({ name: 'Moto do trabalho' }, { url: 'https://farbo.test/ao-vivo/abc', expiresAt });
    expect(text).toMatch(/^Acompanhe Moto do trabalho ao vivo \(até .+\): https:\/\/farbo\.test\/ao-vivo\/abc$/);
  });
});

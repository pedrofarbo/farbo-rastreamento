// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';

import { ApiError } from '@/api/client';

import { clearPartnerSession, needsPartnerPassword, savePartnerSession, storedPartnerSession } from './partnerSession';

afterEach(() => localStorage.clear());

describe('a sessão da página do afiliado com senha', () => {
  it('guarda por link, até vencer', () => {
    const now = Date.parse('2026-11-03T12:00:00Z');
    savePartnerSession('link-a', 'sessao-a', '2026-12-03T12:00:00Z');
    expect(storedPartnerSession('link-a', now)).toBe('sessao-a');
    expect(storedPartnerSession('link-b', now)).toBe('');
    // Vencida: sai do aparelho e da memória.
    expect(storedPartnerSession('link-a', Date.parse('2026-12-04T00:00:00Z'))).toBe('');
    expect(storedPartnerSession('link-a', now)).toBe('');
    savePartnerSession('link-a', 'sessao-a', '2026-12-03T12:00:00Z');
    clearPartnerSession('link-a');
    expect(storedPartnerSession('link-a', now)).toBe('');
  });

  it('reconhece quando a página pede a senha', () => {
    expect(needsPartnerPassword(new ApiError(401, 'x', { code: 'PARTNER_PASSWORD' }))).toBe(true);
    expect(needsPartnerPassword(new ApiError(401, 'x', { code: 'WRONG_PASSWORD' }))).toBe(false);
    expect(needsPartnerPassword(new ApiError(404, 'x'))).toBe(false);
  });
});

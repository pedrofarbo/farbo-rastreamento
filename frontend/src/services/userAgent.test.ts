import { describe, expect, it } from 'vitest';

import { deviceLabel } from './userAgent';

describe('o aparelho da sessão', () => {
  it('lê os navegadores de verdade', () => {
    // Os dois iPhones da conta do Royale (out/2026): o Safari congela o sistema em 18_7.
    expect(
      deviceLabel(
        'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6.1 Mobile/15E148 Safari/604.1',
      ),
    ).toBe('iPhone · Safari (iOS 26.6)');
    expect(
      deviceLabel(
        'Mozilla/5.0 (iPhone; CPU iPhone OS 26_5_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) GSA/441.5.989047059 Mobile/15E148 Safari/604.1',
      ),
    ).toBe('iPhone · app do Google (iOS 26.5)');
    expect(
      deviceLabel(
        'Mozilla/5.0 (iPhone; CPU iPhone OS 26_6_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/155.0.8059.37 Mobile/15E148 Safari/604.1',
      ),
    ).toBe('iPhone · Chrome (iOS 26.6)');
    expect(
      deviceLabel('Mozilla/5.0 (Linux; Android 14; SM-A546E) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36'),
    ).toBe('Android · Chrome (Android 14)');
    expect(
      deviceLabel(
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0',
      ),
    ).toBe('Windows · Edge');
    expect(deviceLabel('')).toBe('Aparelho desconhecido');
  });
});

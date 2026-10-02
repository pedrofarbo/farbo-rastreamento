import { describe, expect, it } from 'vitest';

import { toBase64url, toBytes } from './stepUp';

describe('base64url do WebAuthn', () => {
  it('ida e volta sem perder bytes, sem "=" nem "+/"', () => {
    for (const size of [0, 1, 2, 3, 16, 31, 32, 65]) {
      const bytes = Uint8Array.from({ length: size }, (_, i) => (i * 97 + 251) % 256);
      const text = toBase64url(bytes);
      expect(text).not.toMatch(/[=+/]/);
      expect(Array.from(toBytes(text))).toEqual(Array.from(bytes));
    }
  });

  it('lê o que o servidor manda (Go, RawURLEncoding)', () => {
    // base64.RawURLEncoding.EncodeToString([]byte{0xfb, 0xff, 0x00}) == "-_8A"
    expect(Array.from(toBytes('-_8A'))).toEqual([0xfb, 0xff, 0x00]);
    expect(toBase64url(new Uint8Array([0xfb, 0xff, 0x00]))).toBe('-_8A');
  });
});

import { describe, expect, it } from 'vitest';

import { whatsappDisplay, whatsappUrl } from './contact';

describe('WhatsApp da empresa', () => {
  it('mostra o número no formato de fixo e de celular', () => {
    expect(whatsappDisplay('551151946470')).toBe('(11) 5194-6470');
    expect(whatsappDisplay('5511987654321')).toBe('(11) 98765-4321');
  });

  it('o link abre a conversa, com a mensagem já escrita se houver', () => {
    expect(whatsappUrl('', '551151946470')).toBe('https://wa.me/551151946470');
    expect(whatsappUrl('Olá! Quero saber mais & preços', '551151946470')).toBe(
      'https://wa.me/551151946470?text=Ol%C3%A1!%20Quero%20saber%20mais%20%26%20pre%C3%A7os',
    );
  });
});

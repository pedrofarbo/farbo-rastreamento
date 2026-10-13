// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { InviteLinkModal } from './InviteLinkModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement | undefined;

const link = {
  url: 'https://app.farborastreadores.com.br/redefinir-senha#token=abc&boasvindas=1',
  expiresAt: '2026-10-16T16:00:00Z',
};

async function render(phone: string) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(<InviteLinkModal link={link} name="Pedro Mauricio Resende" phone={phone} onClose={() => undefined} />),
  );
}

afterEach(() => {
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

describe('o link do convite na ficha do cliente', () => {
  it('mostra o link e a validade, copia e abre o WhatsApp do cliente com a mensagem pronta', async () => {
    const writeText = vi.fn(async () => undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    await render('(11) 98888-7777');

    const input = document.querySelector('input') as HTMLInputElement;
    expect(input.value).toBe(link.url);
    expect(input.readOnly).toBe(true);
    expect(text()).toContain('Vale até');
    expect(text()).toContain('inclusive o do e-mail, deixou de valer');

    await act(async () => button('Copiar link')!.click());
    expect(writeText).toHaveBeenCalledWith(link.url);
    expect(button('Copiado')).toBeTruthy();

    await act(async () => button('Enviar pelo WhatsApp')!.click());
    const [url] = open.mock.calls[0] as [string];
    expect(url.startsWith('https://wa.me/5511988887777?text=')).toBe(true);
    expect(decodeURIComponent(url.split('?text=')[1])).toBe(
      `Olá, Pedro! Este é o link para você criar a sua senha e entrar na Farbo Rastreadores: ${link.url}`,
    );
  });

  it('sem telefone com DDD, só copiar', async () => {
    await render('98888-77');
    expect(button('Enviar pelo WhatsApp')).toBeUndefined();
    expect(button('Copiar link')).toBeTruthy();
  });
});

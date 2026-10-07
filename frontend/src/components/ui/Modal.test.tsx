// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it } from 'vitest';

import { Modal } from './Modal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => document.body.textContent ?? '';
const escape = () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));

function Nested() {
  const [outer, setOuter] = useState(true);
  const [inner, setInner] = useState(false);
  return (
    <Modal open={outer} title="Pedido" onClose={() => setOuter(false)}>
      conteúdo do pedido
      <button type="button" onClick={() => setInner(true)}>
        Pagar
      </button>
      <Modal open={inner} title="Pagar por Pix" onClose={() => setInner(false)}>
        conteúdo do Pix
      </Modal>
    </Modal>
  );
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('Modal', () => {
  it('um diálogo aberto de dentro de outro: o Esc fecha só o de cima', async () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () => createRoot(host).render(<Nested />));
    // O de dentro abre depois, por um clique (como a tela de Pix no pedido).
    await act(async () => (Array.from(document.querySelectorAll('button')).find((b) => b.textContent === 'Pagar') as HTMLButtonElement).click());
    expect(text()).toContain('conteúdo do Pix');
    await act(async () => escape());
    expect(text()).not.toContain('conteúdo do Pix');
    expect(text()).toContain('conteúdo do pedido');
    await act(async () => escape());
    expect(text()).not.toContain('conteúdo do pedido');
  });
});

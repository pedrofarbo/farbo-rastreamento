// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it } from 'vitest';

import { TextField } from './Field';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function render(node: React.ReactNode) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  act(() => root.render(node));
  return { host, unmount: () => act(() => root.unmount()) };
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('TextField de senha', () => {
  it('o olhinho mostra e volta a esconder a senha', () => {
    const { host } = render(<TextField label="Senha" type="password" defaultValue="s3gr3do" />);
    const input = host.querySelector('input') as HTMLInputElement;
    const eye = host.querySelector('button') as HTMLButtonElement;

    expect(input.type).toBe('password');
    expect(eye.getAttribute('aria-label')).toBe('Mostrar senha');
    expect(eye.type).toBe('button'); // não envia o formulário

    act(() => eye.click());
    expect(input.type).toBe('text');
    expect(input.value).toBe('s3gr3do');
    expect(eye.getAttribute('aria-label')).toBe('Ocultar senha');
    expect(eye.getAttribute('aria-pressed')).toBe('true');

    act(() => eye.click());
    expect(input.type).toBe('password');
  });

  it('o rótulo continua apontando para o campo', () => {
    const { host } = render(<TextField label="Senha" type="password" />);
    const label = host.querySelector('label') as HTMLLabelElement;
    expect(label.htmlFor).toBe((host.querySelector('input') as HTMLInputElement).id);
  });

  it('campo desabilitado desabilita o olhinho', () => {
    const { host } = render(<TextField label="Senha" type="password" disabled />);
    expect((host.querySelector('button') as HTMLButtonElement).disabled).toBe(true);
  });

  it('outros campos não ganham o olhinho', () => {
    const { host } = render(<TextField label="E-mail" type="email" />);
    expect(host.querySelector('button')).toBeNull();
  });
});

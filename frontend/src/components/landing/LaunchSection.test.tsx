// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// Função simples em vez de vi.fn (ver LeadModal.test.tsx).
const sent: unknown[] = [];
let respond: () => Promise<unknown> = async () => ({ status: 'ok' });
vi.mock('@/api/resources', () => ({
  publicApi: {
    joinLaunch: (input: unknown) => {
      sent.push(input);
      return respond();
    },
  },
}));

import { saveReferral } from '@/services/referral';

import { LaunchSection } from './LaunchSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  act(() => root.render(<LaunchSection />));
  return host;
}

function type(el: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value);
  act(() => {
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

const input = (host: HTMLElement, selector: string) => host.querySelector(selector) as HTMLInputElement;
const submit = (host: HTMLElement) => host.querySelector('button[type="submit"]') as HTMLButtonElement;

beforeEach(() => {
  sent.length = 0;
  respond = async () => ({ status: 'ok' });
});
afterEach(() => {
  document.body.innerHTML = '';
  localStorage.clear();
});

describe('LaunchSection (pré-lançamento)', () => {
  it('fica no lugar dos depoimentos, com a âncora do menu', () => {
    const host = render();
    expect(host.querySelector('section#lancamento')).not.toBeNull();
  });

  it('mostra a promoção de pré-lançamento e as regras dela', () => {
    const text = render().textContent ?? '';
    for (const part of ['R$ 120', 'R$ 150', 'R$ 34,90', 'R$ 69,90', '12 meses', '500 primeiros', '1 veículo por cliente',
      'Insanos MC pagam R$ 27,90', 'R$ 39,90', 'ou 10x de R$ 12,00 sem juros no Pix']) {
      expect(text).toContain(part);
    }
  });

  it('pede só o e-mail e o aceite; inscreve e confirma (com o link do afiliado)', async () => {
    // Veio pelo link de um afiliado: a inscrição vai com o código.
    saveReferral('Joao-Moto');
    const host = render();
    expect(submit(host).disabled).toBe(true);
    type(input(host, 'input[type="email"]'), 'bruno@exemplo.com.br');
    act(() => input(host, 'input[type="checkbox"]').click());
    expect(submit(host).disabled).toBe(true); // falta o WhatsApp
    type(input(host, 'input[type="tel"]'), '11977776666');
    expect(input(host, 'input[type="tel"]').value).toBe('(11) 9-7777-6666');
    act(() => input(host, 'input[type="checkbox"]').click());
    expect(submit(host).disabled).toBe(true); // falta o aceite
    act(() => input(host, 'input[type="checkbox"]').click());
    expect(submit(host).disabled).toBe(false);

    await act(async () => {
      submit(host).click();
    });
    expect(sent).toEqual([
      { name: '', email: 'bruno@exemplo.com.br', phone: '(11) 9-7777-6666', consent: true, website: '', ref: 'joao-moto' },
    ]);
    expect(host.textContent).toContain('Você está na lista!');
    expect(host.textContent).toContain('use este mesmo e-mail');
    expect(host.querySelector('form')).toBeNull();
  });

  it('mostra o erro e continua no formulário', async () => {
    respond = () => Promise.reject(new Error('informe um e-mail válido'));
    const host = render();
    type(input(host, 'input[type="email"]'), 'bruno@x');
    type(input(host, 'input[type="tel"]'), '11977776666');
    act(() => input(host, 'input[type="checkbox"]').click());
    await act(async () => {
      submit(host).click();
    });
    expect(host.querySelector('[role="alert"]')?.textContent).toBe('informe um e-mail válido');
    expect(host.querySelector('form')).not.toBeNull();
  });
});

// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { Chip } from '@/types';

const chip = (over: Partial<Chip> = {}): Chip => ({
  id: 'c1', iccid: '89553202100093795348', phoneNumber: '(34) 99999-0001', carrier: 'Algar', apn: 'ss.algar.br', apnUser: 'algar',
  apnPasswordSet: true, notes: 'Lote outubro', device: null, createdAt: '2026-10-26T12:00:00Z', updatedAt: '2026-10-26T12:00:00Z', ...over,
});
let chips: Chip[] = [];
const calls: unknown[][] = [];

vi.mock('@/api/resources', () => ({
  chipsApi: {
    list: async () => chips,
    create: async (input: unknown) => {
      calls.push(['create', input]);
      return chip();
    },
    update: async (id: string, input: unknown) => {
      calls.push(['update', id, input]);
      return chip({ device: { id: 'd1', imei: '869247061230055' } });
    },
    remove: async (id: string) => {
      calls.push(['remove', id]);
    },
  },
}));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => {} }) }));

import { ChipsPage } from './ChipsPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).filter((b) => b.textContent?.trim() === label);
const field = (label: string) => {
  const l = Array.from(document.querySelectorAll('label')).find((x) => x.textContent === label)!;
  return document.getElementById(l.htmlFor) as HTMLInputElement;
};
const type = async (input: HTMLInputElement, value: string) =>
  act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ChipsPage />
      </QueryClientProvider>,
    ),
  );
  await vi.waitFor(() => expect(text()).not.toContain('Carregando'));
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
  localStorage.clear();
});

describe('o estoque de chips', () => {
  it('lista os livres e os em uso, filtra e busca', async () => {
    chips = [chip(), chip({ id: 'c2', iccid: '89553202100093795330', phoneNumber: '', carrier: 'Vivo', apn: '', device: { id: 'd1', imei: '869247061230055' } })];
    await render();
    expect(text()).toContain('Todos (2)');
    expect(text()).toContain('Livres (1)');
    expect(text()).toContain('Em uso (1)');
    expect(text()).toContain('8955 3202 1000 9379 5348');
    expect(text()).toContain('ss.algar.br · algar · com senha');
    expect(text()).toContain('rastreador 869247061230055');
    // Só o livre pode sair do estoque.
    expect(button('Retirar')).toHaveLength(1);

    await act(async () => button('Livres (1)')[0].click());
    expect(text()).not.toContain('8955 3202 1000 9379 5330');
    await act(async () => button('Todos (2)')[0].click());
    await type(document.querySelector('input[type="search"]') as HTMLInputElement, '869247');
    expect(text()).toContain('8955 3202 1000 9379 5330');
    expect(text()).not.toContain('8955 3202 1000 9379 5348');
  });

  it('pagina, com a quantidade por página; a busca volta para a primeira página', async () => {
    chips = Array.from({ length: 30 }, (_, i) => chip({ id: `c${i + 1}`, iccid: `895532021000937${String(i + 1).padStart(5, '0')}`, notes: `Chip ${i + 1}` }));
    await render();
    expect(document.querySelectorAll('tbody tr')).toHaveLength(25);
    expect(text()).toContain('1–25 de 30');
    expect(text()).toContain('Página 1 de 2');
    expect(button('Anterior')[0].disabled).toBe(true);

    await act(async () => button('Próxima')[0].click());
    expect(document.querySelectorAll('tbody tr')).toHaveLength(5);
    expect(text()).toContain('26–30 de 30');
    expect(text()).toContain('Chip 26');
    expect(text()).not.toContain('Chip 25');

    // Com 10 por página, a lista fica no trecho do chip 26.
    const size = document.querySelector('nav[aria-label="Páginas"] select') as HTMLSelectElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(size, '10');
      size.dispatchEvent(new Event('change', { bubbles: true }));
    });
    expect(text()).toContain('21–30 de 30');
    expect(text()).toContain('Página 3 de 3');
    expect(localStorage.getItem('tracker.pageSize.chips')).toBe('10');

    await type(document.querySelector('input[type="search"]') as HTMLInputElement, 'chip 1');
    expect(text()).toContain('Página 1 de 2');
    expect(text()).toContain('1–10 de 11');
  });

  it('cadastra um chip, com o atalho da Algar', async () => {
    chips = [];
    await render();
    expect(text()).toContain('Nenhum chip no estoque');
    await act(async () => button('Novo chip')[0].click());
    await type(field('ICCID'), '8955 3202 1000 9379 5348 SP');
    await type(field('Linha'), '34999990001');
    await act(async () => button('Autopreencher')[0].click());
    await act(async () => button('Salvar')[0].click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toEqual([
      'create',
      expect.objectContaining({ iccid: '8955 3202 1000 9379 5348 SP', carrier: 'Algar', apn: 'ss.algar.br', apnUser: 'algar', apnPassword: 'algar' }),
    ]);
  });

  it('o chip em uso: a edição avisa que o rastreador muda junto; a senha em branco fica', async () => {
    chips = [chip({ device: { id: 'd1', imei: '869247061230055' } })];
    await render();
    await act(async () => button('Editar')[0].click());
    expect(text()).toContain('Este chip está no rastreador 869247061230055');
    expect(field('Senha APN').placeholder).toBe('definida (em branco mantém)');
    await act(async () => button('Salvar')[0].click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toEqual(['update', 'c1', expect.objectContaining({ phoneNumber: '(34) 99999-0001', apnPassword: '' })]);
  });
});

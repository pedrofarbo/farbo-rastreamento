// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { ChipBulkResult, ChipInput } from '@/api/resources';

const sent: ChipInput[][] = [];
let result: ChipBulkResult = { total: 0, created: 0, errors: [] };
const notes: { title: string }[] = [];

vi.mock('@/api/resources', () => ({
  chipsApi: {
    createBulk: async (chips: ChipInput[]) => {
      sent.push(chips);
      return result;
    },
  },
}));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: (n: { title: string }) => notes.push(n) }) }));

import { ChipBulkModal } from './ChipBulkModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === label)!;
const setValue = async (el: HTMLInputElement | HTMLTextAreaElement, value: string) =>
  act(async () => {
    const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
const field = (label: string) => {
  const l = Array.from(document.querySelectorAll('label')).find((x) => x.textContent === label)!;
  return document.getElementById(l.htmlFor) as HTMLInputElement;
};

let closed = 0;
async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <ChipBulkModal open onClose={() => closed++} existingIccids={['89553202100093795355']} />
      </QueryClientProvider>,
    ),
  );
}

afterEach(() => {
  document.body.innerHTML = '';
  sent.length = 0;
  notes.length = 0;
  closed = 0;
});

describe('importar chips em massa', () => {
  it('cola os ICCIDs, aplica o chip Algar e o lote a todos e importa só os prontos', async () => {
    result = { total: 2, created: 2, errors: [] };
    await render();
    await setValue(
      document.querySelector('textarea') as HTMLTextAreaElement,
      '89553202100093795330\n8955 3202 1000 9379 5348 SP\n89553202100093795355\n89553202100093795349',
    );
    expect(text()).toContain('Prontos: 2');
    expect(text()).toContain('Repetidos: 1');
    expect(text()).toContain('Erros: 1');
    expect(text()).toContain('Já está no estoque');
    await act(async () => button('Chip Algar (ss.algar.br)').click());
    await setValue(field('Observações'), 'Lote 12');
    expect(text()).toContain('Algar · ss.algar.br');
    await act(async () => button('Importar 2 chips').click());
    await vi.waitFor(() => expect(closed).toBe(1));
    expect(sent[0]).toEqual([
      { iccid: '89553202100093795330', phoneNumber: '', carrier: 'Algar', apn: 'ss.algar.br', apnUser: 'algar', apnPassword: 'algar', notes: 'Lote 12' },
      { iccid: '89553202100093795348', phoneNumber: '', carrier: 'Algar', apn: 'ss.algar.br', apnUser: 'algar', apnPassword: 'algar', notes: 'Lote 12' },
    ]);
    expect(notes[0].title).toBe('2 chips no estoque');
  });

  it('o recusado pelo servidor fica na tela, com a linha da lista', async () => {
    result = { total: 2, created: 1, errors: [{ index: 1, iccid: '89553202100093795348', error: 'o chip de ICCID 89553202100093795348 já está cadastrado' }] };
    await render();
    await setValue(document.querySelector('textarea') as HTMLTextAreaElement, '89553202100093795330\n\n89553202100093795348');
    await act(async () => button('Importar 2 chips').click());
    await vi.waitFor(() => expect(text()).toContain('Recusados pelo servidor'));
    expect(text()).toContain('Linha 3 (8955 3202 1000 9379 5348): o chip de ICCID 89553202100093795348 já está cadastrado');
    expect(closed).toBe(0);
    expect(notes[0].title).toBe('1 chip no estoque');
  });
});

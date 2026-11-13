// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { ClosingLine, ClosingMonth } from '@/types';

let month: ClosingMonth;
vi.mock('@/api/resources', () => ({
  closingApi: {
    month: async () => month,
    exportCsv: async () => undefined,
    removeNote: async () => undefined,
    downloadNote: async () => undefined,
    saveNote: async () => undefined,
  },
}));

import { ClosingTab } from './ClosingTab';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const plain = (value: string | null | undefined) => (value ?? '').replace(/\u00a0/g, ' ');
const text = () => plain(document.body.textContent);
function row(name: string): HTMLTableRowElement {
  return Array.from(document.querySelectorAll('tbody tr')).find((tr) => tr.textContent?.includes(name)) as HTMLTableRowElement;
}
const buttons = (tr: HTMLElement) => Array.from(tr.querySelectorAll('button')).map((b) => b.textContent?.trim());

const line = (name: string, extra: Partial<ClosingLine>): ClosingLine => ({
  payerId: name,
  kind: 'customer',
  name,
  document: '',
  email: '',
  invoices: 1,
  amountCents: 15000,
  paidCents: 15000,
  note: null,
  autoNotes: [],
  upcoming: 0,
  needsNote: false,
  ...extra,
});

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ToastProvider>
          <MemoryRouter>
            <ClosingTab />
          </MemoryRouter>
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('o fechamento do mês (Empresa → Fechamento e notas)', () => {
  it('a nota da Notaas (a da contratação) não pede a nota à mão; só a que falta fica por emitir', async () => {
    month = {
      month: '2026-11',
      amountCents: 54500,
      paidCents: 45500,
      notes: 0,
      missingNotes: 1,
      autoNotes: 1,
      lines: [
        line('Cris', {
          invoices: 3,
          amountCents: 23500,
          paidCents: 23500,
          autoNotes: [
            { id: 'n1', invoiceId: 'i1', status: 'issued', number: '1001', amountCents: 15000 },
            { id: 'n2', invoiceId: 'i2', status: 'processing', number: '', amountCents: 6000 },
          ],
        }),
        line('Davi', { amountCents: 9000, paidCents: 0, upcoming: 1 }),
        line('Eva', { amountCents: 8000, paidCents: 8000, needsNote: true }),
        line('Fabio', { autoNotes: [{ id: 'n3', invoiceId: 'i3', status: 'error', number: '', amountCents: 15000 }] }),
        line('Gil', { amountCents: 3000, paidCents: 3000 }),
      ],
    };
    await render();
    expect(text()).toContain('Notas por emitir1');
    expect(text()).toContain('1 emitidas pela Notaas · 0 registradas aqui');

    expect(plain(row('Cris').textContent)).toContain('Nº 1001 · NotaasR$ 150,00');
    expect(plain(row('Cris').textContent)).toContain('1 nota saindo pela Notaas');
    expect(plain(row('Cris').textContent)).not.toContain('Por emitir');
    expect(buttons(row('Cris'))).toEqual([]);

    expect(plain(row('Davi').textContent)).toContain('1 fatura ganha a nota da Notaas ao ser paga');
    expect(buttons(row('Davi'))).toEqual([]);

    expect(plain(row('Eva').textContent)).toContain('Por emitir');
    expect(buttons(row('Eva'))).toEqual(['Registrar nota']);

    expect(plain(row('Fabio').textContent)).toContain('Nota da Notaas recusada');
    expect(buttons(row('Fabio'))).toEqual([]);

    expect(plain(row('Gil').textContent)).toContain('Sem nota de serviço (frete ou avulsa)');
    expect(buttons(row('Gil'))).toEqual([]);
  });

  it('a nota registrada à mão continua com baixar, trocar e tirar', async () => {
    month = {
      month: '2026-10',
      amountCents: 21990,
      paidCents: 21990,
      notes: 1,
      missingNotes: 0,
      autoNotes: 0,
      lines: [
        line('Ana', {
          amountCents: 21990,
          paidCents: 21990,
          needsNote: true,
          note: {
            id: 'm1', customerId: 'Ana', month: '2026-10', number: '124', issuedOn: '2026-11-01', amountCents: 21990,
            filename: 'nota.pdf', hasFile: true, createdAt: '', updatedAt: '',
          },
        }),
      ],
    };
    await render();
    expect(plain(row('Ana').textContent)).toContain('Nº 124 · 01/11/2026R$ 219,90');
    expect(plain(row('Ana').textContent)).not.toContain('Por emitir');
    expect(buttons(row('Ana'))).toEqual(['Baixar', 'Trocar', 'Tirar']);
    expect(text()).toContain('Notas por emitir0');
  });
});

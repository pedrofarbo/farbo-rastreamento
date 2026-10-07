// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { SupplierInput } from '@/api/resources';
import { ToastProvider } from '@/components/ui/Toast';
import type { Supplier } from '@/types';

const supplier: Supplier = {
  id: 's1', name: 'Fornecedor de teste', document: '49757084000100', email: 'contato@fornecedor.test', phone: '',
  pixKey: '12345678901', pixKeyType: 'CPF', notes: '', active: true, createdAt: '2026-10-05T12:00:00Z',
};
const saved: Array<{ id: string | null; input: SupplierInput }> = [];
vi.mock('@/api/resources', () => ({
  financeApi: {
    recurrences: async () => [],
    categories: async () => [],
    suppliers: async () => [supplier],
    saveSupplier: async (id: string | null, input: SupplierInput) => {
      saved.push({ id, input });
      return { ...supplier, ...input };
    },
  },
}));

import { RegistryTab } from './RegistryTab';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;

afterEach(() => {
  document.body.innerHTML = '';
  saved.length = 0;
});

describe('cadastro de fornecedores', () => {
  it('editar manda só os campos do cadastro (o servidor recusa id e createdAt)', async () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient()}>
          <ToastProvider>
            <RegistryTab />
          </ToastProvider>
        </QueryClientProvider>,
      ),
    );
    await flush();
    await act(async () => button('Editar').click());
    await act(async () => button('Salvar').click());
    await flush();
    expect(saved).toHaveLength(1);
    expect(saved[0].id).toBe('s1');
    expect(saved[0].input).toEqual({
      name: 'Fornecedor de teste', document: '49.757.084/0001-00', email: 'contato@fornecedor.test', phone: '',
      pixKey: '12345678901', pixKeyType: 'CPF', notes: '', active: true,
    });
  });
});

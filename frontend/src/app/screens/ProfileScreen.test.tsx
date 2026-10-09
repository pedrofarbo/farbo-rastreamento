// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { DeliveryAddress, User } from '@/types';

const user: User = {
  id: 'u1', email: 'lia@cliente.test', name: 'Lia', role: 'customer', phone: '11987654321', document: '52998224725',
  active: true, createdAt: '2026-10-01T00:00:00Z',
};
const address: DeliveryAddress = {
  zipCode: '01310100', street: 'Avenida Paulista', number: '1000', complement: '', district: 'Bela Vista',
  city: 'São Paulo', state: 'SP',
};
const profiles: unknown[] = [];
const addresses: unknown[] = [];
const updated: User[] = [];

vi.mock('@/api/resources', () => ({
  authApi: { me: async () => user },
  meApi: {
    account: async () => ({ deliveryAddress: address }),
    updateProfile: async (input: { name: string; phone: string; document: string }) => {
      profiles.push(input);
      return { ...user, ...input, phone: input.phone };
    },
    saveAddress: async (input: DeliveryAddress) => {
      addresses.push(input);
      return input;
    },
  },
}));
vi.mock('@/stores/AuthContext', () => ({ useAuth: () => ({ user, updateUser: (u: User) => updated.push(u) }) }));

import { ProfileScreen } from './ProfileScreen';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => document.body.textContent ?? '';
const input = (label: string) => {
  const l = Array.from(document.querySelectorAll('label')).find((el) => el.textContent?.startsWith(label));
  return (l?.htmlFor ? document.getElementById(l.htmlFor) : l?.querySelector('input')) as HTMLInputElement;
};
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;
function type(el: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(el, value);
  el.dispatchEvent(new Event('input', { bubbles: true }));
}

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter>
          <ToastProvider>
            <ProfileScreen />
          </ToastProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  );
  await flush();
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  profiles.length = 0;
  addresses.length = 0;
  updated.length = 0;
});

describe('Meus dados', () => {
  it('mostra o cadastro com as máscaras; o e-mail não muda', async () => {
    await render();
    expect(input('Nome').value).toBe('Lia');
    expect(input('Celular').value).toBe('(11) 9-8765-4321');
    expect(input('CPF').value).toBe('529.982.247-25');
    expect(input('E-mail').disabled).toBe(true);
    expect(text()).toContain('NF-e do rastreador e a NFS-e das mensalidades');
    // Nada mudou: nada a salvar.
    expect(button('Salvar dados').disabled).toBe(true);
    expect(button('Salvar endereço').disabled).toBe(true);
  });

  it('salva nome, celular e CPF (só com o CPF válido) e atualiza quem está conectado', async () => {
    await render();
    await act(async () => type(input('Nome'), 'Lia Martins'));
    await act(async () => type(input('CPF'), '390.533.447-04'));
    await act(async () => button('Salvar dados').click());
    expect(text()).toContain('CPF inválido');
    expect(profiles).toEqual([]);
    await act(async () => type(input('CPF'), '39053344705'));
    expect(input('CPF').value).toBe('390.533.447-05');
    await act(async () => type(input('Celular'), '1133334444'));
    await act(async () => button('Salvar dados').click());
    await flush();
    expect(profiles).toEqual([{ name: 'Lia Martins', phone: '(11) 3333-4444', document: '39053344705' }]);
    expect(updated[0]?.name).toBe('Lia Martins');
    expect(text()).toContain('Dados atualizados');
  });

  it('salva o endereço de entrega', async () => {
    await render();
    await act(async () => type(input('Número'), '2000'));
    expect(button('Salvar endereço').disabled).toBe(false);
    await act(async () => button('Salvar endereço').click());
    await flush();
    expect(addresses).toEqual([{ ...address, number: '2000' }]);
    expect(text()).toContain('Endereço atualizado');
  });
});

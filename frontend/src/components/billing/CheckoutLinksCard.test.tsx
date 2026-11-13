// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { CheckoutLink, CheckoutLinkInput } from '@/types';

let list: CheckoutLink[];
const created: CheckoutLinkInput[] = [];
const updated: [string, CheckoutLinkInput][] = [];
vi.mock('@/api/resources', () => ({
  checkoutLinksApi: {
    list: async () => list,
    create: async (input: CheckoutLinkInput) => {
      created.push(input);
      return { ...input, id: 'novo', token: 'k7m2p9q4r8s3t6', uses: 0, createdAt: '', updatedAt: '' };
    },
    update: async (id: string, input: CheckoutLinkInput) => {
      updated.push([id, input]);
      return list[0];
    },
    remove: async () => undefined,
  },
}));

import { CheckoutLinksCard, checkoutLinkInstall, checkoutLinkOffer, checkoutLinkStatus } from './CheckoutLinksCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const plain = (value: string) => value.replace(/\u00a0/g, ' ');
const text = () => plain(document.body.textContent ?? '');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim().startsWith(label)) as HTMLButtonElement;
const labels = () => Array.from(document.querySelectorAll('label')).map((l) => l.textContent);
function field(label: string): HTMLInputElement {
  const el = Array.from(document.querySelectorAll('label')).find((l) => l.textContent === label);
  return document.getElementById(el!.htmlFor) as HTMLInputElement;
}
function type(el: HTMLInputElement | HTMLSelectElement, value: string) {
  const proto = el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(el, value);
  act(() => {
    el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  });
}
async function click(el: HTMLElement) {
  await act(async () => el.click());
  await flush();
}

const base: CheckoutLink = {
  id: 'l1', token: 'abc123defg4567', label: 'Frota do João', equipmentMode: 'loan', periodMonths: 12, priceCents: 60000,
  farboInstall: true, installCents: 0, uninstallCents: 8500, installBlocking: true, validUntil: null, maxUses: 1, active: true,
  uses: 0, createdAt: '', updatedAt: '',
};

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ToastProvider>
          <CheckoutLinksCard />
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

beforeEach(() => {
  list = [];
  created.length = 0;
  updated.length = 0;
});
afterEach(() => {
  document.body.innerHTML = '';
});

describe('os links de pagamento (central)', () => {
  it('a oferta, a instalação e a situação em texto', () => {
    expect(plain(checkoutLinkOffer(base))).toBe('Comodato · anual · R$ 600,00/ano');
    expect(plain(checkoutLinkOffer({ equipmentMode: 'sale', periodMonths: 1, priceCents: 4990 }))).toBe('Incluso · mensal · R$ 49,90/mês');
    expect(plain(checkoutLinkInstall(base))).toBe('Farbo, com bloqueio · sem custo; desinstalação R$ 85,00');
    expect(checkoutLinkInstall({ ...base, equipmentMode: 'sale', farboInstall: false })).toBe('Por conta própria');
    expect(checkoutLinkStatus(base, '2026-11-13').label).toBe('Valendo');
    expect(checkoutLinkStatus({ ...base, uses: 1 }, '2026-11-13').label).toBe('Usado');
    expect(checkoutLinkStatus({ ...base, uses: 5, maxUses: 5 }, '2026-11-13').label).toBe('Esgotado');
    expect(checkoutLinkStatus({ ...base, validUntil: '2026-11-12' }, '2026-11-13').label).toBe('Vencido');
    expect(checkoutLinkStatus({ ...base, active: false }, '2026-11-13').label).toBe('Desligado');
  });

  it('o novo: confere antes de mandar, cria e mostra o endereço para copiar', async () => {
    await render();
    expect(text()).toContain('Nenhum link ainda');
    await click(button('Novo link'));
    // O incluso por conta própria: sem a taxa, o bloqueio e a desinstalação.
    expect(labels()).not.toContain('Instalação (R$, opcional)');
    expect(labels()).not.toContain('Bloqueio do motor');
    expect(labels()).not.toContain('Desinstalação (R$, opcional)');
    expect(field('Limite de usos (opcional)').value).toBe('1');
    await click(button('Criar link'));
    expect(text()).toContain('Dê um nome ao link');
    type(field('Para quem ou para quê (só a central vê)'), ' Frota do João ');
    type(field('Rastreador') as unknown as HTMLSelectElement, 'loan');
    type(field('Recorrência') as unknown as HTMLSelectElement, '12');
    expect(labels()).not.toContain('Quem instala');
    type(field('Valor (R$ por ano)'), '600,00');
    type(field('Bloqueio do motor') as unknown as HTMLSelectElement, 'yes');
    type(field('Desinstalação (R$, opcional)'), '85,00');
    await click(button('Criar link'));
    expect(created).toEqual([
      {
        label: 'Frota do João', equipmentMode: 'loan', periodMonths: 12, priceCents: 60000, farboInstall: true, installCents: 0,
        uninstallCents: 8500, installBlocking: true, validUntil: null, maxUses: 1, active: true,
      },
    ]);
    expect(text()).toContain('Link criado');
    expect((document.querySelector('input[readonly]') as HTMLInputElement).value).toBe(
      'https://farborastreadores.com.br/contratar?link=k7m2p9q4r8s3t6',
    );
    expect(text()).toContain('Depois de uma contratação paga, o link deixa de valer.');
  });

  it('o incluso com a instalação da Farbo; a lista; o usado edita, mas não exclui', async () => {
    list = [{ ...base, uses: 1 }];
    await render();
    expect(text()).toContain('Frota do Joãosem validadeComodato · anual · R$ 600,00/ano');
    expect(text()).toContain('1 de 1Usado');
    await click(button('Editar'));
    expect(button('Excluir')).toBeUndefined();
    type(field('Rastreador') as unknown as HTMLSelectElement, 'sale');
    type(field('Recorrência') as unknown as HTMLSelectElement, '1');
    type(field('Valor (R$ por mês)'), '59,90');
    type(field('Quem instala') as unknown as HTMLSelectElement, 'farbo');
    type(field('Instalação (R$, opcional)'), '120,00');
    type(field('Limite de usos (opcional)'), '');
    await click(button('Salvar'));
    expect(updated).toEqual([
      [
        'l1',
        {
          label: 'Frota do João', equipmentMode: 'sale', periodMonths: 1, priceCents: 5990, farboInstall: true, installCents: 12000,
          uninstallCents: 0, installBlocking: true, validUntil: null, maxUses: null, active: true,
        },
      ],
    ]);
  });
});

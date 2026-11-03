// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

const calls = vi.hoisted(() => [] as unknown[][]);
vi.mock('@/api/resources', () => ({
  affiliatesApi: {
    create: async (input: Record<string, unknown>) => {
      calls.push(['create', input]);
      return { id: 'a1', code: 'lia', reportToken: 'x', ...input };
    },
    update: async () => ({}),
    renewReportLink: async () => ({}),
  },
  leadsApi: { downloadQr: async () => {} },
}));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => {} }) }));

import { AffiliateModal } from './AffiliateModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => document.body.textContent ?? '';
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === label)!;
const control = <T extends HTMLElement>(label: string) => {
  const l = Array.from(document.querySelectorAll('label')).find((x) => x.textContent?.startsWith(label))!;
  return document.getElementById(l.htmlFor) as T;
};
const setValue = async (el: HTMLInputElement | HTMLSelectElement, value: string) =>
  act(async () => {
    const proto = el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  });

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <AffiliateModal affiliate={null} open defaultCents={600} onClose={() => {}} onSaved={() => {}} />
      </QueryClientProvider>,
    ),
  );
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('o cadastro do afiliado', () => {
  it('comissão em percentual da mensalidade', async () => {
    await render();
    await setValue(control<HTMLInputElement>('Nome'), 'Lia Percentual');
    await setValue(control<HTMLSelectElement>('Comissão'), 'percent');
    expect(text()).toContain('a parcela do rastreador fica fora');
    await setValue(control<HTMLInputElement>('Percentual da mensalidade'), '150');
    await act(async () => button('Criar afiliado').click());
    expect(text()).toContain('Percentual inválido');
    expect(calls).toHaveLength(0);

    await setValue(control<HTMLInputElement>('Percentual da mensalidade'), '12,5');
    await act(async () => button('Criar afiliado').click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0][1]).toMatchObject({ name: 'Lia Percentual', commissionType: 'percent', commissionBps: 1250, commissionCents: null });
  });

  it('a mensalidade pelo link, por tipo de veículo', async () => {
    await render();
    await setValue(control<HTMLInputElement>('Nome'), 'Lia Tabela');
    await setValue(control<HTMLSelectElement>('Mensalidade pelo link'), 'kind');
    await setValue(control<HTMLInputElement>('Moto (R$/mês)'), '39,90');
    await setValue(control<HTMLInputElement>('Carro (R$/mês)'), '49,90');
    await act(async () => button('Criar afiliado').click());
    expect(text()).toContain('Preencha a mensalidade da moto, do carro e do caminhão');
    expect(calls).toHaveLength(0);

    await setValue(control<HTMLInputElement>('Caminhão (R$/mês)'), '59,90');
    await act(async () => button('Criar afiliado').click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0][1]).toMatchObject({ planPrices: { motorcycleCents: 3990, carCents: 4990, truckCents: 5990 } });
  });

  it('valor fixo continua como antes', async () => {
    await render();
    await setValue(control<HTMLInputElement>('Nome'), 'João');
    await setValue(control<HTMLInputElement>('Valor por veículo'), '8,00');
    await act(async () => button('Criar afiliado').click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0][1]).toMatchObject({ commissionType: 'fixed', commissionCents: 800, commissionBps: null, planPrices: null });
  });
});

// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '@/api/client';
import { ToastProvider } from '@/components/ui/Toast';

let outcome: () => Promise<void>;
const downloads: string[] = [];
vi.mock('@/api/resources', () => ({
  fulfillmentsApi: {
    downloadLabel: (id: string) => {
      downloads.push(id);
      return outcome();
    },
  },
}));

import { LabelActions } from './LabelActions';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => document.body.textContent ?? '';
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;

async function render(f: { id: string; labelUrl: string; shippingOrderId: string | null }, compact = false) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <ToastProvider>
          <LabelActions fulfillment={f} compact={compact} />
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
}

const bought = { id: 'f1', labelUrl: 'https://melhorenvio.com.br/imprimir/abc', shippingOrderId: 'ord_1' };

afterEach(() => {
  document.body.innerHTML = '';
  downloads.length = 0;
  vi.restoreAllMocks();
});

describe('etiqueta: imprimir e baixar o PDF', () => {
  it('imprime pelo link e baixa o PDF', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null);
    outcome = async () => undefined;
    await render(bought);
    await act(async () => button('Imprimir etiqueta').click());
    expect(open).toHaveBeenCalledWith('https://melhorenvio.com.br/imprimir/abc', '_blank', 'noopener,noreferrer');
    await act(async () => button('Baixar PDF').click());
    await flush();
    expect(downloads).toEqual(['f1']);
    expect(text()).not.toContain('Abrir a etiqueta');
  });

  it('veio a página de impressão: aparece o link para abrir', async () => {
    outcome = async () => {
      throw new ApiError(409, 'página', { code: 'LABEL_NOT_PDF', url: 'https://melhorenvio.com.br/imprimir/novo' });
    };
    await render(bought);
    await act(async () => button('Baixar PDF').click());
    await flush();
    const link = Array.from(document.querySelectorAll('a')).find((a) => a.textContent?.startsWith('Abrir a etiqueta'));
    expect(link?.getAttribute('href')).toBe('https://melhorenvio.com.br/imprimir/novo');
    expect(text()).toContain('Salvar como PDF');
  });

  it('outro erro avisa; sem etiqueta, nada aparece; na lista, botões curtos', async () => {
    outcome = async () => {
      throw new ApiError(502, 'não deu para baixar a etiqueta agora; tente de novo');
    };
    await render(bought, true);
    expect(button('Imprimir')).toBeTruthy();
    await act(async () => button('PDF').click());
    await flush();
    expect(text()).toContain('Não deu para baixar a etiqueta');
    document.body.innerHTML = '';
    await render({ id: 'f2', labelUrl: '', shippingOrderId: null });
    expect(document.querySelectorAll('button')).toHaveLength(0);
  });
});

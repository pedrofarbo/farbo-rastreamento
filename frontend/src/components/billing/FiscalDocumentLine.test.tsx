// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { FiscalDocument } from '@/types';

vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => {} }) }));

import { FiscalDocumentLine, fiscalLabel } from './FiscalDocumentLine';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const doc = (over: Partial<FiscalDocument>): FiscalDocument => ({
  id: 'f1', invoiceId: 'i1', status: 'issued', number: '1234', verificationCode: '3526', amountCents: 6990, hasPdf: true,
  hasXml: true, error: '', issuedAt: '2026-11-05T11:00:00Z', cancelledAt: null, createdAt: '2026-11-05T10:00:00Z', ...over,
});
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label);

async function render(d: FiscalDocument, onRetry?: (id: string) => void) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const onDownload = vi.fn(async () => {});
  await act(async () => createRoot(host).render(<FiscalDocumentLine doc={d} onDownload={onDownload} onRetry={onRetry} />));
  return onDownload;
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('a NFS-e sob a fatura', () => {
  it('cada situação, como a central e o cliente leem', () => {
    expect(fiscalLabel(doc({}))).toBe('NFS-e nº 1234');
    expect(fiscalLabel(doc({ status: 'processing', number: '' }))).toBe('NFS-e em emissão');
    expect(fiscalLabel(doc({ status: 'error', error: 'o cliente está sem CPF ou CNPJ' }))).toBe(
      'NFS-e recusada: o cliente está sem CPF ou CNPJ',
    );
    expect(fiscalLabel(doc({ status: 'blocked' }))).toContain('esperando créditos na Notaas');
    expect(fiscalLabel(doc({ status: 'cancelled' }))).toBe('NFS-e nº 1234 cancelada');
  });

  it('a nota de um pagamento de várias faturas diz quantas cobre', () => {
    expect(fiscalLabel(doc({ invoices: 1 }))).toBe('NFS-e nº 1234');
    expect(fiscalLabel(doc({ invoices: 4 }))).toBe('NFS-e nº 1234 · uma nota para as 4 faturas pagas juntas');
    expect(fiscalLabel(doc({ status: 'processing', number: '', invoices: 3 }))).toBe(
      'NFS-e em emissão · uma nota para as 3 faturas pagas juntas',
    );
  });

  it('a emitida baixa o PDF e o XML; a recusada emite de novo só para a central', async () => {
    const onDownload = await render(doc({}));
    await act(async () => button('Baixar o PDF')!.click());
    expect(onDownload).toHaveBeenCalledWith('f1', 'pdf');
    await act(async () => button('Baixar o XML')!.click());
    expect(onDownload).toHaveBeenCalledWith('f1', 'xml');
    expect(button('Emitir de novo')).toBeUndefined();

    // Cancelada: o XML de emissão continua; o PDF, não.
    document.body.innerHTML = '';
    await render(doc({ status: 'cancelled', hasPdf: false, hasXml: true }));
    expect(button('Baixar o PDF')).toBeUndefined();
    expect(button('Baixar o XML')).toBeDefined();

    document.body.innerHTML = '';
    const onRetry = vi.fn();
    await render(doc({ status: 'error', error: 'o cliente está sem CPF ou CNPJ', hasPdf: false, hasXml: false }), onRetry);
    expect(button('Baixar o PDF')).toBeUndefined();
    expect(button('Baixar o XML')).toBeUndefined();
    await act(async () => button('Emitir de novo')!.click());
    expect(onRetry).toHaveBeenCalledWith('f1');

    document.body.innerHTML = '';
    await render(doc({ status: 'error', error: 'x' }));
    expect(button('Emitir de novo')).toBeUndefined();
  });
});

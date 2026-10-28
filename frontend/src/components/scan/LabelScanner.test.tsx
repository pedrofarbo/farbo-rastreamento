// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import type { Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const IMEI = '869247061230055';
const IMEI2 = '861234567890127';

// O que os leitores (WebAssembly no navegador) "veem" em cada chamada.
const readers = vi.hoisted(() => ({
  barcodes: [] as string[][],
  texts: [] as string[],
  stopped: 0,
}));
vi.mock('@/services/labelReaders', () => ({
  readBarcodeTexts: async () => readers.barcodes.shift() ?? [],
  readText: async () => readers.texts.shift() ?? '',
  stopTextReader: async () => {
    readers.stopped++;
  },
}));

import { LabelScanner } from './LabelScanner';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => document.body.textContent ?? '';
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === label)!;

const track = { stop: vi.fn() };
let getUserMedia: ReturnType<typeof vi.fn>;
let root: Root;

beforeEach(() => {
  readers.barcodes = [];
  readers.texts = [];
  readers.stopped = 0;
  track.stop.mockClear();
  getUserMedia = vi.fn(async () => ({ getTracks: () => [track] }));
  Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: { getUserMedia } });
  // O jsdom não tem vídeo nem canvas de verdade: um quadro de 640×480 e um
  // contexto 2D que só cumpre o protocolo.
  Object.defineProperty(HTMLMediaElement.prototype, 'play', { configurable: true, value: async () => {} });
  Object.defineProperty(HTMLVideoElement.prototype, 'videoWidth', { configurable: true, get: () => 640 });
  Object.defineProperty(HTMLVideoElement.prototype, 'videoHeight', { configurable: true, get: () => 480 });
  Object.defineProperty(HTMLMediaElement.prototype, 'srcObject', { configurable: true, writable: true, value: null });
  const context = {
    translate() {},
    rotate() {},
    drawImage() {},
    getImageData: (_x: number, _y: number, width: number, height: number) => ({ width, height, data: new Uint8ClampedArray(0) }),
  };
  Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', { configurable: true, value: () => context });
  (globalThis as { createImageBitmap?: unknown }).createImageBitmap = async () => ({ width: 3000, height: 4000, close() {} });
});

afterEach(async () => {
  await act(async () => root.unmount());
  document.body.innerHTML = '';
});

async function render(onRead = vi.fn(), kind: 'imei' | 'iccid' = 'imei') {
  const host = document.createElement('div');
  document.body.appendChild(host);
  root = createRoot(host);
  const onClose = vi.fn();
  await act(async () => root.render(<LabelScanner open kind={kind} onClose={onClose} onRead={onRead} />));
  return { onRead, onClose };
}

describe('LabelScanner', () => {
  it('lê o IMEI do código de barras da câmera traseira e desliga a câmera ao fechar', async () => {
    readers.barcodes = [[], [`IMEI:${IMEI}`]];
    const { onRead } = await render();
    await vi.waitFor(() => expect(onRead).toHaveBeenCalledWith(IMEI, 'barcode'), { timeout: 3000 });
    expect(getUserMedia.mock.calls[0][0]).toMatchObject({ video: { facingMode: { ideal: 'environment' } } });
    expect(text()).toContain('Aponte para o código de barras');
    await act(async () => root.unmount());
    expect(track.stop).toHaveBeenCalled();
    await vi.waitFor(() => expect(readers.stopped).toBeGreaterThan(0));
    root = createRoot(document.createElement('div'));
  });

  it('pelo texto, só aceita o número que sai igual em duas leituras', async () => {
    // A 1ª leitura erra um dígito (o verificador recusa), a 2ª acerta, a 3ª confirma.
    readers.texts = ['IMEI 869247061230056', `IMEI ${IMEI}`, 'borrado', `IMEI ${IMEI}`];
    const { onRead } = await render();
    await vi.waitFor(() => expect(onRead).toHaveBeenCalledWith(IMEI, 'text'), { timeout: 3000 });
    expect(onRead).toHaveBeenCalledTimes(1);
    expect(readers.texts).toEqual([]);
  });

  it('com mais de um IMEI na etiqueta, pergunta qual é', async () => {
    readers.barcodes = [[IMEI, IMEI2]];
    const { onRead } = await render();
    await vi.waitFor(() => expect(text()).toContain('Achei mais de um IMEI'), { timeout: 3000 });
    await act(async () => button(IMEI2).click());
    expect(onRead).toHaveBeenCalledWith(IMEI2, 'barcode');
  });

  it('sem permissão para a câmera, explica e lê de uma foto (girada, se precisar)', async () => {
    getUserMedia.mockRejectedValue(new DOMException('negado', 'NotAllowedError'));
    // Nem código de barras nem texto em pé; o texto aparece com a foto girada.
    readers.texts = ['', `ICCID 8955 3202 1000 9379 5348`];
    const { onRead } = await render(vi.fn(), 'iccid');
    await vi.waitFor(() => expect(text()).toContain('O navegador não liberou a câmera'));
    expect(document.querySelector('video')?.closest('[hidden]')).not.toBeNull();

    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    const photo = new File(['x'], 'etiqueta.jpg', { type: 'image/jpeg' });
    Object.defineProperty(input, 'files', { configurable: true, value: [photo] });
    await act(async () => input.dispatchEvent(new Event('change', { bubbles: true })));
    await vi.waitFor(() => expect(onRead).toHaveBeenCalledWith('89553202100093795348', 'text'));
  });

  it('foto sem número válido: avisa e deixa tentar de novo', async () => {
    getUserMedia.mockRejectedValue(new DOMException('', 'NotFoundError'));
    readers.texts = ['SN 2024', '', ''];
    const { onRead } = await render();
    await vi.waitFor(() => expect(text()).toContain('Nenhuma câmera encontrada'));
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    Object.defineProperty(input, 'files', { configurable: true, value: [new File(['x'], 'a.jpg')] });
    await act(async () => input.dispatchEvent(new Event('change', { bubbles: true })));
    await vi.waitFor(() => expect(text()).toContain('Não achei um IMEI válido na foto'));
    expect(onRead).not.toHaveBeenCalled();
    expect(button('Usar uma foto').disabled).toBe(false);
  });
});

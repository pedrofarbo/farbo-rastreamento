import { useEffect, useRef, useState } from 'react';

import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { formatICCID } from '@/services/format';
import { findCodes, SCAN_LABEL } from '@/services/labelScan';
import type { ScanKind, ScanSource } from '@/services/labelScan';

import styles from './LabelScanner.module.css';

type Readers = typeof import('@/services/labelReaders');
// Os leitores (WebAssembly, alguns MB) só descem quando o leitor abre.
const loadReaders = (): Promise<Readers> => import('@/services/labelReaders');

interface LabelScannerProps {
  open: boolean;
  kind: ScanKind;
  onClose: () => void;
  onRead: (code: string, source: ScanSource) => void;
}

// A moldura na imagem da câmera, em fração do quadro: o OCR lê só o que está
// dentro dela (mais rápido, menos texto solto). O código de barras vale no
// quadro inteiro.
const GUIDE = { x: 0.05, y: 0.275, width: 0.9, height: 0.45 };

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
const unique = (codes: string[]) => [...new Set(codes)];

/** A imagem num canvas de até `max` px no lado maior, girada em `rotate` graus. */
function draw(source: CanvasImageSource, width: number, height: number, max: number, rotate = 0): HTMLCanvasElement {
  const scale = Math.min(1, max / Math.max(width, height));
  const w = Math.round(width * scale);
  const h = Math.round(height * scale);
  const canvas = document.createElement('canvas');
  const sideways = rotate % 180 !== 0;
  canvas.width = sideways ? h : w;
  canvas.height = sideways ? w : h;
  const context = canvas.getContext('2d', { willReadFrequently: true })!;
  context.translate(canvas.width / 2, canvas.height / 2);
  context.rotate((rotate * Math.PI) / 180);
  context.drawImage(source, -w / 2, -h / 2, w, h);
  return canvas;
}

function crop(canvas: HTMLCanvasElement, area: typeof GUIDE): HTMLCanvasElement {
  const out = document.createElement('canvas');
  out.width = Math.round(canvas.width * area.width);
  out.height = Math.round(canvas.height * area.height);
  out
    .getContext('2d')!
    .drawImage(canvas, canvas.width * area.x, canvas.height * area.y, out.width, out.height, 0, 0, out.width, out.height);
  return out;
}

const pixels = (canvas: HTMLCanvasElement) => canvas.getContext('2d')!.getImageData(0, 0, canvas.width, canvas.height);

function cameraProblem(error: unknown): string {
  const name = error instanceof DOMException ? error.name : '';
  if (name === 'NotAllowedError' || name === 'SecurityError') {
    return 'O navegador não liberou a câmera. Libere para este site (no cadeado ao lado do endereço) e abra de novo, ou use uma foto.';
  }
  if (name === 'NotFoundError' || name === 'OverconstrainedError') return 'Nenhuma câmera encontrada neste aparelho. Use uma foto da etiqueta.';
  if (name === 'NotReadableError') return 'A câmera está ocupada por outro programa. Feche-o e abra de novo, ou use uma foto.';
  if (name === 'NotSupportedError') return 'Este navegador não abre a câmera nesta página. Use uma foto da etiqueta.';
  return 'Não deu para abrir a câmera. Use uma foto da etiqueta.';
}

/**
 * Lê o IMEI (etiqueta do rastreador) ou o ICCID (chip) pela câmera: procura o
 * código de barras em cada quadro e, ao mesmo tempo, o número impresso dentro
 * da moldura (OCR). Só aceita número válido (dígito verificador); pelo texto,
 * o mesmo número precisa sair em duas leituras seguidas, porque o OCR erra
 * dígito. Sem câmera (ou sem permissão), lê de uma foto.
 */
export function LabelScanner({ open, kind, onClose, onRead }: LabelScannerProps) {
  const label = SCAN_LABEL[kind];
  const videoRef = useRef<HTMLVideoElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const [camera, setCamera] = useState<'starting' | 'on' | 'off'>('starting');
  const [problem, setProblem] = useState('');
  const [photoBusy, setPhotoBusy] = useState(false);
  const [choices, setChoices] = useState<{ codes: string[]; source: ScanSource } | null>(null);
  // Achou (ou está esperando a escolha): a câmera para de procurar.
  const done = useRef(false);
  // Lendo uma foto: a câmera espera, para o OCR não dividir a fila.
  const paused = useRef(false);
  const onReadRef = useRef(onRead);
  onReadRef.current = onRead;

  const found = (codes: string[], source: ScanSource): boolean => {
    if (done.current || codes.length === 0) return false;
    done.current = true;
    if (codes.length === 1) onReadRef.current(codes[0], source);
    else setChoices({ codes, source });
    return true;
  };

  useEffect(() => {
    if (!open) return;
    done.current = false;
    paused.current = false;
    setChoices(null);
    setProblem('');
    setCamera('starting');
    let stopped = false;
    let stream: MediaStream | null = null;
    const readers = loadReaders();
    readers.catch(() => undefined);

    void (async () => {
      try {
        if (!navigator.mediaDevices?.getUserMedia) throw new DOMException('sem getUserMedia', 'NotSupportedError');
        stream = await navigator.mediaDevices.getUserMedia({
          audio: false,
          video: { facingMode: { ideal: 'environment' }, width: { ideal: 1920 }, height: { ideal: 1080 } },
        });
      } catch (error) {
        if (!stopped) {
          setCamera('off');
          setProblem(cameraProblem(error));
        }
        return;
      }
      const video = videoRef.current;
      if (stopped || !video) {
        stream.getTracks().forEach((track) => track.stop());
        return;
      }
      video.srcObject = stream;
      await video.play().catch(() => undefined);
      setCamera('on');

      let reader: Readers;
      try {
        reader = await readers;
      } catch {
        if (!stopped) setProblem('Não deu para carregar o leitor. Confira a internet e abra de novo.');
        return;
      }
      // Quantas leituras de OCR deram cada número.
      const votes = new Map<string, number>();
      let ocrRunning = false;
      while (!stopped) {
        if (!done.current && !paused.current && video.videoWidth > 0) {
          const frame = draw(video, video.videoWidth, video.videoHeight, 1280);
          const texts = await reader.readBarcodeTexts(pixels(frame)).catch(() => [] as string[]);
          if (stopped) break;
          if (found(unique(texts.flatMap((text) => findCodes(text, kind))), 'barcode')) continue;
          if (!ocrRunning) {
            ocrRunning = true;
            reader
              .readText(crop(frame, GUIDE))
              .then((text) => {
                if (stopped || done.current) return;
                const codes = findCodes(text, kind);
                codes.forEach((code) => votes.set(code, (votes.get(code) ?? 0) + 1));
                found(
                  codes.filter((code) => (votes.get(code) ?? 0) >= 2),
                  'text',
                );
              })
              .catch(() => undefined)
              .finally(() => {
                ocrRunning = false;
              });
          }
        }
        await sleep(150);
      }
    })();

    return () => {
      stopped = true;
      stream?.getTracks().forEach((track) => track.stop());
      void readers.then((r) => r.stopTextReader()).catch(() => undefined);
    };
  }, [open, kind]);

  const readPhoto = async (file: File) => {
    setPhotoBusy(true);
    setProblem('');
    setChoices(null);
    done.current = false;
    paused.current = true;
    try {
      const reader = await loadReaders();
      const bitmap = await createImageBitmap(file);
      const photo = draw(bitmap, bitmap.width, bitmap.height, 2000);
      bitmap.close();
      const texts = await reader.readBarcodeTexts(pixels(photo));
      if (found(unique(texts.flatMap((text) => findCodes(text, kind))), 'barcode')) return;
      // A foto pode ter saído de lado: o OCR só lê texto em pé.
      for (const rotate of [0, 90, 270]) {
        const image = rotate ? draw(photo, photo.width, photo.height, 2000, rotate) : photo;
        if (found(findCodes(await reader.readText(image), kind), 'text')) return;
      }
      setProblem(`Não achei um ${label} válido na foto. Tente mais perto, com luz e sem reflexo, ou digite o número.`);
    } catch {
      setProblem('Não deu para ler a foto. Tente outra, ou digite o número.');
    } finally {
      paused.current = false;
      setPhotoBusy(false);
    }
  };

  const status = photoBusy
    ? 'Lendo a foto…'
    : camera === 'starting'
      ? 'Abrindo a câmera…'
      : camera === 'on'
        ? `Aponte para o código de barras ou para o número do ${label}, dentro da moldura. A leitura é automática.`
        : '';

  return (
    <Modal
      open={open}
      title={`Ler o ${label}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button loading={photoBusy} onClick={() => fileRef.current?.click()}>
            Usar uma foto
          </Button>
        </>
      }
    >
      <div className={styles.scanner}>
        {/* O vídeo fica montado mesmo escondido: é nele que a câmera está ligada. */}
        <div className={styles.viewer} hidden={camera === 'off' || choices !== null}>
          <video ref={videoRef} className={styles.video} muted playsInline aria-label="Imagem da câmera" />
          {camera === 'on' && <div className={styles.guide} aria-hidden="true" />}
        </div>
        {choices && (
          <div className={styles.choices}>
            <p>
              Achei mais de um {label}. Qual é o {kind === 'imei' ? 'deste rastreador' : 'deste chip'}?
            </p>
            {choices.codes.map((code) => (
              <Button key={code} block onClick={() => onReadRef.current(code, choices.source)}>
                <span className={styles.code}>{kind === 'iccid' ? formatICCID(code) : code}</span>
              </Button>
            ))}
            <Button
              variant="ghost"
              onClick={() => {
                setChoices(null);
                done.current = false;
              }}
            >
              Ler de novo
            </Button>
          </div>
        )}
        <p className={styles.status} role="status" aria-live="polite">
          {status}
        </p>
        {problem && (
          <p className={styles.problem} role="alert">
            {problem}
          </p>
        )}
        <input
          ref={fileRef}
          type="file"
          accept="image/*"
          hidden
          onChange={(event) => {
            const file = event.target.files?.[0];
            event.target.value = '';
            if (file) void readPhoto(file);
          }}
        />
      </div>
    </Modal>
  );
}

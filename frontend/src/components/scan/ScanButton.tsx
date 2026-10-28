import { useState } from 'react';

import { fieldStyles } from '@/components/ui/Field';
import { useToast } from '@/components/ui/Toast';
import { SCAN_LABEL } from '@/services/labelScan';
import type { ScanKind } from '@/services/labelScan';

import { LabelScanner } from './LabelScanner';

/**
 * O botão dentro do campo (action do TextField) que abre a câmera e preenche o
 * campo com o IMEI ou o ICCID lido da etiqueta.
 */
export function ScanButton({ kind, onRead }: { kind: ScanKind; onRead: (code: string) => void }) {
  const [open, setOpen] = useState(false);
  const { notify } = useToast();
  const label = SCAN_LABEL[kind];
  return (
    <>
      <button
        type="button"
        className={fieldStyles.adornment}
        aria-label={`Ler o ${label} pela câmera`}
        title={`Ler o ${label} pela câmera (código de barras ou texto da etiqueta)`}
        onClick={() => setOpen(true)}
      >
        <svg
          width={18}
          height={18}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={2}
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M3 7V5a2 2 0 0 1 2-2h2M17 3h2a2 2 0 0 1 2 2v2M21 17v2a2 2 0 0 1-2 2h-2M7 21H5a2 2 0 0 1-2-2v-2" />
          <path d="M7 8v8M10.5 8v8M14 8v8M17 8v8" />
        </svg>
      </button>
      <LabelScanner
        open={open}
        kind={kind}
        onClose={() => setOpen(false)}
        onRead={(code, source) => {
          setOpen(false);
          onRead(code);
          notify({
            tone: 'success',
            title: `${label} lido ${source === 'barcode' ? 'do código de barras' : 'do texto da etiqueta'}`,
            description: 'Confira com a etiqueta antes de salvar.',
          });
        }}
      />
    </>
  );
}

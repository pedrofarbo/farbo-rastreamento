import { useEffect, useState } from 'react';

import { leadsApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { TextField } from '@/components/ui/Field';
import { useToast } from '@/components/ui/Toast';
import { eventSlug, eventUrl } from '@/config/landing';

import styles from './EventQr.module.css';

/**
 * O QR Code do estande: o nome do evento vira o link da tela de cadastro
 * (/evento/<nome>), e quem se inscreve por ele aparece na lista com o evento.
 */
export function EventQrCard() {
  const { notify } = useToast();
  const [name, setName] = useState('');
  const [debounced, setDebounced] = useState('');
  const [preview, setPreview] = useState('');
  const link = eventUrl(debounced);
  const slug = eventSlug(debounced);

  // Espera parar de digitar para pedir o QR.
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(name), 400);
    return () => clearTimeout(timer);
  }, [name]);

  useEffect(() => {
    let url = '';
    let cancelled = false;
    leadsApi
      .qrImage(link)
      .then((blob) => {
        if (cancelled) return;
        url = URL.createObjectURL(blob);
        setPreview(url);
      })
      .catch(() => !cancelled && setPreview(''));
    return () => {
      cancelled = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, [link]);

  const save = (format: 'svg' | 'png') =>
    leadsApi
      .downloadQr(link, slug || 'pre-lancamento', format)
      .catch((err: Error) => notify({ tone: 'error', title: 'Não foi possível baixar', description: err.message }));

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(link);
      notify({ tone: 'success', title: 'Link copiado', description: link });
    } catch {
      notify({ tone: 'error', title: 'Não deu para copiar', description: link });
    }
  };

  return (
    <Card
      title="QR Code para eventos"
      subtitle="Abre no celular a tela de cadastro no pré-lançamento. Quem se inscreve por ele aparece aqui com o nome do evento."
    >
      <div className={styles.layout}>
        <div className={styles.form}>
          <TextField
            label="Nome do evento"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Ex.: Encontro Insanos MC — Out/26"
            maxLength={80}
            hint="Um QR Code por evento, para saber quantos vieram de cada um. Em branco: um QR Code geral."
          />
          <div className={styles.link}>
            <span className={styles.linkText}>{link}</span>
            <span className={styles.linkActions}>
              <Button size="small" variant="ghost" onClick={() => void copy()}>
                Copiar link
              </Button>
              <a className={styles.open} href={link} target="_blank" rel="noopener noreferrer">
                Abrir
              </a>
            </span>
          </div>
          <div className={styles.buttons}>
            <Button variant="primary" onClick={() => void save('svg')}>
              Baixar SVG (para a gráfica)
            </Button>
            <Button onClick={() => void save('png')}>Baixar PNG</Button>
          </div>
          <p className={styles.tip}>
            Imprima com pelo menos 3 × 3 cm (para ler a mais de 1 metro, uns 10 cm) e sem cortar a margem branca.
            Antes de imprimir, aponte a câmera do celular para a tela e confira se abre a página.
          </p>
        </div>
        <div className={styles.preview} aria-live="polite">
          {preview ? <img src={preview} alt={`QR Code de ${link}`} width={220} height={220} /> : <span>Gerando…</span>}
        </div>
      </div>
    </Card>
  );
}

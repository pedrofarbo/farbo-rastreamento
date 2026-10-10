import { useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { publicApi } from '@/api/resources';
import { referralUrl } from '@/services/referral';
import type { PartnerReport } from '@/types';

import { FLYER_FORMATS, drawFlyer, flyerFileName, jpegToPdf } from './partner/flyer';
import type { FlyerAssets, FlyerFormat } from './partner/flyer';

import styles from './Partner.module.css';
import base from './PreLaunch.module.css';

const FORMATS: FlyerFormat[] = ['a5', 'stories', 'post'];

/** Carrega a imagem (do próprio site: o canvas pode virar arquivo). */
function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error(`não carregou ${src}`));
    img.src = src;
  });
}

/** Espera a fonte do site (o flyer usa a Inter), sem travar se ela não vier. */
async function loadFonts() {
  if (typeof document === 'undefined' || !document.fonts?.load) return;
  const weights = [500, 600, 700, 800, 900];
  await Promise.race([
    Promise.all(weights.map((w) => document.fonts.load(`${w} 40px Inter`))),
    new Promise((resolve) => setTimeout(resolve, 3000)),
  ]).catch(() => undefined);
}

function toBlob(canvas: HTMLCanvasElement, type: string, quality?: number): Promise<Blob> {
  return new Promise((resolve, reject) =>
    canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('não deu para gerar a imagem'))), type, quality),
  );
}

function save(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = name;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

/** O ícone do Instagram (a câmera), na cor do texto. */
function InstagramIcon() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <rect x="3" y="3" width="18" height="18" rx="5" />
      <circle cx="12" cy="12" r="4.2" />
      <circle cx="17.4" cy="6.6" r="1" fill="currentColor" stroke="none" />
    </svg>
  );
}

/**
 * O gerador de flyer do afiliado (/parceiro/<link secreto>/flyer): a oferta
 * do site com o QR Code do link de indicação dele, em A5 para a gráfica (PDF
 * ou PNG), stories ou post. Tudo é feito no aparelho; o QR vem do servidor.
 */
export function PartnerFlyerPage() {
  const { token = '' } = useParams();
  const [report, setReport] = useState<PartnerReport | null>(null);
  const [error, setError] = useState('');
  const [assets, setAssets] = useState<FlyerAssets | null>(null);
  const [format, setFormat] = useState<FlyerFormat>('a5');
  const [showReferrer, setShowReferrer] = useState(true);
  const [busy, setBusy] = useState(false);
  // A imagem do formato atual já pronta: compartilhar precisa sair no toque,
  // sem esperar o canvas virar arquivo (o Safari recusa depois de esperar).
  const [file, setFile] = useState<File | null>(null);
  const [notice, setNotice] = useState('');
  const canvas = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    document.title = 'Seu flyer · Farbo Rastreadores';
    const metas = [
      Object.assign(document.createElement('meta'), { name: 'robots', content: 'noindex, nofollow' }),
      Object.assign(document.createElement('meta'), { name: 'referrer', content: 'no-referrer' }),
    ];
    metas.forEach((m) => document.head.appendChild(m));
    return () => metas.forEach((m) => m.remove());
  }, []);

  useEffect(() => {
    let alive = true;
    publicApi
      .partner(token)
      .then((r) => alive && setReport(r))
      .catch((err) => {
        if (!alive) return;
        setError(
          err instanceof ApiError && err.status === 404
            ? 'Este link não vale mais. Peça o link novo à Farbo.'
            : 'Não deu para carregar agora. Tente de novo em instantes.',
        );
      });
    return () => {
      alive = false;
    };
  }, [token]);

  // O logo, o QR e a fonte, uma vez.
  useEffect(() => {
    if (!report?.active) return;
    let alive = true;
    Promise.all([loadImage('/assets/logo-header.png'), loadImage(publicApi.partnerQrUrl(token, 'png')), loadFonts()])
      .then(([logo, qr]) => alive && setAssets({ logo, qr }))
      .catch(() => alive && setError('Não deu para montar o flyer agora. Tente de novo em instantes.'));
    return () => {
      alive = false;
    };
  }, [report?.active, token]);

  const referrer = report ? (report.handle ? `@${report.handle}` : report.name) : '';
  const spec = FLYER_FORMATS[format];

  useEffect(() => {
    const el = canvas.current;
    if (!el || !assets || !report) return;
    el.width = spec.width;
    el.height = spec.height;
    const ctx = el.getContext('2d');
    if (!ctx) return;
    drawFlyer(ctx, format, { link: referralUrl(report.code), referrer: showReferrer ? referrer : '' }, assets);
    setFile(null);
    setNotice('');
    let alive = true;
    toBlob(el, 'image/png')
      .then((blob) => alive && setFile(new File([blob], flyerFileName(report.code, format, 'png'), { type: 'image/png' })))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [assets, report, format, showReferrer, referrer, spec]);

  const download = async (kind: 'png' | 'pdf') => {
    const el = canvas.current;
    if (!el || !report) return;
    setBusy(true);
    try {
      if (kind === 'pdf' && spec.pdf) {
        const jpeg = new Uint8Array(await (await toBlob(el, 'image/jpeg', 0.95)).arrayBuffer());
        const pdf = jpegToPdf(jpeg, el.width, el.height, spec.pdf.width, spec.pdf.height);
        save(new Blob([pdf], { type: 'application/pdf' }), flyerFileName(report.code, format, 'pdf'));
      } else {
        save(await toBlob(el, 'image/png'), flyerFileName(report.code, format, 'png'));
      }
    } finally {
      setBusy(false);
    }
  };

  // No celular, a imagem vai direto para o Instagram, o WhatsApp etc. pela
  // tela de compartilhar do aparelho.
  const canShareFiles =
    typeof navigator !== 'undefined' &&
    typeof navigator.canShare === 'function' &&
    navigator.canShare({ files: [new File([new Uint8Array(1)], 'x.png', { type: 'image/png' })] });
  const link = report ? referralUrl(report.code) : '';
  const copyLink = () => navigator.clipboard?.writeText(link).catch(() => undefined);

  const share = () => {
    if (!file) return;
    void navigator.share({ files: [file], text: link }).catch(() => undefined);
  };

  // O Instagram não aceita post vindo de site: no celular, a imagem vai pela
  // tela de compartilhar (lá se escolhe o Instagram) com o link já copiado
  // para o adesivo de link do stories; no computador, baixa a imagem e abre
  // o Instagram para postar no feed.
  const shareInstagram = () => {
    if (!file) return;
    void copyLink();
    if (canShareFiles) {
      setNotice(
        format === 'stories'
          ? 'Escolha o Instagram e depois Stories. O seu link já está copiado: toque no adesivo “Link” e cole.'
          : 'Escolha o Instagram e depois Feed. O seu link já está copiado: cole na legenda ou na bio.',
      );
      void navigator.share({ files: [file] }).catch(() => undefined);
      return;
    }
    save(file, file.name);
    window.open('https://www.instagram.com/', '_blank', 'noopener,noreferrer');
    setNotice(
      'Baixamos a imagem e copiamos o seu link. No Instagram, clique em Criar, escolha a imagem e cole o link na legenda. Stories só pelo celular: abra esta página no celular.',
    );
  };

  return (
    <main className={base.page}>
      <header className={base.header}>
        <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={base.logo} />
        <span className={base.badge}>Parceiro</span>
      </header>

      {error ? (
        <p className={base.error} role="alert">
          {error}
        </p>
      ) : !report ? (
        <p className={base.hint} role="status">
          Carregando…
        </p>
      ) : !report.active ? (
        <p className={base.error}>Seu link está pausado: o flyer volta quando ele for reativado. Fale com a Farbo.</p>
      ) : (
        <>
          <section className={base.intro}>
            <h1 className={base.title}>Seu flyer</h1>
            <p className={base.lead}>
              A oferta da Farbo com o QR Code do seu link: quem aponta a câmera cai direto no seu cadastro.
            </p>
          </section>

          <div className={styles.formats} role="radiogroup" aria-label="Formato">
            {FORMATS.map((f) => (
              <button
                key={f}
                type="button"
                role="radio"
                aria-checked={format === f}
                className={`${styles.format} ${format === f ? styles.formatActive : ''}`}
                onClick={() => setFormat(f)}
              >
                <strong>{FLYER_FORMATS[f].label}</strong>
                <span>{FLYER_FORMATS[f].hint}</span>
              </button>
            ))}
          </div>

          <label className={styles.option}>
            <input type="checkbox" checked={showReferrer} onChange={(e) => setShowReferrer(e.target.checked)} />
            Mostrar “Indicado por {referrer}”
          </label>

          <div className={styles.preview}>
            {assets ? (
              <canvas
                ref={canvas}
                className={styles.previewCanvas}
                style={{ aspectRatio: `${spec.width} / ${spec.height}` }}
                aria-label={`Prévia do ${spec.label}`}
              />
            ) : (
              <p className={base.hint} role="status">
                Montando o flyer…
              </p>
            )}
          </div>

          <div className={styles.linkActions}>
            {spec.pdf ? (
              <button type="button" className={base.primary} disabled={!assets || busy} onClick={() => void download('pdf')}>
                Baixar PDF (para gráfica)
              </button>
            ) : (
              <button type="button" className={styles.instagram} disabled={!file} onClick={shareInstagram}>
                <InstagramIcon />
                Compartilhar no Instagram
              </button>
            )}
            <button type="button" className={base.secondary} disabled={!assets || busy} onClick={() => void download('png')}>
              Baixar PNG
            </button>
            {canShareFiles && !spec.pdf && (
              <button type="button" className={base.secondary} disabled={!file} onClick={share}>
                Compartilhar em outro app
              </button>
            )}
          </div>
          {notice && (
            <p className={styles.notice} role="status">
              {notice}
            </p>
          )}
          <p className={styles.qrHint}>
            {spec.pdf
              ? 'O PDF sai no tamanho A5 (14,8 × 21 cm), em alta resolução: é só mandar para a gráfica.'
              : 'Poste e deixe o QR à vista: no stories, dá também para colar o seu link no adesivo de link.'}
          </p>

          <Link to={`/parceiro/${token}`} className={styles.back}>
            ← Voltar para as suas indicações
          </Link>
        </>
      )}
    </main>
  );
}

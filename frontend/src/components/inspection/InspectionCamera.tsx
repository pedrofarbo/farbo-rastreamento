import { useEffect, useRef, useState } from 'react';

import { cameraProblem, stampFile, stampedJpeg } from '@/services/inspectionCamera';
import type { InspectionPhotoSource, InspectionSlotDef } from '@/types';

import styles from './InspectionCamera.module.css';

interface InspectionCameraProps {
  /** O ângulo da vez (null: fechada). */
  slot: InspectionSlotDef | null;
  /** As linhas da marca, na hora da foto. */
  stamp: () => string[];
  onClose: () => void;
  /** Sobe a foto; a câmera fecha quando der certo. */
  onShot: (photo: Blob, source: InspectionPhotoSource) => Promise<void>;
}

type Phase = 'starting' | 'live' | 'preview' | 'off';

/**
 * A câmera da vistoria, em tela cheia: a imagem ao vivo com o ângulo e a
 * dica em cima, o botão de tirar, a foto para conferir (usar ou tirar de
 * novo). A foto sai do vídeo, não da galeria. Sem a câmera no navegador, o
 * seletor de arquivos fica como último recurso (a conferência marca).
 */
export function InspectionCamera({ slot, stamp, onClose, onShot }: InspectionCameraProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [phase, setPhase] = useState<Phase>('starting');
  const [problem, setProblem] = useState('');
  const [shot, setShot] = useState<{ blob: Blob; url: string; source: InspectionPhotoSource } | null>(null);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState('');
  const open = slot !== null;

  useEffect(() => {
    if (!open) return;
    setPhase('starting');
    setProblem('');
    setError('');
    let stopped = false;
    let stream: MediaStream | null = null;
    void (async () => {
      try {
        if (!navigator.mediaDevices?.getUserMedia) throw new DOMException('sem getUserMedia', 'NotSupportedError');
        stream = await navigator.mediaDevices.getUserMedia({
          audio: false,
          video: { facingMode: { ideal: 'environment' }, width: { ideal: 1920 }, height: { ideal: 1080 } },
        });
      } catch (err) {
        if (!stopped) {
          setPhase('off');
          setProblem(cameraProblem(err));
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
      if (!stopped) setPhase('live');
    })();
    return () => {
      stopped = true;
      stream?.getTracks().forEach((track) => track.stop());
    };
  }, [open, slot?.id]);

  useEffect(() => () => {
    if (shot) URL.revokeObjectURL(shot.url);
  }, [shot]);

  if (!slot) return null;

  const keep = (blob: Blob, source: InspectionPhotoSource) => setShot({ blob, url: URL.createObjectURL(blob), source });

  const capture = async () => {
    const video = videoRef.current;
    if (!video || video.videoWidth === 0) return;
    try {
      keep(await stampedJpeg(video, video.videoWidth, video.videoHeight, stamp()), 'camera');
      setPhase('preview');
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const use = async () => {
    if (!shot) return;
    setSending(true);
    setError('');
    try {
      await onShot(shot.blob, shot.source);
      setShot(null);
    } catch (err) {
      setError((err as Error).message || 'A foto não subiu: confira a internet e tente de novo.');
    } finally {
      setSending(false);
    }
  };

  const again = () => {
    setShot(null);
    setError('');
    setPhase(videoRef.current?.srcObject ? 'live' : 'off');
  };

  return (
    <div className={styles.overlay} role="dialog" aria-modal="true" aria-label={`Foto: ${slot.label}`}>
      <div className={styles.guide}>
        <strong>{slot.label}</strong>
        <span>{slot.hint}</span>
      </div>

      <div className={styles.stage}>
        <video ref={videoRef} className={styles.video} playsInline muted hidden={phase === 'preview' || phase === 'off'} />
        {phase === 'preview' && shot && <img className={styles.video} src={shot.url} alt={`Foto: ${slot.label}`} />}
        {phase === 'starting' && <p className={styles.message}>Abrindo a câmera…</p>}
        {phase === 'off' && (
          <div className={styles.message}>
            <p>{problem}</p>
            <label className={styles.fallback}>
              <input
                type="file"
                accept="image/*"
                capture="environment"
                onChange={(e) => {
                  const file = e.target.files?.[0];
                  e.target.value = '';
                  if (!file) return;
                  void stampFile(file, stamp()).then((blob) => {
                    keep(blob, 'file');
                    setPhase('preview');
                  });
                }}
              />
              Tirar pela câmera do celular
            </label>
            <small>A foto escolhida assim fica marcada para quem confere a vistoria.</small>
          </div>
        )}
      </div>

      {error && <p className={styles.error}>{error}</p>}
      <div className={styles.bar}>
        {phase === 'preview' ? (
          <>
            <button type="button" className={styles.secondary} disabled={sending} onClick={again}>
              Tirar de novo
            </button>
            <button type="button" className={styles.primary} disabled={sending} onClick={() => void use()}>
              {sending ? 'Enviando…' : 'Usar esta foto'}
            </button>
          </>
        ) : (
          <>
            <button type="button" className={styles.secondary} onClick={onClose}>
              Voltar
            </button>
            <button
              type="button"
              className={styles.shutter}
              aria-label="Tirar a foto"
              disabled={phase !== 'live'}
              onClick={() => void capture()}
            />
            <span className={styles.spacer} />
          </>
        )}
      </div>
    </div>
  );
}

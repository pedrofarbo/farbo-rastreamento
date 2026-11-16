import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { inspectionLinkApi } from '@/api/resources';
import { InspectionCamera } from '@/components/inspection/InspectionCamera';
import { positionText, usePositionWatch } from '@/components/inspection/usePositionWatch';
import { formatDateTime } from '@/services/format';
import { watermarkLines } from '@/services/inspectionCamera';
import { inspectionKindLabel, missingSlots, photosLabel } from '@/services/occurrences';
import type { InspectionPhotoSource, InspectionSlotDef, PublicInspection } from '@/types';

import { Shell } from './InstallerPage';
import local from './InstallerPage.module.css';
import base from './PreLaunch.module.css';
import { useNoIndex } from './PublicLocation';
import page from './PublicInspection.module.css';
import styles from './PublicTheft.module.css';

/**
 * O link da vistoria (/vistoria/<link secreto>), sem login: quem recebeu —
 * o associado, quem vai entrar na associação ou o técnico — tira pela
 * câmera ao vivo a foto de cada ângulo do roteiro, com a localização,
 * declara as avarias que o veículo já tem e envia. Devolvida para refazer, o
 * mesmo link abre a refação, com o motivo e as fotos que ficaram boas. Só o
 * primeiro nome.
 */
export function PublicInspectionPage() {
  const { token = '' } = useParams();
  useNoIndex('Vistoria · Farbo Rastreadores');
  const [view, setView] = useState<PublicInspection | null>(null);
  const [loadError, setLoadError] = useState('');
  const [error, setError] = useState('');
  const [shooting, setShooting] = useState<InspectionSlotDef | null>(null);
  const [version, setVersion] = useState('');
  const [damages, setDamages] = useState('');
  const [accept, setAccept] = useState(false);
  const [sending, setSending] = useState(false);

  const open = view?.status === 'pending' && !view.expired;
  const position = usePositionWatch(open);

  const load = async () => {
    try {
      setView(await inspectionLinkApi.view(token));
      setLoadError('');
    } catch (err) {
      setLoadError(
        err instanceof ApiError && err.status === 404
          ? 'Link não encontrado: confira o endereço que você recebeu.'
          : 'Não deu para abrir a vistoria agora. Confira a internet e tente de novo.',
      );
    }
  };

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  if (loadError) {
    return (
      <Shell label="Vistoria">
        <section className={styles.card}>
          <p className={base.error}>{loadError}</p>
        </section>
      </Shell>
    );
  }
  if (!view) {
    return (
      <Shell label="Vistoria">
        <p className={styles.muted}>Abrindo a vistoria…</p>
      </Shell>
    );
  }

  const technician = view.mode === 'technician';
  const taken = new Set(view.photos.map((p) => p.slot));
  const missing = missingSlots(view.slots, view.photos);
  const vehicle = view.plate || view.vehicleName;

  const upload = async (slot: InspectionSlotDef, photo: Blob, source: InspectionPhotoSource) => {
    const fix = position.current();
    await inspectionLinkApi.savePhoto(token, slot.id, photo, {
      position: fix ? { latitude: fix.latitude, longitude: fix.longitude, accuracy: fix.accuracy } : null,
      source,
    });
    setView((v) => (v && !v.photos.some((p) => p.slot === slot.id) ? { ...v, photos: [...v.photos, { slot: slot.id, takenAt: new Date().toISOString() }] } : v));
    setVersion(String(Date.now()));
    setShooting(null);
  };

  const submit = async () => {
    setSending(true);
    setError('');
    try {
      setView(await inspectionLinkApi.submit(token, damages));
    } catch (err) {
      setError((err as Error).message);
      void load();
    } finally {
      setSending(false);
    }
  };

  const header = (
    <section className={styles.card}>
      <span className={styles.muted}>
        {view.organizationName} · vistoria de {inspectionKindLabel(view.kind).toLowerCase()}
        {technician ? ' com técnico' : ''}
      </span>
      <h1 className={styles.title}>{view.vehicleName}</h1>
      {view.plate && <span className={styles.plate}>{view.plate}</span>}
      {!technician && view.firstName && <p className={styles.text}>Olá, {view.firstName}!</p>}
      {view.requestNote && <p className={styles.text}>📝 {view.requestNote}</p>}
    </section>
  );

  if (view.status !== 'pending' || view.expired) {
    const message =
      view.status === 'canceled'
        ? `A vistoria foi cancelada por ${view.organizationName}.`
        : view.expired
          ? `O link venceu. Peça um novo a ${view.organizationName}.`
          : view.status === 'approved'
            ? `✅ Vistoria aprovada por ${view.organizationName}. Obrigado!`
            : `✅ Vistoria enviada${view.submittedAt ? ` em ${formatDateTime(view.submittedAt)}` : ''}. ${view.organizationName} vai conferir as fotos; se precisar refazer alguma, este mesmo link volta a abrir com o motivo.`;
    return (
      <Shell label="Vistoria">
        {header}
        <section className={styles.card}>
          <p className={styles.text}>{message}</p>
        </section>
      </Shell>
    );
  }

  return (
    <Shell label="Vistoria">
      {header}
      {error && <p className={base.error}>{error}</p>}

      <section className={`${styles.card} ${position.state === 'on' ? local.done : ''}`}>
        <h2 className={local.step}>
          <span className={local.number}>1</span> Antes de começar
        </h2>
        <ul className={page.tips}>
          <li>Fique perto do veículo, num lugar com boa luz (de dia, se der).</li>
          <li>Cada foto abre a câmera: enquadre o que a tela pede e confira antes de usar.</li>
          {view.slots.some((s) => s.id === 'document') && <li>Deixe o documento do veículo (CRLV) à mão.</li>}
          {view.slots.some((s) => s.id === 'dashboard') && <li>Para o painel, ligue o veículo.</li>}
        </ul>
        <p className={position.state === 'denied' ? base.error : styles.muted}>{positionText(position.state, position.accuracy)}</p>
      </section>

      <section className={`${styles.card} ${missing.length === 0 ? local.done : ''}`}>
        <h2 className={local.step}>
          <span className={local.number}>2</span> Fotos · {photosLabel(view.slots, view.photos)}
        </h2>
        <div className={local.slots}>
          {view.slots.map((slot) => (
            <button
              key={slot.id}
              type="button"
              className={`${local.slot} ${page.slotButton} ${taken.has(slot.id) ? local.slotTaken : ''}`}
              onClick={() => setShooting(slot)}
            >
              {taken.has(slot.id) ? (
                <img className={local.photo} src={inspectionLinkApi.photoUrl(token, slot.id, version)} alt={slot.label} />
              ) : (
                <span className={local.placeholder} aria-hidden="true">
                  📷
                </span>
              )}
              <span className={local.slotLabel}>
                {slot.label}
                {taken.has(slot.id) ? ' ✓' : slot.optional ? ' (se houver)' : ''}
              </span>
              <span className={local.slotHint}>{taken.has(slot.id) ? 'Toque para tirar de novo' : slot.hint}</span>
            </button>
          ))}
        </div>
      </section>

      <section className={styles.card}>
        <h2 className={local.step}>
          <span className={local.number}>3</span> Enviar
        </h2>
        <textarea
          className={local.textarea}
          maxLength={1000}
          value={damages}
          aria-label="Avarias que o veículo já tem"
          placeholder="Avarias que o veículo já tem (opcional): riscos, amassados, vidro trincado, pneu gasto…"
          onChange={(e) => setDamages(e.target.value)}
        />
        <label className={page.accept}>
          <input type="checkbox" checked={accept} onChange={(e) => setAccept(e.target.checked)} />
          <span>
            {technician
              ? `Declaro que fiz pessoalmente a vistoria do veículo ${vehicle} e que as fotos foram tiradas agora.`
              : `Declaro que as fotos são do veículo ${vehicle}, foram tiradas agora por mim e que as avarias que ele já tem estão descritas acima.`}
          </span>
        </label>
        {missing.length > 0 && (
          <p className={styles.muted}>Faltam as fotos: {missing.map((s) => s.label.toLowerCase()).join(', ')}.</p>
        )}
        <button
          type="button"
          className={`${base.primary} ${local.action}`}
          disabled={missing.length > 0 || !accept || sending}
          onClick={() => void submit()}
        >
          {sending ? 'Enviando…' : 'Enviar a vistoria'}
        </button>
      </section>

      <InspectionCamera
        slot={shooting}
        stamp={() => watermarkLines(new Date(), view.plate, position.current())}
        onClose={() => setShooting(null)}
        onShot={(photo, source) => (shooting ? upload(shooting, photo, source) : Promise.resolve())}
      />
    </Shell>
  );
}

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from 'react-router-dom';

import { meApi } from '@/api/resources';
import { AuthImage } from '@/components/media/AuthImage';
import { InspectionCamera } from '@/components/inspection/InspectionCamera';
import { positionText, usePositionWatch } from '@/components/inspection/usePositionWatch';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateTime } from '@/services/format';
import { watermarkLines } from '@/services/inspectionCamera';
import { INSPECTION_STATUS, inspectionExpired, inspectionKindLabel, missingSlots, photosLabel } from '@/services/occurrences';
import type { InspectionPhotoSource, InspectionSlotDef } from '@/types';

import { BackIcon } from '../icons';
import local from './Occurrences.module.css';
import styles from './Screen.module.css';

export const inspectionsKey = ['me', 'inspections'] as const;

function NavBar({ title, back }: { title: string; back: string }) {
  const navigate = useNavigate();
  return (
    <div className={styles.navbar}>
      <button type="button" className={styles.back} onClick={() => navigate(back)} aria-label="Voltar">
        <BackIcon />
      </button>
      <div className={styles.navTitle}>
        <h1>{title}</h1>
      </div>
    </div>
  );
}

/** As vistorias pedidas ao associado: a que espera as fotos primeiro. */
export function InspectionsScreen() {
  const list = useQuery({ queryKey: inspectionsKey, queryFn: meApi.inspections });
  return (
    <div className={styles.screen}>
      <NavBar title="Vistorias" back="/conta" />
      <p className={styles.muted}>
        Quando quem cuida do seu rastreamento pede a vistoria, você tira as fotos do veículo por aqui: leva poucos minutos.
      </p>
      {list.isLoading ? (
        <Spinner label="Carregando" />
      ) : (list.data ?? []).length === 0 ? (
        <p className={styles.muted}>Nenhuma vistoria pedida.</p>
      ) : (
        (list.data ?? []).map((i) => (
          <Link key={i.id} to={`/vistorias/${i.id}`} className={local.card}>
            <span className={local.head}>
              <strong>{i.vehicleName}</strong>
              <Badge tone={INSPECTION_STATUS[i.status].tone}>{INSPECTION_STATUS[i.status].label}</Badge>
            </span>
            <span className={styles.muted}>
              {inspectionKindLabel(i.kind)} · {i.plate ? `${i.plate} · ` : ''}pedida em {formatDateTime(i.createdAt)} ·{' '}
              {photosLabel(i.slots, i.photos)}
            </span>
          </Link>
        ))
      )}
    </div>
  );
}

/** A vistoria: a foto de cada ângulo pela câmera (tirar ou refazer), a declaração e o envio. */
export function InspectionScreen() {
  const { id = '' } = useParams();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [shooting, setShooting] = useState<InspectionSlotDef | null>(null);
  const [version, setVersion] = useState(0);
  const [damages, setDamages] = useState('');
  const [accept, setAccept] = useState(false);
  const inspection = useQuery({ queryKey: [...inspectionsKey, id], queryFn: () => meApi.inspection(id), enabled: Boolean(id) });
  const i = inspection.data;
  const expired = i ? inspectionExpired(i) : false;
  const pending = i?.status === 'pending' && !expired;
  const position = usePositionWatch(pending);
  const refresh = () => {
    setVersion((v) => v + 1);
    void queryClient.invalidateQueries({ queryKey: [...inspectionsKey, id] });
    void queryClient.invalidateQueries({ queryKey: inspectionsKey });
  };

  const upload = async (slot: InspectionSlotDef, photo: Blob, source: InspectionPhotoSource) => {
    const fix = position.current();
    await meApi.saveInspectionPhoto(id, slot.id, photo, {
      position: fix ? { latitude: fix.latitude, longitude: fix.longitude, accuracy: fix.accuracy } : null,
      source,
    });
    setShooting(null);
    refresh();
  };

  const submit = useMutation({
    mutationFn: () => meApi.submitInspection(id, damages),
    onSuccess: () => {
      refresh();
      notify({ tone: 'success', title: 'Vistoria enviada', description: 'Você recebe aqui a resposta.' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para enviar', description: err.message }),
  });

  const done = new Set(i?.photos.map((p) => p.slot) ?? []);
  const missing = i ? missingSlots(i.slots, i.photos) : [];
  return (
    <div className={styles.screen}>
      <NavBar title="Vistoria" back="/vistorias" />
      {inspection.isLoading ? (
        <Spinner label="Carregando" />
      ) : !i ? (
        <p className={styles.bad}>Vistoria não encontrada.</p>
      ) : (
        <>
          <section className={local.card}>
            <span className={local.head}>
              <strong>
                {inspectionKindLabel(i.kind)} · {i.vehicleName}
              </strong>
              {expired ? <Badge tone="neutral">Link vencido</Badge> : <Badge tone={INSPECTION_STATUS[i.status].tone}>{INSPECTION_STATUS[i.status].label}</Badge>}
            </span>
            {i.requestNote && <p className={local.note}>{i.requestNote}</p>}
            {i.reviewNote && i.status !== 'pending' && <p className={local.note}>Resposta: {i.reviewNote}</p>}
            <span className={styles.muted}>
              {expired
                ? 'O prazo da vistoria acabou: peça para a associação renovar.'
                : pending
                  ? 'Toque em cada ângulo para abrir a câmera, com boa luz. Dá para refazer qualquer uma antes de enviar.'
                  : i.status === 'submitted'
                    ? 'Enviada: a equipe está revisando as fotos.'
                    : i.status === 'approved'
                      ? 'Aprovada. Obrigado!'
                      : i.status === 'canceled'
                        ? 'A associação cancelou esta vistoria.'
                        : 'Devolvida: veja a vistoria nova na lista para refazer.'}
            </span>
            {pending && <span className={styles.muted}>{positionText(position.state, position.accuracy)}</span>}
          </section>
          <ul className={local.slots}>
            {i.slots.map((slot) => (
              <li key={slot.id} className={`${local.slot} ${done.has(slot.id) ? local.slotDone : ''}`}>
                {done.has(slot.id) ? (
                  <AuthImage
                    key={`${slot.id}-${version}`}
                    src={`${meApi.inspectionPhotoUrl(i.id, slot.id)}?v=${version}`}
                    alt={slot.label}
                    className={local.slotPhoto}
                  />
                ) : (
                  <span className={local.slotEmpty} aria-hidden="true">
                    +
                  </span>
                )}
                <span className={local.slotText}>
                  <strong>
                    {slot.label}
                    {slot.optional ? ' (se houver)' : ''}
                  </strong>
                  <span className={styles.muted}>{slot.hint}</span>
                  {pending && (
                    <button type="button" className={local.picker} onClick={() => setShooting(slot)}>
                      {done.has(slot.id) ? 'Refazer' : 'Tirar foto'}
                    </button>
                  )}
                </span>
              </li>
            ))}
          </ul>
          {pending && (
            <section className={local.card}>
              <textarea
                className={local.textarea}
                maxLength={1000}
                value={damages}
                aria-label="Avarias que o veículo já tem"
                placeholder="Avarias que o veículo já tem (opcional): riscos, amassados, vidro trincado…"
                onChange={(e) => setDamages(e.target.value)}
              />
              <label className={local.accept}>
                <input type="checkbox" checked={accept} onChange={(e) => setAccept(e.target.checked)} />
                <span>
                  Declaro que as fotos são do veículo {i.plate || i.vehicleName}, foram tiradas agora por mim e que as avarias que ele já
                  tem estão descritas acima.
                </span>
              </label>
              <Button
                variant="primary"
                block
                disabled={missing.length > 0 || !accept}
                loading={submit.isPending}
                onClick={() => submit.mutate()}
              >
                {missing.length > 0 ? `Faltam ${missing.length} ${missing.length === 1 ? 'foto' : 'fotos'}` : 'Enviar vistoria'}
              </Button>
            </section>
          )}
          <InspectionCamera
            slot={shooting}
            stamp={() => watermarkLines(new Date(), i.plate, position.current())}
            onClose={() => setShooting(null)}
            onShot={(photo, source) => (shooting ? upload(shooting, photo, source) : Promise.resolve())}
          />
        </>
      )}
    </div>
  );
}

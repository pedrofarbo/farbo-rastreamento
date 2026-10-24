import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from 'react-router-dom';

import { meApi } from '@/api/resources';
import { AuthImage } from '@/components/media/AuthImage';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateTime } from '@/services/format';
import { INSPECTION_SLOTS, INSPECTION_STATUS, photosLabel } from '@/services/occurrences';
import { compressImage, currentPosition } from '@/services/photos';
import type { InspectionSlot } from '@/types';

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
              {i.plate ? `${i.plate} · ` : ''}pedida em {formatDateTime(i.createdAt)} · {photosLabel(i.photos.length)}
            </span>
          </Link>
        ))
      )}
    </div>
  );
}

/** A vistoria: a foto de cada ângulo (tirar ou refazer) e o envio. */
export function InspectionScreen() {
  const { id = '' } = useParams();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [busy, setBusy] = useState<InspectionSlot | null>(null);
  const [version, setVersion] = useState(0);
  const inspection = useQuery({ queryKey: [...inspectionsKey, id], queryFn: () => meApi.inspection(id), enabled: Boolean(id) });
  const refresh = () => {
    setVersion((v) => v + 1);
    void queryClient.invalidateQueries({ queryKey: [...inspectionsKey, id] });
    void queryClient.invalidateQueries({ queryKey: inspectionsKey });
  };

  const take = async (slot: InspectionSlot, file: File | undefined) => {
    if (!file) return;
    setBusy(slot);
    try {
      const [photo, position] = await Promise.all([compressImage(file), currentPosition()]);
      await meApi.saveInspectionPhoto(id, slot, photo, position);
      refresh();
    } catch (err) {
      notify({ tone: 'error', title: 'A foto não subiu', description: (err as Error).message });
    } finally {
      setBusy(null);
    }
  };

  const submit = useMutation({
    mutationFn: () => meApi.submitInspection(id),
    onSuccess: () => {
      refresh();
      notify({ tone: 'success', title: 'Vistoria enviada', description: 'Você recebe aqui a resposta.' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para enviar', description: err.message }),
  });

  const i = inspection.data;
  const pending = i?.status === 'pending';
  const done = new Set(i?.photos.map((p) => p.slot) ?? []);
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
              <strong>{i.vehicleName}</strong>
              <Badge tone={INSPECTION_STATUS[i.status].tone}>{INSPECTION_STATUS[i.status].label}</Badge>
            </span>
            {i.requestNote && <p className={local.note}>{i.requestNote}</p>}
            {i.reviewNote && i.status !== 'pending' && <p className={local.note}>Resposta: {i.reviewNote}</p>}
            <span className={styles.muted}>
              {pending
                ? 'Tire uma foto de cada ângulo, com boa luz. Dá para refazer qualquer uma antes de enviar.'
                : i.status === 'submitted'
                  ? 'Enviada: a equipe está revisando as fotos.'
                  : i.status === 'approved'
                    ? 'Aprovada. Obrigado!'
                    : 'Devolvida: veja a vistoria nova na lista para refazer.'}
            </span>
          </section>
          <ul className={local.slots}>
            {INSPECTION_SLOTS.map((slot) => (
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
                  <strong>{slot.label}</strong>
                  <span className={styles.muted}>{slot.hint}</span>
                  {pending && (
                    <label className={local.picker}>
                      <input
                        type="file"
                        accept="image/*"
                        capture="environment"
                        disabled={busy !== null}
                        onChange={(e) => {
                          const file = e.target.files?.[0];
                          e.target.value = '';
                          void take(slot.id, file);
                        }}
                      />
                      {busy === slot.id ? 'Enviando…' : done.has(slot.id) ? 'Refazer' : 'Tirar foto'}
                    </label>
                  )}
                </span>
              </li>
            ))}
          </ul>
          {pending && (
            <Button
              variant="primary"
              block
              disabled={done.size < INSPECTION_SLOTS.length || busy !== null}
              loading={submit.isPending}
              onClick={() => submit.mutate()}
            >
              {done.size < INSPECTION_SLOTS.length ? `Faltam ${INSPECTION_SLOTS.length - done.size} fotos` : 'Enviar vistoria'}
            </Button>
          )}
        </>
      )}
    </div>
  );
}

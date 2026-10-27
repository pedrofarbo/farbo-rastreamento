import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';

import { orgApi } from '@/api/resources';
import { AuthImage } from '@/components/media/AuthImage';
import { BackButton } from '@/components/ui/BackButton';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import fieldStyles from '@/components/ui/Field.module.css';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatCoordinates, formatDateTime } from '@/services/format';
import { INSPECTION_SLOTS, INSPECTION_STATUS, photosLabel } from '@/services/occurrences';
import { useAuth } from '@/stores/AuthContext';
import type { InspectionStatus } from '@/types';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

import pageStyles from '../Page.module.css';
import styles from './OrgOccurrences.module.css';

export const orgInspectionsKey = ['org', 'inspections'] as const;

/** As vistorias: as enviadas (para revisar) primeiro no filtro, e as pedidas. */
export function OrgInspectionsPage() {
  const [status, setStatus] = useState<InspectionStatus | ''>('');
  const list = useQuery({ queryKey: [...orgInspectionsKey, status], queryFn: () => orgApi.inspections(status) });
  const rows = list.data ?? [];
  const paged = usePagedList(rows, 'inspections', status);
  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>Vistorias</h1>
            <p className={pageStyles.description}>
              Peça a vistoria na ficha do associado (em cada veículo): ele tira as fotos de cada ângulo pelo app e você
              aprova ou devolve para refazer, com o motivo.
            </p>
          </div>
          <label className={styles.filter}>
            Situação
            <select className={fieldStyles.select} value={status} onChange={(e) => setStatus(e.target.value as InspectionStatus | '')}>
              <option value="">Todas</option>
              {(Object.keys(INSPECTION_STATUS) as InspectionStatus[]).map((s) => (
                <option key={s} value={s}>
                  {INSPECTION_STATUS[s].label}
                </option>
              ))}
            </select>
          </label>
        </header>
        <Card flush>
          {list.isLoading ? (
            <Spinner label="Carregando" />
          ) : rows.length === 0 ? (
            <EmptyState icon="📷" title="Nenhuma vistoria" description="Peça a primeira na ficha de um associado." />
          ) : (
            <>
              <div className={pageStyles.tableWrap}>
                <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
                  <thead>
                    <tr>
                      <th>Veículo</th>
                      <th>Associado</th>
                      <th>Pedida em</th>
                      <th>Fotos</th>
                      <th>Situação</th>
                    </tr>
                  </thead>
                  <tbody>
                    {paged.items.map((i) => (
                      <tr key={i.id}>
                        <td>
                          <Link to={`/organizacao/vistorias/${i.id}`} className={styles.rowLink}>
                            {i.vehicleName}
                          </Link>
                          {i.plate && <span className={styles.plate}>{i.plate}</span>}
                        </td>
                        <td data-label="Associado">{i.associateName}</td>
                        <td data-label="Pedida em">{formatDateTime(i.createdAt)}</td>
                        <td data-label="Fotos">{photosLabel(i.photos.length)}</td>
                        <td data-label="Situação">
                          <Badge tone={INSPECTION_STATUS[i.status].tone}>{INSPECTION_STATUS[i.status].label}</Badge>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <Pager {...paged.pager} />
            </>
          )}
        </Card>
      </div>
    </div>
  );
}

/** A vistoria: as fotos de cada ângulo e a revisão. */
export function OrgInspectionPage() {
  const { id = '' } = useParams<{ id: string }>();
  const { canOperateOrg } = useAuth();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [note, setNote] = useState('');
  const inspection = useQuery({ queryKey: [...orgInspectionsKey, 'one', id], queryFn: () => orgApi.inspection(id), enabled: Boolean(id) });
  const review = useMutation({
    mutationFn: (approve: boolean) => orgApi.reviewInspection(id, approve, note),
    onSuccess: (i) => {
      setNote('');
      void queryClient.invalidateQueries({ queryKey: orgInspectionsKey });
      notify({
        tone: 'success',
        title: i.status === 'approved' ? 'Vistoria aprovada' : 'Vistoria devolvida',
        description: 'O associado é avisado no app.',
      });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para revisar', description: err.message }),
  });

  const i = inspection.data;
  if (inspection.isLoading) return <Spinner label="Carregando" />;
  if (!i) return <EmptyState title="Vistoria não encontrada" />;
  const bySlot = new Map(i.photos.map((p) => [p.slot, p]));
  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <div className={pageStyles.titleRow}>
              <BackButton to="/organizacao/vistorias" />
              <h1 className={pageStyles.title}>Vistoria · {i.vehicleName}</h1>
            </div>
            <p className={pageStyles.description}>
              {i.associateName}
              {i.plate ? ` · ${i.plate}` : ''} · pedida em {formatDateTime(i.createdAt)}
              {i.requestedByName ? ` por ${i.requestedByName}` : ''}
              {i.submittedAt ? ` · enviada em ${formatDateTime(i.submittedAt)}` : ''}
            </p>
          </div>
          <Badge tone={INSPECTION_STATUS[i.status].tone}>{INSPECTION_STATUS[i.status].label}</Badge>
        </header>
        {i.requestNote && <p className={pageStyles.note}>Pedido: {i.requestNote}</p>}
        {i.reviewNote && i.status !== 'pending' && <p className={pageStyles.note}>Revisão: {i.reviewNote}</p>}

        <div className={styles.slots}>
          {INSPECTION_SLOTS.map((slot) => {
            const p = bySlot.get(slot.id);
            return (
              <figure key={slot.id} className={styles.slot}>
                {p ? (
                  <AuthImage src={orgApi.inspectionPhotoUrl(i.id, slot.id)} alt={slot.label} className={styles.slotPhoto} />
                ) : (
                  <span className={styles.slotEmpty}>Sem foto</span>
                )}
                <figcaption>
                  <strong>{slot.label}</strong>
                  {p && (
                    <span className={styles.muted}>
                      {formatDateTime(p.takenAt)}
                      {p.latitude !== null && p.longitude !== null ? ` · ${formatCoordinates(p.latitude, p.longitude)}` : ''}
                    </span>
                  )}
                </figcaption>
              </figure>
            );
          })}
        </div>

        {canOperateOrg && i.status === 'submitted' && (
          <Card title="Revisão">
            <div className={pageStyles.form}>
              <textarea
                className={`${fieldStyles.textarea} ${styles.textarea}`}
                maxLength={500}
                value={note}
                aria-label="Nota da revisão"
                placeholder="Para devolver, diga o que refazer (o associado vê no app)."
                onChange={(e) => setNote(e.target.value)}
              />
              <div className={styles.actions}>
                <Button variant="primary" loading={review.isPending && review.variables === true} onClick={() => review.mutate(true)}>
                  Aprovar
                </Button>
                <Button
                  variant="secondary"
                  disabled={!note.trim()}
                  loading={review.isPending && review.variables === false}
                  onClick={() => review.mutate(false)}
                >
                  Devolver para refazer
                </Button>
              </div>
            </div>
          </Card>
        )}
      </div>
    </div>
  );
}

/** Pedir a vistoria de um veículo de associado (na ficha do associado). */
export function RequestInspectionModal({
  vehicle,
  onClose,
}: {
  vehicle: { id: string; name: string } | null;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [note, setNote] = useState('');
  const request = useMutation({
    mutationFn: () => orgApi.requestInspection(vehicle!.id, note),
    onSuccess: () => {
      setNote('');
      void queryClient.invalidateQueries({ queryKey: orgInspectionsKey });
      notify({ tone: 'success', title: 'Vistoria pedida', description: 'O associado recebe o aviso no app para tirar as fotos.' });
      onClose();
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para pedir', description: err.message }),
  });
  return (
    <Modal
      open={vehicle !== null}
      title={vehicle ? `Pedir vistoria · ${vehicle.name}` : ''}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={request.isPending} onClick={() => request.mutate()}>
            Pedir vistoria
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        <p className={pageStyles.description}>
          O associado tira pelo app a foto de cada ângulo: frente, traseira, laterais, painel com o hodômetro e o número do
          chassi.
        </p>
        <textarea
          className={`${fieldStyles.textarea} ${styles.textarea}`}
          maxLength={500}
          value={note}
          aria-label="Observação para o associado"
          placeholder="Observação (opcional): vistoria de entrada, depois do conserto…"
          onChange={(e) => setNote(e.target.value)}
        />
      </div>
    </Modal>
  );
}

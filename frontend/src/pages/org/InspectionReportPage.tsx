import { useQuery } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';

import { orgApi } from '@/api/resources';
import { AuthImage } from '@/components/media/AuthImage';
import { Button } from '@/components/ui/Button';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { formatCoordinates, formatDateTime } from '@/services/format';
import { INSPECTION_MODES, INSPECTION_STATUS, inspectionKindLabel } from '@/services/occurrences';

import styles from '../TheftReport.module.css';
import local from './InspectionReport.module.css';
import { InspectionChecks } from './OrgInspectionsPage';

function Item({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd className={mono ? styles.mono : undefined}>{value}</dd>
    </div>
  );
}

const mapsLink = (lat: number, lon: number) => `https://www.google.com/maps/search/?api=1&query=${lat.toFixed(6)},${lon.toFixed(6)}`;

/**
 * O laudo da vistoria (para imprimir ou salvar em PDF): o veículo, quem fez,
 * as datas, o resultado, as conferências, a declaração e cada foto com a
 * data, a hora e o lugar.
 */
export function InspectionReportPage() {
  const { id = '' } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const inspection = useQuery({ queryKey: ['org', 'inspections', 'one', id], queryFn: () => orgApi.inspection(id), enabled: Boolean(id) });
  const i = inspection.data;
  if (inspection.isLoading) return <Spinner label="Carregando" />;
  if (!i) return <EmptyState title="Vistoria não encontrada" />;
  const bySlot = new Map(i.photos.map((p) => [p.slot, p]));
  return (
    <div className={styles.screen}>
      <div className={styles.toolbar}>
        <Button variant="ghost" size="small" onClick={() => navigate(-1)}>
          Voltar
        </Button>
        <Button variant="primary" size="small" onClick={() => window.print()}>
          Imprimir ou salvar PDF
        </Button>
      </div>

      <article className={styles.report}>
        <header className={styles.head}>
          <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={styles.logo} />
          <div>
            <h1 className={styles.title}>
              Laudo de vistoria · {inspectionKindLabel(i.kind)}
            </h1>
            <p className={styles.muted}>
              {i.organizationName} · vistoria {i.id.slice(0, 8).toUpperCase()} · gerado em {formatDateTime(new Date().toISOString())} pela
              plataforma Farbo Rastreadores.
            </p>
          </div>
        </header>

        <section className={styles.block}>
          <h2 className={styles.heading}>Veículo e vistoria</h2>
          <dl className={styles.grid}>
            <Item label="Veículo" value={i.vehicleName} />
            <Item label="Placa" value={i.plate || '—'} mono />
            <Item label={i.associateId ? 'Associado' : 'Dono do veículo (sem cadastro na associação)'} value={i.associateName || '—'} />
            <Item label="Quem fez" value={i.mode === 'technician' ? 'Técnico (vistoria com técnico)' : 'O associado (autovistoria)'} />
            <Item label="Pedida em" value={`${formatDateTime(i.createdAt)}${i.requestedByName ? ` por ${i.requestedByName}` : ''}`} />
            <Item label="Enviada em" value={i.submittedAt ? formatDateTime(i.submittedAt) : '—'} />
            <Item
              label="Resultado"
              value={`${INSPECTION_STATUS[i.status].label}${i.reviewedAt ? ` em ${formatDateTime(i.reviewedAt)}` : ''}${i.reviewedByName ? ` por ${i.reviewedByName}` : ''}`}
            />
            <Item label="Tipo" value={`${inspectionKindLabel(i.kind)} · ${INSPECTION_MODES[i.mode].label}${i.redoOf ? ' · refação' : ''}`} />
          </dl>
          {i.reviewNote && <p className={styles.muted}>Nota da revisão: {i.reviewNote}</p>}
        </section>

        <section className={styles.block}>
          <h2 className={styles.heading}>Declaração</h2>
          <p>{i.damagesNote ? `Avarias declaradas: ${i.damagesNote}` : 'Nenhuma avaria declarada.'}</p>
          {i.acceptedAt && (
            <p className={styles.muted}>
              {i.mode === 'technician' ? 'O técnico declarou' : 'Quem fez a vistoria declarou'} que as fotos são deste veículo e foram tiradas
              na hora, em {formatDateTime(i.acceptedAt)}.
            </p>
          )}
        </section>

        <section className={styles.block}>
          <h2 className={styles.heading}>Conferências da plataforma</h2>
          <InspectionChecks checks={i.checks} />
        </section>

        <section className={styles.block}>
          <h2 className={styles.heading}>Fotos</h2>
          <div className={local.photos}>
            {i.slots.map((slot) => {
              const p = bySlot.get(slot.id);
              if (!p) return null;
              return (
                <figure key={slot.id} className={local.photo}>
                  <AuthImage src={orgApi.inspectionPhotoUrl(i.id, slot.id)} alt={slot.label} className={local.image} />
                  <figcaption>
                    <strong>{slot.label}</strong>
                    <span>
                      {formatDateTime(p.takenAt)}
                      {p.source === 'file' ? ' · escolhida do aparelho' : ' · câmera da vistoria'}
                    </span>
                    {p.latitude !== null && p.longitude !== null ? (
                      <a href={mapsLink(p.latitude, p.longitude)} target="_blank" rel="noreferrer noopener">
                        {formatCoordinates(p.latitude, p.longitude)}
                        {p.accuracyM !== null ? ` (±${Math.round(p.accuracyM)} m)` : ''}
                      </a>
                    ) : (
                      <span>sem localização</span>
                    )}
                  </figcaption>
                </figure>
              );
            })}
          </div>
        </section>
      </article>
    </div>
  );
}

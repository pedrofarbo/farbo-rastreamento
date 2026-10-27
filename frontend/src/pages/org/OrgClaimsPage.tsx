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
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateTime } from '@/services/format';
import { CLAIM_STATUS, claimKindLabel } from '@/services/occurrences';
import { useAuth } from '@/stores/AuthContext';
import type { ClaimStatus } from '@/types';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

import pageStyles from '../Page.module.css';
import styles from './OrgOccurrences.module.css';

const claimsKey = ['org', 'claims'] as const;

/** As ocorrências dos associados, das mais recentes, com o filtro da situação. */
export function OrgClaimsPage() {
  const [status, setStatus] = useState<ClaimStatus | ''>('');
  const list = useQuery({ queryKey: [...claimsKey, status], queryFn: () => orgApi.claims(status) });
  const claims = list.data ?? [];
  const paged = usePagedList(claims, 'claims', status);
  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>Ocorrências</h1>
            <p className={pageStyles.description}>
              O que os associados abriram pelo app (batida, roubo, furto, vidros), com as fotos e o boletim. Mude a
              situação e escreva para o associado: ele é avisado no app.
            </p>
          </div>
          <label className={styles.filter}>
            Situação
            <select className={fieldStyles.select} value={status} onChange={(e) => setStatus(e.target.value as ClaimStatus | '')}>
              <option value="">Todas</option>
              {(Object.keys(CLAIM_STATUS) as ClaimStatus[]).map((s) => (
                <option key={s} value={s}>
                  {CLAIM_STATUS[s].label}
                </option>
              ))}
            </select>
          </label>
        </header>
        <Card flush>
          {list.isLoading ? (
            <Spinner label="Carregando" />
          ) : claims.length === 0 ? (
            <EmptyState icon="📋" title="Nenhuma ocorrência" description="Quando um associado abrir uma ocorrência pelo app, ela aparece aqui e a equipe recebe um e-mail." />
          ) : (
            <>
              <div className={pageStyles.tableWrap}>
                <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
                  <thead>
                    <tr>
                      <th>Ocorrência</th>
                      <th>Associado</th>
                      <th>Veículo</th>
                      <th>Quando</th>
                      <th>Fotos</th>
                      <th>Situação</th>
                    </tr>
                  </thead>
                  <tbody>
                    {paged.items.map((c) => (
                      <tr key={c.id}>
                        <td>
                          <Link to={`/organizacao/ocorrencias/${c.id}`} className={styles.rowLink}>
                            {claimKindLabel(c.kind)}
                          </Link>
                        </td>
                        <td data-label="Associado">{c.associateName}</td>
                        <td data-label="Veículo">
                          {c.vehicleName || '—'}
                          {c.plate && <span className={styles.plate}>{c.plate}</span>}
                        </td>
                        <td data-label="Quando">{formatDateTime(c.occurredAt)}</td>
                        <td data-label="Fotos">{c.photos}</td>
                        <td data-label="Situação">
                          <Badge tone={CLAIM_STATUS[c.status].tone}>{CLAIM_STATUS[c.status].label}</Badge>
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

/** A ocorrência: o relato, as fotos, o B.O., o histórico e a mudança de situação. */
export function OrgClaimPage() {
  const { id = '' } = useParams<{ id: string }>();
  const { canOperateOrg } = useAuth();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const claim = useQuery({ queryKey: [...claimsKey, 'one', id], queryFn: () => orgApi.claim(id), enabled: Boolean(id) });
  const [next, setNext] = useState<ClaimStatus | ''>('');
  const [note, setNote] = useState('');
  const refresh = () => void queryClient.invalidateQueries({ queryKey: claimsKey });

  const save = useMutation({
    mutationFn: () => (next ? orgApi.setClaimStatus(id, next, note) : orgApi.claimNote(id, note)),
    onSuccess: (c) => {
      setNext('');
      setNote('');
      refresh();
      notify({ tone: 'success', title: next ? `Ocorrência ${CLAIM_STATUS[c.status].label.toLowerCase()}` : 'Nota enviada', description: 'O associado é avisado no app.' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para salvar', description: err.message }),
  });

  const c = claim.data;
  if (claim.isLoading) return <Spinner label="Carregando" />;
  if (!c) return <EmptyState title="Ocorrência não encontrada" />;
  const photos = (c.files ?? []).filter((f) => f.contentType.startsWith('image/'));
  const documents = (c.files ?? []).filter((f) => !f.contentType.startsWith('image/'));
  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <div className={pageStyles.titleRow}>
              <BackButton to="/organizacao/ocorrencias" />
              <h1 className={pageStyles.title}>
                {claimKindLabel(c.kind)} · {c.associateName}
              </h1>
            </div>
            <p className={pageStyles.description}>
              Aberta em {formatDateTime(c.createdAt)} · aconteceu em {formatDateTime(c.occurredAt)}
            </p>
          </div>
          <Badge tone={CLAIM_STATUS[c.status].tone}>{CLAIM_STATUS[c.status].label}</Badge>
        </header>

        <div className={styles.columns}>
          <div className={styles.main}>
            <Card title="Relato">
              <dl className={styles.facts}>
                <div>
                  <dt>Veículo</dt>
                  <dd>
                    {c.vehicleName || '—'}
                    {c.plate && <span className={styles.plate}>{c.plate}</span>}
                  </dd>
                </div>
                <div>
                  <dt>Local</dt>
                  <dd>{c.place || '—'}</dd>
                </div>
                <div>
                  <dt>Boletim de ocorrência</dt>
                  <dd>{c.policeReport || '—'}</dd>
                </div>
              </dl>
              <p className={styles.text}>{c.description}</p>
              {c.vehicleId && (
                <Link
                  className={styles.reportLink}
                  to={`/relatorio-sinistro/${c.vehicleId}?quando=${encodeURIComponent(c.occurredAt)}&janela=30`}
                >
                  Ver o trajeto e a velocidade na hora (relatório de sinistro)
                </Link>
              )}
            </Card>

            <Card title={`Fotos (${photos.length})`}>
              {photos.length === 0 ? (
                <p className={styles.muted}>Nenhuma foto ainda.</p>
              ) : (
                <div className={styles.gallery}>
                  {photos.map((f) => (
                    <AuthImage key={f.id} src={orgApi.claimFileUrl(c.id, f.id)} alt={f.filename || 'Foto'} className={styles.photo} />
                  ))}
                </div>
              )}
              {documents.length > 0 && (
                <ul className={styles.documents}>
                  {documents.map((f) => (
                    <li key={f.id}>
                      <Button variant="ghost" size="small" onClick={() => void orgApi.downloadClaimFile(c.id, f.id)}>
                        {f.kind === 'police_report' ? 'Boletim de ocorrência' : 'Documento'} ({f.filename || 'PDF'})
                      </Button>
                    </li>
                  ))}
                </ul>
              )}
            </Card>
          </div>

          <div className={styles.side}>
            <Card title="Histórico">
              <ul className={styles.timeline}>
                {(c.updates ?? []).map((u) => (
                  <li key={u.id} className={u.authorKind === 'organization' ? styles.fromOrg : undefined}>
                    <span className={styles.muted}>
                      {formatDateTime(u.createdAt)} · {u.authorName || (u.authorKind === 'organization' ? 'Equipe' : c.associateName)}
                      {u.status ? ` · ${CLAIM_STATUS[u.status].label}` : ''}
                    </span>
                    {u.note && <p className={styles.text}>{u.note}</p>}
                  </li>
                ))}
              </ul>
            </Card>
            {canOperateOrg && (
              <Card title="Responder">
                <div className={pageStyles.form}>
                  <label className={styles.filter}>
                    Situação
                    <select className={fieldStyles.select} value={next} onChange={(e) => setNext(e.target.value as ClaimStatus | '')}>
                      <option value="">Manter ({CLAIM_STATUS[c.status].label})</option>
                      {(Object.keys(CLAIM_STATUS) as ClaimStatus[])
                        .filter((s) => s !== c.status)
                        .map((s) => (
                          <option key={s} value={s}>
                            {CLAIM_STATUS[s].label}
                          </option>
                        ))}
                    </select>
                  </label>
                  <textarea
                    className={`${fieldStyles.textarea} ${styles.textarea}`}
                    maxLength={1000}
                    value={note}
                    aria-label="Nota para o associado"
                    placeholder="O que falta, o próximo passo, a decisão…"
                    onChange={(e) => setNote(e.target.value)}
                  />
                  <Button variant="primary" disabled={!next && !note.trim()} loading={save.isPending} onClick={() => save.mutate()}>
                    {next ? `Marcar como ${CLAIM_STATUS[next].label.toLowerCase()}` : 'Mandar a nota'}
                  </Button>
                </div>
              </Card>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { orgApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import fieldStyles from '@/components/ui/Field.module.css';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatAddress, formatDateTime } from '@/services/format';
import { INSPECTION_STATUS, whatsappLink } from '@/services/occurrences';
import { formatTaxId } from '@/services/taxid';
import { useAuth } from '@/stores/AuthContext';
import type { Adhesion, AdhesionStatus } from '@/types';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

import pageStyles from '../Page.module.css';
import styles from './OrgOccurrences.module.css';

export const adhesionsKey = ['org', 'adhesions'] as const;

export const ADHESION_STATUS: Record<AdhesionStatus, { label: string; tone: 'accent' | 'warning' | 'success' | 'danger' }> = {
  pending: { label: 'Em preenchimento', tone: 'accent' },
  submitted: { label: 'Para analisar', tone: 'warning' },
  approved: { label: 'Aprovada', tone: 'success' },
  rejected: { label: 'Recusada', tone: 'danger' },
};

/** O que falta na adesão em preenchimento. */
export function adhesionStep(a: Pick<Adhesion, 'status' | 'acceptedAt' | 'inspection'>): string {
  if (a.status !== 'pending') return '';
  const sent = a.inspection && (a.inspection.status === 'submitted' || a.inspection.status === 'approved');
  if (!sent) return 'falta a vistoria';
  if (!a.acceptedAt) return 'falta o termo';
  return '';
}

/** A mensagem para divulgar o link de adesão. */
export function adhesionInvite(orgName: string, url: string): string {
  return (
    `Quer entrar na ${orgName} com rastreamento? Faça a sua adesão pelo celular, com a vistoria do veículo e o aceite do termo, ` +
    `em uns 15 minutos: ${url}`
  );
}

/**
 * As adesões pelo link: o link da associação (ligar, copiar e divulgar) e
 * as adesões — as para analisar primeiro —, com a vistoria e o termo.
 * Aprovar cadastra o associado e o veículo, que entra na fila de instalação.
 */
export function OrgAdhesionsPage() {
  const { canOperateOrg, user } = useAuth();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [status, setStatus] = useState<AdhesionStatus | ''>('');
  const [open, setOpen] = useState<Adhesion | null>(null);
  const settings = useQuery({ queryKey: [...adhesionsKey, 'settings'], queryFn: orgApi.adhesionSettings });
  const list = useQuery({ queryKey: [...adhesionsKey, status], queryFn: () => orgApi.adhesions(status) });
  const toggle = useMutation({
    mutationFn: (on: boolean) => orgApi.setAdhesionSettings(on),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: [...adhesionsKey, 'settings'] }),
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para mudar', description: err.message }),
  });
  const rows = list.data ?? [];
  const sorted = [...rows].sort((a, b) => Number(b.status === 'submitted') - Number(a.status === 'submitted'));
  const paged = usePagedList(sorted, 'adhesions', status);
  const orgName = user?.organization?.name ?? 'associação';
  const s = settings.data;
  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>Adesões pelo link</h1>
            <p className={pageStyles.description}>
              Divulgue o link: quem quer entrar manda os dados e o veículo, faz a vistoria pelo celular e aceita o termo do associado.
              Você aprova aqui e o associado e o veículo são cadastrados, com a instalação na fila da Farbo.
            </p>
          </div>
          <label className={styles.filter}>
            Situação
            <select className={fieldStyles.select} value={status} onChange={(e) => setStatus(e.target.value as AdhesionStatus | '')}>
              <option value="">Todas</option>
              {(Object.keys(ADHESION_STATUS) as AdhesionStatus[]).map((k) => (
                <option key={k} value={k}>
                  {ADHESION_STATUS[k].label}
                </option>
              ))}
            </select>
          </label>
        </header>

        {s && (
          <Card title="O link de adesão">
            <div className={pageStyles.form}>
              {canOperateOrg && (
                <label className={styles.toggle}>
                  <input type="checkbox" checked={s.enabled} disabled={toggle.isPending} onChange={(e) => toggle.mutate(e.target.checked)} />
                  <span>
                    Link aberto
                    <small>Desligado, quem abrir o link vê que a adesão não está aberta.</small>
                  </span>
                </label>
              )}
              {s.enabled && (
                <div className={styles.linkBox}>
                  <input className={fieldStyles.input} readOnly value={s.url} aria-label="Link de adesão" onFocus={(e) => e.target.select()} />
                  <div className={styles.actions}>
                    <Button
                      variant="secondary"
                      size="small"
                      onClick={() =>
                        void navigator.clipboard
                          ?.writeText(adhesionInvite(orgName, s.url))
                          .then(() => notify({ tone: 'success', title: 'Convite com o link copiado' }))
                          .catch(() => undefined)
                      }
                    >
                      Copiar convite
                    </Button>
                    <a className={styles.whatsapp} href={whatsappLink('', adhesionInvite(orgName, s.url))} target="_blank" rel="noreferrer noopener">
                      Mandar pelo WhatsApp
                    </a>
                  </div>
                </div>
              )}
            </div>
          </Card>
        )}

        <Card flush>
          {list.isLoading ? (
            <Spinner label="Carregando" />
          ) : rows.length === 0 ? (
            <EmptyState icon="📝" title="Nenhuma adesão" description="Ligue e divulgue o link: as adesões aparecem aqui." />
          ) : (
            <>
              <div className={pageStyles.tableWrap}>
                <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
                  <thead>
                    <tr>
                      <th>Pessoa</th>
                      <th>Veículo</th>
                      <th>Vistoria</th>
                      <th>Começou em</th>
                      <th>Situação</th>
                    </tr>
                  </thead>
                  <tbody>
                    {paged.items.map((a) => (
                      <tr key={a.id}>
                        <td>
                          <button type="button" className={`${styles.rowLink} ${styles.linkButton}`} onClick={() => setOpen(a)}>
                            {a.name}
                          </button>
                        </td>
                        <td data-label="Veículo">
                          {[a.brand, a.model].filter(Boolean).join(' ')}
                          <span className={styles.plate}>{a.plate}</span>
                        </td>
                        <td data-label="Vistoria">
                          {a.inspection ? (
                            <Link to={`/organizacao/vistorias/${a.inspection.id}`}>{INSPECTION_STATUS[a.inspection.status].label}</Link>
                          ) : (
                            '—'
                          )}
                        </td>
                        <td data-label="Começou em">{formatDateTime(a.createdAt)}</td>
                        <td data-label="Situação">
                          <Badge tone={ADHESION_STATUS[a.status].tone}>{ADHESION_STATUS[a.status].label}</Badge>
                          {adhesionStep(a) && <span className={styles.muted}> · {adhesionStep(a)}</span>}
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
      <AdhesionModal adhesion={open} canOperate={canOperateOrg} onClose={() => setOpen(null)} />
    </div>
  );
}

/** A adesão: os dados, o endereço, a vistoria, o termo e a decisão. */
function AdhesionModal({ adhesion, canOperate, onClose }: { adhesion: Adhesion | null; canOperate: boolean; onClose: () => void }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [note, setNote] = useState('');
  const [rejecting, setRejecting] = useState(false);
  const close = () => {
    setNote('');
    setRejecting(false);
    onClose();
  };
  const decide = useMutation({
    mutationFn: (approve: boolean) => (approve ? orgApi.approveAdhesion(adhesion!.id) : orgApi.rejectAdhesion(adhesion!.id, note)),
    onSuccess: (a) => {
      void queryClient.invalidateQueries({ queryKey: adhesionsKey });
      notify(
        a.status === 'approved'
          ? { tone: 'success', title: 'Adesão aprovada', description: 'O associado recebe o acesso ao app por e-mail; a instalação foi para a fila da Farbo.' }
          : { tone: 'success', title: 'Adesão recusada', description: 'A pessoa vê o motivo no link de acompanhamento.' },
      );
      close();
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu', description: err.message }),
  });
  const a = adhesion;
  const vehicle = a ? [a.brand, a.model, a.year, a.color].filter(Boolean).join(' · ') : '';
  const inspectionOk = a?.inspection && (a.inspection.status === 'submitted' || a.inspection.status === 'approved');
  return (
    <Modal
      open={a !== null}
      title={a ? `Adesão · ${a.name}` : ''}
      onClose={close}
      footer={
        a && canOperate && (a.status === 'submitted' || a.status === 'pending') ? (
          rejecting ? (
            <>
              <Button variant="ghost" onClick={() => setRejecting(false)}>
                Voltar
              </Button>
              <Button variant="primary" disabled={!note.trim()} loading={decide.isPending} onClick={() => decide.mutate(false)}>
                Recusar
              </Button>
            </>
          ) : (
            <>
              <Button variant="ghost" onClick={() => setRejecting(true)}>
                Recusar
              </Button>
              {a.status === 'submitted' && (
                <Button variant="primary" loading={decide.isPending} onClick={() => decide.mutate(true)}>
                  Aprovar e cadastrar
                </Button>
              )}
            </>
          )
        ) : (
          <Button variant="primary" onClick={close}>
            Fechar
          </Button>
        )
      }
    >
      {a && (
        <div className={pageStyles.form}>
          <dl className={styles.facts}>
            <div>
              <dt>CPF</dt>
              <dd>{formatTaxId(a.document)}</dd>
            </div>
            <div>
              <dt>E-mail</dt>
              <dd>{a.email}</dd>
            </div>
            <div>
              <dt>Celular</dt>
              <dd>{a.phone}</dd>
            </div>
            <div>
              <dt>Veículo</dt>
              <dd>
                {vehicle} <span className={styles.plate}>{a.plate}</span>
              </dd>
            </div>
            <div>
              <dt>Onde instalar</dt>
              <dd>{formatAddress(a.address)}</dd>
            </div>
            <div>
              <dt>Preferência para a instalação</dt>
              <dd>{a.installPreference || '—'}</dd>
            </div>
            <div>
              <dt>Vistoria</dt>
              <dd>
                {a.inspection ? (
                  <Link to={`/organizacao/vistorias/${a.inspection.id}`} onClick={close}>
                    {INSPECTION_STATUS[a.inspection.status].label} · abrir
                  </Link>
                ) : (
                  '—'
                )}
              </dd>
            </div>
            <div>
              <dt>Termo do associado</dt>
              <dd>{a.acceptedAt ? `aceito em ${formatDateTime(a.acceptedAt)} (versão ${a.termVersion})` : 'ainda não aceito'}</dd>
            </div>
          </dl>
          {a.status === 'submitted' && a.inspection?.status === 'submitted' && (
            <p className={styles.muted}>Aprovar a adesão aprova junto a vistoria em revisão. Confira as fotos antes.</p>
          )}
          {a.status === 'pending' && (
            <p className={styles.muted}>
              {inspectionOk ? 'Falta a pessoa aceitar o termo.' : 'Falta a pessoa fazer a vistoria.'} O link de acompanhamento dela:{' '}
              <a href={whatsappLink(a.phone, `Olá! Continue a sua adesão por aqui: ${a.followUrl}`)} target="_blank" rel="noreferrer noopener">
                mandar pelo WhatsApp
              </a>
              .
            </p>
          )}
          {a.status === 'rejected' && <p className={styles.text}>Recusada: {a.reviewNote}</p>}
          {a.status === 'approved' && a.associateId && (
            <p className={styles.text}>
              Cadastrada{a.reviewedByName ? ` por ${a.reviewedByName}` : ''}{a.reviewedAt ? ` em ${formatDateTime(a.reviewedAt)}` : ''}:{' '}
              <Link to={`/associados/${a.associateId}`} onClick={close}>
                ficha do associado
              </Link>
              .
            </p>
          )}
          {rejecting && (
            <textarea
              className={`${fieldStyles.textarea} ${styles.textarea}`}
              maxLength={500}
              value={note}
              aria-label="Motivo da recusa"
              placeholder="O motivo (a pessoa vê no link de acompanhamento)."
              onChange={(e) => setNote(e.target.value)}
            />
          )}
        </div>
      )}
    </Modal>
  );
}

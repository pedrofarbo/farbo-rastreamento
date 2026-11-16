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
import { SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatCoordinates, formatDateTime, formatMoney, formatPhoneInput } from '@/services/format';
import {
  INSPECTION_KINDS,
  INSPECTION_MODES,
  INSPECTION_STATUS,
  inspectionExpired,
  inspectionKindLabel,
  inspectionMessage,
  photosLabel,
  whatsappLink,
} from '@/services/occurrences';
import { useAuth } from '@/stores/AuthContext';
import type { Inspection, InspectionCheck, InspectionKind, InspectionMode, InspectionStatus, InspectionVehicleType } from '@/types';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

import pageStyles from '../Page.module.css';
import styles from './OrgOccurrences.module.css';

export const orgInspectionsKey = ['org', 'inspections'] as const;

const CHECK_ICON: Record<InspectionCheck['level'], string> = { ok: '✅', warn: '⚠️', info: 'ℹ️' };

/** A situação, com o link vencido à parte. */
function StatusBadge({ i }: { i: Inspection }) {
  if (inspectionExpired(i)) return <Badge tone="neutral">Link vencido</Badge>;
  return <Badge tone={INSPECTION_STATUS[i.status].tone}>{INSPECTION_STATUS[i.status].label}</Badge>;
}

/** As vistorias: as pedidas pela ficha do associado e as por link, de quem ainda vai entrar. */
export function OrgInspectionsPage() {
  const { canOperateOrg } = useAuth();
  const [status, setStatus] = useState<InspectionStatus | ''>('');
  const [creating, setCreating] = useState(false);
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
              Mande o link da vistoria pelo WhatsApp: o associado (ou quem vai entrar) tira as fotos pela câmera do celular,
              com o lugar e a hora, ou um técnico faz no lugar dele. Você confere e aprova ou devolve para refazer. A de um
              associado com veículo também sai pela ficha dele.
            </p>
          </div>
          <div className={styles.headerActions}>
            {canOperateOrg && (
              <Button variant="primary" onClick={() => setCreating(true)}>
                Nova vistoria
              </Button>
            )}
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
          </div>
        </header>
        <Card flush>
          {list.isLoading ? (
            <Spinner label="Carregando" />
          ) : rows.length === 0 ? (
            <EmptyState icon="📷" title="Nenhuma vistoria" description="Mande a primeira pelo botão Nova vistoria." />
          ) : (
            <>
              <div className={pageStyles.tableWrap}>
                <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
                  <thead>
                    <tr>
                      <th>Veículo</th>
                      <th>Pessoa</th>
                      <th>Tipo</th>
                      <th>Pedida em</th>
                      <th>Fotos</th>
                      <th>Situação</th>
                    </tr>
                  </thead>
                  <tbody>
                    {paged.items.map((i) => {
                      const warnings = i.checks.filter((c) => c.level === 'warn').length;
                      return (
                        <tr key={i.id}>
                          <td>
                            <Link to={`/organizacao/vistorias/${i.id}`} className={styles.rowLink}>
                              {i.vehicleName}
                            </Link>
                            {i.plate && i.plate !== i.vehicleName && <span className={styles.plate}>{i.plate}</span>}
                          </td>
                          <td data-label="Pessoa">
                            {i.associateName}
                            {!i.associateId && <span className={styles.muted}> · sem cadastro</span>}
                          </td>
                          <td data-label="Tipo">
                            {inspectionKindLabel(i.kind)}
                            <span className={styles.muted}> · {INSPECTION_MODES[i.mode].label}</span>
                            {i.redoOf && <span className={styles.muted}> · refação</span>}
                          </td>
                          <td data-label="Pedida em">{formatDateTime(i.createdAt)}</td>
                          <td data-label="Fotos">{photosLabel(i.slots, i.photos)}</td>
                          <td data-label="Situação">
                            <StatusBadge i={i} />
                            {warnings > 0 && <span className={styles.warnCount}> ⚠️ {warnings}</span>}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
              <Pager {...paged.pager} />
            </>
          )}
        </Card>
      </div>
      <NewInspectionModal open={creating} onClose={() => setCreating(false)} />
    </div>
  );
}

/** O link para mandar: copiar ou abrir o WhatsApp com a mensagem. */
export function InspectionLinkBox({ inspection }: { inspection: Inspection }) {
  const { notify } = useToast();
  if (!inspection.link) return null;
  const link = inspection.link;
  const message = inspectionMessage(inspection, link);
  const phone = inspection.mode === 'self' ? inspection.phone : '';
  return (
    <div className={styles.linkBox}>
      <input className={fieldStyles.input} readOnly value={link} aria-label="Link da vistoria" onFocus={(e) => e.target.select()} />
      <div className={styles.actions}>
        <Button
          variant="secondary"
          size="small"
          onClick={() =>
            void navigator.clipboard
              ?.writeText(message)
              .then(() => notify({ tone: 'success', title: 'Mensagem com o link copiada' }))
              .catch(() => undefined)
          }
        >
          Copiar mensagem
        </Button>
        <a className={styles.whatsapp} href={whatsappLink(phone, message)} target="_blank" rel="noreferrer noopener">
          {phone ? 'Mandar pelo WhatsApp' : 'Escolher no WhatsApp'}
        </a>
      </div>
      {inspection.priceCents > 0 && (
        <p className={styles.muted}>
          {INSPECTION_MODES[inspection.mode].label}: {formatMoney(inspection.priceCents)} na fatura do mês em que for enviada.
        </p>
      )}
    </div>
  );
}

const PLATE = /^[A-Z]{3}[0-9][A-Z0-9][0-9]{2}$/;

/** A placa como a pessoa digitou → ABC1D23. */
export const normalizePlate = (raw: string) => raw.toUpperCase().replace(/[^A-Z0-9]/g, '');

/** Quem vai fazer a vistoria e o veículo (de quem ainda vai entrar na associação). */
interface ProspectDraft {
  kind: InspectionKind;
  mode: InspectionMode;
  personName: string;
  personPhone: string;
  plate: string;
  vehicleType: InspectionVehicleType;
  vehicleDesc: string;
  note: string;
}

const EMPTY_PROSPECT: ProspectDraft = {
  kind: 'entry', mode: 'self', personName: '', personPhone: '', plate: '', vehicleType: 'car', vehicleDesc: '', note: '',
};

/** O pedido conferido, ou a mensagem do que falta. */
export function prospectProblem(d: ProspectDraft): string | null {
  if (d.personName.trim().length < 2) return 'Informe o nome de quem vai fazer a vistoria (ou do dono do veículo).';
  if (!PLATE.test(normalizePlate(d.plate))) return 'Informe a placa do veículo (ABC1D23 ou ABC1234).';
  return null;
}

/** A vistoria por link de quem ainda vai entrar: o nome, o celular e a placa. */
function NewInspectionModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [draft, setDraft] = useState<ProspectDraft>(EMPTY_PROSPECT);
  const [done, setDone] = useState<Inspection | null>(null);
  const set = (patch: Partial<ProspectDraft>) => setDraft((d) => ({ ...d, ...patch }));
  const close = () => {
    setDraft(EMPTY_PROSPECT);
    setDone(null);
    onClose();
  };
  const request = useMutation({
    mutationFn: () =>
      orgApi.requestInspection({
        kind: draft.kind, mode: draft.mode, personName: draft.personName.trim(), personPhone: draft.personPhone.trim(),
        plate: normalizePlate(draft.plate), vehicleType: draft.vehicleType, vehicleDesc: draft.vehicleDesc.trim(), note: draft.note.trim(),
      }),
    onSuccess: (i) => {
      setDone(i);
      void queryClient.invalidateQueries({ queryKey: orgInspectionsKey });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para pedir', description: err.message }),
  });
  const problem = prospectProblem(draft);
  return (
    <Modal
      open={open}
      title={done ? 'Vistoria pedida: mande o link' : 'Nova vistoria por link'}
      onClose={close}
      footer={
        done ? (
          <Button variant="primary" onClick={close}>
            Pronto
          </Button>
        ) : (
          <>
            <Button variant="ghost" onClick={close}>
              Cancelar
            </Button>
            <Button variant="primary" disabled={problem !== null} loading={request.isPending} onClick={() => request.mutate()}>
              Criar o link
            </Button>
          </>
        )
      }
    >
      {done ? (
        <div className={pageStyles.form}>
          <p className={pageStyles.description}>
            O link abre sem login, por 7 dias. Mande pelo WhatsApp para {done.mode === 'self' ? done.associateName : 'o técnico'}: a
            vistoria aparece aqui, para conferir, assim que for enviada.
          </p>
          <InspectionLinkBox inspection={done} />
        </div>
      ) : (
        <div className={pageStyles.form}>
          <InspectionKindFields kind={draft.kind} mode={draft.mode} onChange={set} />
          <TextField label="Nome" value={draft.personName} maxLength={120} onChange={(e) => set({ personName: e.target.value })} />
          <TextField
            label="Celular (WhatsApp)"
            inputMode="tel"
            value={draft.personPhone}
            hint={draft.mode === 'technician' ? 'De quem é o veículo (o link vai para o técnico).' : 'O link vai para este número.'}
            onChange={(e) => set({ personPhone: formatPhoneInput(e.target.value) })}
          />
          <div className={styles.twoColumns}>
            <TextField
              label="Placa"
              value={draft.plate}
              maxLength={8}
              placeholder="ABC1D23"
              onChange={(e) => set({ plate: e.target.value.toUpperCase() })}
            />
            <SelectField label="Veículo" value={draft.vehicleType} onChange={(e) => set({ vehicleType: e.target.value as InspectionVehicleType })}>
              <option value="car">Carro, caminhonete ou caminhão</option>
              <option value="motorcycle">Moto</option>
            </SelectField>
          </div>
          <TextField
            label="Marca, modelo, ano e cor (opcional)"
            value={draft.vehicleDesc}
            maxLength={80}
            placeholder="Gol 1.0 2015 prata"
            onChange={(e) => set({ vehicleDesc: e.target.value })}
          />
          <textarea
            className={`${fieldStyles.textarea} ${styles.textarea}`}
            maxLength={500}
            value={draft.note}
            aria-label="Observação"
            placeholder="Observação (opcional): aparece no link para quem vai fazer a vistoria."
            onChange={(e) => set({ note: e.target.value })}
          />
        </div>
      )}
    </Modal>
  );
}

/** O tipo da vistoria e quem tira as fotos. */
function InspectionKindFields({
  kind,
  mode,
  onChange,
}: {
  kind: InspectionKind;
  mode: InspectionMode;
  onChange: (patch: { kind?: InspectionKind; mode?: InspectionMode }) => void;
}) {
  return (
    <>
      <SelectField
        label="Tipo"
        value={kind}
        hint={INSPECTION_KINDS.find((k) => k.id === kind)?.hint}
        onChange={(e) => onChange({ kind: e.target.value as InspectionKind })}
      >
        {INSPECTION_KINDS.map((k) => (
          <option key={k.id} value={k.id}>
            {k.label}
          </option>
        ))}
      </SelectField>
      <SelectField
        label="Quem tira as fotos"
        value={mode}
        hint={INSPECTION_MODES[mode].who}
        onChange={(e) => onChange({ mode: e.target.value as InspectionMode })}
      >
        <option value="self">O associado (autovistoria)</option>
        <option value="technician">Um técnico</option>
      </SelectField>
    </>
  );
}

/** A vistoria: as conferências, a declaração, as fotos de cada ângulo e a revisão. */
export function OrgInspectionPage() {
  const { id = '' } = useParams<{ id: string }>();
  const { canOperateOrg } = useAuth();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [note, setNote] = useState('');
  const inspection = useQuery({ queryKey: [...orgInspectionsKey, 'one', id], queryFn: () => orgApi.inspection(id), enabled: Boolean(id) });
  const refresh = () => void queryClient.invalidateQueries({ queryKey: orgInspectionsKey });
  const review = useMutation({
    mutationFn: (approve: boolean) => orgApi.reviewInspection(id, approve, note),
    onSuccess: (i) => {
      setNote('');
      refresh();
      notify({
        tone: 'success',
        title: i.status === 'approved' ? 'Vistoria aprovada' : 'Vistoria devolvida',
        description:
          i.status === 'approved'
            ? undefined
            : 'O mesmo link abre a refação, com o motivo. Mande de novo se a pessoa não usa o app.',
      });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para revisar', description: err.message }),
  });
  const linkAction = useMutation({
    mutationFn: (action: 'renew' | 'cancel') => (action === 'renew' ? orgApi.renewInspection(id) : orgApi.cancelInspection(id)),
    onSuccess: (i) => {
      refresh();
      notify({ tone: 'success', title: i.status === 'canceled' ? 'Vistoria cancelada' : 'Link renovado por mais 7 dias' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu', description: err.message }),
  });

  const i = inspection.data;
  if (inspection.isLoading) return <Spinner label="Carregando" />;
  if (!i) return <EmptyState title="Vistoria não encontrada" />;
  const bySlot = new Map(i.photos.map((p) => [p.slot, p]));
  const expired = inspectionExpired(i);
  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <div className={pageStyles.titleRow}>
              <BackButton to="/organizacao/vistorias" />
              <h1 className={pageStyles.title}>
                {inspectionKindLabel(i.kind)} · {i.vehicleName}
              </h1>
            </div>
            <p className={pageStyles.description}>
              {i.associateName}
              {!i.associateId ? ' (sem cadastro na associação)' : ''}
              {i.plate ? ` · ${i.plate}` : ''} · {INSPECTION_MODES[i.mode].label} · pedida em {formatDateTime(i.createdAt)}
              {i.requestedByName ? ` por ${i.requestedByName}` : ''}
              {i.submittedAt ? ` · enviada em ${formatDateTime(i.submittedAt)}` : ''}
            </p>
          </div>
          <div className={styles.headerActions}>
            {i.status !== 'pending' && i.status !== 'canceled' && (
              <Link className={styles.reportLink} to={`/organizacao/vistorias/${i.id}/laudo`} target="_blank" rel="noreferrer">
                Laudo (imprimir ou PDF)
              </Link>
            )}
            <StatusBadge i={i} />
          </div>
        </header>
        {i.requestNote && <p className={pageStyles.note}>{i.redoOf ? 'Para refazer' : 'Pedido'}: {i.requestNote}</p>}
        {i.reviewNote && i.status !== 'pending' && <p className={pageStyles.note}>Revisão: {i.reviewNote}</p>}

        {i.status === 'pending' && (
          <Card title={expired ? 'O link venceu' : 'O link da vistoria'}>
            <div className={pageStyles.form}>
              <p className={pageStyles.description}>
                {expired
                  ? 'Renove para o link voltar a abrir por mais 7 dias.'
                  : `Abre sem login${i.expiresAt ? ` até ${formatDateTime(i.expiresAt)}` : ''}. ${photosLabel(i.slots, i.photos)} até agora.`}
              </p>
              {!expired && <InspectionLinkBox inspection={i} />}
              {canOperateOrg && (
                <div className={styles.actions}>
                  <Button variant="secondary" size="small" loading={linkAction.isPending && linkAction.variables === 'renew'} onClick={() => linkAction.mutate('renew')}>
                    Renovar o link
                  </Button>
                  <Button variant="ghost" size="small" loading={linkAction.isPending && linkAction.variables === 'cancel'} onClick={() => linkAction.mutate('cancel')}>
                    Cancelar a vistoria
                  </Button>
                </div>
              )}
            </div>
          </Card>
        )}

        {i.submittedAt && (
          <div className={styles.columns}>
            <Card title="Conferências">
              <InspectionChecks checks={i.checks} />
            </Card>
            <Card title="Declaração">
              <p className={styles.text}>{i.damagesNote || 'Nenhuma avaria declarada.'}</p>
              {i.acceptedAt && (
                <p className={styles.muted}>
                  {i.mode === 'technician' ? 'O técnico declarou' : 'Declarou'} que as fotos são do veículo e foram tiradas na hora, em{' '}
                  {formatDateTime(i.acceptedAt)}.
                </p>
              )}
            </Card>
          </div>
        )}

        <div className={styles.slots}>
          {i.slots.map((slot) => {
            const p = bySlot.get(slot.id);
            if (!p && slot.optional && i.status !== 'pending') return null;
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
                      {p.latitude !== null && p.longitude !== null ? ` · ${formatCoordinates(p.latitude, p.longitude)}` : ' · sem localização'}
                      {p.source === 'file' ? ' · escolhida do aparelho' : ''}
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
                placeholder="Para devolver, diga o que refazer (quem fez a vistoria vê no link)."
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

/** As conferências do envio: o que conferiu, o que olhar com atenção e o que é só para saber. */
export function InspectionChecks({ checks }: { checks: InspectionCheck[] }) {
  if (checks.length === 0) return <p className={styles.muted}>Sem conferências.</p>;
  const order = { warn: 0, info: 1, ok: 2 } as const;
  return (
    <ul className={styles.checks}>
      {[...checks]
        .sort((a, b) => order[a.level] - order[b.level])
        .map((c) => (
          <li key={c.code} className={c.level === 'warn' ? styles.checkWarn : undefined}>
            <span aria-hidden="true">{CHECK_ICON[c.level]}</span> {c.text}
          </li>
        ))}
    </ul>
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
  const [kind, setKind] = useState<InspectionKind>('periodic');
  const [mode, setMode] = useState<InspectionMode>('self');
  const [note, setNote] = useState('');
  const [done, setDone] = useState<Inspection | null>(null);
  const close = () => {
    setNote('');
    setDone(null);
    setKind('periodic');
    setMode('self');
    onClose();
  };
  const request = useMutation({
    mutationFn: () => orgApi.requestInspection({ vehicleId: vehicle!.id, kind, mode, note }),
    onSuccess: (i) => {
      setDone(i);
      void queryClient.invalidateQueries({ queryKey: orgInspectionsKey });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para pedir', description: err.message }),
  });
  return (
    <Modal
      open={vehicle !== null}
      title={done ? 'Vistoria pedida' : vehicle ? `Pedir vistoria · ${vehicle.name}` : ''}
      onClose={close}
      footer={
        done ? (
          <Button variant="primary" onClick={close}>
            Pronto
          </Button>
        ) : (
          <>
            <Button variant="ghost" onClick={close}>
              Cancelar
            </Button>
            <Button variant="primary" loading={request.isPending} onClick={() => request.mutate()}>
              Pedir vistoria
            </Button>
          </>
        )
      }
    >
      {done ? (
        <div className={pageStyles.form}>
          <p className={pageStyles.description}>
            {done.mode === 'self'
              ? 'O associado recebe o aviso no app. Se ele não usa o app, mande o link pelo WhatsApp.'
              : 'Mande o link para o técnico. Se a instalação deste veículo estiver agendada, o link também aparece no passo a passo do instalador.'}
          </p>
          <InspectionLinkBox inspection={done} />
        </div>
      ) : (
        <div className={pageStyles.form}>
          <InspectionKindFields
            kind={kind}
            mode={mode}
            onChange={(patch) => {
              if (patch.kind) setKind(patch.kind);
              if (patch.mode) setMode(patch.mode);
            }}
          />
          <textarea
            className={`${fieldStyles.textarea} ${styles.textarea}`}
            maxLength={500}
            value={note}
            aria-label="Observação para quem vai fazer a vistoria"
            placeholder="Observação (opcional): depois do conserto, a avaria do para-choque…"
            onChange={(e) => setNote(e.target.value)}
          />
        </div>
      )}
    </Modal>
  );
}

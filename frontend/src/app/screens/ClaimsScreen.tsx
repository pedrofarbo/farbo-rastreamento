import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from 'react-router-dom';

import { meApi } from '@/api/resources';
import { AuthImage } from '@/components/media/AuthImage';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Field, SelectField, TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { useVehicles } from '@/hooks/useVehicles';
import { formatDateTime } from '@/services/format';
import { CLAIM_KINDS, CLAIM_STATUS, claimEditable, claimKindLabel } from '@/services/occurrences';
import { compressImage } from '@/services/photos';
import type { ClaimKind } from '@/types';

import { BackIcon } from '../icons';
import local from './Occurrences.module.css';
import styles from './Screen.module.css';

export const claimsKey = ['me', 'claims'] as const;

/** "2026-10-23T14:05" (o campo datetime-local) de agora. */
function nowInput(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

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

/** As ocorrências do associado: as abertas e as encerradas, e o botão de abrir. */
export function ClaimsScreen() {
  const claims = useQuery({ queryKey: claimsKey, queryFn: meApi.claims });
  return (
    <div className={styles.screen}>
      <NavBar title="Ocorrências" back="/conta" />
      <p className={styles.muted}>
        Bateu, foi roubado ou teve outro problema? Abra a ocorrência aqui, com as fotos e o boletim: quem cuida do seu
        rastreamento acompanha e responde por aqui.
      </p>
      <Link to="/ocorrencias/nova" className={local.picker}>
        Abrir ocorrência
      </Link>
      {claims.isLoading ? (
        <Spinner label="Carregando" />
      ) : claims.error ? (
        <p className={styles.bad}>{(claims.error as Error).message}</p>
      ) : (claims.data ?? []).length === 0 ? (
        <p className={styles.muted}>Nenhuma ocorrência aberta.</p>
      ) : (
        (claims.data ?? []).map((c) => (
          <Link key={c.id} to={`/ocorrencias/${c.id}`} className={local.card}>
            <span className={local.head}>
              <strong>{claimKindLabel(c.kind)}</strong>
              <Badge tone={CLAIM_STATUS[c.status].tone}>{CLAIM_STATUS[c.status].label}</Badge>
            </span>
            <span className={styles.muted}>
              {c.vehicleName ? `${c.vehicleName} · ` : ''}
              {formatDateTime(c.occurredAt)} · {c.photos} {c.photos === 1 ? 'foto' : 'fotos'}
            </span>
          </Link>
        ))
      )}
    </div>
  );
}

/** Abrir a ocorrência: o que, quando, onde, o B.O. e as fotos (reduzidas antes de subir). */
export function ClaimNewScreen() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const { data: vehicles = [] } = useVehicles();
  const mine = vehicles.filter((v) => !v.shared);
  const [kind, setKind] = useState<ClaimKind>('collision');
  const [vehicleId, setVehicleId] = useState('');
  const [when, setWhen] = useState(nowInput);
  const [place, setPlace] = useState('');
  const [description, setDescription] = useState('');
  const [policeReport, setPoliceReport] = useState('');
  const [photos, setPhotos] = useState<File[]>([]);
  const [reportFile, setReportFile] = useState<File | null>(null);
  const [progress, setProgress] = useState('');
  const [error, setError] = useState('');
  // As miniaturas das fotos escolhidas (soltas quando a lista muda).
  const thumbs = useMemo(() => photos.map((p) => URL.createObjectURL(p)), [photos]);
  useEffect(() => () => thumbs.forEach((url) => URL.revokeObjectURL(url)), [thumbs]);

  const open = useMutation({
    mutationFn: async () => {
      const occurredAt = new Date(when);
      if (Number.isNaN(occurredAt.getTime())) throw new Error('Informe quando aconteceu.');
      if (!description.trim()) throw new Error('Conte o que aconteceu.');
      const claim = await meApi.openClaim({
        kind,
        vehicleId: vehicleId || mine[0]?.id || null,
        occurredAt: occurredAt.toISOString(),
        place,
        description,
        policeReport,
      });
      let sent = 0;
      for (const photo of photos) {
        setProgress(`Enviando as fotos (${sent + 1} de ${photos.length})…`);
        await meApi.addClaimFile(claim.id, 'photo', await compressImage(photo), photo.name);
        sent++;
      }
      if (reportFile) {
        setProgress('Enviando o boletim…');
        const file = reportFile.type.startsWith('image/') ? await compressImage(reportFile) : reportFile;
        await meApi.addClaimFile(claim.id, 'police_report', file, reportFile.name);
      }
      return claim;
    },
    onSuccess: (claim) => {
      setProgress('');
      void queryClient.invalidateQueries({ queryKey: claimsKey });
      notify({ tone: 'success', title: 'Ocorrência aberta', description: 'Você recebe aqui as respostas.' });
      navigate(`/ocorrencias/${claim.id}`, { replace: true });
    },
    onError: (err: Error) => {
      setProgress('');
      setError(err.message);
    },
  });

  return (
    <div className={styles.screen}>
      <NavBar title="Abrir ocorrência" back="/ocorrencias" />
      <section className={styles.section}>
        <div className={styles.form}>
          {error && (
            <p className={styles.bad} role="alert">
              {error}
            </p>
          )}
          <SelectField label="O que aconteceu" value={kind} onChange={(e) => setKind(e.target.value as ClaimKind)}>
            {CLAIM_KINDS.map((k) => (
              <option key={k.id} value={k.id}>
                {k.label}
              </option>
            ))}
          </SelectField>
          {mine.length > 1 && (
            <SelectField label="Veículo" value={vehicleId || mine[0].id} onChange={(e) => setVehicleId(e.target.value)}>
              {mine.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.name}
                  {v.plate ? ` · ${v.plate}` : ''}
                </option>
              ))}
            </SelectField>
          )}
          <TextField label="Quando" type="datetime-local" value={when} max={nowInput()} onChange={(e) => setWhen(e.target.value)} />
          <TextField label="Onde (opcional)" maxLength={200} value={place} placeholder="Rua, bairro, cidade" onChange={(e) => setPlace(e.target.value)} />
          <Field label="Conte o que aconteceu">
            {(fieldId) => (
              <textarea
                id={fieldId}
                className={local.textarea}
                maxLength={2000}
                value={description}
                placeholder="Como foi, se alguém se machucou, se houve outro veículo envolvido…"
                onChange={(e) => setDescription(e.target.value)}
              />
            )}
          </Field>
          <TextField
            label="Número do boletim de ocorrência (se tiver)"
            maxLength={60}
            value={policeReport}
            onChange={(e) => setPoliceReport(e.target.value)}
          />
          <div className={styles.row}>
            <label className={local.picker}>
              <input
                type="file"
                accept="image/*"
                multiple
                onChange={(e) => {
                  const files = Array.from(e.target.files ?? []);
                  e.target.value = '';
                  setPhotos((current) => [...current, ...files].slice(0, 20));
                }}
              />
              {photos.length > 0 ? `${photos.length} ${photos.length === 1 ? 'foto' : 'fotos'} · mais` : 'Fotos'}
            </label>
            <label className={local.picker}>
              <input
                type="file"
                accept="image/*,application/pdf"
                onChange={(e) => {
                  setReportFile(e.target.files?.[0] ?? null);
                  e.target.value = '';
                }}
              />
              {reportFile ? 'B.O. anexado' : 'Arquivo do B.O.'}
            </label>
          </div>
          {photos.length > 0 && (
            <div className={local.thumbs}>
              {thumbs.map((url, i) => (
                <img key={url} src={url} alt={`Foto ${i + 1}`} />
              ))}
            </div>
          )}
          {progress && <p className={styles.muted}>{progress}</p>}
          <Button variant="primary" block loading={open.isPending} onClick={() => open.mutate()}>
            Abrir ocorrência
          </Button>
        </div>
      </section>
    </div>
  );
}

/** A ocorrência: a situação, o que foi contado, as fotos e o histórico, com a nota e mais fotos. */
export function ClaimScreen() {
  const { id = '' } = useParams();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [note, setNote] = useState('');
  const [sending, setSending] = useState('');
  const claim = useQuery({ queryKey: [...claimsKey, id], queryFn: () => meApi.claim(id), enabled: Boolean(id) });
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: [...claimsKey, id] });
    void queryClient.invalidateQueries({ queryKey: claimsKey });
  };
  const addNote = useMutation({
    mutationFn: () => meApi.claimNote(id, note),
    onSuccess: () => {
      setNote('');
      refresh();
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para mandar', description: err.message }),
  });
  const addPhotos = async (files: File[]) => {
    try {
      for (let i = 0; i < files.length; i++) {
        setSending(`Enviando ${i + 1} de ${files.length}…`);
        await meApi.addClaimFile(id, 'photo', await compressImage(files[i]), files[i].name);
      }
      refresh();
    } catch (err) {
      notify({ tone: 'error', title: 'Não deu para enviar', description: (err as Error).message });
    } finally {
      setSending('');
    }
  };

  const c = claim.data;
  return (
    <div className={styles.screen}>
      <NavBar title="Ocorrência" back="/ocorrencias" />
      {claim.isLoading ? (
        <Spinner label="Carregando" />
      ) : !c ? (
        <p className={styles.bad}>Ocorrência não encontrada.</p>
      ) : (
        <>
          <section className={local.card}>
            <span className={local.head}>
              <strong>{claimKindLabel(c.kind)}</strong>
              <Badge tone={CLAIM_STATUS[c.status].tone}>{CLAIM_STATUS[c.status].label}</Badge>
            </span>
            <span className={styles.muted}>
              {c.vehicleName ? `${c.vehicleName}${c.plate ? ` · ${c.plate}` : ''} · ` : ''}
              {formatDateTime(c.occurredAt)}
            </span>
            {c.place && <span className={styles.muted}>{c.place}</span>}
            <p className={local.note}>{c.description}</p>
            {c.policeReport && <span className={styles.muted}>B.O.: {c.policeReport}</span>}
          </section>

          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>Fotos e documentos</h2>
            <div className={local.photos}>
              {(c.files ?? [])
                .filter((f) => f.contentType.startsWith('image/'))
                .map((f) => (
                  <AuthImage key={f.id} src={meApi.claimFileUrl(c.id, f.id)} alt={f.filename || 'Foto'} className={local.photo} />
                ))}
            </div>
            {(c.files ?? []).some((f) => f.contentType === 'application/pdf') && (
              <p className={styles.muted}>E {(c.files ?? []).filter((f) => f.contentType === 'application/pdf').length} PDF anexado.</p>
            )}
            {claimEditable(c.status) && (
              <label className={local.picker}>
                <input
                  type="file"
                  accept="image/*"
                  multiple
                  disabled={Boolean(sending)}
                  onChange={(e) => {
                    const files = Array.from(e.target.files ?? []);
                    e.target.value = '';
                    void addPhotos(files);
                  }}
                />
                {sending || 'Mandar mais fotos'}
              </label>
            )}
          </section>

          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>Histórico</h2>
            <ul className={local.timeline}>
              {(c.updates ?? []).map((u) => (
                <li key={u.id} className={u.authorKind === 'organization' ? local.org : undefined}>
                  <span className={styles.muted}>
                    {formatDateTime(u.createdAt)} · {u.authorKind === 'organization' ? 'Equipe' : 'Você'}
                    {u.status ? ` · ${CLAIM_STATUS[u.status].label}` : ''}
                  </span>
                  {u.note && <p className={local.note}>{u.note}</p>}
                </li>
              ))}
            </ul>
            {claimEditable(c.status) && (
              <div className={styles.form}>
                <textarea
                  className={local.textarea}
                  maxLength={1000}
                  value={note}
                  placeholder="Escreva para a equipe"
                  aria-label="Nota para a equipe"
                  onChange={(e) => setNote(e.target.value)}
                />
                <Button variant="secondary" disabled={!note.trim()} loading={addNote.isPending} onClick={() => addNote.mutate()}>
                  Mandar
                </Button>
              </div>
            )}
          </section>
        </>
      )}
    </div>
  );
}

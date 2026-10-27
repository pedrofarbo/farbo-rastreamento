import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { orgApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Field, TextField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateTime } from '@/services/format';
import { useAuth } from '@/stores/AuthContext';
import type { OrgAnnouncement } from '@/types';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

import pageStyles from '../Page.module.css';
import styles from './OrgAnnouncements.module.css';

const announcementsKey = ['org', 'announcements'] as const;

/** Quantos receberam: "1 de 1 no app · 1 por e-mail"; enquanto envia, "enviando para 120". */
export function deliveryLabel(a: Pick<OrgAnnouncement, 'recipients' | 'delivered' | 'emailed' | 'email' | 'finishedAt'>): string {
  if (!a.finishedAt) return `enviando para ${a.recipients} ${a.recipients === 1 ? 'associado' : 'associados'}…`;
  const app = `${a.delivered} de ${a.recipients} no app`;
  return a.email ? `${app} · ${a.emailed} por e-mail` : app;
}

/**
 * Os comunicados da associação ou seguradora: uma mensagem para todos os
 * associados ativos, na área de notificações do app (e no celular, para quem
 * ligou as notificações) e, se quiser, por e-mail. O Visualizador só vê o
 * histórico.
 */
export function OrgAnnouncementsPage() {
  const { canOperateOrg, orgMembership } = useAuth();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [email, setEmail] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState('');

  const list = useQuery({
    queryKey: announcementsKey,
    queryFn: orgApi.announcements,
    // O último ainda enviando: acompanha.
    refetchInterval: (query) => (query.state.data?.announcements.some((a) => !a.finishedAt) ? 3_000 : false),
  });
  const associates = useQuery({ queryKey: ['org', 'associates'], queryFn: orgApi.associates, enabled: canOperateOrg });
  const audience = (associates.data ?? []).filter((a) => a.active).length;
  const maxTitle = list.data?.maxTitle ?? 80;
  const maxBody = list.data?.maxBody ?? 1000;

  const send = useMutation({
    mutationFn: () => orgApi.announce({ title: title.trim(), body: body.trim(), email }),
    onSuccess: (a) => {
      setTitle('');
      setBody('');
      setEmail(false);
      setConfirming(false);
      setError('');
      void queryClient.invalidateQueries({ queryKey: announcementsKey });
      notify({
        tone: 'success',
        title: 'Comunicado enviado',
        description: `${a.recipients} ${a.recipients === 1 ? 'associado recebe' : 'associados recebem'} no app${a.email ? ' e por e-mail' : ''}.`,
      });
    },
    onError: (err: Error) => {
      setConfirming(false);
      setError(err.message);
    },
  });

  const review = () => {
    if (!title.trim()) return setError('Dê um título ao comunicado.');
    if (!body.trim()) return setError('Escreva a mensagem.');
    setError('');
    setConfirming(true);
  };

  const sent = list.data?.announcements ?? [];
  const paged = usePagedList(sent, 'announcements');

  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>Comunicados</h1>
            <p className={pageStyles.description}>
              Uma mensagem da {orgMembership?.name ?? 'organização'} para todos os associados: aparece na área de
              notificações do app (e no celular de quem ligou as notificações) e, se quiser, também por e-mail. São até{' '}
              {list.data?.maxPerDay ?? 5} por dia.
            </p>
          </div>
        </header>

        {canOperateOrg && (
          <Card title="Novo comunicado">
            <div className={pageStyles.form}>
              {error && (
                <div className={pageStyles.note} role="alert">
                  {error}
                </div>
              )}
              <TextField
                label="Título"
                maxLength={maxTitle}
                value={title}
                placeholder="Ex.: Assembleia geral no dia 30"
                disabled={confirming}
                onChange={(e) => setTitle(e.target.value)}
              />
              <Field label="Mensagem" hint={`${body.length} de ${maxBody} caracteres`}>
                {(id) => (
                  <textarea
                    id={id}
                    className={`${fieldStyles.textarea} ${styles.message}`}
                    maxLength={maxBody}
                    value={body}
                    disabled={confirming}
                    placeholder="O que os associados precisam saber."
                    onChange={(e) => setBody(e.target.value)}
                  />
                )}
              </Field>
              <label className={styles.check}>
                <input type="checkbox" checked={email} disabled={confirming} onChange={(e) => setEmail(e.target.checked)} />
                Mandar também por e-mail
              </label>
              {confirming ? (
                <div className={styles.confirm}>
                  <strong>
                    Enviar “{title.trim()}” para {audience} {audience === 1 ? 'associado' : 'associados'}
                    {email ? ', no app e por e-mail' : ', no app'}?
                  </strong>
                  <span className={styles.muted}>Depois de enviado, não dá para apagar.</span>
                  <div className={styles.row}>
                    <Button variant="primary" loading={send.isPending} onClick={() => send.mutate()}>
                      Enviar
                    </Button>
                    <Button variant="ghost" disabled={send.isPending} onClick={() => setConfirming(false)}>
                      Voltar
                    </Button>
                  </div>
                </div>
              ) : (
                <Button variant="primary" disabled={audience === 0} onClick={review}>
                  {audience === 0 ? 'Nenhum associado ativo' : `Enviar para ${audience} ${audience === 1 ? 'associado' : 'associados'}`}
                </Button>
              )}
            </div>
          </Card>
        )}

        <Card title="Enviados">
          {list.isLoading ? (
            <Spinner label="Carregando" />
          ) : sent.length === 0 ? (
            <EmptyState icon="📣" title="Nenhum comunicado ainda" description="Os comunicados enviados aparecem aqui, com quantos receberam." />
          ) : (
            <>
              <ul className={styles.list}>
                {paged.items.map((a) => (
                  <li key={a.id} className={styles.item}>
                    <div className={styles.head}>
                      <strong>{a.title}</strong>
                      <time className={styles.muted} dateTime={a.createdAt}>
                        {formatDateTime(a.createdAt)}
                      </time>
                    </div>
                    <p className={styles.body}>{a.body}</p>
                    <span className={styles.muted}>
                      {a.createdByName ? `${a.createdByName} · ` : ''}
                      {deliveryLabel(a)}
                    </span>
                  </li>
                ))}
              </ul>
              <Pager {...paged.pager} />
            </>
          )}
        </Card>
      </div>
    </div>
  );
}

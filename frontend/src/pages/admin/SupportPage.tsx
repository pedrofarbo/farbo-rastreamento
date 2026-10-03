import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';

import { whatsappApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateTime, formatRelative, formatTime } from '@/services/format';
import { useAuth } from '@/stores/AuthContext';
import type { ConversationMode, WhatsAppConversation, WhatsAppMessage, WhatsAppStatus } from '@/types';

import pageStyles from '../Page.module.css';
import styles from './Support.module.css';

/** O que a IA não lê também aparece para a equipe como aviso. */
const KIND_LABELS: Record<string, string> = {
  audio: 'Áudio',
  image: 'Imagem',
  video: 'Vídeo',
  document: 'Documento',
  sticker: 'Figurinha',
  location: 'Localização',
  contacts: 'Contato compartilhado',
};

const STATUS_LABELS: Record<string, string> = {
  sent: 'enviada',
  delivered: 'entregue',
  read: 'lida',
  failed: 'não entregue',
};

function contactTitle(c: WhatsAppConversation): string {
  return c.customerName || c.contactName || c.phone;
}

function preview(m: WhatsAppMessage | undefined): string {
  if (!m) return '';
  const who = m.author === 'BOT' ? 'IA: ' : m.author === 'AGENT' ? 'Equipe: ' : '';
  const body = m.kind === 'text' ? m.body : `[${KIND_LABELS[m.kind] ?? 'Mensagem'}] ${m.body}`;
  return who + body;
}

/**
 * Atendimento pelo WhatsApp: a IA responde os contatos e passa para a equipe
 * quando precisa. Aqui a equipe acompanha, assume e responde.
 */
export function SupportPage() {
  const [params, setParams] = useSearchParams();
  const selected = params.get('conversa');
  const [onlyAttention, setOnlyAttention] = useState(false);

  const status = useQuery({ queryKey: ['whatsapp', 'status'], queryFn: whatsappApi.status, refetchInterval: 15_000 });
  const list = useQuery({
    queryKey: ['whatsapp', 'conversations', onlyAttention],
    queryFn: () => whatsappApi.list(onlyAttention),
    refetchInterval: 5_000,
  });

  const select = (id: string | null) => setParams(id ? { conversa: id } : {}, { replace: !id });
  const conversations = list.data ?? [];
  const attention = status.data?.attention ?? 0;

  return (
    <div className={pageStyles.page}>
      <div className={`${pageStyles.inner} ${styles.inner}`}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>Atendimento</h1>
            <p className={pageStyles.description}>
              As conversas do WhatsApp. A IA responde os contatos e passa para a equipe quando precisa de uma
              pessoa; você também pode assumir qualquer conversa.
            </p>
          </div>
        </header>

        {status.data && <StatusNote status={status.data} />}

        <div className={`${styles.layout} ${selected ? styles.hasSelection : ''}`}>
          <div className={`${styles.panel} ${styles.listPanel}`}>
            <div className={pageStyles.tabs} role="tablist">
              <button
                type="button"
                role="tab"
                aria-selected={!onlyAttention}
                className={`${pageStyles.tab} ${!onlyAttention ? pageStyles.tabActive : ''}`}
                onClick={() => setOnlyAttention(false)}
              >
                Todas
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={onlyAttention}
                className={`${pageStyles.tab} ${onlyAttention ? pageStyles.tabActive : ''}`}
                onClick={() => setOnlyAttention(true)}
              >
                Esperando a equipe{attention > 0 ? ` (${attention})` : ''}
              </button>
            </div>
            {list.isLoading ? (
              <Spinner label="Carregando conversas" />
            ) : conversations.length === 0 ? (
              <EmptyState
                icon="💬"
                title={onlyAttention ? 'Ninguém esperando' : 'Nenhuma conversa ainda'}
                description={onlyAttention ? 'Toda conversa transferida já foi respondida.' : 'As mensagens do WhatsApp aparecem aqui.'}
              />
            ) : (
              <ul className={styles.list}>
                {conversations.map((c) => (
                  <li key={c.id}>
                    <button
                      type="button"
                      className={`${styles.item} ${c.id === selected ? styles.itemActive : ''}`}
                      onClick={() => select(c.id)}
                    >
                      <span className={styles.itemTop}>
                        <strong className={styles.itemName}>{contactTitle(c)}</strong>
                        <span className={styles.itemTime} title={formatDateTime(c.lastMessageAt)}>
                          {formatRelative(c.lastMessageAt)}
                        </span>
                      </span>
                      <span className={styles.itemPreview}>{preview(c.lastMessage)}</span>
                      <span className={styles.itemBadges}>
                        {c.needsAttention && <Badge tone="warning">Esperando a equipe</Badge>}
                        <Badge tone={c.mode === 'BOT' ? 'accent' : 'neutral'}>{c.mode === 'BOT' ? 'IA' : 'Equipe'}</Badge>
                        {c.customerId && <Badge tone="success">Cliente</Badge>}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <section className={styles.pane}>
            {selected ? (
              <ConversationPane
                id={selected}
                configured={status.data?.configured ?? false}
                aiReady={status.data?.aiReady ?? false}
                onBack={() => select(null)}
              />
            ) : (
              <div className={`${styles.panel} ${styles.placeholder}`}>
                <EmptyState icon="👈" title="Escolha uma conversa" description="O histórico e a resposta aparecem aqui." />
              </div>
            )}
          </section>
        </div>
      </div>
    </div>
  );
}

function StatusNote({ status }: { status: WhatsAppStatus }) {
  const webhook = `${window.location.origin}/api/whatsapp/webhook`;
  if (!status.configured) {
    return (
      <div className={styles.warning}>
        <strong>WhatsApp não configurado no servidor.</strong> Defina WHATSAPP_ACCESS_TOKEN,
        WHATSAPP_PHONE_NUMBER_ID, WHATSAPP_APP_SECRET e WHATSAPP_VERIFY_TOKEN e cadastre o webhook na Meta em{' '}
        <code>{webhook}</code> (campo "messages").
      </div>
    );
  }
  if (!status.aiReady) {
    return (
      <div className={styles.warning}>
        <strong>IA desligada:</strong> as conversas chegam aqui e só a equipe responde. Para ligar, defina
        ANTHROPIC_API_KEY no servidor.
      </div>
    );
  }
  return <p className={pageStyles.note}>IA respondendo os contatos ({status.model}).</p>;
}

function ConversationPane({
  id,
  configured,
  aiReady,
  onBack,
}: {
  id: string;
  configured: boolean;
  aiReady: boolean;
  onBack: () => void;
}) {
  const { canManage } = useAuth();
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const [text, setText] = useState('');
  const bottom = useRef<HTMLDivElement>(null);

  useEffect(() => setText(''), [id]);

  const details = useQuery({
    queryKey: ['whatsapp', 'conversation', id],
    queryFn: () => whatsappApi.get(id),
    refetchInterval: 4_000,
  });
  const c = details.data;
  const lastId = c?.messages[c.messages.length - 1]?.id;

  // Mensagem nova: rola até ela.
  useEffect(() => {
    bottom.current?.scrollIntoView({ block: 'end' });
  }, [lastId]);

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['whatsapp'] });
  };
  const send = useMutation({
    mutationFn: () => whatsappApi.send(id, text),
    onSuccess: () => {
      setText('');
      refresh();
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Mensagem não enviada', description: err.message }),
  });
  const mode = useMutation({
    mutationFn: (m: ConversationMode) => whatsappApi.setMode(id, m),
    onSuccess: (updated) => {
      refresh();
      notify({
        tone: 'success',
        title: updated.mode === 'BOT' ? 'Conversa devolvida para a IA' : 'Você assumiu a conversa',
      });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível mudar', description: err.message }),
  });

  if (details.isLoading || !c) {
    return (
      <Card>
        {details.isError ? (
          <EmptyState icon="⚠️" title="Conversa não encontrada" description={(details.error as Error).message} />
        ) : (
          <Spinner label="Carregando conversa" />
        )}
      </Card>
    );
  }

  const canSend = configured && c.windowOpen && text.trim() !== '' && !send.isPending;
  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && (event.ctrlKey || event.metaKey) && canSend) {
      event.preventDefault();
      send.mutate();
    }
  };

  return (
    <div className={`${styles.panel} ${styles.conversation}`}>
      <div className={styles.convHeader}>
        <Button size="small" variant="ghost" className={styles.back} onClick={onBack}>
          ← Conversas
        </Button>
        <div className={styles.convTitle}>
          <strong>{contactTitle(c)}</strong>
          <span className={styles.convMeta}>
            <a href={`https://wa.me/${c.waId}`} target="_blank" rel="noreferrer">
              {c.phone}
            </a>
            {c.contactName && c.customerName && c.contactName !== c.customerName && ` · no WhatsApp: ${c.contactName}`}
            {c.customerId &&
              (canManage ? (
                <>
                  {' · '}
                  <Link to={`/clientes/${c.customerId}`}>ficha do cliente</Link>
                </>
              ) : (
                ' · cliente'
              ))}
          </span>
        </div>
        <div className={styles.convActions}>
          <Badge tone={c.mode === 'BOT' ? 'accent' : 'neutral'}>{c.mode === 'BOT' ? 'IA respondendo' : 'Com a equipe'}</Badge>
          {c.mode === 'BOT' ? (
            <Button size="small" variant="secondary" loading={mode.isPending} onClick={() => mode.mutate('HUMAN')}>
              Assumir conversa
            </Button>
          ) : (
            <Button
              size="small"
              variant="secondary"
              loading={mode.isPending}
              disabled={!aiReady}
              title={aiReady ? undefined : 'A IA não está ligada no servidor'}
              onClick={() => mode.mutate('BOT')}
            >
              Devolver para a IA
            </Button>
          )}
        </div>
      </div>

      {c.mode === 'HUMAN' && c.handoffReason && (
        <div className={c.needsAttention ? styles.warning : styles.info}>
          {c.needsAttention ? 'Esperando a equipe' : 'Com a equipe'} · {c.handoffReason}
        </div>
      )}

      <div className={styles.messages}>
        {c.messages.map((m) => (
          <MessageBubble key={m.id} m={m} />
        ))}
        <div ref={bottom} />
      </div>

      <form
        className={styles.composer}
        onSubmit={(event) => {
          event.preventDefault();
          if (canSend) send.mutate();
        }}
      >
        {!c.windowOpen ? (
          <div className={styles.warning}>
            Passaram 24 horas desde a última mensagem do contato: o WhatsApp só deixa responder quando ele escrever
            de novo.
          </div>
        ) : (
          <>
            <textarea
              className={styles.input}
              rows={3}
              maxLength={4096}
              placeholder={
                c.mode === 'BOT'
                  ? 'Escreva para assumir a conversa (a IA para de responder)…'
                  : 'Escreva a resposta… (Ctrl+Enter envia)'
              }
              value={text}
              disabled={!configured}
              onChange={(event) => setText(event.target.value)}
              onKeyDown={onKeyDown}
            />
            <div className={styles.composerRow}>
              <span className={styles.hint}>
                Pode responder até {formatDateTime(new Date(new Date(c.lastInboundAt ?? c.lastMessageAt).getTime() + 24 * 3600_000))}.
              </span>
              <Button type="submit" variant="primary" size="small" loading={send.isPending} disabled={!canSend}>
                Enviar
              </Button>
            </div>
          </>
        )}
      </form>
    </div>
  );
}

function MessageBubble({ m }: { m: WhatsAppMessage }) {
  const out = m.direction === 'OUT';
  const author = m.author === 'BOT' ? 'IA' : m.author === 'AGENT' ? m.agentName || 'Equipe' : null;
  return (
    <div className={`${styles.bubbleRow} ${out ? styles.outRow : ''}`}>
      <div className={`${styles.bubble} ${out ? (m.author === 'BOT' ? styles.bot : styles.agent) : styles.contact}`}>
        {author && <span className={styles.author}>{author}</span>}
        {m.kind !== 'text' && <span className={styles.kind}>[{KIND_LABELS[m.kind] ?? 'Mensagem sem suporte'}]</span>}
        {m.body && <span className={styles.body}>{m.body}</span>}
        <span className={styles.meta} title={formatDateTime(m.createdAt)}>
          {formatTime(m.createdAt)}
          {out && m.status && ` · ${STATUS_LABELS[m.status] ?? m.status}`}
        </span>
        {m.error && <span className={styles.error}>{m.error}</span>}
      </div>
    </div>
  );
}

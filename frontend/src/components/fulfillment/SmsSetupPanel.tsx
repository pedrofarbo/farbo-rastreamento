import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { smsSetupApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import type { BadgeTone } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatTime } from '@/services/format';
import type { SmsSession, SmsStepView } from '@/types';

import styles from './SmsSetup.module.css';

/** "+5511999998888" → "(11) 99999-8888". */
export function phoneLabel(e164: string): string {
  const d = e164.replace(/\D/g, '');
  if (!d.startsWith('55') || (d.length !== 12 && d.length !== 13)) return e164;
  const local = d.slice(2);
  return `(${local.slice(0, 2)}) ${local.slice(2, -4)}-${local.slice(-4)}`;
}

/** O andamento de um SMS, como a tela mostra. */
export function stepStatus(step: SmsStepView): { label: string; tone: BadgeTone } {
  switch (step.status) {
    case 'pending':
      return { label: 'Na fila', tone: 'neutral' };
    case 'delivered':
      return { label: 'Entregue', tone: 'success' };
    case 'undelivered':
    case 'failed':
    case 'canceled':
      return { label: 'Não chegou', tone: 'danger' };
    case 'sent':
      return { label: 'Enviado', tone: 'accent' };
    default:
      return { label: 'Enviando', tone: 'accent' };
  }
}

const LIVE: SmsSession['status'][] = ['SENDING', 'WAITING'];

/**
 * Configurar o rastreador por SMS na ativação: os comandos do J16 vão para o
 * número do chip, um por vez, pelo SMSDev. Quando o rastreador conecta no
 * servidor, o pedido passa sozinho para "Configurado".
 */
export function SmsSetupPanel({ deviceId, fulfillmentId }: { deviceId: string; fulfillmentId: string }) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const [unlock, setUnlock] = useState(false);
  const [ask, setAsk] = useState(false);
  const [error, setError] = useState('');
  const view = useQuery({
    queryKey: ['sms-setup', deviceId],
    queryFn: () => smsSetupApi.get(deviceId),
    // Enquanto a configuração anda, a tela acompanha.
    refetchInterval: (q) => (q.state.data?.session && LIVE.includes(q.state.data.session.status) ? 3000 : false),
  });
  const session = view.data?.session ?? null;
  const live = session !== null && LIVE.includes(session.status);
  // Conectou com a tela aberta: avisa e recarrega o pedido, que mudou para
  // "Configurado" (e este painel sai, junto com a etapa).
  const status = session?.status;
  const previous = useRef(status);
  useEffect(() => {
    const was = previous.current;
    previous.current = status;
    if (status !== 'DONE' || !was || !LIVE.includes(was)) return;
    notify({ tone: 'success', title: 'Rastreador conectado', description: session?.note || 'A configuração por SMS pegou.' });
    queryClient.invalidateQueries({ queryKey: ['fulfillment', fulfillmentId] });
    queryClient.invalidateQueries({ queryKey: ['fulfillments'] });
    queryClient.invalidateQueries({ queryKey: ['vehicles'] });
  }, [status, session?.note, fulfillmentId, notify, queryClient]);

  const refresh = (updated: SmsSession) => {
    queryClient.setQueryData(['sms-setup', deviceId], (old: typeof view.data) => (old ? { ...old, session: updated } : old));
    void view.refetch();
  };
  const start = useMutation({
    mutationFn: () => smsSetupApi.start(deviceId, { fulfillmentId, unlock, query: ask }),
    onSuccess: (s) => {
      setError('');
      refresh(s);
      if (s.status === 'FAILED') setError(s.error);
      else notify({ tone: 'success', title: 'Configuração por SMS iniciada', description: `Para ${phoneLabel(s.phone)}` });
    },
    onError: (err: Error) => setError(err.message),
  });
  const cancel = useMutation({
    mutationFn: () => smsSetupApi.cancel(session!.id),
    onSuccess: refresh,
    onError: (err: Error) => setError(err.message),
  });

  if (view.isLoading) return <Spinner label="Carregando a configuração por SMS" />;
  if (!view.data) return null;
  const { plan, enabled } = view.data;
  const blocked = !enabled || plan.problems.length > 0;

  return (
    <section className={styles.panel} aria-label="Configurar por SMS">
      <header className={styles.head}>
        <strong>Configurar por SMS</strong>
        {plan.phone && <span className={styles.muted}>Chip {phoneLabel(plan.phone)}</span>}
      </header>

      {session && session.status !== 'CANCELED' && <SessionView session={session} />}

      {!live && (
        <>
          {!enabled && (
            <p className={styles.warn}>
              O envio de SMS não está configurado (SMSDev). Os comandos abaixo podem ser mandados à mão pelo celular.
            </p>
          )}
          {plan.problems.map((p) => (
            <p key={p} className={styles.warn}>
              {p}{' '}
              <Link to="/dispositivos" className={styles.link}>
                Abrir o cadastro
              </Link>
            </p>
          ))}
          {plan.steps.length > 0 && (
            <ol className={styles.commands}>
              {unlock && (
                <li>
                  <code>{plan.unlock.text}</code>
                  <span className={styles.muted}>{plan.unlock.label}</span>
                </li>
              )}
              {plan.steps.map((s) => (
                <li key={s.kind}>
                  <code>{s.text}</code>
                  <span className={styles.muted}>{s.label}</span>
                </li>
              ))}
              {ask && (
                <li>
                  <code>{plan.query.text}</code>
                  <span className={styles.muted}>{plan.query.label}</span>
                </li>
              )}
            </ol>
          )}
          {plan.steps.length > 0 && (
            <div className={styles.options}>
              <label>
                <input type="checkbox" checked={unlock} onChange={(e) => setUnlock(e.target.checked)} /> Destravar o canal de
                comandos antes (se o rastreador não responder)
              </label>
              <label>
                <input type="checkbox" checked={ask} onChange={(e) => setAsk(e.target.checked)} /> Pedir a configuração de volta
                no fim (PARAM#)
              </label>
            </div>
          )}
          {error && (
            <p className={styles.danger} role="alert">
              {error}
            </p>
          )}
          <div className={styles.actions}>
            <Button size="small" variant="primary" disabled={blocked} loading={start.isPending} onClick={() => start.mutate()}>
              {session && session.status !== 'DONE' ? 'Mandar de novo' : 'Configurar por SMS'}
            </Button>
            <span className={styles.muted}>
              Um SMS por vez; quando o rastreador conectar no servidor, o pedido passa para "Configurado".
            </span>
          </div>
        </>
      )}

      {live && (
        <div className={styles.actions}>
          <Button size="small" variant="ghost" loading={cancel.isPending} onClick={() => cancel.mutate()}>
            Interromper
          </Button>
        </div>
      )}
    </section>
  );
}

function SessionView({ session }: { session: SmsSession }) {
  const headline: Record<SmsSession['status'], { text: string; tone: BadgeTone }> = {
    SENDING: { text: 'Mandando os comandos…', tone: 'accent' },
    WAITING: { text: 'Esperando o rastreador conectar…', tone: 'warning' },
    DONE: { text: 'Rastreador conectado', tone: 'success' },
    FAILED: { text: 'Não deu certo', tone: 'danger' },
    TIMEOUT: { text: 'O rastreador não conectou', tone: 'danger' },
    CANCELED: { text: 'Interrompida', tone: 'neutral' },
  };
  const h = headline[session.status];
  return (
    <div className={styles.session} aria-live="polite">
      <Badge tone={h.tone} pulse={session.status === 'SENDING' || session.status === 'WAITING'}>
        {h.text}
      </Badge>
      <ol className={styles.steps}>
        {session.steps.map((s, i) => {
          const st = stepStatus(s);
          return (
            <li key={`${s.kind}-${i}`}>
              <code>{s.text}</code>
              <span className={styles.stepEnd}>
                {s.sentAt && <span className={styles.muted}>{formatTime(s.sentAt)}</span>}
                <Badge tone={st.tone}>{st.label}</Badge>
              </span>
              {s.error && <span className={styles.danger}>{s.error}</span>}
            </li>
          );
        })}
      </ol>
      {session.replies.length > 0 && (
        <div className={styles.replies}>
          <span className={styles.muted}>Respostas do rastreador</span>
          {session.replies.map((r) => (
            <p key={r.at + r.body}>
              <span className={styles.muted}>{formatTime(r.at)}</span> {r.body}
            </p>
          ))}
        </div>
      )}
      {session.note && <p className={styles.ok}>{session.note}</p>}
      {session.error && <p className={styles.danger}>{session.error}</p>}
    </div>
  );
}

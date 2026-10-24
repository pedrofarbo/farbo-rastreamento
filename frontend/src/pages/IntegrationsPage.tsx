import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { integrationsApi } from '@/api/resources';
import { BackButton } from '@/components/ui/BackButton';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateTime, formatEvent, formatRelative } from '@/services/format';

import pageStyles from './Page.module.css';
import styles from './Integrations.module.css';

/** Os eventos que dá para escolher no webhook (sem nenhum: todos). */
export const WEBHOOK_EVENTS = [
  'IGNITION_ON',
  'IGNITION_OFF',
  'OVERSPEED',
  'GEOFENCE_ENTER',
  'GEOFENCE_EXIT',
  'POWER_LOSS',
  'SOS',
  'LOW_BATTERY',
  'VEHICLE_BATTERY_LOW',
  'VIBRATION',
  'ENGINE_CUT_ACK',
  'ENGINE_RESUME_ACK',
  'DEVICE_CONNECTED',
  'DEVICE_DISCONNECTED',
];

/** O que a lista mostra da última entrega. */
export function deliveryLine(w: { lastStatus: number | null; lastError: string; lastSentAt: string | null; failures: number }): string {
  if (!w.lastSentAt) return 'nenhum envio ainda';
  const when = formatRelative(w.lastSentAt);
  if (!w.lastError) return `último envio ${when}: ${w.lastStatus}`;
  return `último envio ${when}: ${w.lastError}${w.failures > 1 ? ` (${w.failures} falhas seguidas)` : ''}`;
}

/**
 * As integrações: as chaves da API (o sistema da empresa lê os veículos, as
 * posições e os eventos) e os webhooks (os eventos chegam por POST,
 * assinados). base: /api/me (o titular da conta) ou /api/org (o
 * Administrador da organização).
 */
export function IntegrationsPage({ base, backTo }: { base: '/api/me' | '/api/org'; backTo?: string }) {
  const api = useMemo(() => integrationsApi(base), [base]);
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const keysKey = ['integrations', base, 'keys'];
  const hooksKey = ['integrations', base, 'webhooks'];
  const keys = useQuery({ queryKey: keysKey, queryFn: api.keys });
  const hooks = useQuery({ queryKey: hooksKey, queryFn: api.webhooks });
  const [naming, setNaming] = useState(false);
  const [name, setName] = useState('');
  const [secret, setSecret] = useState<{ title: string; value: string; hint: string } | null>(null);
  const [url, setUrl] = useState('');
  const [events, setEvents] = useState<string[]>([]);
  const [error, setError] = useState('');

  const createKey = useMutation({
    mutationFn: () => api.createKey(name),
    onSuccess: ({ secret: value }) => {
      setNaming(false);
      setName('');
      void queryClient.invalidateQueries({ queryKey: keysKey });
      setSecret({ title: 'Sua chave da API', value, hint: 'Guarde agora: ela não aparece de novo. Use no cabeçalho Authorization: Bearer <chave>.' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para gerar', description: err.message }),
  });
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeKey(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: keysKey });
      notify({ tone: 'success', title: 'Chave revogada', description: 'O sistema que a usava parou na hora.' });
    },
  });
  const createHook = useMutation({
    mutationFn: () => api.createWebhook({ url, events }),
    onSuccess: (hook) => {
      setUrl('');
      setEvents([]);
      setError('');
      void queryClient.invalidateQueries({ queryKey: hooksKey });
      setSecret({
        title: 'O segredo do webhook',
        value: hook.secret ?? '',
        hint: 'Guarde agora: ele assina cada envio (X-Farbo-Signature: sha256=HMAC do "<X-Farbo-Timestamp>.<corpo>").',
      });
    },
    onError: (err: Error) => setError(err.message),
  });
  const toggle = useMutation({
    mutationFn: (h: { id: string; url: string; events: string[]; active: boolean }) => api.updateWebhook(h.id, h),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: hooksKey }),
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para mudar', description: err.message }),
  });
  const test = useMutation({
    mutationFn: (id: string) => api.testWebhook(id),
    onSuccess: (h) => {
      void queryClient.invalidateQueries({ queryKey: hooksKey });
      notify(
        h.lastError
          ? { tone: 'error', title: 'O teste não chegou', description: h.lastError }
          : { tone: 'success', title: 'Teste entregue', description: `O endereço respondeu ${h.lastStatus}.` },
      );
    },
  });
  const remove = useMutation({
    mutationFn: (id: string) => api.deleteWebhook(id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: hooksKey }),
  });

  const origin = typeof window !== 'undefined' ? window.location.origin : '';
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      notify({ tone: 'success', title: 'Copiado' });
    } catch {
      /* sem permissão: a pessoa copia na mão */
    }
  };

  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <div className={pageStyles.titleRow}>
              {backTo && <BackButton to={backTo} />}
              <h1 className={pageStyles.title}>Integrações</h1>
            </div>
            <p className={pageStyles.description}>
              Ligue o sistema da sua empresa à Farbo: pela API, ele lê os veículos, as posições e os eventos; pelos
              webhooks, recebe os eventos na hora em que acontecem.
            </p>
          </div>
        </header>

        <Card
          title="Chaves da API"
          actions={
            <Button size="small" variant="primary" onClick={() => setNaming(true)}>
              Nova chave
            </Button>
          }
          flush
        >
          {keys.isLoading ? (
            <Spinner label="Carregando" />
          ) : (keys.data ?? []).length === 0 ? (
            <EmptyState icon="🔑" title="Nenhuma chave" description="Gere uma chave para cada sistema que vai ler os dados." />
          ) : (
            <ul className={styles.list}>
              {(keys.data ?? []).map((k) => (
                <li key={k.id} className={styles.item}>
                  <div className={styles.what}>
                    <strong>{k.name}</strong>
                    <span className={styles.mono}>fk_{k.prefix}_…</span>
                    <span className={styles.muted}>
                      criada em {formatDateTime(k.createdAt)} ·{' '}
                      {k.lastUsedAt ? `usada ${formatRelative(k.lastUsedAt)}` : 'ainda não usada'}
                    </span>
                  </div>
                  {k.revokedAt ? (
                    <Badge tone="neutral">Revogada</Badge>
                  ) : (
                    <Button size="small" variant="ghost" loading={revoke.isPending && revoke.variables === k.id} onClick={() => revoke.mutate(k.id)}>
                      Revogar
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </Card>

        <Card title="Webhooks" subtitle="Os eventos dos veículos chegam por POST (JSON) no endereço HTTPS do seu sistema.">
          <div className={pageStyles.form}>
            {error && <div className={pageStyles.note}>{error}</div>}
            <TextField label="Endereço (HTTPS)" value={url} placeholder="https://sistema.suaempresa.com.br/farbo" onChange={(e) => setUrl(e.target.value)} />
            <fieldset className={styles.events}>
              <legend>Eventos (nenhum marcado: todos)</legend>
              {WEBHOOK_EVENTS.map((e) => (
                <label key={e} className={styles.event}>
                  <input
                    type="checkbox"
                    checked={events.includes(e)}
                    onChange={(ev) => setEvents(ev.target.checked ? [...events, e] : events.filter((x) => x !== e))}
                  />
                  {formatEvent(e)}
                </label>
              ))}
            </fieldset>
            <Button variant="primary" disabled={!url.trim()} loading={createHook.isPending} onClick={() => createHook.mutate()}>
              Adicionar webhook
            </Button>
          </div>
          {(hooks.data ?? []).length > 0 && (
            <ul className={styles.list}>
              {(hooks.data ?? []).map((h) => (
                <li key={h.id} className={styles.item}>
                  <div className={styles.what}>
                    <strong className={styles.mono}>{h.url}</strong>
                    <span className={styles.muted}>
                      {h.events.length === 0 ? 'todos os eventos' : h.events.map((e) => formatEvent(e)).join(', ')}
                    </span>
                    <span className={h.lastError ? styles.bad : styles.muted}>{deliveryLine(h)}</span>
                  </div>
                  <div className={styles.actions}>
                    <Badge tone={h.active ? 'success' : 'neutral'}>{h.active ? 'Ligado' : 'Desligado'}</Badge>
                    <Button size="small" variant="secondary" loading={test.isPending && test.variables === h.id} onClick={() => test.mutate(h.id)}>
                      Testar
                    </Button>
                    <Button size="small" variant="ghost" onClick={() => toggle.mutate({ id: h.id, url: h.url, events: h.events, active: !h.active })}>
                      {h.active ? 'Desligar' : 'Ligar'}
                    </Button>
                    <Button size="small" variant="ghost" onClick={() => remove.mutate(h.id)}>
                      Apagar
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Card>

        <Card title="Como usar a API">
          <div className={styles.docs}>
            <p>Cada pedido leva a chave no cabeçalho. São até 5 pedidos por segundo.</p>
            <pre className={styles.code}>{`curl -H "Authorization: Bearer fk_SUA_CHAVE" ${origin}/api/v1/vehicles`}</pre>
            <ul>
              <li>
                <code>GET /api/v1/vehicles</code>: os veículos, com a última posição, a ignição e a voltagem.
              </li>
              <li>
                <code>GET /api/v1/vehicles/&#123;id&#125;/positions?from=…&amp;to=…</code>: as posições (RFC 3339, até 7 dias).
              </li>
              <li>
                <code>GET /api/v1/events?after=&#123;id&#125;</code>: os eventos depois do último lido (o <code>next</code> da
                resposta é o próximo <code>after</code>).
              </li>
            </ul>
            <p>
              No webhook, confira a assinatura: <code>X-Farbo-Signature</code> é <code>sha256=</code> mais o HMAC-SHA256
              (com o segredo) de <code>X-Farbo-Timestamp</code>, um ponto e o corpo. Responda 2xx em até 10 s; 20 falhas
              seguidas desligam o webhook.
            </p>
          </div>
        </Card>
      </div>

      <Modal
        open={naming}
        title="Nova chave da API"
        onClose={() => setNaming(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setNaming(false)}>
              Cancelar
            </Button>
            <Button variant="primary" disabled={!name.trim()} loading={createKey.isPending} onClick={() => createKey.mutate()}>
              Gerar
            </Button>
          </>
        }
      >
        <TextField label="Para qual sistema" maxLength={60} value={name} placeholder="ERP da frota" onChange={(e) => setName(e.target.value)} />
      </Modal>

      <Modal
        open={secret !== null}
        title={secret?.title ?? ''}
        onClose={() => setSecret(null)}
        footer={
          <>
            <Button variant="secondary" onClick={() => secret && void copy(secret.value)}>
              Copiar
            </Button>
            <Button variant="primary" onClick={() => setSecret(null)}>
              Já guardei
            </Button>
          </>
        }
      >
        <div className={pageStyles.form}>
          <pre className={styles.code}>{secret?.value}</pre>
          <p className={pageStyles.description}>{secret?.hint}</p>
        </div>
      </Modal>
    </div>
  );
}

/** As integrações do titular da conta. */
export function MyIntegrationsPage() {
  return <IntegrationsPage base="/api/me" backTo="/frota" />;
}

/** As integrações da associação ou seguradora (o Administrador). */
export function OrgIntegrationsPage() {
  return <IntegrationsPage base="/api/org" />;
}

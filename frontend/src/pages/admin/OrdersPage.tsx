import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';

import { fulfillmentsApi, shippingIntegrationApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { FulfillmentAdminModal } from '@/components/fulfillment/FulfillmentAdminModal';
import { LabelActions } from '@/components/fulfillment/LabelActions';
import { ShippingWallet, walletKey } from '@/components/fulfillment/ShippingWallet';
import fstyles from '@/components/fulfillment/Fulfillment.module.css';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateTime, formatRelative } from '@/services/format';
import { stepOf, trackerTone } from '@/services/fulfillment';
import { useAuth } from '@/stores/AuthContext';
import type { Fulfillment, TrackerStatus } from '@/types';

import styles from '../Page.module.css';

/** As etapas em que a fila se divide — o que a central faz a seguir. */
const STAGES: { id: string; label: string; hint: string; statuses: TrackerStatus[] }[] = [
  { id: 'supplier', label: 'Com o fornecedor', hint: 'aguardando chegar', statuses: ['AWAITING_SUPPLIER'] },
  { id: 'base', label: 'Na base', hint: 'inclui aguardando o chip', statuses: ['AT_BASE', 'AWAITING_CHIP'] },
  { id: 'config', label: 'Em configuração', hint: 'vincular o aparelho', statuses: ['CONFIGURING'] },
  { id: 'ready', label: 'Prontos para envio', hint: 'comprar a etiqueta', statuses: ['CONFIGURED'] },
  { id: 'transit', label: 'A caminho', hint: 'rastreio automático', statuses: ['SHIPPED', 'IN_TRANSIT'] },
];

/**
 * Pedidos: o chip M2M e o rastreador de cada veículo, do fornecedor até a
 * casa do cliente. É a fila de trabalho da central.
 */
export function OrdersPage() {
  const { canManage } = useAuth();
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const [showAll, setShowAll] = useState(false);
  const [stage, setStage] = useState<string | null>(null);
  const [opened, setOpened] = useState<string | null>(null);

  const list = useQuery({ queryKey: ['fulfillments', showAll], queryFn: () => fulfillmentsApi.list(showAll) });

  // Volta do "Conectar Melhor Envios" e do pagamento do saldo (avisa uma
  // vez só, mesmo com o efeito rodando de novo).
  const handledReturn = useRef('');
  useEffect(() => {
    const result = params.get('melhorenvio');
    const topUp = params.get('saldo');
    if (!result && !topUp) return;
    if (handledReturn.current === params.toString()) return;
    handledReturn.current = params.toString();
    if (result === 'conectado') notify({ tone: 'success', title: 'Melhor Envios conectado' });
    else if (result) notify({ tone: 'error', title: 'Melhor Envios não conectado', description: params.get('motivo') ?? undefined });
    if (topUp) {
      notify({
        tone: 'success',
        title: 'Pagamento enviado ao Melhor Envios',
        description: 'O saldo aparece na carteira assim que ele confirmar.',
      });
      void queryClient.invalidateQueries({ queryKey: walletKey });
    }
    setParams({}, { replace: true });
  }, [params, setParams, notify, queryClient]);

  const all = list.data ?? [];
  const counts = useMemo(
    () => Object.fromEntries(STAGES.map((s) => [s.id, all.filter((f) => s.statuses.includes(f.trackerStatus)).length])),
    [all],
  );
  const current = STAGES.find((s) => s.id === stage);
  const rows = current ? all.filter((f) => current.statuses.includes(f.trackerStatus)) : all;

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Pedidos</h1>
            <p className={styles.description}>
              O chip M2M e o rastreador de cada veículo, do fornecedor até a casa do cliente. O cliente
              acompanha as mesmas etapas no painel dele.
            </p>
          </div>
          <div className={styles.actions}>
            <Button variant={showAll ? 'ghost' : 'primary'} size="small" onClick={() => setShowAll(false)}>
              Em andamento
            </Button>
            <Button variant={showAll ? 'primary' : 'ghost'} size="small" onClick={() => setShowAll(true)}>
              Todos
            </Button>
          </div>
        </header>

        {canManage && <ShippingIntegrationCard />}

        <div className={billing.tiles} style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))' }}>
          {STAGES.map((s) => (
            <button
              key={s.id}
              type="button"
              className={`${billing.tile} ${fstyles.stageTile} ${stage === s.id ? fstyles.stageActive : ''}`}
              aria-pressed={stage === s.id}
              onClick={() => setStage(stage === s.id ? null : s.id)}
            >
              <span className={billing.tileLabel}>{s.label}</span>
              <span className={billing.tileValue}>{counts[s.id] ?? 0}</span>
              <span className={billing.tileHint}>{s.hint}</span>
            </button>
          ))}
        </div>

        <Card
          title={current ? current.label : showAll ? 'Todos os pedidos' : 'Em andamento'}
          subtitle={current ? 'Clique de novo na etapa para ver todos.' : undefined}
          flush
        >
          {list.isLoading ? (
            <Spinner label="Carregando pedidos" />
          ) : rows.length === 0 ? (
            <EmptyState icon="📦" title="Nenhum pedido aqui" description="Os pedidos entram por Novo veículo." />
          ) : (
            <div className={styles.tableWrap}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>Cliente</th>
                    <th>Veículo</th>
                    <th>Chip M2M</th>
                    <th>Rastreador</th>
                    <th>Envio</th>
                    <th>Atualizado</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {rows.map((f) => (
                    <OrderRow key={f.id} f={f} onOpen={() => setOpened(f.id)} />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </div>

      <FulfillmentAdminModal fulfillmentId={opened} onClose={() => setOpened(null)} />
    </div>
  );
}

function OrderRow({ f, onOpen }: { f: Fulfillment; onOpen: () => void }) {
  return (
    <tr>
      <td>
        <Link to={`/clientes/${f.customerId}`}>
          <strong>{f.customerName}</strong>
        </Link>
      </td>
      <td>
        {f.vehicleName}
        {f.vehiclePlate && <div className={billing.muted}>{f.vehiclePlate}</div>}
      </td>
      <td>
        <Badge tone={f.chipStatus === 'SEPARATED' ? 'success' : 'neutral'}>{stepOf('CHIP', f.chipStatus)?.short}</Badge>
      </td>
      <td>
        <Badge tone={trackerTone(f.trackerStatus)}>{stepOf('TRACKER', f.trackerStatus)?.short}</Badge>
      </td>
      <td>
        {f.trackingCode ? (
          <>
            <div>{f.trackingCode}</div>
            <div className={billing.muted}>{f.shippingService}</div>
            <LabelActions fulfillment={f} compact />
          </>
        ) : f.deliveryArranged ? (
          <span>Entrega combinada</span>
        ) : (
          <span className={billing.muted}>—</span>
        )}
      </td>
      <td title={formatDateTime(f.updatedAt)}>{formatRelative(f.updatedAt)}</td>
      <td>
        <Button size="small" variant="primary" onClick={onOpen}>
          Abrir
        </Button>
      </td>
    </tr>
  );
}

/** Situação da conexão com o Melhor Envios, com Conectar/Desconectar. */
function ShippingIntegrationCard() {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const integration = useQuery({ queryKey: ['integration', 'melhorenvio'], queryFn: shippingIntegrationApi.get });

  const connect = useMutation({
    mutationFn: shippingIntegrationApi.connect,
    // O Melhor Envios pede login e autorização e volta para esta tela.
    onSuccess: ({ url }) => window.location.assign(url),
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível conectar', description: err.message }),
  });
  const disconnect = useMutation({
    mutationFn: shippingIntegrationApi.disconnect,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['integration', 'melhorenvio'] });
      notify({ tone: 'success', title: 'Melhor Envios desconectado' });
    },
  });

  const data = integration.data;
  if (!data) return null;

  return (
    <Card
      title={
        <span style={{ display: 'inline-flex', gap: 'var(--space-2)', alignItems: 'center' }}>
          Melhor Envios
          {data.configured && data.sandbox && <Badge tone="warning">sandbox</Badge>}
          {data.connected ? (
            <Badge tone="success" dot>
              Conectado
            </Badge>
          ) : (
            <Badge tone="neutral">Desconectado</Badge>
          )}
        </span>
      }
      subtitle="Etiquetas compradas pelo sistema e rastreio automático das entregas."
      actions={
        data.canConnect &&
        (data.connected ? (
          <Button size="small" variant="ghost" loading={disconnect.isPending} onClick={() => disconnect.mutate()}>
            Desconectar
          </Button>
        ) : (
          <Button size="small" variant="primary" loading={connect.isPending} onClick={() => connect.mutate()}>
            Conectar Melhor Envios
          </Button>
        ))
      }
    >
      <div className={fstyles.actionGroup}>
        {!data.configured ? (
          <span className={fstyles.hint}>
            Configure no servidor MELHORENVIO_CLIENT_ID e MELHORENVIO_CLIENT_SECRET (aplicativo em Integrações &gt;
            Área Dev. do Melhor Envios), com o callback <code>{data.redirectUrl}</code>.
          </span>
        ) : data.connected ? (
          <span className={fstyles.hint}>
            {data.accountName.trim() || 'Conta conectada'}
            {data.accountEmail ? ` · ${data.accountEmail}` : ''}
            {data.expiresAt ? ` · acesso renovado sozinho (vale até ${formatDateTime(data.expiresAt)})` : ''}
          </span>
        ) : (
          <span className={fstyles.hint}>
            Clique em Conectar e autorize no Melhor Envios. O callback cadastrado no aplicativo precisa ser{' '}
            <code>{data.redirectUrl}</code>.
          </span>
        )}
        {data.accountError && <div className={fstyles.warning}>{data.accountError}</div>}
        {data.connected && !data.accountError && <ShippingWallet panelUrl={data.panelUrl} payWithAbacate={data.payWithAbacate} />}
        {data.missingOrigin.length > 0 && (
          <div className={fstyles.warning}>
            Para comprar etiquetas, falta configurar o remetente no servidor: {data.missingOrigin.join(', ')}.
          </div>
        )}
      </div>
    </Card>
  );
}

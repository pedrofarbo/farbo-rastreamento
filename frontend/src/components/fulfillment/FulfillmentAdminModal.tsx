import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { devicesApi, fulfillmentsApi, shippingIntegrationApi, vehiclesApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatMoney } from '@/services/format';
import { CHIP_STEPS, TRACKER_STEPS, stepIndex, stepOf } from '@/services/fulfillment';
import { useAuth } from '@/stores/AuthContext';
import type { Fulfillment, FulfillmentTrack, ShippingQuote } from '@/types';

import { FulfillmentTimeline } from './FulfillmentTimeline';
import { SmsSetupPanel } from './SmsSetupPanel';
import styles from './Fulfillment.module.css';

/** Status do Melhor Envios que pedem atenção da central. */
const SHIPPING_TROUBLE: Record<string, string> = {
  undelivered: 'A transportadora não conseguiu entregar.',
  paused: 'Entrega interrompida: o destinatário precisa agir.',
  suspended: 'Envio suspenso pela transportadora.',
};

/**
 * Um pedido na visão da central: as duas linhas do tempo e, embaixo, a
 * próxima ação de cada uma — do fornecedor até a etiqueta do Melhor Envios.
 */
export function FulfillmentAdminModal({
  fulfillmentId,
  onClose,
}: {
  fulfillmentId: string | null;
  onClose: () => void;
}) {
  const { canManage } = useAuth();
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const open = fulfillmentId !== null;

  const [note, setNote] = useState('');
  const [deviceId, setDeviceId] = useState('');
  const [quotes, setQuotes] = useState<ShippingQuote[] | null>(null);
  const [serviceId, setServiceId] = useState<number | null>(null);
  const [fixing, setFixing] = useState<{ track: FulfillmentTrack; status: string } | null>(null);
  // Entrega em mãos pede confirmação: avisa o cliente por e-mail.
  const [handing, setHanding] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    setNote('');
    setDeviceId('');
    setQuotes(null);
    setServiceId(null);
    setFixing(null);
    setHanding(false);
    setError('');
  }, [fulfillmentId]);

  const query = useQuery({
    queryKey: ['fulfillment', fulfillmentId],
    queryFn: () => fulfillmentsApi.get(fulfillmentId as string),
    enabled: open,
  });
  const f = query.data;
  const devices = useQuery({ queryKey: ['devices'], queryFn: devicesApi.list, enabled: open && f?.trackerStatus === 'CONFIGURING' });
  const vehicles = useQuery({ queryKey: ['vehicles'], queryFn: vehiclesApi.list, enabled: open && f?.trackerStatus === 'CONFIGURING' });
  const integration = useQuery({
    queryKey: ['integration', 'melhorenvio'],
    queryFn: shippingIntegrationApi.get,
    enabled: open && canManage && f?.trackerStatus === 'CONFIGURED',
  });

  const done = (updated: Fulfillment, message: string) => {
    queryClient.setQueryData(['fulfillment', updated.id], updated);
    queryClient.invalidateQueries({ queryKey: ['fulfillments'] });
    queryClient.invalidateQueries({ queryKey: ['customer', updated.customerId] });
    setNote('');
    setFixing(null);
    setHanding(false);
    setError('');
    notify({ tone: 'success', title: message });
  };
  const fail = (err: Error) => {
    setError(err.message);
    if (err instanceof ApiError && (err.body as { code?: string })?.code === 'MELHORENVIO_NOT_CONNECTED') {
      queryClient.invalidateQueries({ queryKey: ['integration', 'melhorenvio'] });
    }
  };

  const change = useMutation({
    mutationFn: (input: { track: FulfillmentTrack; status: string; deviceId?: string | null }) =>
      fulfillmentsApi.change(fulfillmentId as string, { ...input, note }),
    onSuccess: (updated, input) => {
      if (input.deviceId) queryClient.invalidateQueries({ queryKey: ['vehicles'] });
      done(updated, stepOf(input.track, input.status)?.label ?? 'Etapa atualizada');
    },
    onError: fail,
  });
  const quote = useMutation({
    mutationFn: () => fulfillmentsApi.quote(fulfillmentId as string),
    onSuccess: (list) => {
      setQuotes(list);
      setServiceId(list.find((q) => !q.error)?.serviceId ?? null);
      setError('');
    },
    onError: fail,
  });
  // serviceId 0 conclui uma etiqueta já paga que estava em geração.
  const buy = useMutation({
    mutationFn: (service: number) => fulfillmentsApi.buyLabel(fulfillmentId as string, service),
    onSuccess: (updated) =>
      done(
        updated,
        updated.trackerStatus === 'SHIPPED'
          ? 'Etiqueta comprada: rastreador enviado'
          : 'Etiqueta paga; o Melhor Envios está gerando — o envio é concluído sozinho',
      ),
    onError: fail,
  });
  const sync = useMutation({
    mutationFn: () => fulfillmentsApi.sync(fulfillmentId as string),
    onSuccess: (updated) => done(updated, 'Rastreio atualizado'),
    onError: fail,
  });
  const busy = change.isPending || quote.isPending || buy.isPending || sync.isPending;

  // Aparelhos livres (ou o que já está neste veículo).
  const linked = new Map((vehicles.data ?? []).filter((v) => v.deviceId).map((v) => [v.deviceId as string, v.id]));
  const freeDevices = (devices.data ?? []).filter((d) => !linked.has(d.id) || linked.get(d.id) === f?.vehicleId);

  const chipIndex = f ? stepIndex('CHIP', f.chipStatus) : 0;
  const nextChip = f ? CHIP_STEPS[chipIndex + 1] : undefined;
  const chipSeparated = f?.chipStatus === 'SEPARATED';
  const chosen = quotes?.find((q) => q.serviceId === serviceId);

  return (
    <Modal
      open={open}
      wide
      title={f ? `Pedido · ${f.vehicleName}${f.vehiclePlate ? ` (${f.vehiclePlate})` : ''}` : 'Pedido'}
      onClose={onClose}
      footer={
        <Button variant="ghost" onClick={onClose}>
          Fechar
        </Button>
      }
    >
      {!f ? (
        <Spinner label="Carregando pedido" />
      ) : (
        <>
          <p className={styles.hint}>
            Cliente: <Link to={`/clientes/${f.customerId}`}>{f.customerName}</Link>
          </p>
          <FulfillmentTimeline
            chipStatus={f.chipStatus}
            trackerStatus={f.trackerStatus}
            events={f.events}
            carrier={f.shippingService}
            trackingCode={f.trackingCode}
          />

          <div className={styles.actions}>
            {error && <div className={styles.warning}>{error}</div>}

            {/* Chip M2M */}
            <div className={styles.actionGroup}>
              <span className={styles.actionTitle}>Chip M2M</span>
              {nextChip ? (
                <div className={styles.actionRow}>
                  <Button
                    size="small"
                    variant="secondary"
                    disabled={busy}
                    onClick={() => change.mutate({ track: 'CHIP', status: nextChip.status })}
                  >
                    Avançar: {nextChip.label}
                  </Button>
                </div>
              ) : (
                <span className={styles.hint}>Chip pronto para a configuração.</span>
              )}
            </div>

            {/* Rastreador */}
            <div className={styles.actionGroup}>
              <span className={styles.actionTitle}>Rastreador</span>

              {f.trackerStatus === 'AWAITING_SUPPLIER' && (
                <>
                  <div className={styles.actionRow}>
                    <Button
                      size="small"
                      variant="secondary"
                      disabled={busy}
                      onClick={() => change.mutate({ track: 'TRACKER', status: 'AT_BASE' })}
                    >
                      Avançar: Rastreador chegou em nossa base
                    </Button>
                  </div>
                  {stepIndex('CHIP', f.chipStatus) < stepIndex('CHIP', 'AT_BASE') && (
                    <span className={styles.hint}>
                      O chip ainda não chegou: o rastreador fica em "Aguardando chegada do chip M2M".
                    </span>
                  )}
                </>
              )}

              {(f.trackerStatus === 'AT_BASE' || f.trackerStatus === 'AWAITING_CHIP') && (
                <>
                  <div className={styles.actionRow}>
                    <Button
                      size="small"
                      variant="secondary"
                      disabled={busy || !chipSeparated}
                      onClick={() => change.mutate({ track: 'TRACKER', status: 'CONFIGURING' })}
                    >
                      Avançar: Rastreador em configuração
                    </Button>
                  </div>
                  {!chipSeparated && (
                    <span className={styles.hint}>Antes, o chip precisa estar "Separado para configuração".</span>
                  )}
                </>
              )}

              {f.trackerStatus === 'CONFIGURING' && (
                <>
                  <SelectField
                    label="Aparelho configurado (IMEI)"
                    hint="Fica vinculado ao veículo: assim que for instalado, ele aparece no mapa."
                    value={deviceId}
                    onChange={(e) => setDeviceId(e.target.value)}
                  >
                    <option value="">Escolha o aparelho</option>
                    {freeDevices.map((d) => (
                      <option key={d.id} value={d.id}>
                        {d.imei} {d.model ? `· ${d.model}` : ''}
                      </option>
                    ))}
                  </SelectField>
                  {deviceId && <SmsSetupPanel deviceId={deviceId} fulfillmentId={f.id} />}
                  <div className={styles.actionRow}>
                    <Button
                      size="small"
                      variant="secondary"
                      disabled={busy || !deviceId}
                      onClick={() => change.mutate({ track: 'TRACKER', status: 'CONFIGURED', deviceId })}
                    >
                      Avançar: Rastreador configurado
                    </Button>
                    <Link to="/dispositivos" className={styles.hint}>
                      Cadastrar aparelho
                    </Link>
                  </div>
                </>
              )}

              {f.trackerStatus === 'CONFIGURED' && f.shippingOrderId && (
                <>
                  <div className={styles.warning}>
                    Etiqueta paga ({f.shippingService}
                    {f.shippingProtocol ? ` · ${f.shippingProtocol}` : ''}) e em geração no Melhor Envios. Assim que ela
                    sair, o envio é concluído sozinho e o cliente recebe o código de rastreio.
                  </div>
                  <div className={styles.actionRow}>
                    <Button
                      size="small"
                      variant="primary"
                      loading={buy.isPending}
                      disabled={busy || !canManage}
                      onClick={() => buy.mutate(0)}
                    >
                      Concluir envio
                    </Button>
                  </div>
                </>
              )}

              {f.trackerStatus === 'CONFIGURED' &&
                !f.shippingOrderId &&
                (!canManage ? (
                  <span className={styles.hint}>Pronto para envio: a etiqueta é comprada por um administrador.</span>
                ) : integration.data && !integration.data.connected ? (
                  <div className={styles.warning}>
                    {integration.data.configured
                      ? 'Conecte o Melhor Envios para comprar a etiqueta.'
                      : 'Envio pelo Melhor Envios não configurado no servidor.'}{' '}
                    <Link to="/pedidos">Ir para Pedidos</Link>
                  </div>
                ) : quotes === null ? (
                  <div className={styles.actionRow}>
                    <Button size="small" variant="primary" loading={quote.isPending} disabled={busy} onClick={() => quote.mutate()}>
                      Cotar frete
                    </Button>
                    <span className={styles.hint}>Para o endereço de entrega do pedido.</span>
                  </div>
                ) : (
                  <>
                    <div className={styles.quotes} role="radiogroup" aria-label="Serviço de frete">
                      {quotes.map((q) => (
                        <label key={q.serviceId} className={`${styles.quote} ${q.error ? styles.quoteDisabled : ''}`}>
                          <input
                            type="radio"
                            name="frete"
                            disabled={Boolean(q.error)}
                            checked={serviceId === q.serviceId}
                            onChange={() => setServiceId(q.serviceId)}
                          />
                          <span className={styles.quoteName}>
                            <strong>
                              {q.company} {q.service}
                            </strong>
                            <br />
                            <span className={styles.hint}>
                              {q.error || `${q.deliveryDays} ${q.deliveryDays === 1 ? 'dia útil' : 'dias úteis'}`}
                            </span>
                          </span>
                          {!q.error && <span className={styles.quotePrice}>{formatMoney(q.priceCents)}</span>}
                        </label>
                      ))}
                    </div>
                    <div className={styles.actionRow}>
                      <Button
                        size="small"
                        variant="primary"
                        loading={buy.isPending}
                        disabled={busy || !chosen}
                        onClick={() => chosen && buy.mutate(chosen.serviceId)}
                      >
                        Comprar etiqueta{chosen ? ` · ${formatMoney(chosen.priceCents)}` : ''}
                      </Button>
                      <Button size="small" variant="ghost" disabled={busy} onClick={() => quote.mutate()}>
                        Cotar de novo
                      </Button>
                    </div>
                    <span className={styles.hint}>
                      Debita o saldo da carteira do Melhor Envios, gera a etiqueta e avisa o cliente por e-mail com o
                      código de rastreio.
                    </span>
                  </>
                ))}

              {/* Entrega em mãos: retirada na base ou entrega própria, sem etiqueta. */}
              {f.trackerStatus === 'CONFIGURED' &&
                !f.shippingOrderId &&
                (handing ? (
                  <>
                    <div className={styles.warning}>
                      Confirma que o rastreador já está com o cliente? Ele recebe o e-mail de que o rastreador chegou e
                      passa a ver os instaladores no painel.
                    </div>
                    <div className={styles.actionRow}>
                      <Button
                        size="small"
                        variant="primary"
                        loading={change.isPending}
                        disabled={busy}
                        onClick={() => change.mutate({ track: 'TRACKER', status: 'DELIVERED' })}
                      >
                        Confirmar entrega
                      </Button>
                      <Button size="small" variant="ghost" disabled={busy} onClick={() => setHanding(false)}>
                        Cancelar
                      </Button>
                    </div>
                  </>
                ) : (
                  <div className={styles.actionRow}>
                    <Button size="small" variant="ghost" disabled={busy} onClick={() => setHanding(true)}>
                      Marcar como entregue em mãos
                    </Button>
                    <span className={styles.hint}>Sem etiqueta: retirada na base ou entrega própria.</span>
                  </div>
                ))}

              {(f.trackerStatus === 'SHIPPED' || f.trackerStatus === 'IN_TRANSIT') && (
                <>
                  {SHIPPING_TROUBLE[f.shippingStatus] && (
                    <div className={styles.warning}>{SHIPPING_TROUBLE[f.shippingStatus]}</div>
                  )}
                  <span className={styles.hint}>
                    {f.shippingService}
                    {f.shippingPriceCents !== null ? ` · frete ${formatMoney(f.shippingPriceCents)}` : ''}
                    {f.shippingProtocol ? ` · ${f.shippingProtocol}` : ''}
                  </span>
                  <div className={styles.actionRow}>
                    {f.labelUrl && (
                      <Button
                        size="small"
                        variant="primary"
                        onClick={() => window.open(f.labelUrl, '_blank', 'noopener,noreferrer')}
                      >
                        Imprimir etiqueta
                      </Button>
                    )}
                    <Button size="small" variant="secondary" loading={sync.isPending} disabled={busy} onClick={() => sync.mutate()}>
                      Atualizar rastreio
                    </Button>
                    <Button
                      size="small"
                      variant="ghost"
                      disabled={busy}
                      onClick={() => change.mutate({ track: 'TRACKER', status: 'DELIVERED' })}
                    >
                      Marcar como entregue
                    </Button>
                  </div>
                  <span className={styles.hint}>
                    Em trânsito e entregue mudam sozinhos pelo rastreio do Melhor Envios.
                  </span>
                </>
              )}

              {f.trackerStatus === 'DELIVERED' && (
                <span className={styles.hint}>
                  Entregue ao cliente. Ele já vê os instaladores no painel.
                  {!f.shippingOrderId && ' Marcado por engano? Corrija a etapa para "Rastreador configurado".'}
                </span>
              )}
            </div>

            <TextField
              label="Observação da próxima mudança (opcional)"
              placeholder="Ex.: NF do fornecedor, lote do chip…"
              value={note}
              maxLength={300}
              onChange={(e) => setNote(e.target.value)}
            />

            {/* Correção manual */}
            {fixing ? (
              <div className={styles.actionGroup}>
                <span className={styles.actionTitle}>Corrigir etapa</span>
                <div className={styles.actionRow}>
                  <SelectField
                    label="Linha do tempo"
                    value={fixing.track}
                    onChange={(e) => {
                      const track = e.target.value as FulfillmentTrack;
                      setFixing({ track, status: track === 'CHIP' ? f.chipStatus : f.trackerStatus });
                    }}
                  >
                    <option value="CHIP">Chip M2M</option>
                    <option value="TRACKER">Rastreador</option>
                  </SelectField>
                  <SelectField
                    label="Etapa"
                    value={fixing.status}
                    onChange={(e) => setFixing({ ...fixing, status: e.target.value })}
                  >
                    {(fixing.track === 'CHIP' ? CHIP_STEPS : TRACKER_STEPS).map((step) => (
                      <option key={step.status} value={step.status}>
                        {step.label}
                      </option>
                    ))}
                  </SelectField>
                </div>
                <div className={styles.actionRow}>
                  <Button
                    size="small"
                    variant="secondary"
                    disabled={busy}
                    onClick={() => change.mutate({ track: fixing.track, status: fixing.status })}
                  >
                    Aplicar correção
                  </Button>
                  <Button size="small" variant="ghost" onClick={() => setFixing(null)}>
                    Cancelar
                  </Button>
                </div>
              </div>
            ) : (
              <div>
                <Button
                  size="small"
                  variant="ghost"
                  onClick={() => setFixing({ track: 'TRACKER', status: f.trackerStatus })}
                >
                  Corrigir uma etapa
                </Button>
              </div>
            )}
          </div>
        </>
      )}
    </Modal>
  );
}

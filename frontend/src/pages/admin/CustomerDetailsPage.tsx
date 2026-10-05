import { useCallback, useState } from 'react';
import type { ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';

import { customersApi, devicesApi, vehicleInputFrom, vehiclesApi } from '@/api/resources';
import type { VehicleInput } from '@/api/resources';
import { AddressModal } from '@/components/address/AddressModal';
import billing from '@/components/billing/Billing.module.css';
import { CustomerStatus, InvoiceStatus } from '@/components/billing/InvoiceStatus';
import { MonthlyPrice } from '@/components/billing/MonthlyPrice';
import { NewVehicleWizard } from '@/components/billing/NewVehicleWizard';
import { FulfillmentAdminModal } from '@/components/fulfillment/FulfillmentAdminModal';
import { PixPaymentModal } from '@/components/billing/PixPaymentModal';
import {
  DEFAULT_SUBSCRIPTION,
  SubscriptionFields,
  subscriptionFromDraft,
} from '@/components/billing/SubscriptionFields';
import type { SubscriptionDraft } from '@/components/billing/SubscriptionFields';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { CustomerAlertsSection } from '@/components/alerts/CustomerAlertsSection';
import { TextField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { EMPTY_VEHICLE, VehicleFields } from '@/components/vehicle/VehicleFields';
import {
  centsToInput,
  formatDateOnly,
  formatDateTime,
  formatAddress,
  formatAddressLines,
  formatDeviceStatus,
  formatMoney,
  parseMoney,
  todayISO,
} from '@/services/format';
import { stepOf, trackerTone } from '@/services/fulfillment';
import { subscriptionsByVehicle, subscriptionsWithoutVehicle } from '@/services/subscriptions';
import { HISTORY_RETENTION_OPTIONS } from '@/types';
import type { HistoryRetention, Invoice, PixPayment, Subscription, VehicleView } from '@/types';

import styles from '../Page.module.css';
import { CustomerReferralCard } from './affiliates/CustomerReferralCard';

interface Confirmation {
  title: string;
  body: ReactNode;
  label: string;
  success: string;
  tone: 'primary' | 'danger';
  run: () => Promise<unknown>;
}

export function CustomerDetailsPage() {
  const { id = '' } = useParams<{ id: string }>();
  const { notify } = useToast();
  const queryClient = useQueryClient();

  const customer = useQuery({ queryKey: ['customer', id], queryFn: () => customersApi.get(id) });
  const devices = useQuery({ queryKey: ['devices'], queryFn: devicesApi.list });
  const allVehicles = useQuery({ queryKey: ['vehicles'], queryFn: vehiclesApi.list });

  const [profile, setProfile] = useState<{ name: string; phone: string; document: string } | null>(null);
  // Veículo sem assinatura ativa: nova assinatura para ele.
  const [reactivate, setReactivate] = useState<{ vehicle: VehicleView; draft: SubscriptionDraft } | null>(null);
  const [editSubscription, setEditSubscription] = useState<{ sub: Subscription; planName: string; price: string } | null>(null);
  const [payment, setPayment] = useState<{ invoice: Invoice; paymentUrl: string; pixCode: string } | null>(null);
  const [newInvoice, setNewInvoice] = useState<{ description: string; amount: string; dueDate: string; paymentUrl: string; pixCode: string } | null>(null);
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null);
  const [formError, setFormError] = useState('');
  const [pix, setPix] = useState<Invoice | null>(null);
  const [refund, setRefund] = useState<{ payment: PixPayment; reason: string } | null>(null);
  const [ordering, setOrdering] = useState(false);
  // Assinatura antiga sem veículo: a central informa qual é.
  const [attach, setAttach] = useState<{ subscription: Subscription; input: VehicleInput } | null>(null);
  const [following, setFollowing] = useState<string | null>(null);
  const [editingAddress, setEditingAddress] = useState(false);

  const refresh = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['customer', id] });
    queryClient.invalidateQueries({ queryKey: ['customers'] });
    queryClient.invalidateQueries({ queryKey: ['vehicles'] });
  }, [queryClient, id]);

  // Uma mutação genérica: cada ação passa a própria chamada e mensagem.
  const action = useMutation({
    mutationFn: async ({ run }: { run: () => Promise<unknown>; success: string }) => run(),
    onSuccess: (_, { success }) => {
      refresh();
      notify({ tone: 'success', title: success });
      setProfile(null);
      setReactivate(null);
      setAttach(null);
      setEditSubscription(null);
      setPayment(null);
      setNewInvoice(null);
      setConfirmation(null);
      setRefund(null);
      setFormError('');
    },
    onError: (error: Error) => {
      setFormError(error.message);
      notify({ tone: 'error', title: 'Não foi possível concluir', description: error.message });
    },
  });
  const run = (success: string, fn: () => Promise<unknown>) => action.mutate({ run: fn, success });

  if (customer.isLoading) return <Spinner label="Carregando cliente" />;
  const data = customer.data;
  if (!data) {
    return <EmptyState title="Cliente não encontrado" description={<Link to="/clientes">Voltar aos clientes</Link>} />;
  }

  const activeSubs = data.subscriptions.filter((s) => s.status === 'ACTIVE').length;
  const byVehicle = subscriptionsByVehicle(data.subscriptions);
  const withoutVehicle = subscriptionsWithoutVehicle(data.subscriptions);
  const awaitingInstall = data.vehicles.filter((v) => !v.device?.lastSeenAt).length;
  // O pedido mais recente de cada veículo (a lista vem do mais novo).
  const orderOf = new Map<string, (typeof data.fulfillments)[number]>();
  for (const f of data.fulfillments) if (!orderOf.has(f.vehicleId)) orderOf.set(f.vehicleId, f);

  // Rastreador livre = sem veículo, ou já vinculado ao próprio veículo da linha.
  const linkedTo = new Map(
    (allVehicles.data ?? []).filter((v) => v.deviceId).map((v) => [v.deviceId as string, v.id]),
  );
  const availableDevices = (vehicle: VehicleView) =>
    (devices.data ?? []).filter((d) => !linkedTo.has(d.id) || linkedTo.get(d.id) === vehicle.id);

  const openModal = (open: () => void) => {
    setFormError('');
    open();
  };

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <Link to="/clientes" className={billing.muted}>
              ← Clientes
            </Link>
            <h1 className={styles.title} style={{ display: 'flex', gap: 'var(--space-3)', alignItems: 'center' }}>
              {data.name} <CustomerStatus customer={data} />
            </h1>
            <p className={styles.description}>
              {[data.email, data.phone, data.document].filter(Boolean).join(' · ')}
            </p>
          </div>
          <div className={styles.actions}>
            <Button
              variant="secondary"
              onClick={() => openModal(() => setProfile({ name: data.name, phone: data.phone, document: data.document }))}
            >
              Editar dados
            </Button>
            <Button
              variant="secondary"
              disabled={!data.active}
              onClick={() => run(`Convite reenviado para ${data.email}`, () => customersApi.invite(id))}
            >
              Reenviar convite
            </Button>
            <Button
              variant={data.active ? 'danger' : 'primary'}
              onClick={() =>
                setConfirmation({
                  title: data.active ? 'Desativar cliente?' : 'Reativar cliente?',
                  body: data.active
                    ? 'O cliente perde o acesso ao painel na hora. Veículos, assinaturas e faturas continuam como estão.'
                    : 'O cliente volta a entrar no painel com a senha que já tinha.',
                  label: data.active ? 'Desativar' : 'Reativar',
                  success: data.active ? 'Cliente desativado' : 'Cliente reativado',
                  tone: data.active ? 'danger' : 'primary',
                  run: () =>
                    customersApi.update(id, {
                      name: data.name, phone: data.phone, document: data.document, active: !data.active,
                    }),
                })
              }
            >
              {data.active ? 'Desativar' : 'Reativar'}
            </Button>
          </div>
        </header>

        <div className={billing.tiles}>
          <div className={billing.tile}>
            <span className={billing.tileLabel}>Assinaturas ativas</span>
            <span className={billing.tileValue}>{activeSubs}</span>
          </div>
          <div className={billing.tile}>
            <span className={billing.tileLabel}>Veículos</span>
            <span className={billing.tileValue}>{data.vehicles.length}</span>
            <span className={billing.tileHint}>
              {awaitingInstall > 0 ? `${awaitingInstall} aguardando instalação` : 'todos com rastreador'}
            </span>
          </div>
          <div className={`${billing.tile} ${data.overdueInvoices > 0 ? billing.tileDanger : ''}`}>
            <span className={billing.tileLabel}>Em aberto</span>
            <span className={billing.tileValue}>{formatMoney(data.openAmountCents)}</span>
            <span className={billing.tileHint}>
              {data.overdueInvoices > 0 ? `${data.overdueInvoices} vencida(s)` : `${data.openInvoices} fatura(s)`}
            </span>
          </div>
        </div>

        {data.suspended && (
          <div className={`${billing.banner} ${billing.bannerDanger}`}>
            <span>
              <strong>Acesso do cliente suspenso</strong> por fatura vencida além da tolerância. Dar
              baixa na fatura devolve o acesso na hora.
            </span>
          </div>
        )}

        <Card
          title="Endereço de entrega"
          subtitle="Para onde vão os rastreadores contratados. Sem ele, o cliente não contrata pelo painel."
          actions={
            <Button size="small" variant="secondary" onClick={() => setEditingAddress(true)}>
              {data.deliveryAddress ? 'Alterar' : 'Cadastrar'}
            </Button>
          }
        >
          {data.deliveryAddress ? (
            <div className={billing.deliveryText}>
              {formatAddressLines(data.deliveryAddress).map((line, index) =>
                index === 0 ? <strong key={line}>{line}</strong> : <span key={line} className={billing.muted}>{line}</span>,
              )}
            </div>
          ) : (
            <span className={billing.muted}>Nenhum endereço cadastrado.</span>
          )}
        </Card>

        <Card
          title="Histórico dos veículos"
          subtitle="Por quantos dias guardar o trajeto e os eventos. O que passa do prazo é apagado; cada veículo pode ter o próprio prazo."
        >
          <select
            className={fieldStyles.select}
            aria-label="Prazo do histórico do cliente"
            value={data.historyRetentionDays ?? ''}
            disabled={action.isPending}
            onChange={(event) => {
              const days = event.target.value ? (Number(event.target.value) as HistoryRetention) : null;
              run('Prazo do histórico atualizado', () => customersApi.setHistoryRetention(id, days));
            }}
          >
            <option value="">Padrão da central ({data.defaultHistoryDays} dias)</option>
            {HISTORY_RETENTION_OPTIONS.map((days) => (
              <option key={days} value={days}>
                {days} dias
              </option>
            ))}
          </select>
        </Card>

        <CustomerReferralCard customerId={id} referral={data.affiliate} />

        <Card
          title="Veículos"
          subtitle="Cada veículo tem o seu rastreador e a sua assinatura. Tudo entra por Novo veículo."
          actions={
            <Button size="small" variant="primary" onClick={() => setOrdering(true)}>
              Novo veículo
            </Button>
          }
          flush
        >
          {data.vehicles.length === 0 && withoutVehicle.length === 0 ? (
            <EmptyState
              icon="🚗"
              title="Nenhum veículo"
              description="Novo veículo cadastra o veículo, lança o rastreador e começa a assinatura de uma vez."
            />
          ) : (
            <div className={styles.tableWrap}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>Veículo</th>
                    <th>Rastreador</th>
                    <th>Assinatura</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {data.vehicles.map((vehicle) => {
                    const sub = byVehicle.get(vehicle.id);
                    const active = sub?.status === 'ACTIVE';
                    return (
                      <tr key={vehicle.id}>
                        <td>
                          <Link to={`/veiculos/${vehicle.id}`}>
                            <strong>{vehicle.name}</strong>
                          </Link>
                          <div className={billing.muted}>
                            {[vehicle.plate, vehicle.brand, vehicle.model, vehicle.year].filter(Boolean).join(' · ') || '—'}
                          </div>
                          <select
                            className={`${fieldStyles.select} ${billing.inlineSelect}`}
                            aria-label={`Histórico de ${vehicle.name}`}
                            value={vehicle.historyRetentionDays ?? ''}
                            disabled={action.isPending}
                            onChange={(event) => {
                              const days = event.target.value ? (Number(event.target.value) as HistoryRetention) : null;
                              run(`Histórico de ${vehicle.name}: ${days ? `${days} dias` : 'prazo do cliente'}`, () =>
                                vehiclesApi.setHistoryRetention(vehicle.id, days),
                              );
                            }}
                          >
                            <option value="">
                              Histórico: do cliente ({data.historyRetentionDays ?? data.defaultHistoryDays} dias)
                            </option>
                            {HISTORY_RETENTION_OPTIONS.map((days) => (
                              <option key={days} value={days}>
                                Histórico: {days} dias
                              </option>
                            ))}
                          </select>
                          {sub?.deliveryAddress && (
                            <span className={billing.subAddress} title={formatAddress(sub.deliveryAddress)}>
                              Enviado para {sub.deliveryAddress.street}, {sub.deliveryAddress.number} ·{' '}
                              {sub.deliveryAddress.city}/{sub.deliveryAddress.state}
                            </span>
                          )}
                        </td>
                        <td>
                          <select
                            className={fieldStyles.select}
                            aria-label={`Rastreador de ${vehicle.name}`}
                            value={vehicle.deviceId ?? ''}
                            disabled={action.isPending}
                            onChange={(event) => {
                              // Lido agora: quando a chamada rodar, o select controlado já
                              // terá voltado ao valor antigo.
                              const deviceId = event.target.value || null;
                              run(deviceId ? 'Rastreador vinculado' : 'Rastreador desvinculado', () =>
                                vehiclesApi.update(vehicle.id, vehicleInputFrom(vehicle, { deviceId })),
                              );
                            }}
                          >
                            <option value="">aguardando instalação</option>
                            {availableDevices(vehicle).map((device) => (
                              <option key={device.id} value={device.id}>
                                {device.imei} {device.model ? `· ${device.model}` : ''}
                              </option>
                            ))}
                          </select>
                          <div style={{ marginTop: 'var(--space-1)', display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap', alignItems: 'center' }}>
                            {vehicle.device?.lastSeenAt ? (
                              <Badge
                                tone={vehicle.device.status === 'ONLINE' ? 'success' : vehicle.device.status === 'STALE' ? 'warning' : 'neutral'}
                                dot
                              >
                                {formatDeviceStatus(vehicle.device.status)}
                              </Badge>
                            ) : orderOf.get(vehicle.id) ? (
                              <Badge tone={trackerTone(orderOf.get(vehicle.id)!.trackerStatus)}>
                                {stepOf('TRACKER', orderOf.get(vehicle.id)!.trackerStatus)?.short}
                              </Badge>
                            ) : (
                              <Badge tone="warning">Aguardando instalação</Badge>
                            )}
                            {orderOf.get(vehicle.id) && (
                              <Button size="small" variant="ghost" onClick={() => setFollowing(orderOf.get(vehicle.id)!.id)}>
                                Pedido
                              </Button>
                            )}
                          </div>
                        </td>
                        <td>
                          {active && sub ? (
                            <>
                              <div className={billing.subscriptionLine}>
                                {sub.planName} · <MonthlyPrice sub={sub} />
                              </div>
                              <div className={billing.muted}>
                                dia {sub.dueDay} · próxima fatura {formatDateOnly(sub.nextDueDate)}
                              </div>
                            </>
                          ) : sub ? (
                            <Badge tone="neutral">Encerrada</Badge>
                          ) : (
                            <Badge tone="warning">Sem assinatura</Badge>
                          )}
                        </td>
                        <td>
                          <div className={styles.actions}>
                            {active && sub ? (
                              <>
                                <Button
                                  size="small"
                                  variant="ghost"
                                  onClick={() =>
                                    openModal(() => setEditSubscription({ sub, planName: sub.planName, price: centsToInput(sub.priceCents) }))
                                  }
                                >
                                  Editar assinatura
                                </Button>
                                <Button
                                  size="small"
                                  variant="ghost"
                                  onClick={() =>
                                    setConfirmation({
                                      title: 'Encerrar assinatura?',
                                      body: `As faturas de ${vehicle.name} que ainda não venceram são canceladas; as vencidas continuam em aberto. O veículo continua na ficha, sem assinatura.`,
                                      label: 'Encerrar assinatura',
                                      success: 'Assinatura encerrada',
                                      tone: 'danger',
                                      run: () => customersApi.cancelSubscription(sub.id),
                                    })
                                  }
                                >
                                  Encerrar
                                </Button>
                              </>
                            ) : (
                              <>
                                <Button
                                  size="small"
                                  variant="secondary"
                                  onClick={() =>
                                    openModal(() =>
                                      setReactivate({
                                        vehicle,
                                        draft: sub
                                          ? { preset: 'custom', planName: sub.planName, price: centsToInput(sub.priceCents), dueDay: sub.dueDay }
                                          : DEFAULT_SUBSCRIPTION,
                                      }),
                                    )
                                  }
                                >
                                  Reativar assinatura
                                </Button>
                                <Button
                                  size="small"
                                  variant="ghost"
                                  onClick={() =>
                                    setConfirmation({
                                      title: 'Excluir veículo?',
                                      body: `${vehicle.name} sai da ficha do cliente e o rastreador dele fica livre para outro veículo.`,
                                      label: 'Excluir veículo',
                                      success: 'Veículo excluído',
                                      tone: 'danger',
                                      run: () => vehiclesApi.remove(vehicle.id),
                                    })
                                  }
                                >
                                  Excluir
                                </Button>
                              </>
                            )}
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                  {withoutVehicle.map((sub) => (
                    <tr key={sub.id}>
                      <td>
                        <strong>Sem veículo</strong>
                        <div className={billing.muted}>Assinatura de antes do cadastro por veículo.</div>
                      </td>
                      <td>—</td>
                      <td>
                        <div className={billing.subscriptionLine}>
                          {sub.planName} · <MonthlyPrice sub={sub} />
                        </div>
                        <div className={billing.muted}>
                          dia {sub.dueDay} · próxima fatura {formatDateOnly(sub.nextDueDate)}
                        </div>
                      </td>
                      <td>
                        <div className={styles.actions}>
                          <Button
                            size="small"
                            variant="primary"
                            onClick={() => openModal(() => setAttach({ subscription: sub, input: EMPTY_VEHICLE }))}
                          >
                            Informar veículo
                          </Button>
                          <Button
                            size="small"
                            variant="ghost"
                            onClick={() =>
                              setConfirmation({
                                title: 'Encerrar assinatura?',
                                body: 'As faturas desta assinatura que ainda não venceram são canceladas; as vencidas continuam em aberto.',
                                label: 'Encerrar assinatura',
                                success: 'Assinatura encerrada',
                                tone: 'danger',
                                run: () => customersApi.cancelSubscription(sub.id),
                              })
                            }
                          >
                            Encerrar
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>

        {data.payments.length > 0 && (
          <Card title="Pagamentos Pix" subtitle="Pagamentos recebidos pela AbacatePay. O estorno devolve o valor integral ao cliente." flush>
            <div className={styles.tableWrap}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>Pago em</th>
                    <th>Fatura</th>
                    <th>Valor</th>
                    <th>Situação</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {data.payments.map((payment) => (
                    <tr key={payment.id}>
                      <td>{formatDateTime(payment.paidAt)}</td>
                      <td>
                        {payment.invoiceDescription}
                        {!payment.settledInvoice && payment.status !== 'REFUNDED' && (
                          <div className={billing.muted}>pagamento a mais — a fatura foi quitada por outro meio</div>
                        )}
                        {payment.refundReason && <div className={billing.muted}>estorno: {payment.refundReason}</div>}
                      </td>
                      <td className={billing.amount}>{formatMoney(payment.amountCents)}</td>
                      <td>
                        <PaymentStatus payment={payment} />
                      </td>
                      <td>
                        {payment.status === 'PAID' && !payment.refundRequestedAt && (
                          <div className={styles.actions}>
                            <Button
                              size="small"
                              variant="ghost"
                              onClick={() => openModal(() => setRefund({ payment, reason: '' }))}
                            >
                              Estornar
                            </Button>
                          </div>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>
        )}

        <Card
          title="Faturas"
          subtitle="As mensalidades saem sozinhas antes do vencimento. Lance aqui cobranças avulsas, como a troca de um equipamento."
          actions={
            <Button
              size="small"
              variant="primary"
              onClick={() =>
                openModal(() => setNewInvoice({ description: '', amount: '', dueDate: todayISO(), paymentUrl: '', pixCode: '' }))
              }
            >
              Nova fatura avulsa
            </Button>
          }
          flush
        >
          {data.invoices.length === 0 ? (
            <EmptyState icon="🧾" title="Nenhuma fatura" />
          ) : (
            <div className={styles.tableWrap}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>Descrição</th>
                    <th>Vencimento</th>
                    <th>Valor</th>
                    <th>Situação</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {data.invoices.map((invoice) => (
                    <tr key={invoice.id}>
                      <td>
                        {invoice.description}
                        {invoice.status === 'OPEN' && !data.onlinePayment && !invoice.paymentUrl && !invoice.pixCode && (
                          <div className={billing.muted}>sem link nem Pix informado</div>
                        )}
                        {invoice.paidAt && <div className={billing.muted}>paga em {formatDateTime(invoice.paidAt)}</div>}
                      </td>
                      <td>{formatDateOnly(invoice.dueDate)}</td>
                      <td className={billing.amount}>{formatMoney(invoice.amountCents)}</td>
                      <td>
                        <InvoiceStatus invoice={invoice} />
                      </td>
                      <td>
                        {invoice.status === 'OPEN' && (
                          <div className={styles.actions}>
                            <Button
                              size="small"
                              variant="primary"
                              onClick={() =>
                                setConfirmation({
                                  title: 'Dar baixa na fatura?',
                                  body: (
                                    <>
                                      Confirme que <strong>{formatMoney(invoice.amountCents)}</strong> de “{invoice.description}” foi recebido.
                                      {data.suspended && ' Se era o motivo da suspensão, o acesso do cliente volta na hora.'}
                                    </>
                                  ),
                                  label: 'Dar baixa',
                                  success: 'Baixa registrada',
                                  tone: 'primary',
                                  run: () => customersApi.payInvoice(invoice.id),
                                })
                              }
                            >
                              Dar baixa
                            </Button>
                            {data.onlinePayment && (
                              <Button size="small" variant="secondary" onClick={() => setPix(invoice)}>
                                Gerar Pix
                              </Button>
                            )}
                            <Button
                              size="small"
                              variant="ghost"
                              onClick={() =>
                                openModal(() => setPayment({ invoice, paymentUrl: invoice.paymentUrl, pixCode: invoice.pixCode }))
                              }
                            >
                              {data.onlinePayment ? 'Outro meio' : 'Pagamento'}
                            </Button>
                            <Button
                              size="small"
                              variant="ghost"
                              onClick={() =>
                                setConfirmation({
                                  title: 'Cancelar fatura?',
                                  body: `“${invoice.description}” deixa de ser cobrada. Não dá para reabrir depois.`,
                                  label: 'Cancelar fatura',
                                  success: 'Fatura cancelada',
                                  tone: 'danger',
                                  run: () => customersApi.cancelInvoice(invoice.id),
                                })
                              }
                            >
                              Cancelar
                            </Button>
                          </div>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>

        <h2 className={styles.sectionTitle}>Alertas por e-mail</h2>
        <CustomerAlertsSection customerId={id} />
      </div>

      {/* O único caminho de um veículo novo: veículo → rastreador → assinatura */}
      <NewVehicleWizard
        open={ordering}
        admin={{
          customerId: id,
          devices: (devices.data ?? []).filter((d) => !linkedTo.has(d.id)),
          deliveryAddress: data.deliveryAddress,
          currentPlan: data.subscriptions.find((s) => s.status === 'ACTIVE') ?? null,
        }}
        onClose={() => setOrdering(false)}
        onDone={(result) => {
          setOrdering(false);
          refresh();
          notify({
            tone: 'success',
            title: 'Veículo incluído',
            description: result.setupInvoice
              ? `${result.vehicle.name}: assinatura iniciada e fatura do equipamento de ${formatMoney(result.setupInvoice.amountCents)} lançada.`
              : `${result.vehicle.name}: assinatura iniciada, sem cobrança de equipamento.`,
          });
        }}
      />

      <FulfillmentAdminModal fulfillmentId={following} onClose={() => { setFollowing(null); refresh(); }} />

      {/* Assinatura antiga sem veículo */}
      <Modal
        open={attach !== null}
        title="Informar veículo"
        onClose={() => setAttach(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setAttach(null)}>Cancelar</Button>
            <Button
              variant="primary"
              loading={action.isPending}
              disabled={!attach?.input.name.trim()}
              onClick={() =>
                attach &&
                run('Veículo informado', () => customersApi.attachVehicle(id, attach.subscription.id, attach.input))
              }
            >
              Salvar
            </Button>
          </>
        }
      >
        {attach && (
          <div className={styles.form}>
            {formError && <div className={styles.note}>{formError}</div>}
            <p className={billing.muted}>
              {attach.subscription.planName} · {formatMoney(attach.subscription.priceCents)}/mês. Sem nova
              cobrança: a assinatura já existe.
            </p>
            <VehicleFields
              value={attach.input}
              onChange={(input) => setAttach({ ...attach, input })}
              devices={(devices.data ?? []).filter((d) => !linkedTo.has(d.id))}
              autoFocus
            />
          </div>
        )}
      </Modal>

      {/* Veículo sem assinatura ativa */}
      <Modal
        open={reactivate !== null}
        wide
        title={reactivate ? `Reativar assinatura · ${reactivate.vehicle.name}` : ''}
        onClose={() => setReactivate(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setReactivate(null)}>Cancelar</Button>
            <Button
              variant="primary"
              loading={action.isPending}
              onClick={() => {
                if (!reactivate) return;
                const plan = subscriptionFromDraft(reactivate.draft);
                if (typeof plan === 'string') return setFormError(plan);
                run('Assinatura reativada', () => customersApi.reactivate(id, reactivate.vehicle.id, plan));
              }}
            >
              Reativar
            </Button>
          </>
        }
      >
        {reactivate && (
          <div className={styles.form}>
            {formError && <div className={styles.note}>{formError}</div>}
            <SubscriptionFields
              draft={reactivate.draft}
              onChange={(draft) => setReactivate({ ...reactivate, draft })}
            />
            <p className={billing.muted}>
              Sem fatura de equipamento: o rastreador já está com o cliente. A primeira mensalidade vence
              no próximo dia escolhido.
            </p>
          </div>
        )}
      </Modal>

      <AddressModal
        open={editingAddress}
        initial={data.deliveryAddress}
        save={(address) => customersApi.saveAddress(id, address)}
        onClose={() => setEditingAddress(false)}
        onSaved={() => {
          setEditingAddress(false);
          refresh();
          notify({ tone: 'success', title: 'Endereço de entrega salvo' });
        }}
      />

      {/* Estorno de um pagamento Pix */}
      <Modal
        open={refund !== null}
        title="Estornar pagamento?"
        onClose={() => setRefund(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRefund(null)}>Voltar</Button>
            <Button
              variant="danger"
              loading={action.isPending}
              disabled={!refund?.reason.trim()}
              onClick={() =>
                refund &&
                run('Estorno pedido à AbacatePay', () => customersApi.refundCharge(refund.payment.id, refund.reason))
              }
            >
              Estornar {refund ? formatMoney(refund.payment.amountCents) : ''}
            </Button>
          </>
        }
      >
        {refund && (
          <div className={styles.form}>
            {formError && <div className={styles.note}>{formError}</div>}
            <p>
              <strong>{formatMoney(refund.payment.amountCents)}</strong> de “{refund.payment.invoiceDescription}” volta
              para o cliente pelo Pix. A AbacatePay só faz estorno do valor integral, e o valor sai do saldo da conta.
            </p>
            <div className={styles.note}>
              {refund.payment.settledInvoice
                ? 'Foi este Pix que quitou a fatura: depois do estorno ela volta a ficar em aberto. Se a cobrança não for mais devida, cancele a fatura em seguida.'
                : 'Este Pix é um pagamento a mais (a fatura já estava quitada por outro meio): a fatura continua como está.'}
            </div>
            <TextField
              label="Motivo do estorno"
              placeholder="Ex.: pagamento em duplicidade"
              autoFocus
              value={refund.reason}
              onChange={(e) => setRefund({ ...refund, reason: e.target.value })}
            />
          </div>
        )}
      </Modal>

      {/* Editar dados */}
      <Modal
        open={profile !== null}
        title="Editar dados do cliente"
        onClose={() => setProfile(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setProfile(null)}>Cancelar</Button>
            <Button
              variant="primary"
              loading={action.isPending}
              onClick={() =>
                profile && run('Dados atualizados', () => customersApi.update(id, { ...profile, active: data.active }))
              }
            >
              Salvar
            </Button>
          </>
        }
      >
        {profile && (
          <div className={styles.form}>
            {formError && <div className={styles.note}>{formError}</div>}
            <TextField label="Nome" value={profile.name} onChange={(e) => setProfile({ ...profile, name: e.target.value })} />
            <div className={styles.formRow}>
              <TextField label="Telefone" value={profile.phone} onChange={(e) => setProfile({ ...profile, phone: e.target.value })} />
              <TextField label="CPF ou CNPJ" value={profile.document} onChange={(e) => setProfile({ ...profile, document: e.target.value })} />
            </div>
            <p className={billing.muted}>O e-mail é o login do cliente e não muda por aqui.</p>
          </div>
        )}
      </Modal>

      {/* Editar assinatura */}
      <Modal
        open={editSubscription !== null}
        title="Editar assinatura"
        onClose={() => setEditSubscription(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setEditSubscription(null)}>Cancelar</Button>
            <Button
              variant="primary"
              loading={action.isPending}
              onClick={() => {
                if (!editSubscription) return;
                const priceCents = parseMoney(editSubscription.price);
                if (priceCents === null) return setFormError('Valor mensal inválido. Use, por exemplo, 69,90.');
                run('Assinatura atualizada', () =>
                  customersApi.updateSubscription(editSubscription.sub.id, { planName: editSubscription.planName, priceCents }),
                );
              }}
            >
              Salvar
            </Button>
          </>
        }
      >
        {editSubscription && (
          <div className={styles.form}>
            {formError && <div className={styles.note}>{formError}</div>}
            <TextField
              label="Nome do plano"
              value={editSubscription.planName}
              onChange={(e) => setEditSubscription({ ...editSubscription, planName: e.target.value })}
            />
            <TextField
              label="Valor mensal (R$)"
              inputMode="decimal"
              hint="Vale para as faturas geradas daqui em diante."
              value={editSubscription.price}
              onChange={(e) => setEditSubscription({ ...editSubscription, price: e.target.value })}
            />
          </div>
        )}
      </Modal>

      {/* Link de pagamento / Pix */}
      <Modal
        open={payment !== null}
        title="Dados de pagamento"
        onClose={() => setPayment(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setPayment(null)}>Cancelar</Button>
            <Button
              variant="primary"
              loading={action.isPending}
              onClick={() =>
                payment &&
                run('Dados de pagamento salvos', () =>
                  customersApi.updateInvoice(payment.invoice.id, { paymentUrl: payment.paymentUrl, pixCode: payment.pixCode }),
                )
              }
            >
              Salvar
            </Button>
          </>
        }
      >
        {payment && (
          <div className={styles.form}>
            {formError && <div className={styles.note}>{formError}</div>}
            <p className={billing.muted}>
              {payment.invoice.description} · {formatMoney(payment.invoice.amountCents)} · vence{' '}
              {formatDateOnly(payment.invoice.dueDate)}
            </p>
            <TextField
              label="Link de pagamento (boleto ou checkout)"
              placeholder="https://"
              value={payment.paymentUrl}
              onChange={(e) => setPayment({ ...payment, paymentUrl: e.target.value })}
            />
            <TextField
              label="Pix copia-e-cola"
              value={payment.pixCode}
              onChange={(e) => setPayment({ ...payment, pixCode: e.target.value })}
            />
            <p className={billing.muted}>O cliente vê os botões “Pagar” e “Copiar Pix” na tela de faturas.</p>
          </div>
        )}
      </Modal>

      {/* Fatura avulsa */}
      <Modal
        open={newInvoice !== null}
        wide
        title="Nova fatura avulsa"
        onClose={() => setNewInvoice(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setNewInvoice(null)}>Cancelar</Button>
            <Button
              variant="primary"
              loading={action.isPending}
              onClick={() => {
                if (!newInvoice) return;
                const amountCents = parseMoney(newInvoice.amount);
                if (amountCents === null || amountCents <= 0) return setFormError('Valor inválido. Use, por exemplo, 150,00.');
                run('Fatura lançada', () =>
                  customersApi.createInvoice(id, {
                    description: newInvoice.description,
                    amountCents,
                    dueDate: newInvoice.dueDate,
                    paymentUrl: newInvoice.paymentUrl,
                    pixCode: newInvoice.pixCode,
                  }),
                );
              }}
            >
              Lançar fatura
            </Button>
          </>
        }
      >
        {newInvoice && (
          <div className={styles.form}>
            {formError && <div className={styles.note}>{formError}</div>}
            <TextField
              label="Descrição"
              value={newInvoice.description}
              onChange={(e) => setNewInvoice({ ...newInvoice, description: e.target.value })}
            />
            <div className={styles.formRow}>
              <TextField
                label="Valor (R$)"
                inputMode="decimal"
                placeholder="150,00"
                value={newInvoice.amount}
                onChange={(e) => setNewInvoice({ ...newInvoice, amount: e.target.value })}
              />
              <TextField
                label="Vencimento"
                type="date"
                value={newInvoice.dueDate}
                onChange={(e) => setNewInvoice({ ...newInvoice, dueDate: e.target.value })}
              />
            </div>
            <TextField
              label="Link de pagamento (opcional)"
              placeholder="https://"
              value={newInvoice.paymentUrl}
              onChange={(e) => setNewInvoice({ ...newInvoice, paymentUrl: e.target.value })}
            />
            <TextField
              label="Pix copia-e-cola (opcional)"
              value={newInvoice.pixCode}
              onChange={(e) => setNewInvoice({ ...newInvoice, pixCode: e.target.value })}
            />
          </div>
        )}
      </Modal>

      {/* Pix online (AbacatePay) gerado pela central */}
      <PixPaymentModal
        invoice={pix}
        api={{
          invoicePix: customersApi.invoicePix,
          charge: customersApi.charge,
          simulateCharge: customersApi.simulateCharge,
        }}
        audience="staff"
        onClose={() => setPix(null)}
        onPaid={refresh}
      />

      {/* Confirmação genérica */}
      <Modal
        open={confirmation !== null}
        title={confirmation?.title ?? ''}
        onClose={() => setConfirmation(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirmation(null)}>Voltar</Button>
            <Button
              variant={confirmation?.tone === 'danger' ? 'danger' : 'primary'}
              loading={action.isPending}
              onClick={() => confirmation && run(confirmation.success, confirmation.run)}
            >
              {confirmation?.label}
            </Button>
          </>
        }
      >
        <p>{confirmation?.body}</p>
      </Modal>
    </div>
  );
}

/** Situação de um pagamento Pix na visão da central. */
function PaymentStatus({ payment }: { payment: PixPayment }) {
  if (payment.status === 'REFUNDED') return <Badge tone="neutral">Estornado</Badge>;
  if (payment.status === 'UNDER_DISPUTE') {
    return (
      <Badge tone="danger" dot>
        Em disputa
      </Badge>
    );
  }
  if (payment.refundRequestedAt) {
    return (
      <Badge tone="warning" dot>
        Estorno em andamento
      </Badge>
    );
  }
  if (!payment.settledInvoice) {
    return (
      <Badge tone="warning" dot>
        Revisar
      </Badge>
    );
  }
  return (
    <Badge tone="success" dot>
      Recebido
    </Badge>
  );
}

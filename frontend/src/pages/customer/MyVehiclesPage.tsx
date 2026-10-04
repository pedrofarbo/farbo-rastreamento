import { useCallback, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';

import { meApi, sharesApi, vehiclesApi, vehicleInputFrom } from '@/api/resources';
import type { VehicleInput } from '@/api/resources';
import { AddressModal } from '@/components/address/AddressModal';
import { DeliveryBox } from '@/components/address/DeliveryBox';
import billing from '@/components/billing/Billing.module.css';
import { MonthlyPrice } from '@/components/billing/MonthlyPrice';
import { NewVehicleWizard } from '@/components/billing/NewVehicleWizard';
import { PixPaymentModal } from '@/components/billing/PixPaymentModal';
import { SuspendedNotice, isSuspendedError } from '@/components/billing/SuspendedNotice';
import { FulfillmentTimeline } from '@/components/fulfillment/FulfillmentTimeline';
import fstyles from '@/components/fulfillment/Fulfillment.module.css';
import { InstallersModal } from '@/components/landing/InstallersModal';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDeviceStatus, formatRelative } from '@/services/format';
import { stepOf, trackerTone } from '@/services/fulfillment';
import { subscriptionsByVehicle, subscriptionsWithoutVehicle } from '@/services/subscriptions';
import { SharesModal, sharesKey } from '@/components/vehicle/SharesModal';
import { EMPTY_VEHICLE, VehicleFields } from '@/components/vehicle/VehicleFields';
import type { CustomerAccount, CustomerFulfillment, Invoice, Subscription, VehicleView } from '@/types';

import styles from '../Page.module.css';

/** Edição de um veículo, ou o veículo de uma assinatura antiga que ficou sem. */
type VehicleForm =
  | { mode: 'edit'; vehicle: VehicleView; input: VehicleInput }
  | { mode: 'attach'; subscription: Subscription; input: VehicleInput };

/**
 * Veículos do cliente. Cada um tem o seu rastreador e a sua assinatura, e
 * todos entram pelo mesmo caminho: Novo veículo (veículo → rastreador →
 * assinatura).
 */
export function MyVehiclesPage() {
  const navigate = useNavigate();
  const { notify } = useToast();
  const queryClient = useQueryClient();

  const [form, setForm] = useState<VehicleForm | null>(null);
  const [ordering, setOrdering] = useState(false);
  const [paying, setPaying] = useState<Invoice | null>(null);
  const [showInstallers, setShowInstallers] = useState(false);
  const [editingAddress, setEditingAddress] = useState(false);
  const [following, setFollowing] = useState<CustomerFulfillment | null>(null);
  // Acessos de terceiros: o veículo cuja lista está aberta, e o compartilhado
  // que a pessoa está deixando de acompanhar.
  const [sharing, setSharing] = useState<VehicleView | null>(null);
  const [leaving, setLeaving] = useState<string | null>(null);

  const vehicles = useQuery({ queryKey: ['vehicles'], queryFn: vehiclesApi.list });
  const account = useQuery({ queryKey: ['me', 'account'], queryFn: meApi.account });
  const subscriptions = useQuery({ queryKey: ['me', 'subscriptions'], queryFn: meApi.subscriptions });
  const fulfillments = useQuery({ queryKey: ['me', 'fulfillments'], queryFn: meApi.fulfillments });
  const shares = useQuery({ queryKey: sharesKey, queryFn: sharesApi.list });

  const leave = useMutation({
    mutationFn: (shareId: string) => sharesApi.remove(shareId),
    onSuccess: () => {
      setLeaving(null);
      queryClient.invalidateQueries({ queryKey: ['vehicles'] });
      notify({ tone: 'success', title: 'Você não acompanha mais esse veículo' });
    },
    onError: (error: Error) =>
      notify({ tone: 'error', title: 'Não foi possível sair', description: error.message }),
  });

  const refresh = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['vehicles'] });
    queryClient.invalidateQueries({ queryKey: ['me'] });
  }, [queryClient]);

  const save = useMutation({
    mutationFn: (f: VehicleForm) =>
      f.mode === 'edit'
        ? vehiclesApi.update(f.vehicle.id, f.input)
        : meApi.attachVehicle(f.subscription.id, f.input),
    onSuccess: (_, f) => {
      refresh();
      notify({ tone: 'success', title: f.mode === 'edit' ? 'Veículo atualizado' : 'Veículo informado' });
      setForm(null);
    },
    onError: (error: Error) =>
      notify({ tone: 'error', title: 'Não foi possível salvar', description: error.message }),
  });

  if (isSuspendedError(vehicles.error)) {
    return <SuspendedNotice message={(vehicles.error as Error).message} />;
  }

  // Os do cliente e os que outros clientes compartilharam com ele.
  const all = vehicles.data ?? [];
  const list = all.filter((v) => !v.shared);
  const sharedWithMe = all.filter((v) => v.shared);
  const sharesOf = new Map<string, number>();
  for (const share of shares.data ?? []) sharesOf.set(share.vehicleId, (sharesOf.get(share.vehicleId) ?? 0) + 1);
  const subs = subscriptions.data ?? [];
  const byVehicle = subscriptionsByVehicle(subs);
  const withoutVehicle = subscriptionsWithoutVehicle(subs);
  // Só acompanha veículos de outras pessoas (nenhum próprio nem assinatura).
  const onlyShared = list.length === 0 && withoutVehicle.length === 0 && sharedWithMe.length > 0;
  const address = account.data?.deliveryAddress ?? null;
  // O pedido mais recente de cada veículo (a lista vem do mais novo).
  const orderOf = new Map<string, CustomerFulfillment>();
  for (const f of fulfillments.data ?? []) if (!orderOf.has(f.vehicleId)) orderOf.set(f.vehicleId, f);

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Meus veículos</h1>
            <p className={styles.description}>
              Cada veículo tem o seu rastreador e a sua assinatura. Para incluir outro, use Novo
              veículo: você informa o veículo, recebe o rastreador e a assinatura começa.
            </p>
          </div>
          <div className={styles.actions}>
            <Button variant="primary" onClick={() => setOrdering(true)}>
              Novo veículo
            </Button>
          </div>
        </header>

        {onlyShared && (
          <p className={billing.muted}>
            Você ainda não tem veículo próprio. Para rastrear um seu, use Novo veículo.
          </p>
        )}

        {account.data &&
          !onlyShared &&
          (address ? (
            <DeliveryBox address={address} label="Endereço de entrega" onChange={() => setEditingAddress(true)} />
          ) : (
            <div className={`${billing.delivery} ${billing.deliveryEmpty}`}>
              <div className={billing.deliveryText}>
                <span className={billing.deliveryLabel}>Endereço de entrega</span>
                <strong>Nenhum endereço cadastrado</strong>
                <span className={billing.muted}>
                  É para onde enviamos o rastreador. Dá para cadastrar agora ou durante o Novo veículo.
                </span>
              </div>
              <Button size="small" variant="primary" onClick={() => setEditingAddress(true)}>
                Cadastrar
              </Button>
            </div>
          ))}

        {vehicles.isLoading || subscriptions.isLoading ? (
          <Spinner label="Carregando veículos" />
        ) : onlyShared ? null : list.length === 0 && withoutVehicle.length === 0 ? (
          <Card>
            <EmptyState
              icon="🚗"
              title="Nenhum veículo ainda"
              description="Em Novo veículo você informa o veículo, recebe o rastreador e a assinatura começa."
              action={
                <Button variant="primary" onClick={() => setOrdering(true)}>
                  Novo veículo
                </Button>
              }
            />
          </Card>
        ) : (
          <div className={billing.vehicleGrid}>
            {list.map((vehicle) => (
              <VehicleCard
                key={vehicle.id}
                vehicle={vehicle}
                subscription={byVehicle.get(vehicle.id) ?? null}
                order={orderOf.get(vehicle.id) ?? null}
                onFollow={(order) => setFollowing(order)}
                onOpen={() => navigate(`/veiculos/${vehicle.id}`)}
                onEdit={() => setForm({ mode: 'edit', vehicle, input: vehicleInputFrom(vehicle) })}
                onShowInstallers={() => setShowInstallers(true)}
                shareCount={sharesOf.get(vehicle.id) ?? 0}
                onShares={() => setSharing(vehicle)}
              />
            ))}
            {withoutVehicle.map((sub) => (
              <article key={sub.id} className={`${billing.vehicleCard} ${billing.slotCard}`}>
                <div>
                  <div className={billing.vehicleName}>Assinatura sem veículo</div>
                  <div className={billing.subscriptionLine}>
                    {sub.planName} · <MonthlyPrice sub={sub} /> · vence dia {sub.dueDay}
                  </div>
                </div>
                <div className={billing.vehicleMeta}>
                  Esta assinatura é de antes do cadastro por veículo. Informe qual veículo ela cobre.
                </div>
                <div className={billing.vehicleActions}>
                  <Button
                    size="small"
                    variant="primary"
                    onClick={() => setForm({ mode: 'attach', subscription: sub, input: EMPTY_VEHICLE })}
                  >
                    Informar veículo
                  </Button>
                </div>
              </article>
            ))}
          </div>
        )}

        {sharedWithMe.length > 0 && (
          <section className={billing.sharedSection}>
            <h2 className={billing.sharedTitle}>Compartilhados com você</h2>
            <p className={billing.muted}>
              Veículos de outras pessoas que deram acesso a você: a posição ao vivo e, se liberado, o
              bloqueio de emergência.
            </p>
            <div className={billing.vehicleGrid}>
              {sharedWithMe.map((vehicle) => (
                <article key={vehicle.id} className={billing.vehicleCard}>
                  <div className={billing.vehicleHead}>
                    <div>
                      <div className={billing.vehicleName}>{vehicle.name}</div>
                      <div className={billing.vehicleMeta}>De {vehicle.shared?.ownerName}</div>
                    </div>
                    {vehicle.plate && <span className={billing.plate}>{vehicle.plate}</span>}
                  </div>
                  <div>
                    <Badge tone={vehicle.shared?.canBlock ? 'warning' : 'neutral'}>
                      {vehicle.shared?.canBlock ? 'Acompanha e pode bloquear' : 'Só acompanha'}
                    </Badge>
                  </div>
                  <div className={billing.vehicleActions}>
                    {vehicle.device && (
                      <Button size="small" variant="primary" onClick={() => navigate(`/veiculos/${vehicle.id}`)}>
                        Ver no mapa
                      </Button>
                    )}
                    {leaving === vehicle.id ? (
                      <Button
                        size="small"
                        variant="danger"
                        loading={leave.isPending}
                        onClick={() => vehicle.shared && leave.mutate(vehicle.shared.shareId)}
                      >
                        Confirmar
                      </Button>
                    ) : (
                      <Button size="small" variant="ghost" onClick={() => setLeaving(vehicle.id)}>
                        Deixar de acompanhar
                      </Button>
                    )}
                  </div>
                </article>
              ))}
            </div>
          </section>
        )}
      </div>

      <SharesModal vehicle={sharing} onClose={() => setSharing(null)} />

      <Modal
        open={form !== null}
        title={form?.mode === 'attach' ? 'Informar veículo' : 'Editar veículo'}
        onClose={() => setForm(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setForm(null)}>
              Cancelar
            </Button>
            <Button
              variant="primary"
              loading={save.isPending}
              disabled={!form?.input.name.trim()}
              onClick={() => form && save.mutate(form)}
            >
              Salvar
            </Button>
          </>
        }
      >
        {form && (
          <div className={styles.form}>
            <VehicleFields value={form.input} onChange={(input) => setForm({ ...form, input })} autoFocus />
            <TextField
              label="Alerta de velocidade (km/h)"
              type="number"
              hint="Opcional. Avisamos quando o veículo passar deste limite."
              value={form.input.speedLimitKmh ?? ''}
              onChange={(event) =>
                setForm({
                  ...form,
                  input: {
                    ...form.input,
                    speedLimitKmh: event.target.value ? Number(event.target.value) : null,
                  },
                })
              }
            />
          </div>
        )}
      </Modal>

      <NewVehicleWizard
        open={ordering}
        onClose={() => setOrdering(false)}
        onDone={(result) => {
          setOrdering(false);
          refresh();
          notify({
            tone: 'success',
            title: 'Pedido feito',
            description: `${result.vehicle.name} foi cadastrado; o rastreador segue para instalação.`,
          });
          // Com o Pix online, já abre o pagamento do equipamento.
          if (result.setupInvoice && account.data?.onlinePayment) setPaying(result.setupInvoice);
        }}
      />

      <InstallersModal isOpen={showInstallers} onClose={() => setShowInstallers(false)} />

      {/* Acompanhamento do pedido: chip M2M e rastreador até a casa do cliente */}
      <Modal
        open={following !== null}
        wide
        title={following ? `Acompanhar pedido · ${following.vehicleName}` : ''}
        onClose={() => setFollowing(null)}
        footer={
          <Button variant="ghost" onClick={() => setFollowing(null)}>
            Fechar
          </Button>
        }
      >
        {following && (
          <FulfillmentTimeline
            chipStatus={following.chipStatus}
            trackerStatus={following.trackerStatus}
            events={following.events}
            carrier={following.carrier}
            trackingCode={following.trackingCode}
          >
            {following.trackerStatus === 'DELIVERED' && (
              <div className={fstyles.arrived}>
                <span>
                  <strong>Chegou!</strong> Agende a instalação com um dos nossos prestadores recomendados.
                </span>
                <Button size="small" variant="primary" onClick={() => setShowInstallers(true)}>
                  Ver instaladores
                </Button>
              </div>
            )}
          </FulfillmentTimeline>
        )}
      </Modal>

      <AddressModal
        open={editingAddress}
        initial={address}
        intro="É para onde enviamos os rastreadores dos seus próximos veículos."
        save={meApi.saveAddress}
        onClose={() => setEditingAddress(false)}
        onSaved={(saved) => {
          queryClient.setQueryData<CustomerAccount>(['me', 'account'], (old) =>
            old ? { ...old, deliveryAddress: saved } : old,
          );
          setEditingAddress(false);
          notify({ tone: 'success', title: 'Endereço de entrega salvo' });
        }}
      />

      <PixPaymentModal
        invoice={paying}
        api={meApi}
        audience="customer"
        onClose={() => setPaying(null)}
        onPaid={refresh}
      />
    </div>
  );
}

function VehicleCard({
  vehicle,
  subscription,
  order,
  onFollow,
  onOpen,
  onEdit,
  onShowInstallers,
  shareCount,
  onShares,
}: {
  vehicle: VehicleView;
  subscription: Subscription | null;
  order: CustomerFulfillment | null;
  onFollow: (order: CustomerFulfillment) => void;
  onOpen: () => void;
  onEdit: () => void;
  onShowInstallers: () => void;
  /** Quantas pessoas acompanham o veículo (acessos de terceiros). */
  shareCount: number;
  onShares: () => void;
}) {
  const status = vehicle.device?.status;
  // Instalado = o rastreador já deu sinal. O aparelho é vinculado antes, na
  // configuração, então ter aparelho não basta.
  const installed = Boolean(vehicle.device?.lastSeenAt);
  const arrived = order?.trackerStatus === 'DELIVERED';
  const details = [vehicle.brand, vehicle.model, vehicle.year, vehicle.color].filter(Boolean).join(' · ');

  return (
    <article className={billing.vehicleCard}>
      <div className={billing.vehicleHead}>
        <div>
          <div className={billing.vehicleName}>{vehicle.name}</div>
          {details && <div className={billing.vehicleMeta}>{details}</div>}
        </div>
        {vehicle.plate && <span className={billing.plate}>{vehicle.plate}</span>}
      </div>

      <div>
        <div className={billing.deliveryLabel}>Rastreador</div>
        {installed ? (
          <>
            <Badge
              tone={status === 'ONLINE' ? 'success' : status === 'STALE' ? 'warning' : 'neutral'}
              dot
              pulse={status === 'ONLINE'}
            >
              {formatDeviceStatus(status)}
            </Badge>
            <div className={billing.vehicleMeta} style={{ marginTop: 'var(--space-1)' }}>
              Último contato {formatRelative(vehicle.device?.lastSeenAt)}
            </div>
          </>
        ) : order && !arrived ? (
          <>
            <Badge tone={trackerTone(order.trackerStatus)}>{stepOf('TRACKER', order.trackerStatus)?.label}</Badge>
            <div className={billing.vehicleMeta} style={{ marginTop: 'var(--space-1)' }}>
              {order.trackingCode
                ? `Código de rastreio ${order.trackingCode}`
                : `Chip M2M: ${stepOf('CHIP', order.chipStatus)?.label.replace(/^Chip /, '').toLowerCase()}`}
            </div>
          </>
        ) : (
          <>
            <Badge tone={arrived ? 'success' : 'warning'}>{arrived ? 'Chegou' : 'Aguardando instalação'}</Badge>
            <div className={billing.vehicleMeta} style={{ marginTop: 'var(--space-1)' }}>
              Agende a instalação com um dos prestadores recomendados.
            </div>
          </>
        )}
      </div>

      <div>
        <div className={billing.deliveryLabel}>Assinatura</div>
        {subscription?.status === 'ACTIVE' ? (
          <div className={billing.subscriptionLine}>
            {subscription.planName} · <MonthlyPrice sub={subscription} /> · vence dia{' '}
            {subscription.dueDay}
          </div>
        ) : subscription ? (
          <Badge tone="neutral">Assinatura encerrada</Badge>
        ) : (
          <span className={billing.muted}>Sem assinatura — fale com a central.</span>
        )}
      </div>

      <div className={billing.vehicleActions}>
        {installed && (
          <Button size="small" variant="primary" onClick={onOpen}>
            Ver no mapa
          </Button>
        )}
        {!installed && order && (
          <Button size="small" variant={arrived ? 'secondary' : 'primary'} onClick={() => onFollow(order)}>
            Acompanhar pedido
          </Button>
        )}
        {!installed && (arrived || !order) && (
          <Button size="small" variant="primary" onClick={onShowInstallers}>
            Ver prestadores
          </Button>
        )}
        <Button size="small" variant="secondary" onClick={onEdit}>
          Editar
        </Button>
        <Button
          size="small"
          variant="secondary"
          onClick={onShares}
          title="Quem mais acompanha este veículo e pode bloqueá-lo numa emergência"
        >
          {shareCount > 0 ? `Acessos (${shareCount})` : 'Acessos'}
        </Button>
      </div>
    </article>
  );
}

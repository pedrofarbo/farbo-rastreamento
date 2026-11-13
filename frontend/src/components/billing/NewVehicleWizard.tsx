import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ApiError } from '@/api/client';
import { catalogApi, contractApi, customersApi, meApi } from '@/api/resources';
import type { VehicleInput } from '@/api/resources';
import { AddressFields, EMPTY_ADDRESS, isAddressComplete } from '@/components/address/AddressFields';
import { DeliveryBox } from '@/components/address/DeliveryBox';
import { ARRANGE_DELIVERY, ShippingOptions, deliveryLabel, quoteName } from '@/components/billing/ShippingOptions';
import { promoMonthlyFor } from '@/components/billing/MonthlyPrice';
import { TrackerFields, trackerDraftFrom, trackerFromDraft, trackerMode } from '@/components/billing/TrackerFields';
import type { TrackerDraft } from '@/components/billing/TrackerFields';
import {
  DEFAULT_SUBSCRIPTION,
  PLAN_PRESETS,
  SubscriptionFields,
  subscriptionFromDraft,
} from '@/components/billing/SubscriptionFields';
import type { SubscriptionDraft } from '@/components/billing/SubscriptionFields';
import { ContractAcceptForm, contractKey } from '@/components/contract/ContractAcceptForm';
import { InstallersModal } from '@/components/landing/InstallersModal';
import { Button } from '@/components/ui/Button';
import { DeviceSelectField, deviceOptionLabel } from '@/components/devices/DeviceSelectField';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { EMPTY_VEHICLE, VehicleFields } from '@/components/vehicle/VehicleFields';
import { periodPrice } from '@/services/checkout';
import { kindPrice } from '@/services/kindPrices';
import { centsToInput, formatDateOnly, formatMoney, parseMoney, todayISO } from '@/services/format';
import { errorCode } from '@/services/stepUp';
import type {
  AccountPlan,
  Catalog,
  ChargeMethod,
  CustomerAccount,
  DeliveryAddress,
  Device,
  EquipmentTerms,
  LoanOffer,
  PeriodOffer,
  Subscription,
  Tracker,
  TrackerOrderResult,
  FleetSettings,
} from '@/types';

import pageStyles from '@/pages/Page.module.css';
import styles from './Billing.module.css';

const STEPS = ['Veículo', 'Rastreador', 'Assinatura'] as const;

/** Para a central: o cliente da ficha, os aparelhos livres e o endereço dele. */
export interface WizardAdmin {
  customerId: string;
  devices: Device[];
  deliveryAddress: DeliveryAddress | null;
  /** Assinatura ativa do cliente: o plano dela é a sugestão para o novo veículo. */
  currentPlan: Subscription | null;
  /** O plano definido para o cliente: quando há, é ele o do novo veículo. */
  accountPlan?: AccountPlan | null;
  /** O vencimento da conta (o das assinaturas ativas): o veículo novo entra nele. */
  accountDueDay?: number | null;
  /** Pessoa física: o pedido vence no ato (a empresa, na próxima fatura do mês). */
  payNow?: boolean;
}

interface AdminDraft {
  deviceId: string;
  /** O rastreador incluso: a parte dele nas primeiras mensalidades (a Farbo não vende o rastreador). */
  tracker: TrackerDraft;
  plan: SubscriptionDraft;
  /** Aplicar a promoção de pré-lançamento (o cliente tem direito). */
  promo: boolean;
}

/**
 * A fatura em que um veículo contratado hoje entra: o próximo dia da conta
 * depois de hoje (o servidor ainda pula a fatura do mês já paga).
 */
export function nextChargeDate(dueDay: number, today: string): string {
  const [y, m, d] = today.split('-').map(Number);
  const pad = (n: number) => String(n).padStart(2, '0');
  const month = d < dueDay ? m : m + 1;
  const date = new Date(y, month - 1, dueDay);
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/** Os dias de hoje até o vencimento e o proporcional da mensalidade (mês de 30 dias). */
export function prorataUntil(monthlyCents: number, today: string, due: string): { days: number; cents: number } {
  const [y1, m1, d1] = today.split('-').map(Number);
  const [y2, m2, d2] = due.split('-').map(Number);
  const days = Math.max(0, Math.round((Date.UTC(y2, m2 - 1, d2) - Date.UTC(y1, m1 - 1, d1)) / 86_400_000));
  return { days, cents: monthlyCents > 0 ? Math.round((monthlyCents * days) / 30) : 0 };
}

export function addDays(iso: string, days: number): string {
  const [y, m, d] = iso.split('-').map(Number);
  const date = new Date(y, m - 1, d + days);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/**
 * O plano sugerido para um veículo novo do cliente: o definido para ele; sem
 * ele, o que ele já paga; no primeiro veículo, o padrão. Sempre no vencimento
 * da conta, se ela tiver um.
 */
export function suggestedPlan(
  c: Catalog,
  subscription: Subscription | null,
  account: AccountPlan | null = null,
  accountDueDay: number | null = null,
): SubscriptionDraft {
  const current = account ?? subscription;
  const preset = current
    ? PLAN_PRESETS.find((p) => p.planName === current.planName && p.priceCents === current.priceCents)
    : undefined;
  const plan: SubscriptionDraft = current
    ? {
        preset: preset?.id ?? 'custom',
        planName: current.planName,
        price: centsToInput(current.priceCents),
        dueDay: current.dueDay,
      }
    : { ...DEFAULT_SUBSCRIPTION, dueDay: c.defaultDueDay };
  // A conta vence num dia só: o veículo novo entra no dela.
  if (accountDueDay) plan.dueDay = accountDueDay;
  return plan;
}

function adminDraftFrom(
  c: Catalog,
  subscription: Subscription | null,
  account: AccountPlan | null = null,
  accountDueDay: number | null = null,
): AdminDraft {
  const plan = suggestedPlan(c, subscription, account, accountDueDay);
  return { deviceId: '', tracker: trackerDraftFrom(c.tracker), plan, promo: false };
}

/** A API responde 409 com este código quando falta o endereço de entrega. */
function isAddressRequired(error: unknown): boolean {
  return error instanceof ApiError && (error.body as { code?: string } | undefined)?.code === 'ADDRESS_REQUIRED';
}

/** E com este quando a promoção de pré-lançamento não vale mais (vagas acabaram, já usou). */
function isPromoUnavailable(error: unknown): boolean {
  return error instanceof ApiError && (error.body as { code?: string } | undefined)?.code === 'PROMO_UNAVAILABLE';
}

/**
 * O único caminho para incluir um veículo, para o cliente e para a central:
 * 1. Veículo, 2. Rastreador (entrega e equipamento) e 3. Assinatura. No fim,
 * um pedido só cria os três de uma vez.
 *
 * Sem `admin`, é o próprio cliente: preços e plano vêm do servidor e o
 * endereço de entrega é obrigatório. Com `admin`, a central escolhe valores,
 * plano e pode vincular um aparelho já instalado — menos para o cliente que
 * veio por um representante (sem plano da central): aí o pedido sai nas
 * condições dele, como o cliente veria, e a central só escolhe o que ele
 * escolheria.
 */
export function NewVehicleWizard({
  open,
  admin,
  fleet,
  onClose,
  onDone,
}: {
  open: boolean;
  admin?: WizardAdmin;
  /** A organização da frota do cliente: o grupo e os campos personalizados já no cadastro. */
  fleet?: FleetSettings;
  onClose: () => void;
  /** O pedido feito e, para a pessoa física, a forma que ela escolheu para pagar agora. */
  onDone: (result: TrackerOrderResult, payment?: ChargeMethod) => void;
}) {
  const queryClient = useQueryClient();
  const isAdmin = Boolean(admin);

  const [step, setStep] = useState(0);
  const [vehicle, setVehicle] = useState<VehicleInput>(EMPTY_VEHICLE);
  // Endereço em edição (cliente); nulo mostra o endereço salvo.
  const [addressDraft, setAddressDraft] = useState<DeliveryAddress | null>(null);
  const [adminDraft, setAdminDraft] = useState<AdminDraft | null>(null);
  const [error, setError] = useState('');
  const [showInstallers, setShowInstallers] = useState(false);
  const [contractOpen, setContractOpen] = useState(false);
  const contract = useQuery({ queryKey: contractKey, queryFn: contractApi.mine, enabled: contractOpen });
  // O frete escolhido (serviço do Melhor Envios); 0: sem frete;
  // ARRANGE_DELIVERY: combinar a entrega. Nulo: ainda não escolhido (o mais
  // barato vem marcado quando a cotação chega).
  const [shippingId, setShippingId] = useState<number | null>(null);
  // O rastreador incluso ou em comodato (quando a central oferece).
  const [mode, setMode] = useState<'sale' | 'loan'>('sale');
  // Como a pessoa física paga o pedido (a janela de pagamento abre nela).
  const [payment, setPayment] = useState<ChargeMethod>('PIX');
  // O rastreador incluso: instalado pelos instaladores da Farbo (pago) ou
  // por conta própria.
  const [farboInstall, setFarboInstall] = useState(false);
  // O semestral ou o anual do representante que indicou o cliente (1: o mensal).
  const [periodMonths, setPeriodMonths] = useState(1);
  // A instalação com o relé do bloqueio (quando o representante oferece a escolha).
  const [installBlocking, setInstallBlocking] = useState(false);

  const catalog = useQuery({ queryKey: ['catalog'], queryFn: catalogApi.get, enabled: open });
  // Para a central: as condições do cliente, como ele veria (as do representante que o indicou).
  const customerCatalog = useQuery({
    queryKey: ['customer', admin?.customerId, 'catalog'],
    queryFn: () => customersApi.catalog(admin!.customerId),
    enabled: open && isAdmin,
  });
  // O cliente de um representante: a central não escolhe valor nem plano.
  const representative = isAdmin ? (customerCatalog.data?.representative ?? '') : '';
  const referred = representative !== '';
  // Os preços e as escolhas vêm do servidor: o cliente, ou a central pelo cliente do representante.
  const priced = !isAdmin || referred;
  // Para a central: se o cliente pode contratar com a promoção de pré-lançamento.
  const promoStatus = useQuery({
    queryKey: ['customer', admin?.customerId, 'launch-promo'],
    queryFn: () => customersApi.launchPromo(admin!.customerId),
    enabled: open && isAdmin,
  });
  const account = useQuery({ queryKey: ['me', 'account'], queryFn: meApi.account, enabled: open && !isAdmin });
  const card = useQuery({ queryKey: ['me', 'card'], queryFn: meApi.card, enabled: open && !isAdmin });
  // A central libera a promoção para quem não está na lista; ela já vem marcada.
  const grantPromo = useMutation({
    mutationFn: () => customersApi.grantLaunchPromo(admin!.customerId),
    onSuccess: (next) => {
      queryClient.setQueryData(['customer', admin!.customerId, 'launch-promo'], next);
      if (next.eligible) setAdminDraft((d) => (d ? { ...d, promo: true } : d));
    },
    onError: (err: Error) => setError(err.message),
  });
  // O cliente indicado por um afiliado com tabela: a mensalidade é a do tipo
  // do veículo escolhido.
  const base = referred ? customerCatalog.data : catalog.data;
  const c = base?.planByKind ? { ...base, planPriceCents: kindPrice(base.planByKind, vehicle.kind) } : base;
  const address = isAdmin ? (admin?.deliveryAddress ?? null) : (account.data?.deliveryAddress ?? null);

  useEffect(() => {
    if (!open) return;
    setStep(0);
    setVehicle(EMPTY_VEHICLE);
    setAddressDraft(null);
    setAdminDraft(null);
    setError('');
    setShippingId(null);
    setMode('sale');
    setPayment('PIX');
    setFarboInstall(false);
    setPeriodMonths(1);
    setInstallBlocking(false);
  }, [open]);

  // Os valores da central partem do catálogo, uma vez por abertura: uma nova
  // busca do catálogo não pode apagar o que já foi digitado.
  const seeded = useRef(false);
  useEffect(() => {
    if (!open) seeded.current = false;
    else if (isAdmin && c && !seeded.current) {
      seeded.current = true;
      setAdminDraft(adminDraftFrom(c, admin?.currentPlan ?? null, admin?.accountPlan ?? null, admin?.accountDueDay ?? null));
    }
    // O plano atual só importa na abertura (o ref impede semear de novo).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, isAdmin, c]);

  // Com direito à promoção, ela já vem marcada (uma vez por abertura).
  const promoSeeded = useRef(false);
  useEffect(() => {
    if (!open) promoSeeded.current = false;
    else if (adminDraft && promoStatus.data && !promoSeeded.current) {
      promoSeeded.current = true;
      if (promoStatus.data.eligible) setAdminDraft({ ...adminDraft, promo: true });
    }
  }, [open, adminDraft, promoStatus.data]);

  const saveAddress = useMutation({
    mutationFn: meApi.saveAddress,
    onSuccess: (saved) => {
      queryClient.setQueryData<CustomerAccount>(['me', 'account'], (old) =>
        old ? { ...old, deliveryAddress: saved } : old,
      );
      setAddressDraft(null);
      setError('');
      setStep(2);
    },
    onError: (err: Error) => setError(err.message),
  });

  const order = useMutation({
    mutationFn: async () => {
      if (!admin) {
        return meApi.orderTracker({
          vehicle,
          launchPromo: Boolean(customerPromo),
          ...(arranged ? { arrangeDelivery: true } : freight ? { shippingServiceId: freight.serviceId } : {}),
          ...(loanOffer ? { equipmentMode: mode } : {}),
          ...(farbo ? { farboInstall: true } : {}),
          ...(blocking ? { installBlocking: true } : {}),
          ...(period ? { periodMonths: period.months } : {}),
        });
      }
      const d = adminDraft as AdminDraft;
      if (referred && monthly) {
        // O cliente do representante: o pedido sai nas condições dele (o servidor confere).
        return customersApi.orderTracker(admin.customerId, {
          vehicle: { ...vehicle, deviceId: d.deviceId || null },
          plan: { planName: monthly.name, priceCents: monthly.cents ?? 0, dueDay: monthly.dueDay },
          launchPromo: Boolean(customerPromo),
          shippingServiceId: freight?.serviceId ?? 0,
          ...(loanOffer ? { equipmentMode: mode } : {}),
          ...(farbo ? { farboInstall: true } : {}),
          ...(blocking ? { installBlocking: true } : {}),
          ...(period ? { periodMonths: period.months } : {}),
        });
      }
      const plan = subscriptionFromDraft(d.plan);
      if (typeof plan === 'string') throw new Error(plan);
      if (typeof adminTracker === 'string') throw new Error(adminTracker);
      return customersApi.orderTracker(admin.customerId, {
        vehicle: { ...vehicle, deviceId: d.deviceId || null },
        tracker: loan ? { cents: 0, months: 0, added: false } : (adminTracker ?? undefined),
        plan,
        launchPromo: d.promo,
        shippingServiceId: freight?.serviceId ?? 0,
        ...(farbo ? { farboInstall: true } : {}),
      });
    },
    onSuccess: (result) => onDone(result, isAdmin ? undefined : payment),
    onError: (err: Error) => {
      // Quem só acompanhava o veículo de outra pessoa aceita o contrato ao
      // pedir o primeiro rastreador: o contrato abre e o pedido segue depois.
      if (errorCode(err) === 'CONTRACT_REQUIRED') {
        setError('');
        setContractOpen(true);
        return;
      }
      setError(err.message);
      if (isPromoUnavailable(err)) {
        // Os preços voltam aos normais; quem decide continuar confirma de novo.
        setError(`${err.message}. Os valores foram atualizados para os preços normais.`);
        queryClient.invalidateQueries({ queryKey: ['catalog'] });
        if (admin) {
          queryClient.invalidateQueries({ queryKey: ['customer', admin.customerId, 'launch-promo'] });
          setAdminDraft((d) => (d ? { ...d, promo: false } : d));
        }
      }
      if (isAddressRequired(err)) {
        setAddressDraft(EMPTY_ADDRESS);
        setStep(1);
        queryClient.invalidateQueries({ queryKey: ['me', 'account'] });
      }
      // A forma de entrega mudou (ou o frete não deu para calcular): cota de novo.
      if (/entrega|frete/.test(err.message)) void shippingQuote.refetch();
    },
  });

  const loading = !c || (!isAdmin && account.isLoading) || (isAdmin && (!adminDraft || customerCatalog.isLoading));
  // O rastreador do cliente: o do plano definido pela central; sem ele, o que
  // ele escolhe — incluso, ou em comodato quando a central oferece.
  const loanOffer: LoanOffer | null = priced && c && !c.planTerms ? (c.loan ?? null) : null;
  // Pelo link de um afiliado só de comodato: o comodato, sem a escolha.
  const onlyLoan = loanOffer !== null && c?.equipmentModes === 'loan';
  useEffect(() => {
    if (onlyLoan && mode !== 'loan') setMode('loan');
  }, [onlyLoan, mode]);
  const terms: EquipmentTerms | null = !priced
    ? (admin?.accountPlan ?? null)
    : c
      ? loanOffer && mode === 'loan'
        ? { equipmentMode: 'loan', installFeeCents: loanOffer.installFeeCents, uninstallFeeCents: loanOffer.uninstallFeeCents }
        : { equipmentMode: c.equipmentMode ?? 'sale', installFeeCents: c.installFeeCents ?? 0, uninstallFeeCents: c.uninstallFeeCents ?? 0 }
      : null;
  const loan = terms?.equipmentMode === 'loan';
  // O cliente veio pelo link de um representante: ele instala, desinstala e
  // recebe direto (os preços aparecem, mas não entram na cobrança).
  const direct = priced && Boolean(c?.installDirect);
  // A instalação pelos instaladores da Farbo no rastreador incluso (no
  // comodato, ela já é da Farbo, com a taxa dele).
  const installService = c?.install ?? null;
  const farbo = !loan && farboInstall && installService !== null;
  // A instalação com o relé do bloqueio: pelo representante que oferece a
  // escolha, no comodato ou com a instalação contratada.
  const blockingCents = priced ? (installService?.blockingCents ?? 0) : 0;
  const blocking = installBlocking && blockingCents > 0 && (loan || farbo);
  const installFee = loan && !direct ? (blocking ? blockingCents : (terms?.installFeeCents ?? 0)) : 0;
  const farboCents = farbo && !direct ? (blocking ? blockingCents : (installService?.priceCents ?? 0)) : 0;
  // O semestral ou o anual do representante que indicou o cliente (pessoa
  // física): o valor do período, sem a parte do rastreador nem a promoção.
  const periods = priced ? (c?.periods ?? []) : [];
  const period = periodMonths > 1 ? (periods.find((p) => p.months === periodMonths) ?? null) : null;
  // A promoção do cliente vem do catálogo (só com o rastreador incluso, no
  // mensal); a da central, da opção marcada.
  const customerPromo = priced && !loan && !period ? (c?.launchPromo ?? null) : null;
  const adminPromo = !priced && adminDraft?.promo && promoStatus.data?.eligible ? promoStatus.data.offer : null;
  const promo = customerPromo ?? adminPromo;
  // A do cliente já vem no plano dele; a da central depende do plano escolhido.
  const promoMonthly = adminPromo && adminDraft ? promoMonthlyFor(adminPromo, adminDraft.plan.planName) : promo?.monthlyCents;
  // O cliente sem endereço salvo cai direto no formulário.
  const editingAddress = !isAdmin && (addressDraft !== null || !address);
  const monthly = !priced
    ? adminDraft
      ? { name: adminDraft.plan.planName, cents: parseMoney(adminDraft.plan.price), dueDay: adminDraft.plan.dueDay }
      : null
    : c
      ? period
        ? { name: period.planName, cents: periodPrice(period, loan), dueDay: c.defaultDueDay }
        : {
            name: c.planName,
            // No comodato da pessoa física, a garantia de troca do rastreador soma na mensalidade.
            cents: c.planPriceCents + (loan && c.payNow ? (loanOffer?.surchargeCents ?? 0) : 0),
            dueDay: c.defaultDueDay,
          }
      : null;
  const installedDevice = isAdmin && Boolean(adminDraft?.deviceId);
  // O rastreador incluso (a Farbo não vende o rastreador): o do catálogo do
  // cliente; o da central, como ela definiu (pela regra, a parte é somada
  // abaixo do preço do site). No comodato, nenhum.
  const sitePlanCents = c?.sitePlanCents ?? 0;
  const adminMode = !priced && adminDraft ? trackerMode(adminDraft.tracker, monthly?.cents ?? null, sitePlanCents) : null;
  const adminTracker = !priced && adminDraft && adminMode ? trackerFromDraft(adminDraft.tracker, adminMode) : null;
  const tracker = loan || period ? null : !priced ? (adminTracker && typeof adminTracker !== 'string' ? adminTracker : null) : (c?.tracker ?? null);
  const trackerAdd = tracker && tracker.added && tracker.cents > 0 && tracker.months > 0 ? tracker : null;

  // O frete: as formas de entrega até o endereço salvo, na última etapa.
  const ships = !installedDevice && Boolean(address);
  const shippingQuote = useQuery({
    queryKey: ['shipping-quote', admin?.customerId ?? 'me', address?.zipCode ?? ''],
    queryFn: () => (admin ? customersApi.shippingQuote(admin.customerId) : meApi.shippingQuote()),
    enabled: open && step === 2 && ships,
    staleTime: 5 * 60_000,
  });
  const quotes = shippingQuote.data?.enabled ? shippingQuote.data.quotes : [];
  // O mais barato vem marcado (com o representante que entrega em mãos, a
  // entrega em mãos, a única).
  const handBy = shippingQuote.data?.handBy ?? '';
  useEffect(() => {
    if (handBy && shippingId !== ARRANGE_DELIVERY) setShippingId(ARRANGE_DELIVERY);
    else if (!handBy && shippingId === null && quotes.length > 0) setShippingId(quotes[0].serviceId);
  }, [shippingId, quotes, handBy]);
  const chosen = ships ? quotes.find((q) => q.serviceId === shippingId) : undefined;
  const freight = chosen ? { serviceId: chosen.serviceId, name: quoteName(chosen), cents: chosen.priceCents, days: chosen.deliveryDays } : null;
  // Perto da base, o cliente pode combinar a entrega com a central (sem frete).
  const arranged = ships && shippingId === ARRANGE_DELIVERY && shippingQuote.data?.arrange === true;
  // O cliente só confirma com a entrega escolhida (quando o frete é cotado).
  const needsShipping = !isAdmin && ships && shippingQuote.data?.enabled === true;
  // A pessoa física paga no ato (a 1ª mensalidade começa hoje); a empresa
  // não paga nada na hora: tudo entra na próxima fatura do mês da conta,
  // com o proporcional dos dias até ela.
  const payNow = isAdmin ? Boolean(admin?.payNow) : Boolean(c?.payNow);
  const today = todayISO();
  const chargeDate =
    priced && c?.chargeDate
      ? c.chargeDate
      : payNow
        ? addDays(today, c?.setupDueDays ?? 0)
        : nextChargeDate(monthly?.dueDay ?? c?.defaultDueDay ?? 10, today);
  // Quando o pedido é cobrado: "pago no pedido" ou "na fatura de 10/11".
  const when = payNow
    ? chargeDate === today
      ? 'pago no pedido'
      : `vence em ${formatDateOnly(chargeDate)}`
    : `na fatura de ${formatDateOnly(chargeDate)}`;
  const monthlyFirst = promoMonthly ?? monthly?.cents ?? 0;
  // O proporcional é do plano (a parte do rastreador vai só nas mensalidades).
  const prorata = payNow ? { days: 0, cents: 0 } : prorataUntil(monthlyFirst, today, chargeDate);
  const firstMonthly = monthlyFirst + (trackerAdd?.cents ?? 0);
  const invoiceTotal = prorata.cents + firstMonthly + installFee + farboCents + (freight?.cents ?? 0);

  const goTo = (next: number) => {
    setError('');
    setStep(next);
  };

  const next = () => {
    if (step === 0) return goTo(1);
    if (step === 1) {
      if (!priced && !loan && typeof adminTracker === 'string') return setError(adminTracker);
      if (editingAddress) {
        setError('');
        return saveAddress.mutate(addressDraft ?? EMPTY_ADDRESS);
      }
      return goTo(2);
    }
    setError('');
    order.mutate();
  };

  const canContinue =
    step === 0
      ? vehicle.name.trim() !== ''
      : step === 1
        ? !editingAddress || isAddressComplete(addressDraft ?? EMPTY_ADDRESS)
        : !needsShipping || ((freight !== null || arranged) && !shippingQuote.isFetching);

  const primaryLabel =
    step === 0
      ? 'Continuar'
      : step === 1
        ? editingAddress
          ? 'Salvar endereço e continuar'
          : 'Continuar'
        : isAdmin
          ? 'Confirmar'
          : 'Confirmar pedido';

  // As escolhas do pedido nas condições do cliente (o rastreador, o período, a promoção e a instalação):
  // o próprio cliente, ou a central pelo cliente do representante.
  const choices = c ? (
    <>
      {loanOffer && !onlyLoan && (
        <EquipmentChoice
          mode={mode}
          onChange={setMode}
          offer={loanOffer}
          catalog={c}
          direct={direct}
        />
      )}
      {periods.length > 0 && (
        <PeriodChoice
          periods={periods}
          value={period?.months ?? 1}
          onChange={setPeriodMonths}
          loan={loan}
          monthlyCents={c.planPriceCents + (loan ? (loanOffer?.surchargeCents ?? 0) : 0)}
        />
      )}
      {customerPromo && (
        <div className={styles.promoBanner}>
          <strong>Promoção de pré-lançamento</strong> — {isAdmin ? 'o cliente está' : 'você está'} na lista de lançamento: mensalidade de{' '}
          {formatMoney(customerPromo.monthlyCents)} nos {customerPromo.months} primeiros meses.
        </div>
      )}
      {loan ? (
        <>
          <LoanNote terms={terms!} payNow={payNow} direct={direct} />
          {blockingCents > 0 && (
            <BlockingChoice
              blocking={installBlocking}
              onChange={setInstallBlocking}
              plainCents={terms?.installFeeCents ?? 0}
              blockingCents={blockingCents}
              direct={direct}
              when={when}
            />
          )}
        </>
      ) : (
        <>
          <IncludedNote equipmentName={c.equipmentName} tracker={tracker} />
          {installService ? (
            <InstallChoice
              farbo={farboInstall}
              onChange={(on, relay) => {
                setFarboInstall(on);
                setInstallBlocking(relay);
              }}
              priceCents={installService.priceCents}
              blockingCents={blockingCents}
              blocking={installBlocking}
              when={when}
              direct={direct}
              onInstallers={() => setShowInstallers(true)}
            />
          ) : (
            <div className={styles.installNote}>
              <span>
                <strong>Instalação:</strong> é feita por um prestador parceiro e paga direto a ele —
                não entra na fatura.
              </span>
              <Button size="small" variant="secondary" onClick={() => setShowInstallers(true)}>
                Ver prestadores
              </Button>
            </div>
          )}
        </>
      )}
    </>
  ) : null;

  return (
    <>
      <Modal
        open={open}
        wide
        title="Novo veículo"
        onClose={onClose}
        footer={
          <>
            {step === 0 ? (
              <Button variant="ghost" onClick={onClose}>
                Cancelar
              </Button>
            ) : (
              <Button variant="ghost" onClick={() => goTo(step - 1)}>
                Voltar
              </Button>
            )}
            <Button
              variant="primary"
              loading={saveAddress.isPending || order.isPending}
              disabled={loading || !canContinue}
              onClick={next}
            >
              {primaryLabel}
            </Button>
          </>
        }
      >
        <ol className={styles.steps} aria-label="Etapas">
          {STEPS.map((label, index) => (
            <li
              key={label}
              className={`${styles.step} ${index === step ? styles.stepActive : ''} ${index < step ? styles.stepDone : ''}`}
              aria-current={index === step ? 'step' : undefined}
            >
              <button type="button" disabled={index >= step} onClick={() => goTo(index)}>
                <span className={styles.stepNumber}>{index < step ? '✓' : index + 1}</span>
                {label}
              </button>
            </li>
          ))}
        </ol>

        {loading ? (
          <Spinner label="Carregando" />
        ) : (
          <div className={pageStyles.form}>
            {error && <div className={pageStyles.note}>{error}</div>}

            {step === 0 && (
              <>
                <p className={pageStyles.description}>
                  Qual veículo vai receber o rastreador?
                </p>
                <VehicleFields value={vehicle} onChange={setVehicle} fleet={fleet} autoFocus />
              </>
            )}

            {step === 1 && admin && adminDraft && (
              <>
                <DeviceSelectField
                  devices={admin.devices}
                  label="Aparelho"
                  hint="Se o rastreador já foi instalado no veículo, escolha o aparelho para vincular agora."
                  value={adminDraft.deviceId}
                  emptyOption="Enviar ao cliente (instalação depois)"
                  optionLabel={(device) => `Já instalado: ${deviceOptionLabel(device)}`}
                  onChange={(deviceId) => setAdminDraft({ ...adminDraft, deviceId })}
                />
                {referred && (
                  <div className={styles.promoBanner} role="note">
                    <strong>Cliente do representante {representative}</strong> — o veículo sai nas condições dele: a
                    mensalidade, o rastreador, a instalação, as taxas e o semestral ou o anual. Para outro valor, defina
                    um plano para a conta na ficha do cliente.
                  </div>
                )}
                {referred ? null : promoStatus.data?.eligible ? (
                  <label className={styles.promoOption}>
                    <input
                      type="checkbox"
                      checked={adminDraft.promo}
                      onChange={(e) => setAdminDraft({ ...adminDraft, promo: e.target.checked })}
                    />
                    <span>
                      <strong>Aplicar a promoção de pré-lançamento</strong>
                      <br />
                      Mensalidade de {formatMoney(promoStatus.data.offer.monthlyCents)} (
                      {formatMoney(promoStatus.data.offer.insanosMonthlyCents)} no plano{' '}
                      {promoStatus.data.offer.insanosPlanName}) nos {promoStatus.data.offer.months} primeiros meses
                      (depois, o plano escolhido).{' '}
                      {promoStatus.data.claimed
                        ? 'O cliente já tem a vaga: a promoção vale para todos os veículos dele.'
                        : 'O cliente está na lista de lançamento; ocupa 1 das vagas (e vale para todos os veículos dele).'}
                    </span>
                  </label>
                ) : (
                  promoStatus.data && (
                    <div className={styles.promoGrant}>
                      <p className={styles.muted}>Promoção de pré-lançamento: {promoStatus.data.reason}.</p>
                      {/* Fora da lista: a central pode liberar para este cliente. */}
                      {promoStatus.data.code === 'NOT_ON_LIST' && (
                        <Button size="small" variant="secondary" loading={grantPromo.isPending} onClick={() => grantPromo.mutate()}>
                          Liberar para este cliente
                        </Button>
                      )}
                    </div>
                  )
                )}
                {referred ? (
                  choices
                ) : loan ? (
                  <LoanNote terms={terms!} payNow={payNow} direct={direct} />
                ) : (
                  <TrackerFields
                    draft={adminDraft.tracker}
                    onChange={(next) => setAdminDraft({ ...adminDraft, tracker: next })}
                    planCents={monthly?.cents ?? null}
                    sitePlanCents={sitePlanCents}
                  />
                )}
                {!installedDevice &&
                  (address ? (
                    <DeliveryBox address={address} />
                  ) : (
                    <div className={`${styles.delivery} ${styles.deliveryEmpty}`}>
                      <div className={styles.deliveryText}>
                        <span className={styles.deliveryLabel}>Entrega do rastreador</span>
                        <span>
                          O cliente não tem endereço de entrega. Tudo bem se o aparelho for entregue em
                          mãos; para enviar, cadastre o endereço na ficha antes.
                        </span>
                      </div>
                    </div>
                  ))}
                {!referred &&
                  !loan &&
                  (installService ? (
                    <label className={styles.promoOption}>
                      <input type="checkbox" checked={farboInstall} onChange={(e) => setFarboInstall(e.target.checked)} />
                      <span>
                        <strong>Instalação pelos instaladores da Farbo ({formatMoney(installService.priceCents)})</strong>
                        <br />
                        {payNow ? 'Vence com a 1ª mensalidade' : 'Entra na fatura'}; a central agenda o dia na Agenda. Sem
                        marcar, o cliente instala por conta própria (pago direto ao prestador).
                      </span>
                    </label>
                  ) : (
                    <p className={styles.muted}>A instalação é paga direto ao prestador e não entra na fatura.</p>
                  ))}
              </>
            )}

            {step === 1 && !admin && (
              <>
                {editingAddress ? (
                  <>
                    <p className={pageStyles.description}>
                      {address ? 'Para onde enviamos o rastreador agora?' : 'Para onde enviamos o rastreador?'}
                    </p>
                    <AddressFields value={addressDraft ?? EMPTY_ADDRESS} onChange={setAddressDraft} autoFocus />
                    {address && (
                      <Button size="small" variant="ghost" onClick={() => setAddressDraft(null)}>
                        Manter o endereço atual
                      </Button>
                    )}
                  </>
                ) : (
                  address && <DeliveryBox address={address} onChange={() => setAddressDraft(address)} />
                )}
                {choices}
              </>
            )}

            {step === 2 && (
              <>
                {admin && adminDraft && !referred ? (
                  <SubscriptionFields
                    draft={adminDraft.plan}
                    onChange={(plan) => setAdminDraft({ ...adminDraft, plan })}
                    lockedDueDay={admin.accountDueDay ?? null}
                  />
                ) : referred && monthly ? (
                  <p className={pageStyles.description}>
                    {period
                      ? `A assinatura sai no ${period.planName} do representante ${representative}: ${formatMoney(monthly.cents ?? 0)} a cada ${period.months} meses, no dia ${monthly.dueDay}.`
                      : `A assinatura sai no ${monthly.name} do representante ${representative}: ${formatMoney(monthly.cents ?? 0)} por mês${trackerAdd ? `, mais ${formatMoney(trackerAdd.cents)} nas ${trackerAdd.months} primeiras (a parte do rastreador incluso)` : ''}, no dia ${monthly.dueDay}.`}{' '}
                    {payNow ? 'O pedido vence no ato.' : `Tudo entra na fatura de ${formatDateOnly(chargeDate)}.`}
                  </p>
                ) : (
                  <p className={pageStyles.description}>
                    {period
                      ? `A assinatura começa com o pedido, no ${period.planName}: hoje você paga o 1º ${period.months === 12 ? 'ano' : 'semestre'} e o resto do pedido; depois, uma fatura a cada ${period.months} meses, no dia ${c.defaultDueDay}. Se cancelar antes do fim do período pago, não há devolução: o rastreamento segue até o fim dele e não renova.`
                      : payNow
                      ? `A assinatura começa com o pedido, no plano da sua conta: ${c.planName}. Hoje você paga a 1ª mensalidade e o resto do pedido; depois, uma fatura por mês, todo dia ${c.defaultDueDay} — a 2ª pelo menos um mês depois da 1ª.`
                      : `A assinatura começa com o pedido, no plano da sua conta: ${c.planName}. Nada é cobrado agora: tudo entra na sua fatura de ${formatDateOnly(chargeDate)}, com os dias até lá proporcionais. Depois, uma fatura por mês, todo dia ${c.defaultDueDay}.`}
                  </p>
                )}

                {loan && <LoanNote terms={terms!} payNow={payNow} />}

                {ships && (
                  <ShippingOptions
                    view={shippingQuote.data}
                    loading={shippingQuote.isLoading}
                    value={shippingId ?? -1}
                    onChange={setShippingId}
                    onRetry={() => void shippingQuote.refetch()}
                    allowNone={isAdmin}
                  />
                )}

                <div className={styles.orderSummary}>
                  <div className={styles.orderLine}>
                    <span>Veículo</span>
                    <span>{[vehicle.name, vehicle.plate].filter(Boolean).join(' · ')}</span>
                  </div>
                  <div className={styles.orderLine}>
                    <span>Rastreador</span>
                    <span>
                      {installedDevice
                        ? 'já instalado'
                        : address
                          ? `entrega em ${address.city}/${address.state}`
                          : 'entrega em mãos'}
                    </span>
                  </div>
                  {prorata.cents > 0 && (
                    <div className={styles.orderLine}>
                      <span>
                        Proporcional de hoje a {formatDateOnly(chargeDate)} ({prorata.days} {prorata.days === 1 ? 'dia' : 'dias'})
                      </span>
                      <span>{formatMoney(prorata.cents)}</span>
                    </div>
                  )}
                  {firstMonthly > 0 && (
                    <div className={styles.orderLine}>
                      <span>{period ? (period.months === 12 ? '1º ano' : '1º semestre') : '1ª mensalidade'}</span>
                      <span>{formatMoney(firstMonthly)}</span>
                    </div>
                  )}
                  {farbo && !direct && (
                    <div className={styles.orderLine}>
                      <span>Instalação (instaladores Farbo{blocking ? ', com bloqueio' : ''})</span>
                      <span>{formatMoney(farboCents)}</span>
                    </div>
                  )}
                  {loan ? (
                    installFee > 0 && (
                      <div className={styles.orderLine}>
                        <span>Instalação (comodato{blocking ? ', com bloqueio' : ''})</span>
                        <span>{formatMoney(installFee)}</span>
                      </div>
                    )
                  ) : (
                    <div className={styles.orderLine}>
                      <span>{tracker && tracker.cents > 0 ? 'Rastreador incluso (fica com o cliente)' : 'Rastreador do cliente'}</span>
                      <span>sem cobrança</span>
                    </div>
                  )}
                  {freight && (
                    <div className={styles.orderLine}>
                      <span>
                        Frete: {freight.name} ({deliveryLabel(freight.days)})
                      </span>
                      <span>{formatMoney(freight.cents)}</span>
                    </div>
                  )}
                  {arranged && (
                    <div className={styles.orderLine}>
                      <span>Entrega: combinar com a Farbo</span>
                      <span>sem frete</span>
                    </div>
                  )}
                  <div className={`${styles.orderLine} ${styles.orderTotal}`}>
                    {payNow ? (
                      <span>
                        Agora{chargeDate !== today ? ` · vence ${formatDateOnly(chargeDate)}` : ''}
                        <br />
                        <span className={styles.muted}>
                          {period
                            ? `O período começa hoje; o próximo vence em ${period.months} meses, no dia da conta.`
                            : 'A 1ª mensalidade começa hoje; a 2ª vence pelo menos um mês depois, no dia da conta.'}
                        </span>
                      </span>
                    ) : (
                      <span>
                        Na fatura de {formatDateOnly(chargeDate)}
                        <br />
                        <span className={styles.muted}>Hoje não é cobrado nada.</span>
                      </span>
                    )}
                    <span>{invoiceTotal ? formatMoney(invoiceTotal) : 'sem cobrança'}</span>
                  </div>
                  {monthly && (
                    <div className={`${styles.orderLine} ${styles.orderMonthly}`}>
                      <span>
                        Assinatura: {monthly.name || 'plano'}, {period ? `a cada ${period.months} meses, no dia` : 'todo dia'}{' '}
                        {monthly.dueDay}
                        <br />
                        <span className={styles.muted}>
                          {period ? 'Paga de uma vez, a cada período.' : 'Uma fatura por mês, com todos os veículos da conta.'}
                        </span>
                        {promo && (
                          <>
                            <br />
                            <span className={styles.muted}>
                              Promoção de pré-lançamento nos {promo.months} primeiros meses; depois,{' '}
                              {monthly.cents !== null ? `${formatMoney(monthly.cents)}/mês` : 'o plano'}.
                            </span>
                          </>
                        )}
                        {trackerAdd && monthly.cents !== null && (
                          <>
                            <br />
                            <span className={styles.muted}>
                              Com o rastreador incluso: {formatMoney(monthlyFirst + trackerAdd.cents)}/mês nas{' '}
                              {trackerAdd.months} primeiras mensalidades; depois, {formatMoney(monthlyFirst)}/mês.
                            </span>
                          </>
                        )}
                      </span>
                      <span>
                        {promoMonthly !== undefined
                          ? `${formatMoney(promoMonthly + (trackerAdd?.cents ?? 0))}/mês`
                          : monthly.cents !== null
                            ? `${formatMoney(monthly.cents + (trackerAdd?.cents ?? 0))}${period ? (period.months === 12 ? '/ano' : '/semestre') : '/mês'}`
                            : '—'}
                      </span>
                    </div>
                  )}
                </div>
                {tracker && tracker.cents > 0 && tracker.months > 0 && (
                  <p className={styles.muted}>
                    {isAdmin ? 'O rastreador incluso fica com o cliente.' : 'O rastreador incluso fica com você.'} Se a
                    assinatura for encerrada antes da {tracker.months}ª mensalidade, a parte do rastreador que falta (
                    {formatMoney(tracker.cents)} por mensalidade) vence de uma vez (contrato, cláusula 7).
                  </p>
                )}
                {!isAdmin && payNow && invoiceTotal > 0 && account.data?.onlinePayment && (
                  <PaymentChoice value={payment} onChange={setPayment} card={Boolean(card.data?.enabled)} />
                )}
              </>
            )}
          </div>
        )}
      </Modal>

      <InstallersModal isOpen={showInstallers} onClose={() => setShowInstallers(false)} />

      <Modal open={contractOpen} title="Contrato de prestação de serviços" onClose={() => setContractOpen(false)}>
        {contract.data ? (
          <ContractAcceptForm
            status={contract.data}
            onAccepted={() => {
              setContractOpen(false);
              order.mutate();
            }}
          />
        ) : (
          <Spinner label="Carregando o contrato" />
        )}
      </Modal>
    </>
  );
}

/**
 * O rastreador incluso para o cliente: fica com ele, sem preço à parte (só o
 * frete); quando a parte dele soma à mensalidade, em quantas.
 */
function IncludedNote({ equipmentName, tracker }: { equipmentName: string; tracker: Tracker | null }) {
  const added = tracker && tracker.added && tracker.cents > 0 && tracker.months > 0;
  return (
    <div className={styles.installNote}>
      <span>
        <strong>Rastreador incluso:</strong> o {equipmentName} fica com você, sem pagar o rastreador à parte — só o frete,
        quando houver.
        {added && ` Nas ${tracker.months} primeiras mensalidades, + ${formatMoney(tracker.cents)}.`}
      </span>
    </div>
  );
}

/**
 * O rastreador em comodato: sem equipamento a pagar; a instalação com a 1ª
 * mensalidade (no pedido, para a pessoa física; na fatura, para a empresa) e
 * a desinstalação no encerramento.
 */
function LoanNote({ terms, payNow, direct = false }: { terms: EquipmentTerms; payNow: boolean; direct?: boolean }) {
  // Pelo representante: a instalação e a desinstalação são pagas direto a ele.
  const install = direct ? 'é paga direto ao representante, no dia' : payNow ? 'é paga no pedido' : 'entra na fatura';
  return (
    <div className={styles.installNote}>
      <span>
        <strong>Rastreador em comodato:</strong> emprestado, volta na desinstalação — não há equipamento a pagar.
        {terms.installFeeCents > 0 ? ` A instalação (${formatMoney(terms.installFeeCents)}) ${install}.` : ' Instalação sem custo.'}
        {terms.uninstallFeeCents > 0
          ? ` Ao encerrar, a desinstalação: ${formatMoney(terms.uninstallFeeCents)}${direct ? ', também direto a ele' : ''}.`
          : ''}
      </span>
    </div>
  );
}

/**
 * Incluso ou comodato (quando a central oferece o comodato e não há plano
 * definido para o cliente): a mensalidade de cada um.
 */
function EquipmentChoice({
  mode,
  onChange,
  offer,
  catalog,
  direct = false,
}: {
  mode: 'sale' | 'loan';
  onChange: (mode: 'sale' | 'loan') => void;
  offer: LoanOffer;
  catalog: Catalog;
  /** Pelo representante: a instalação e a desinstalação pagas direto a ele. */
  direct?: boolean;
}) {
  // A garantia de troca do rastreador: só a pessoa física (que paga no ato).
  const guarantee = catalog.payNow ? offer.surchargeCents : 0;
  const loanMonthly = catalog.planPriceCents + guarantee;
  const t = catalog.tracker;
  const includedAdd = t && t.added && t.cents > 0 && t.months > 0 ? t : null;
  const fees = [
    offer.installFeeCents > 0 ? `instalação ${formatMoney(offer.installFeeCents)}` : 'instalação sem custo',
    offer.uninstallFeeCents > 0 ? `desinstalação ${formatMoney(offer.uninstallFeeCents)} ao encerrar` : '',
    direct ? 'pagas direto ao representante' : '',
  ].filter(Boolean);
  return (
    <fieldset className={styles.paymentChoice}>
      <legend>O rastreador</legend>
      <label className={mode === 'sale' ? styles.paymentOn : undefined}>
        <input type="radio" name="equipment-mode" checked={mode === 'sale'} onChange={() => onChange('sale')} />
        <span>
          <strong>Rastreador incluso</strong>
          <span className={styles.muted}>
            {' '}
            · fica com você, sem pagar o rastreador à parte. Mensalidade de{' '}
            {formatMoney(catalog.launchPromo?.monthlyCents ?? catalog.planPriceCents)}
            {includedAdd ? ` (+ ${formatMoney(includedAdd.cents)} nas ${includedAdd.months} primeiras)` : ''}.
          </span>
        </span>
      </label>
      <label className={mode === 'loan' ? styles.paymentOn : undefined}>
        <input type="radio" name="equipment-mode" checked={mode === 'loan'} onChange={() => onChange('loan')} />
        <span>
          <strong>Comodato (rastreador emprestado)</strong>
          <span className={styles.muted}>
            {' '}
            · o rastreador emprestado; ele volta na desinstalação. Mensalidade de {formatMoney(loanMonthly)}
            {guarantee > 0 ? `, com ${formatMoney(guarantee)} de garantia de troca do rastreador` : ''}; {fees.join(', ')}.
          </span>
        </span>
      </label>
    </fieldset>
  );
}

/** O preço de uma instalação: direto ao representante (fora da cobrança) ou na cobrança do pedido. */
function installPrice(cents: number, direct: boolean, when: string): string {
  if (direct) return cents > 0 ? `${formatMoney(cents)}, pago direto a ele no dia (não entra na cobrança)` : 'sem custo';
  return `${formatMoney(cents)}, ${when}`;
}

/**
 * Quem instala o rastreador incluso: o cliente, por conta própria (com o
 * instalador dele, pago direto), ou os instaladores da Farbo, pelo preço das
 * opções do pedido (a central agenda o dia). Pelo representante que oferece
 * a escolha, a instalação dele sem ou com o relé do bloqueio.
 */
function InstallChoice({
  farbo,
  onChange,
  priceCents,
  blockingCents = 0,
  blocking = false,
  when,
  direct = false,
  onInstallers,
}: {
  farbo: boolean;
  /** on: com a instalação contratada; relay: com o relé do bloqueio. */
  onChange: (on: boolean, relay: boolean) => void;
  priceCents: number;
  /** A instalação com o relé do bloqueio (zero: sem a escolha). */
  blockingCents?: number;
  blocking?: boolean;
  /** "pago no pedido" ou "na fatura de 10/11". */
  when: string;
  /** Pelo representante: ele instala e recebe direto (fora da cobrança). */
  direct?: boolean;
  onInstallers: () => void;
}) {
  const who = direct ? 'Com o representante' : 'Com os instaladores da Farbo';
  const options =
    blockingCents > 0
      ? [
          { relay: false, title: `${who}, sem bloqueio`, cents: priceCents, extra: '' },
          { relay: true, title: `${who}, com bloqueio`, cents: blockingCents, extra: 'com o relé que bloqueia o motor pelo app; ' },
        ]
      : [{ relay: false, title: who, cents: priceCents, extra: '' }];
  return (
    <fieldset className={styles.paymentChoice}>
      <legend>Instalação</legend>
      <label className={!farbo ? styles.paymentOn : undefined}>
        <input type="radio" name="install-choice" checked={!farbo} onChange={() => onChange(false, false)} />
        <span>
          <strong>Por conta própria</strong>
          <span className={styles.muted}>
            {' '}
            · com o instalador da sua preferência, pago direto a ele; não entra na cobrança.{' '}
            <button type="button" className={styles.linkButton} onClick={onInstallers}>
              Ver prestadores
            </button>
          </span>
        </span>
      </label>
      {options.map((o) => {
        const on = farbo && (blockingCents <= 0 || blocking === o.relay);
        return (
          <label key={o.title} className={on ? styles.paymentOn : undefined}>
            <input type="radio" name="install-choice" checked={on} onChange={() => onChange(true, o.relay)} />
            <span>
              <strong>{o.title}</strong>
              <span className={styles.muted}>
                {' '}
                · {o.extra}
                {installPrice(o.cents, direct, when)}; agendamos o dia com você.
              </span>
            </span>
          </label>
        );
      })}
    </fieldset>
  );
}

/** No comodato, pelo representante que oferece a escolha: a instalação sem ou com o relé do bloqueio. */
function BlockingChoice({
  blocking,
  onChange,
  plainCents,
  blockingCents,
  direct,
  when,
}: {
  blocking: boolean;
  onChange: (blocking: boolean) => void;
  plainCents: number;
  blockingCents: number;
  direct: boolean;
  when: string;
}) {
  return (
    <fieldset className={styles.paymentChoice}>
      <legend>Instalação</legend>
      <label className={!blocking ? styles.paymentOn : undefined}>
        <input type="radio" name="install-blocking" checked={!blocking} onChange={() => onChange(false)} />
        <span>
          <strong>Sem bloqueio</strong>
          <span className={styles.muted}> · {installPrice(plainCents, direct, when)}.</span>
        </span>
      </label>
      <label className={blocking ? styles.paymentOn : undefined}>
        <input type="radio" name="install-blocking" checked={blocking} onChange={() => onChange(true)} />
        <span>
          <strong>Com bloqueio</strong>
          <span className={styles.muted}>
            {' '}
            · com o relé que bloqueia o motor pelo app; {installPrice(blockingCents, direct, when)}.
          </span>
        </span>
      </label>
    </fieldset>
  );
}

/**
 * Mensal, semestral ou anual: pelo representante que indicou o cliente (pessoa
 * física) com o semestral e o anual ligados. O período é pago de uma vez (no
 * comodato, com a garantia de troca de cada mês), sem devolução.
 */
function PeriodChoice({
  periods,
  value,
  onChange,
  loan,
  monthlyCents,
}: {
  periods: PeriodOffer[];
  value: number;
  onChange: (months: number) => void;
  loan: boolean;
  monthlyCents: number;
}) {
  return (
    <fieldset className={styles.paymentChoice}>
      <legend>A assinatura</legend>
      <label className={value <= 1 ? styles.paymentOn : undefined}>
        <input type="radio" name="period-choice" checked={value <= 1} onChange={() => onChange(1)} />
        <span>
          <strong>Mensal</strong>
          <span className={styles.muted}> · {formatMoney(monthlyCents)}/mês, uma fatura por mês.</span>
        </span>
      </label>
      {periods.map((p) => {
        const cents = periodPrice(p, loan);
        return (
          <label key={p.months} className={value === p.months ? styles.paymentOn : undefined}>
            <input type="radio" name="period-choice" checked={value === p.months} onChange={() => onChange(p.months)} />
            <span>
              <strong>{p.months === 12 ? 'Anual' : 'Semestral'}</strong>
              <span className={styles.muted}>
                {' '}
                · {formatMoney(cents)} a cada {p.months} meses ({formatMoney(Math.floor(cents / p.months))} por mês),{' '}
                {loan
                  ? cents > p.cents
                    ? 'com a garantia de troca do rastreador emprestado'
                    : 'com o rastreador emprestado'
                  : 'com o rastreador incluso no valor'}
                . Se cancelar antes do fim do período pago, não há devolução: segue até o fim dele e não renova.
              </span>
            </span>
          </label>
        );
      })}
    </fieldset>
  );
}

const PAYMENT_CHOICES: { id: ChargeMethod; label: string; hint: string }[] = [
  { id: 'PIX', label: 'Pix', hint: 'confirma na hora' },
  { id: 'BOLETO', label: 'Boleto', hint: 'compensa em até 3 dias úteis' },
  {
    id: 'CARD',
    label: 'Cartão de crédito',
    hint: 'à vista; marcando o débito automático, as mensalidades também caem nele, no vencimento',
  },
];

/** Como pagar o pedido (pessoa física, no ato): a janela de pagamento abre na forma escolhida. */
function PaymentChoice({
  value,
  onChange,
  card,
}: {
  value: ChargeMethod;
  onChange: (value: ChargeMethod) => void;
  card: boolean;
}) {
  return (
    <fieldset className={styles.paymentChoice}>
      <legend>Como pagar</legend>
      {PAYMENT_CHOICES.filter((choice) => choice.id !== 'CARD' || card).map((choice) => (
        <label key={choice.id} className={value === choice.id ? styles.paymentOn : undefined}>
          <input type="radio" name="order-payment" checked={value === choice.id} onChange={() => onChange(choice.id)} />
          <span>
            <strong>{choice.label}</strong>
            <span className={styles.muted}> · {choice.hint}</span>
          </span>
        </label>
      ))}
    </fieldset>
  );
}

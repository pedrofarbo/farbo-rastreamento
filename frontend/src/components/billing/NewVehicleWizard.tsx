import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ApiError } from '@/api/client';
import { catalogApi, contractApi, customersApi, meApi } from '@/api/resources';
import type { VehicleInput } from '@/api/resources';
import { AddressFields, EMPTY_ADDRESS, isAddressComplete } from '@/components/address/AddressFields';
import { DeliveryBox } from '@/components/address/DeliveryBox';
import { ARRANGE_DELIVERY, ShippingOptions, deliveryLabel, quoteName } from '@/components/billing/ShippingOptions';
import { promoMonthlyFor } from '@/components/billing/MonthlyPrice';
import { installmentChoices, splitInstallments } from '@/components/billing/installments';
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
import { SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { EMPTY_VEHICLE, VehicleFields } from '@/components/vehicle/VehicleFields';
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
  Subscription,
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
  equipment: string;
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

/** Valor opcional: vazio é zero (não cobra). */
function optionalMoney(text: string): number | null {
  return text.trim() === '' ? 0 : parseMoney(text);
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
  return {
    deviceId: '',
    equipment: account?.equipmentMode === 'loan' ? '0,00' : centsToInput(c.equipmentPriceCents),
    plan,
    promo: false,
  };
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
 * plano e pode vincular um aparelho já instalado.
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
  // O rastreador em quantas vezes (1: à vista) e, parcelado, o "entendi" do
  // cliente sobre manter a assinatura até a última parcela.
  const [installments, setInstallments] = useState(1);
  const [committed, setCommitted] = useState(false);
  // O rastreador comprado ou em comodato (quando a central oferece).
  const [mode, setMode] = useState<'sale' | 'loan'>('sale');
  // Como a pessoa física paga o pedido (a janela de pagamento abre nela).
  const [payment, setPayment] = useState<ChargeMethod>('PIX');
  // O rastreador comprado: instalado pelos instaladores da Farbo (pago) ou
  // por conta própria.
  const [farboInstall, setFarboInstall] = useState(false);

  const catalog = useQuery({ queryKey: ['catalog'], queryFn: catalogApi.get, enabled: open });
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
      if (next.eligible) {
        setAdminDraft((d) => (d ? { ...d, promo: true, equipment: centsToInput(next.offer.equipmentCents) } : d));
      }
    },
    onError: (err: Error) => setError(err.message),
  });
  const c = catalog.data;
  const address = isAdmin ? (admin?.deliveryAddress ?? null) : (account.data?.deliveryAddress ?? null);

  useEffect(() => {
    if (!open) return;
    setStep(0);
    setVehicle(EMPTY_VEHICLE);
    setAddressDraft(null);
    setAdminDraft(null);
    setError('');
    setShippingId(null);
    setInstallments(1);
    setCommitted(false);
    setMode('sale');
    setPayment('PIX');
    setFarboInstall(false);
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
      if (promoStatus.data.eligible) {
        setAdminDraft({ ...adminDraft, promo: true, equipment: centsToInput(promoStatus.data.offer.equipmentCents) });
      }
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
          ...(parcels ? { installments: parcels.length } : {}),
          ...(loanOffer ? { equipmentMode: mode } : {}),
          ...(farbo ? { farboInstall: true } : {}),
        });
      }
      const d = adminDraft as AdminDraft;
      const plan = subscriptionFromDraft(d.plan);
      if (typeof plan === 'string') throw new Error(plan);
      return customersApi.orderTracker(admin.customerId, {
        vehicle: { ...vehicle, deviceId: d.deviceId || null },
        equipmentCents: loan ? 0 : (optionalMoney(d.equipment) ?? 0),
        plan,
        launchPromo: d.promo,
        shippingServiceId: freight?.serviceId ?? 0,
        ...(parcels ? { installments: parcels.length } : {}),
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
          setAdminDraft((d) => (d && c ? { ...d, promo: false, equipment: centsToInput(c.equipmentPriceCents) } : d));
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

  const loading = !c || (!isAdmin && account.isLoading) || (isAdmin && !adminDraft);
  // O rastreador do cliente: o do plano definido pela central; sem ele, o que
  // ele escolhe — comprado, ou em comodato quando a central oferece.
  const loanOffer: LoanOffer | null = !isAdmin && c && !c.planTerms ? (c.loan ?? null) : null;
  const terms: EquipmentTerms | null = isAdmin
    ? (admin?.accountPlan ?? null)
    : c
      ? loanOffer && mode === 'loan'
        ? { equipmentMode: 'loan', installFeeCents: loanOffer.installFeeCents, uninstallFeeCents: loanOffer.uninstallFeeCents }
        : { equipmentMode: c.equipmentMode ?? 'sale', installFeeCents: c.installFeeCents ?? 0, uninstallFeeCents: c.uninstallFeeCents ?? 0 }
      : null;
  const loan = terms?.equipmentMode === 'loan';
  const installFee = loan ? (terms?.installFeeCents ?? 0) : 0;
  // A instalação pelos instaladores da Farbo no rastreador comprado (no
  // comodato, ela já é da Farbo, com a taxa dele).
  const installService = c?.install ?? null;
  const farbo = !loan && farboInstall && installService !== null;
  const farboCents = farbo ? installService.priceCents : 0;
  // A promoção do cliente vem do catálogo (só com o rastreador comprado); a
  // da central, da opção marcada.
  const customerPromo = !isAdmin && !loan ? (c?.launchPromo ?? null) : null;
  const adminPromo = isAdmin && adminDraft?.promo && promoStatus.data?.eligible ? promoStatus.data.offer : null;
  const promo = customerPromo ?? adminPromo;
  // A do cliente já vem no plano dele; a da central depende do plano escolhido.
  const promoMonthly = adminPromo && adminDraft ? promoMonthlyFor(adminPromo, adminDraft.plan.planName) : promo?.monthlyCents;
  // O cliente sem endereço salvo cai direto no formulário.
  const editingAddress = !isAdmin && (addressDraft !== null || !address);
  const equipmentCents = loan
    ? 0
    : isAdmin
      ? adminDraft
        ? optionalMoney(adminDraft.equipment)
        : null
      : (customerPromo?.equipmentCents ?? c?.equipmentPriceCents ?? 0);
  const monthly = isAdmin
    ? adminDraft
      ? { name: adminDraft.plan.planName, cents: parseMoney(adminDraft.plan.price), dueDay: adminDraft.plan.dueDay }
      : null
    : c
      ? {
          name: c.planName,
          // No comodato da pessoa física, a garantia de troca do rastreador soma na mensalidade.
          cents: c.planPriceCents + (loan && c.payNow ? (loanOffer?.surchargeCents ?? 0) : 0),
          dueDay: c.defaultDueDay,
        }
      : null;
  const installedDevice = isAdmin && Boolean(adminDraft?.deviceId);

  // O frete: as formas de entrega até o endereço salvo, na última etapa.
  const ships = !installedDevice && Boolean(address);
  const shippingQuote = useQuery({
    queryKey: ['shipping-quote', admin?.customerId ?? 'me', address?.zipCode ?? ''],
    queryFn: () => (admin ? customersApi.shippingQuote(admin.customerId) : meApi.shippingQuote()),
    enabled: open && step === 2 && ships,
    staleTime: 5 * 60_000,
  });
  const quotes = shippingQuote.data?.enabled ? shippingQuote.data.quotes : [];
  // O mais barato vem marcado.
  useEffect(() => {
    if (shippingId === null && quotes.length > 0) setShippingId(quotes[0].serviceId);
  }, [shippingId, quotes]);
  const chosen = ships ? quotes.find((q) => q.serviceId === shippingId) : undefined;
  const freight = chosen ? { serviceId: chosen.serviceId, name: quoteName(chosen), cents: chosen.priceCents, days: chosen.deliveryDays } : null;
  // Perto da base, o cliente pode combinar a entrega com a central (sem frete).
  const arranged = ships && shippingId === ARRANGE_DELIVERY && shippingQuote.data?.arrange === true;
  // O cliente só confirma com a entrega escolhida (quando o frete é cotado).
  const needsShipping = !isAdmin && ships && shippingQuote.data?.enabled === true;
  // Parcelado: o equipamento vai nas mensalidades (a 1ª parcela na 1ª).
  const choices = installmentChoices(equipmentCents ?? 0, c?.equipmentMaxInstallments ?? 1);
  const parcels = installments > 1 && choices.length >= installments ? splitInstallments(equipmentCents ?? 0, installments) : null;
  // A pessoa física paga no ato (a 1ª mensalidade começa hoje); a empresa
  // não paga nada na hora: tudo entra na próxima fatura do mês da conta,
  // com o proporcional dos dias até ela.
  const payNow = isAdmin ? Boolean(admin?.payNow) : Boolean(c?.payNow);
  const today = todayISO();
  const chargeDate =
    !isAdmin && c?.chargeDate
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
  const prorata = payNow ? { days: 0, cents: 0 } : prorataUntil(monthlyFirst, today, chargeDate);
  const firstMonthly = monthlyFirst + (parcels?.[0] ?? 0);
  const invoiceTotal =
    prorata.cents + firstMonthly + (parcels ? 0 : (equipmentCents ?? 0)) + installFee + farboCents + (freight?.cents ?? 0);

  const goTo = (next: number) => {
    setError('');
    setStep(next);
  };

  const next = () => {
    if (step === 0) return goTo(1);
    if (step === 1) {
      if (isAdmin && equipmentCents === null) {
        return setError('Valor do equipamento inválido. Use, por exemplo, 150,00 (ou 0 para não cobrar).');
      }
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
        : (!needsShipping || ((freight !== null || arranged) && !shippingQuote.isFetching)) &&
          (isAdmin || !parcels || committed);

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
                <SelectField
                  label="Aparelho"
                  hint="Se o rastreador já foi instalado no veículo, escolha o aparelho para vincular agora."
                  value={adminDraft.deviceId}
                  onChange={(e) => setAdminDraft({ ...adminDraft, deviceId: e.target.value })}
                >
                  <option value="">Enviar ao cliente (instalação depois)</option>
                  {admin.devices.map((device) => (
                    <option key={device.id} value={device.id}>
                      Já instalado: {device.imei} {device.model ? `· ${device.model}` : ''}
                    </option>
                  ))}
                </SelectField>
                {promoStatus.data?.eligible ? (
                  <label className={styles.promoOption}>
                    <input
                      type="checkbox"
                      checked={adminDraft.promo}
                      onChange={(e) =>
                        setAdminDraft({
                          ...adminDraft,
                          promo: e.target.checked,
                          equipment: centsToInput(
                            e.target.checked ? promoStatus.data!.offer.equipmentCents : c.equipmentPriceCents,
                          ),
                        })
                      }
                    />
                    <span>
                      <strong>Aplicar a promoção de pré-lançamento</strong>
                      <br />
                      Rastreador por {formatMoney(promoStatus.data.offer.equipmentCents)} e mensalidade de{' '}
                      {formatMoney(promoStatus.data.offer.monthlyCents)} (
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
                {loan ? (
                  <LoanNote terms={terms!} payNow={payNow} />
                ) : (
                  <>
                    <TextField
                      label={`${c.equipmentName} (R$)`}
                      inputMode="decimal"
                      hint={`${adminDraft.promo ? 'Preço da promoção de pré-lançamento.' : '0 se o cliente já tem o aparelho.'} ${payNow ? 'Pessoa física: vence com a 1ª mensalidade' : 'Empresa: entra na fatura'} de ${formatDateOnly(chargeDate)}.`}
                      disabled={adminDraft.promo}
                      value={adminDraft.equipment}
                      onChange={(e) => setAdminDraft({ ...adminDraft, equipment: e.target.value })}
                    />
                    {choices.length > 1 && (
                      <InstallmentsField choices={choices} value={installments} onChange={setInstallments} staff payNow={payNow} />
                    )}
                  </>
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
                {!loan &&
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
                {loanOffer && (
                  <EquipmentChoice
                    mode={mode}
                    onChange={(next) => {
                      setMode(next);
                      setInstallments(1);
                      setCommitted(false);
                    }}
                    offer={loanOffer}
                    catalog={c}
                  />
                )}
                {customerPromo && (
                  <div className={styles.promoBanner}>
                    <strong>Promoção de pré-lançamento</strong> — você está na lista de lançamento: rastreador por{' '}
                    {formatMoney(customerPromo.equipmentCents)} e mensalidade de {formatMoney(customerPromo.monthlyCents)}{' '}
                    nos {customerPromo.months} primeiros meses.
                  </div>
                )}
                {loan ? (
                  <LoanNote terms={terms!} payNow={payNow} />
                ) : (
                  <>
                    <div className={styles.orderSummary}>
                      <div className={`${styles.orderLine} ${styles.orderTotal}`}>
                        <span>
                          {c.equipmentName} ({when})
                        </span>
                        <span>
                          {customerPromo && <s className={styles.muted}>{formatMoney(c.equipmentPriceCents)}</s>}{' '}
                          {formatMoney(customerPromo?.equipmentCents ?? c.equipmentPriceCents)}
                        </span>
                      </div>
                    </div>
                    {choices.length > 1 && (
                      <InstallmentsField choices={choices} value={installments} onChange={setInstallments} payNow={payNow} />
                    )}
                    {installService ? (
                      <InstallChoice
                        farbo={farboInstall}
                        onChange={setFarboInstall}
                        priceCents={installService.priceCents}
                        when={when}
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
            )}

            {step === 2 && (
              <>
                {admin && adminDraft ? (
                  <SubscriptionFields
                    draft={adminDraft.plan}
                    onChange={(plan) => setAdminDraft({ ...adminDraft, plan })}
                    lockedDueDay={admin.accountDueDay ?? null}
                  />
                ) : (
                  <p className={pageStyles.description}>
                    {payNow
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
                      <span>1ª mensalidade{parcels ? ', com a 1ª parcela' : ''}</span>
                      <span>{formatMoney(firstMonthly)}</span>
                    </div>
                  )}
                  {farbo && (
                    <div className={styles.orderLine}>
                      <span>Instalação (instaladores Farbo)</span>
                      <span>{formatMoney(farboCents)}</span>
                    </div>
                  )}
                  {loan ? (
                    installFee > 0 && (
                      <div className={styles.orderLine}>
                        <span>Instalação (comodato)</span>
                        <span>{formatMoney(installFee)}</span>
                      </div>
                    )
                  ) : (
                    <div className={styles.orderLine}>
                      <span>
                        {parcels
                          ? `Equipamento: ${formatMoney(equipmentCents)} em ${parcels.length}x sem juros`
                          : 'Equipamento'}
                      </span>
                      <span>
                        {parcels ? 'nas mensalidades' : equipmentCents ? formatMoney(equipmentCents) : 'sem cobrança'}
                      </span>
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
                          A 1ª mensalidade começa hoje; a 2ª vence pelo menos um mês depois, no dia da conta.
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
                        Assinatura: {monthly.name || 'plano'}, todo dia {monthly.dueDay}
                        <br />
                        <span className={styles.muted}>Uma fatura por mês, com todos os veículos da conta.</span>
                        {promo && (
                          <>
                            <br />
                            <span className={styles.muted}>
                              Promoção de pré-lançamento nos {promo.months} primeiros meses; depois,{' '}
                              {monthly.cents !== null ? `${formatMoney(monthly.cents)}/mês` : 'o plano'}.
                            </span>
                          </>
                        )}
                        {parcels && (
                          <>
                            <br />
                            <span className={styles.muted}>{parcelsNote(parcels)}</span>
                          </>
                        )}
                      </span>
                      <span>
                        {promoMonthly !== undefined
                          ? `${formatMoney(promoMonthly)}/mês`
                          : monthly.cents !== null
                            ? `${formatMoney(monthly.cents)}/mês`
                            : '—'}
                      </span>
                    </div>
                  )}
                </div>
                {!isAdmin && payNow && invoiceTotal > 0 && account.data?.onlinePayment && (
                  <PaymentChoice value={payment} onChange={setPayment} card={Boolean(card.data?.enabled)} />
                )}
                {parcels && (
                  <Commitment
                    installments={parcels.length}
                    staff={isAdmin}
                    checked={committed}
                    onChange={setCommitted}
                  />
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
 * As parcelas na assinatura: "+ R$ 15,00 do rastreador em cada uma das 10
 * primeiras mensalidades." (com a 1ª diferente, ela à parte).
 */
function parcelsNote(parcels: number[]): string {
  const [first, rest] = parcels;
  if (first === rest) {
    return `+ ${formatMoney(rest)} do rastreador em cada uma das ${parcels.length} primeiras mensalidades.`;
  }
  return `+ ${formatMoney(first)} do rastreador na 1ª mensalidade e ${formatMoney(rest)} em cada uma das ${parcels.length - 1} seguintes.`;
}

/** A escolha de como pagar o rastreador: à vista ou parcelado sem juros. */
function InstallmentsField({
  choices,
  value,
  onChange,
  staff = false,
  payNow = false,
}: {
  choices: { value: number; label: string }[];
  value: number;
  onChange: (value: number) => void;
  staff?: boolean;
  /** A 1ª mensalidade é paga no pedido (pessoa física). */
  payNow?: boolean;
}) {
  return (
    <SelectField
      label={staff ? 'Pagamento do equipamento' : 'Como pagar o rastreador'}
      hint={
        value > 1
          ? `As ${value} parcelas vêm somadas às mensalidades: a 1ª junto com a 1ª mensalidade, ${payNow ? 'paga no pedido' : 'na próxima fatura'}.`
          : `À vista, ou parcelado em até ${choices.length}x sem juros nas mensalidades.`
      }
      value={value}
      onChange={(e) => onChange(Number(e.target.value))}
    >
      {choices.map((choice) => (
        <option key={choice.value} value={choice.value}>
          {choice.label}
        </option>
      ))}
    </SelectField>
  );
}

/**
 * Parcelado, a assinatura fica ativa até a última parcela: o cliente marca
 * que entendeu antes de confirmar; a central vê o aviso para combinar com ele.
 */
function Commitment({
  installments,
  staff,
  checked,
  onChange,
}: {
  installments: number;
  staff: boolean;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  const rule = `a assinatura deste veículo fica ativa até a mensalidade com a última parcela (a ${installments}ª). Se for encerrada antes, as parcelas que faltam vencem de uma vez, numa fatura só (contrato, cláusula 7).`;
  if (staff) {
    return <p className={styles.muted}>Parcelado: {rule} Combine isso com o cliente.</p>;
  }
  return (
    <label className={styles.promoOption}>
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      <span>
        <strong>Entendi: mantenho a assinatura até a última parcela.</strong>
        <br />
        Parcelando o rastreador, {rule}
      </span>
    </label>
  );
}

/**
 * O rastreador em comodato: sem equipamento a pagar; a instalação com a 1ª
 * mensalidade (no pedido, para a pessoa física; na fatura, para a empresa) e
 * a desinstalação no encerramento.
 */
function LoanNote({ terms, payNow }: { terms: EquipmentTerms; payNow: boolean }) {
  return (
    <div className={styles.installNote}>
      <span>
        <strong>Rastreador em comodato:</strong> emprestado, volta na desinstalação — não há equipamento a pagar.
        {terms.installFeeCents > 0
          ? ` A instalação (${formatMoney(terms.installFeeCents)}) ${payNow ? 'é paga no pedido' : 'entra na fatura'}.`
          : ' Instalação sem custo.'}
        {terms.uninstallFeeCents > 0 ? ` Ao encerrar, a desinstalação: ${formatMoney(terms.uninstallFeeCents)}.` : ''}
      </span>
    </div>
  );
}

/**
 * Comprar ou comodato (quando a central oferece o comodato e não há plano
 * definido para o cliente): o preço do rastreador e a mensalidade de cada um.
 */
function EquipmentChoice({
  mode,
  onChange,
  offer,
  catalog,
}: {
  mode: 'sale' | 'loan';
  onChange: (mode: 'sale' | 'loan') => void;
  offer: LoanOffer;
  catalog: Catalog;
}) {
  // A garantia de troca do rastreador: só a pessoa física (que paga no ato).
  const guarantee = catalog.payNow ? offer.surchargeCents : 0;
  const loanMonthly = catalog.planPriceCents + guarantee;
  const fees = [
    offer.installFeeCents > 0 ? `instalação ${formatMoney(offer.installFeeCents)}` : 'instalação sem custo',
    offer.uninstallFeeCents > 0 ? `desinstalação ${formatMoney(offer.uninstallFeeCents)} ao encerrar` : '',
  ].filter(Boolean);
  return (
    <fieldset className={styles.paymentChoice}>
      <legend>O rastreador</legend>
      <label className={mode === 'sale' ? styles.paymentOn : undefined}>
        <input type="radio" name="equipment-mode" checked={mode === 'sale'} onChange={() => onChange('sale')} />
        <span>
          <strong>Comprar o rastreador</strong>
          <span className={styles.muted}>
            {' '}
            · {formatMoney(catalog.launchPromo?.equipmentCents ?? catalog.equipmentPriceCents)}
            {catalog.equipmentMaxInstallments > 1 ? ` à vista ou em até ${catalog.equipmentMaxInstallments}x sem juros` : ''}; ele é
            seu. Mensalidade de {formatMoney(catalog.launchPromo?.monthlyCents ?? catalog.planPriceCents)}.
          </span>
        </span>
      </label>
      <label className={mode === 'loan' ? styles.paymentOn : undefined}>
        <input type="radio" name="equipment-mode" checked={mode === 'loan'} onChange={() => onChange('loan')} />
        <span>
          <strong>Comodato (rastreador emprestado)</strong>
          <span className={styles.muted}>
            {' '}
            · sem pagar o rastreador; ele volta na desinstalação. Mensalidade de {formatMoney(loanMonthly)}
            {guarantee > 0 ? `, com ${formatMoney(guarantee)} de garantia de troca do rastreador` : ''}; {fees.join(', ')}.
          </span>
        </span>
      </label>
    </fieldset>
  );
}

/**
 * Quem instala o rastreador comprado: o cliente, por conta própria (com o
 * instalador dele, pago direto), ou os instaladores da Farbo, pelo preço das
 * opções do pedido (a central agenda o dia).
 */
function InstallChoice({
  farbo,
  onChange,
  priceCents,
  when,
  onInstallers,
}: {
  farbo: boolean;
  onChange: (farbo: boolean) => void;
  priceCents: number;
  /** "pago no pedido" ou "na fatura de 10/11". */
  when: string;
  onInstallers: () => void;
}) {
  return (
    <fieldset className={styles.paymentChoice}>
      <legend>Instalação</legend>
      <label className={!farbo ? styles.paymentOn : undefined}>
        <input type="radio" name="install-choice" checked={!farbo} onChange={() => onChange(false)} />
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
      <label className={farbo ? styles.paymentOn : undefined}>
        <input type="radio" name="install-choice" checked={farbo} onChange={() => onChange(true)} />
        <span>
          <strong>Com os instaladores da Farbo</strong>
          <span className={styles.muted}>
            {' '}
            · {formatMoney(priceCents)}, {when}; agendamos o dia com você.
          </span>
        </span>
      </label>
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

/**
 * A conta da contratação pelo site: a mensalidade (o plano normal ou o do
 * Insanos MC; no comodato, mais a garantia de troca do rastreador) e o que se
 * paga agora com Pix — a 1ª mensalidade (com a 1ª parcela, se o rastreador
 * for parcelado), o rastreador à vista ou a instalação, e o frete.
 */
import { splitInstallments } from '@/components/billing/installments';
import type { CheckoutOffer } from '@/types';

export interface CheckoutChoice {
  insanos: boolean;
  equipmentMode: 'sale' | 'loan';
  /** O rastreador comprado em quantas vezes (1: à vista). */
  installments: number;
  /** O rastreador comprado instalado pelos instaladores da Farbo. */
  farboInstall: boolean;
}

export interface CheckoutTotals {
  /** A mensalidade do plano (com a garantia de troca, no comodato). */
  monthlyCents: number;
  guaranteeCents: number;
  /** As parcelas do rastreador (vazio: à vista ou comodato). */
  parcels: number[];
  /** A 1ª mensalidade, com a 1ª parcela. */
  firstMonthlyCents: number;
  /** O rastreador à vista (zero parcelado ou no comodato). */
  equipmentNowCents: number;
  /** A instalação: a do comodato ou a dos instaladores da Farbo. */
  installNowCents: number;
  freightCents: number;
  /** O que se paga agora, com Pix. */
  totalNowCents: number;
}

export function checkoutTotals(offer: CheckoutOffer, choice: CheckoutChoice, freightCents = 0): CheckoutTotals {
  const loan = choice.equipmentMode === 'loan' && offer.loan !== null;
  const guaranteeCents = loan ? (offer.loan?.surchargeCents ?? 0) : 0;
  const monthlyCents = (choice.insanos ? offer.insanosCents : offer.planCents) + guaranteeCents;
  const parcels =
    !loan && choice.installments > 1 && choice.installments <= offer.maxInstallments
      ? splitInstallments(offer.equipmentCents, choice.installments)
      : [];
  const firstMonthlyCents = monthlyCents + (parcels[0] ?? 0);
  const equipmentNowCents = loan || parcels.length > 0 ? 0 : offer.equipmentCents;
  const installNowCents = loan
    ? (offer.loan?.installFeeCents ?? 0)
    : choice.farboInstall && offer.install
      ? offer.install.priceCents
      : 0;
  return {
    monthlyCents,
    guaranteeCents,
    parcels,
    firstMonthlyCents,
    equipmentNowCents,
    installNowCents,
    freightCents,
    totalNowCents: firstMonthlyCents + equipmentNowCents + installNowCents + freightCents,
  };
}

/**
 * O Pix do pedido, com a marca de cliente novo (a página de pagamento chama
 * para o app depois do pago): o caminho, quando o link é o da página de
 * pagamento (/pagar/..., que este site abre sem recarregar), ou o link inteiro.
 */
export function payDestination(payUrl: string, origin = window.location.origin): { path: string } | { url: string } {
  const url = new URL(payUrl, origin);
  url.searchParams.set('novo', '1');
  return url.pathname.startsWith('/pagar/') ? { path: url.pathname + url.search } : { url: url.toString() };
}

/** O nome do veículo, se a pessoa não deu um: "Fiat Uno" (ou "Meu carro"/"Minha moto"). */
export function vehicleName(kind: 'CAR' | 'MOTORCYCLE', brand: string, model: string): string {
  const name = [brand.trim(), model.trim()].filter(Boolean).join(' ');
  return name || (kind === 'MOTORCYCLE' ? 'Minha moto' : 'Meu carro');
}

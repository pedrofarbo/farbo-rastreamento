/**
 * A mensalidade por tipo de veículo: a tabela do link de um afiliado (moto,
 * carro e caminhão, cada um com o seu preço). Os valores vêm do servidor.
 */
import { formatMoney } from '@/services/format';
import type { CheckoutOffer, KindPrices, VehicleKind } from '@/types';

/** Os tipos na ordem das páginas: moto, carro e caminhão. */
export const PRICED_KINDS: VehicleKind[] = ['MOTORCYCLE', 'CAR', 'TRUCK'];

export const KIND_NAMES: Record<VehicleKind, string> = { MOTORCYCLE: 'Moto', CAR: 'Carro', TRUCK: 'Caminhão' };

/** A mensalidade do tipo (sem tipo, a do carro, como no cadastro). */
export function kindPrice(prices: KindPrices, kind: VehicleKind | undefined): number {
  if (kind === 'MOTORCYCLE') return prices.motorcycleCents;
  if (kind === 'TRUCK') return prices.truckCents;
  return prices.carCents;
}

/** A menor das três: o "a partir de". */
export function lowestPrice(prices: KindPrices): number {
  return Math.min(prices.motorcycleCents, prices.carCents, prices.truckCents);
}

/**
 * "Moto R$ 39,90 · Carro R$ 49,90 · Caminhão R$ 59,90". Cada tipo com o
 * preço fica inteiro na linha (espaço sem quebra): a quebra é entre eles.
 */
export function kindPricesLabel(prices: KindPrices, money: (cents: number) => string = formatMoney): string {
  return PRICED_KINDS.map((kind) => `${KIND_NAMES[kind]} ${money(kindPrice(prices, kind))}`.replace(/\s/g, '\u00a0')).join(' · ');
}

/**
 * A mensalidade em destaque nas páginas públicas: com a contratação aberta, a
 * do site — pelo link de um afiliado com tabela, a menor dela (o "a partir
 * de"), com a tabela —; fechada, a normal.
 */
export function headlineMonthly(offer: CheckoutOffer, open: boolean): { cents: number; table: KindPrices | null } {
  const table = open ? offer.planByKind : null;
  return { cents: table ? lowestPrice(table) : open ? offer.planCents : offer.planRegularCents, table };
}

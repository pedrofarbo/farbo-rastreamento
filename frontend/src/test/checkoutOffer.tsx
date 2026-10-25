import type { ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import type { CheckoutOffer } from '@/types';

/** A oferta da contratação pelo site nos testes (o servidor manda os valores). */
export const OFFER: CheckoutOffer = {
  enabled: true,
  planName: 'Plano Mensal',
  planCents: 3490,
  insanosPlanName: 'Especial Insanos MC',
  insanosCents: 2790,
  equipmentName: 'Rastreador J16',
  equipmentCents: 12000,
  planRegularCents: 6990,
  insanosRegularCents: 3990,
  equipmentRegularCents: 15000,
  maxInstallments: 10,
  dueDay: 10,
  loan: { enabled: true, surchargeCents: 300, installFeeCents: 15000, uninstallFeeCents: 8500 },
  install: { enabled: true, priceCents: 15000 },
  contractVersion: '7',
  shippingEnabled: true,
};

export const offerWith = (over: Partial<CheckoutOffer> = {}): CheckoutOffer => ({ ...OFFER, ...over });

/** O que usa a oferta (useCheckoutOffer) precisa do react-query. */
export function withQuery(node: ReactNode) {
  return (
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{node}</QueryClientProvider>
  );
}

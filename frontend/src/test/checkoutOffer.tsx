import type { ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import type { CheckoutOffer } from '@/types';

/** A oferta da contratação pelo site nos testes (o servidor manda os valores). */
export const OFFER: CheckoutOffer = {
  enabled: true,
  planName: 'Plano Mensal',
  planCents: 3490,
  equipmentName: 'Rastreador J16',
  planRegularCents: 6990,
  planByKind: null,
  tracker: { cents: 1200, months: 10, added: false },
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

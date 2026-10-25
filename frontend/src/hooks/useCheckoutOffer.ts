import { useQuery } from '@tanstack/react-query';

import { checkoutApi } from '@/api/resources';

/**
 * A oferta da contratação pelo site: os preços que as páginas públicas
 * mostram (a landing, os eventos, a indicação, o flyer) e o funil cobra. Uma
 * consulta só, guardada uns minutos. Enquanto carrega, a contratação conta
 * como aberta (o normal); fechada na central, os botões abrem o pré-cadastro.
 */
export function useCheckoutOffer() {
  const query = useQuery({ queryKey: ['checkout-offer'], queryFn: checkoutApi.offer, staleTime: 5 * 60_000 });
  return { offer: query.data, open: query.data?.enabled ?? true, query };
}

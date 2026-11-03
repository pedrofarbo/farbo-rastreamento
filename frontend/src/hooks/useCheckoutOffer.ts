import { useQuery } from '@tanstack/react-query';

import { checkoutApi } from '@/api/resources';
import { currentReferral } from '@/services/referral';

/**
 * A oferta da contratação pelo site: os preços que as páginas públicas
 * mostram (a landing, os eventos, a indicação, o flyer) e o funil cobra. Uma
 * consulta só, guardada uns minutos. Enquanto carrega, a contratação conta
 * como aberta (o normal); fechada na central, os botões abrem o pré-cadastro.
 * Pelo link de um afiliado (o código guardado no aparelho, ou o informado;
 * vazio: nenhum), a oferta traz a tabela dele por tipo de veículo.
 */
export function useCheckoutOffer(ref: string = currentReferral()) {
  const query = useQuery({ queryKey: ['checkout-offer', ref], queryFn: () => checkoutApi.offer(ref), staleTime: 5 * 60_000 });
  return { offer: query.data, open: query.data?.enabled ?? true, query };
}

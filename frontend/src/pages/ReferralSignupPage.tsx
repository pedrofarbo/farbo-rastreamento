import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';

import { publicApi } from '@/api/resources';
import { referralCode, saveReferral } from '@/services/referral';
import type { PublicAffiliate } from '@/types';

import { CheckoutInvite } from './CheckoutInvite';

/**
 * O link de indicação do afiliado (/indicacao/<código>): a chamada para
 * contratar pelo site com "Indicado por @fulano". O código fica guardado no
 * aparelho por 60 dias: a contratação (agora ou depois, pela landing) e o
 * contato saem com ele. Um link desconhecido (ou de afiliado inativo) abre a
 * mesma tela, sem o selo: o servidor não liga a ninguém.
 */
export function ReferralSignupPage() {
  const { codigo = '' } = useParams();
  const code = referralCode(codigo);
  const [referrer, setReferrer] = useState<PublicAffiliate | null>(null);

  useEffect(() => {
    if (!code) return;
    saveReferral(code);
    let alive = true;
    publicApi
      .affiliate(code)
      .then((found) => alive && setReferrer(found))
      .catch(() => alive && setReferrer(null));
    return () => {
      alive = false;
    };
  }, [code]);

  return <CheckoutInvite source="indicacao" campaign={code} referral={code} referrer={referrer} highlight="plan" />;
}

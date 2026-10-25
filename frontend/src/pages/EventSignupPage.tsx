import { useParams, useSearchParams } from 'react-router-dom';

import { eventSlug } from '@/config/landing';

import { CheckoutInvite } from './CheckoutInvite';

/**
 * A tela que o QR Code do estande abre no celular (/evento/<nome-do-evento>):
 * o preço do Insanos MC em destaque — ou, no QR Code do público geral
 * (?publico=geral), o do plano normal — e o botão para contratar pelo site,
 * com o evento. Quem prefere conversar deixa o contato, que vai para a lista
 * da central com o nome do evento — e sem o link de indicação guardado no
 * aparelho, que pode ser o da equipe; depois, o botão para cadastrar a
 * próxima pessoa (quando alguém da equipe cadastra no tablet).
 */
export function EventSignupPage() {
  const { evento = '' } = useParams();
  const [params] = useSearchParams();
  const event = eventSlug(evento);
  // O QR Code do público geral destaca o preço do plano normal, sem o Insanos.
  const general = params.get('publico') === 'geral';
  return (
    <CheckoutInvite
      source="evento"
      campaign={event}
      event={event}
      highlight={general ? 'plan' : 'insanos'}
      mentionInsanos={!general}
      allowNext
    />
  );
}

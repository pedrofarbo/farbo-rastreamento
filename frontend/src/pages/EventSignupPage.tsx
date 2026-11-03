import { useParams } from 'react-router-dom';

import { eventSlug } from '@/config/landing';

import { CheckoutInvite } from './CheckoutInvite';

/**
 * A tela que o QR Code do estande abre no celular (/evento/<nome-do-evento>):
 * o preço do plano em destaque (o do Insanos MC é só com a central; os QR
 * Codes já impressos, com ou sem ?publico=geral, abrem esta mesma tela) e o
 * botão para contratar pelo site, com o evento. Quem prefere conversar deixa o contato, que vai para a lista
 * da central com o nome do evento — e sem o link de indicação guardado no
 * aparelho, que pode ser o da equipe; depois, o botão para cadastrar a
 * próxima pessoa (quando alguém da equipe cadastra no tablet).
 */
export function EventSignupPage() {
  const { evento = '' } = useParams();
  const event = eventSlug(evento);
  return <CheckoutInvite source="evento" campaign={event} event={event} allowNext />;
}

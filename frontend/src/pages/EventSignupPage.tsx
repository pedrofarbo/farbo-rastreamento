import { useParams } from 'react-router-dom';

import { eventSlug } from '@/config/landing';

import { PreLaunchSignup } from './PreLaunchSignup';

/**
 * Cadastro no pré-lançamento em eventos: a tela que o QR Code do estande abre
 * no celular (/evento/<nome-do-evento>), com o preço do Insanos MC em
 * destaque e, depois do cadastro, o botão para cadastrar a próxima pessoa
 * (quando alguém da equipe cadastra no tablet). A inscrição vai para a lista
 * de lançamento com o nome do evento — e sem o link de indicação guardado no
 * aparelho, que pode ser o da equipe.
 */
export function EventSignupPage() {
  const { evento = '' } = useParams();
  const event = eventSlug(evento);
  return <PreLaunchSignup source="evento" campaign={event} event={event} highlight="insanos" allowNext />;
}

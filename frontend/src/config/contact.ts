/**
 * Contato da empresa na landing.
 *
 * Com WHATSAPP_NUMBER preenchido (só dígitos, com o 55), a landing mostra o
 * WhatsApp: o número no rodapé, o botão flutuante e o "chame no WhatsApp" do
 * pré-cadastro. Vazio, fica só o e-mail.
 */
export const CONTACT_EMAIL = 'contato@farborastreadores.com.br';

/** O Instagram da empresa: o ícone no rodapé da landing leva ao perfil. */
export const INSTAGRAM_HANDLE = 'farborastreadores';
export const INSTAGRAM_URL = `https://www.instagram.com/${INSTAGRAM_HANDLE}/`;

/** Só dígitos, com o 55 (ex.: 5511999999999). Vazio: sem WhatsApp na landing. */
export const WHATSAPP_NUMBER = '551151946470';

/**
 * Como se contrata: false, "Quero meu rastreador" abre o pré-cadastro (salva o
 * pré-cliente e inscreve na lista de lançamento), com o WhatsApp como canal ao
 * lado; true volta ao modal antigo, que leva direto para a conversa no
 * WhatsApp (precisa do número).
 */
export const HIRE_VIA_WHATSAPP = false;

/** A primeira mensagem de quem chama pelo site. */
export const WHATSAPP_GREETING = 'Olá! Vim pelo site da Farbo Rastreadores e quero saber mais sobre o rastreador.';

/** O link que abre a conversa (com a mensagem já escrita, se houver). */
export function whatsappUrl(text = '', number: string = WHATSAPP_NUMBER): string {
  return `https://wa.me/${number}${text ? `?text=${encodeURIComponent(text)}` : ''}`;
}

/** O número como aparece no rodapé, ex.: (11) 99999-9999. */
export function whatsappDisplay(number: string = WHATSAPP_NUMBER): string {
  const national = number.replace(/\D/g, '').replace(/^55/, '');
  if (national.length < 10) return number;
  const ddd = national.slice(0, 2);
  const local = national.slice(2);
  return `(${ddd}) ${local.slice(0, -4)}-${local.slice(-4)}`;
}

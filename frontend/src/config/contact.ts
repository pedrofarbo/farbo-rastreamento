/**
 * Contato da empresa na landing.
 *
 * Enquanto o número oficial não existe, WHATSAPP_NUMBER fica vazio: a landing
 * mostra só o e-mail e "Quero meu rastreador" abre o cadastro de interesse
 * (pré-cliente). Com o número preenchido (só dígitos, com o 55), volta o
 * atendimento pelo WhatsApp: o número no rodapé e o modal que abre a conversa.
 */
export const CONTACT_EMAIL = 'contato@farborastreadores.com.br';

/** O Instagram da empresa: o ícone no rodapé da landing leva ao perfil. */
export const INSTAGRAM_HANDLE = 'farborastreadores';
export const INSTAGRAM_URL = `https://www.instagram.com/${INSTAGRAM_HANDLE}/`;

/** Só dígitos, com o 55 (ex.: 5511999999999). Vazio: sem WhatsApp na landing. */
export const WHATSAPP_NUMBER = '';

/** O número como aparece no rodapé, ex.: (11) 99999-9999. */
export function whatsappDisplay(number: string = WHATSAPP_NUMBER): string {
  const national = number.replace(/\D/g, '').replace(/^55/, '');
  if (national.length < 10) return number;
  const ddd = national.slice(0, 2);
  const local = national.slice(2);
  return `(${ddd}) ${local.slice(0, -4)}-${local.slice(-4)}`;
}

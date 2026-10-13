/**
 * O formulário de cartão da AbacatePay (abacatepay.js). Os campos do cartão
 * ficam num iframe dela (número e CVV nunca passam pelo nosso código); a
 * autenticação do banco (3DS) abre sozinha quando o banco pede. Daqui sai só
 * o token de uso único (ctok_..., 60 s), que o backend cobra.
 *
 * O script vem do domínio dela e não pode ser copiado para cá (ela o
 * atualiza), por isso a CSP do site libera abacatepay.com e os parceiros que
 * ele carrega (Rinne e Evervault).
 */

const SCRIPT_URL = 'https://abacatepay.com/abacatepay.js';

export interface CardPayerData {
  name: string;
  email: string;
  taxId: string;
  cellphone?: string;
}

export interface CardToken {
  token: string;
  expiresIn: number;
  brand: string;
  lastFour: string;
}

export interface AbacateCheckout {
  mount: (target: string | HTMLElement) => Promise<unknown>;
  tokenize: (payer: CardPayerData) => Promise<CardToken>;
  destroy: () => void;
}

export interface AbacateFactory {
  create: (options: { publicKey: string; amount: number; locale?: 'pt' | 'en' | 'es'; icons?: boolean }) => AbacateCheckout;
}

declare global {
  interface Window {
    AbacatePay?: AbacateFactory;
  }
}

let loading: Promise<AbacateFactory> | null = null;

/** Carrega o abacatepay.js uma vez (só quando alguém vai pagar com cartão). */
export function loadAbacatePay(): Promise<AbacateFactory> {
  if (window.AbacatePay) return Promise.resolve(window.AbacatePay);
  if (!loading) {
    loading = new Promise<AbacateFactory>((resolve, reject) => {
      const script = document.createElement('script');
      script.src = SCRIPT_URL;
      script.async = true;
      script.onload = () => {
        if (window.AbacatePay) resolve(window.AbacatePay);
        else reject(new Error('O formulário do cartão não carregou.'));
      };
      script.onerror = () => {
        loading = null;
        script.remove();
        reject(new Error('Não deu para carregar o formulário do cartão. Confira a conexão e tente de novo.'));
      };
      document.head.appendChild(script);
    });
  }
  return loading;
}

/** A mensagem de um erro do formulário do cartão, em português. */
export function cardErrorMessage(err: unknown): string {
  switch ((err as { code?: string } | null)?.code) {
    case 'CARD_INCOMPLETE':
      return 'Preencha todos os dados do cartão.';
    case 'CARD_INVALID':
      return 'Confira os dados do cartão.';
    case 'THREE_DS_FAILED':
      return 'O banco não confirmou a compra (autenticação do cartão). Tente de novo ou use outro cartão.';
    case 'PAYER_INVALID':
      return 'Faltam dados do pagador no cadastro (nome, e-mail ou CPF/CNPJ).';
    case 'NETWORK':
    case 'PROVIDER_LOAD':
      return 'Sem conexão com o formulário do cartão. Tente de novo.';
  }
  return err instanceof Error && err.message ? err.message : 'Não deu para usar o cartão agora.';
}

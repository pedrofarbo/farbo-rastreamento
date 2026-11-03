/**
 * O cartão pela Pagar.me: o navegador manda os dados do cartão direto para a
 * API dela, com a chave pública, e recebe um token de uso único (token_...,
 * vale poucos minutos), que o backend cobra. O número e o CVV não passam
 * pelo servidor da Farbo. A CSP do site libera api.pagar.me em connect-src.
 */
import { luhnValid } from './imei';

const TOKENS_URL = 'https://api.pagar.me/core/v5/tokens';

export interface PagarmeCardInput {
  number: string;
  holderName: string;
  /** "MM/AA" ou "MM/AAAA". */
  expiry: string;
  cvv: string;
}

export interface PagarmeCardToken {
  token: string;
  brand: string;
  lastFour: string;
}

/** A validade "MM/AA" → mês e ano com 4 dígitos (null se inválida ou vencida). */
export function parseExpiry(expiry: string, now = new Date()): { month: number; year: number } | null {
  const m = expiry.replace(/\s/g, '').match(/^(\d{1,2})\/?(\d{2}|\d{4})$/);
  if (!m) return null;
  const month = Number(m[1]);
  const year = m[2].length === 2 ? 2000 + Number(m[2]) : Number(m[2]);
  if (month < 1 || month > 12) return null;
  // Vale até o último dia do mês da validade.
  if (year < now.getFullYear() || (year === now.getFullYear() && month < now.getMonth() + 1)) return null;
  return { month, year };
}

/** O que falta ou está errado no cartão digitado ("" se está tudo certo). */
export function cardProblem(card: PagarmeCardInput): string {
  const number = card.number.replace(/\D/g, '');
  if (number.length < 13 || number.length > 19 || !luhnValid(number)) return 'Confira o número do cartão.';
  if (card.holderName.trim().length < 3 || /\d/.test(card.holderName)) return 'Escreva o nome como está no cartão.';
  if (!parseExpiry(card.expiry)) return 'Confira a validade (MM/AA).';
  if (!/^\d{3,4}$/.test(card.cvv)) return 'Confira o código de segurança (CVV).';
  return '';
}

/** "4000000000000010" → "4000 0000 0000 0010" (o campo do número). */
export function formatCardNumber(text: string): string {
  return text
    .replace(/\D/g, '')
    .slice(0, 19)
    .replace(/(\d{4})(?=\d)/g, '$1 ');
}

/** "1228" → "12/28" (o campo da validade). */
export function formatExpiry(text: string): string {
  const digits = text.replace(/\D/g, '').slice(0, 4);
  return digits.length > 2 ? `${digits.slice(0, 2)}/${digits.slice(2)}` : digits;
}

/** Gera o token do cartão na Pagar.me (o cartão já conferido por cardProblem). */
export async function tokenizePagarmeCard(publicKey: string, card: PagarmeCardInput): Promise<PagarmeCardToken> {
  const expiry = parseExpiry(card.expiry);
  if (!expiry) throw new Error('Confira a validade (MM/AA).');
  let response: Response;
  try {
    response = await fetch(`${TOKENS_URL}?appId=${encodeURIComponent(publicKey)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        type: 'card',
        card: {
          number: card.number.replace(/\D/g, ''),
          holder_name: card.holderName.trim().replace(/\s+/g, ' ').toUpperCase(),
          exp_month: expiry.month,
          exp_year: expiry.year,
          cvv: card.cvv,
        },
      }),
    });
  } catch {
    throw new Error('Não deu para falar com a Pagar.me. Confira a conexão e tente de novo.');
  }
  const body = (await response.json().catch(() => null)) as {
    id?: string;
    card?: { brand?: string; last_four_digits?: string };
    message?: string;
  } | null;
  if (!response.ok || !body?.id) {
    throw new Error(
      response.status === 422 || response.status === 400
        ? 'A Pagar.me recusou os dados do cartão. Confira o número, a validade e o CVV.'
        : 'Não deu para validar o cartão agora. Tente de novo em instantes.',
    );
  }
  return { token: body.id, brand: body.card?.brand ?? '', lastFour: body.card?.last_four_digits ?? '' };
}

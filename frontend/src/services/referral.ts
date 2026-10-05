import { SITE_URL } from '@/config/landing';

/**
 * O link de indicação do afiliado: quem abre /indicacao/<código> (ou a
 * landing com ?ref=<código>) fica com o código guardado por 60 dias, e os
 * cadastros da landing nesse tempo vão com ele. O servidor decide se o
 * código vale (afiliado ativo); aqui é só o recado.
 */
const KEY = 'farbo.ref';
const TTL_MS = 60 * 24 * 60 * 60 * 1000;

/** O código como vai no link (o servidor faz igual): minúsculo, letras, números e hífen. */
export function referralCode(text: string): string {
  return text
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+/, '')
    .slice(0, 40)
    .replace(/-+$/, '');
}

/** O link de cadastro do afiliado. */
export function referralUrl(code: string): string {
  return `${SITE_URL}/indicacao/${code}`;
}

/** O link da página do afiliado (os números dele). */
export function partnerUrl(token: string): string {
  return `${SITE_URL}/parceiro/${token}`;
}

/** Guarda o código do link (o mais recente vale). */
export function saveReferral(code: string, now = Date.now()): void {
  const clean = referralCode(code);
  if (clean.length < 2) return;
  try {
    localStorage.setItem(KEY, JSON.stringify({ code: clean, at: now }));
  } catch {
    // Navegação anônima ou armazenamento bloqueado: o cadastro na própria
    // tela do link já vai com o código.
  }
}

/** O código guardado, se ainda vale; vazio se não há. */
export function currentReferral(now = Date.now()): string {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return '';
    const saved = JSON.parse(raw) as { code?: unknown; at?: unknown };
    if (typeof saved.code !== 'string' || typeof saved.at !== 'number' || now - saved.at > TTL_MS) {
      localStorage.removeItem(KEY);
      return '';
    }
    return saved.code;
  } catch {
    return '';
  }
}

const MONTHS = ['jan', 'fev', 'mar', 'abr', 'mai', 'jun', 'jul', 'ago', 'set', 'out', 'nov', 'dez'];

/** "2026-10" → "out/2026". */
export function monthLabel(month: string): string {
  const [year, m] = month.split('-').map(Number);
  return MONTHS[m - 1] ? `${MONTHS[m - 1]}/${year}` : month;
}

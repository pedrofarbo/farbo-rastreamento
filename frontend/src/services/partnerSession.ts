/**
 * A sessão da página do afiliado com senha: a entrada vale 30 dias e fica
 * guardada no aparelho, por link. Navegação anônima ou armazenamento
 * bloqueado: vale enquanto a página estiver aberta.
 */
import { ApiError } from '@/api/client';

const key = (token: string) => `farbo.partner.${token}`;
const memory = new Map<string, { session: string; expiresAt: string }>();

/** A sessão guardada para o link, se ainda vale; vazio se não há. */
export function storedPartnerSession(token: string, now = Date.now()): string {
  let saved: { session?: unknown; expiresAt?: unknown } | undefined = memory.get(token);
  try {
    const raw = localStorage.getItem(key(token));
    if (raw) saved = JSON.parse(raw) as typeof saved;
  } catch {
    // Sem o armazenamento: a da memória.
  }
  if (saved && typeof saved.session === 'string' && typeof saved.expiresAt === 'string' && Date.parse(saved.expiresAt) > now) {
    return saved.session;
  }
  if (saved) clearPartnerSession(token);
  return '';
}

export function savePartnerSession(token: string, session: string, expiresAt: string) {
  memory.set(token, { session, expiresAt });
  try {
    localStorage.setItem(key(token), JSON.stringify({ session, expiresAt }));
  } catch {
    // Fica só na memória.
  }
}

export function clearPartnerSession(token: string) {
  memory.delete(token);
  try {
    localStorage.removeItem(key(token));
  } catch {
    // Nada guardado.
  }
}

/** A página pede a senha: sem sessão, ou com uma que caiu (a senha ou o link mudaram). */
export function needsPartnerPassword(err: unknown): boolean {
  return err instanceof ApiError && err.status === 401 && (err.body as { code?: unknown } | undefined)?.code === 'PARTNER_PASSWORD';
}

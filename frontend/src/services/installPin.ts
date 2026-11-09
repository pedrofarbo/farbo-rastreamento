/**
 * A senha do link do instalador (a central passa junto com o link): fica
 * guardada no aparelho, por link, e vai em toda chamada a ele. Navegação
 * anônima ou armazenamento bloqueado: vale enquanto a página estiver aberta.
 */
import { ApiError } from '@/api/client';

const key = (token: string) => `farbo.installPin.${token}`;
const memory = new Map<string, string>();

/** A senha guardada para o link (vazia: nenhuma). */
export function storedInstallPin(token: string): string {
  try {
    const saved = localStorage.getItem(key(token));
    if (saved) return saved;
  } catch {
    // Sem o armazenamento: a da memória.
  }
  return memory.get(token) ?? '';
}

export function saveInstallPin(token: string, pin: string) {
  memory.set(token, pin);
  try {
    localStorage.setItem(key(token), pin);
  } catch {
    // Fica só na memória.
  }
}

export function clearInstallPin(token: string) {
  memory.delete(token);
  try {
    localStorage.removeItem(key(token));
  } catch {
    // Nada guardado.
  }
}

/** O que a API disse da senha: pede (required), errada (wrong) ou o link travou (locked). */
export type PinProblem = 'required' | 'wrong' | 'locked';

/** O problema de senha da resposta, se for um. */
export function pinProblem(err: unknown): PinProblem | null {
  if (!(err instanceof ApiError)) return null;
  const code = (err.body as { code?: string } | undefined)?.code;
  if (code === 'PIN_REQUIRED') return 'required';
  if (code === 'PIN_WRONG') return 'wrong';
  if (code === 'PIN_LOCKED') return 'locked';
  return null;
}

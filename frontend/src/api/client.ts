import type { AuthTokens } from '@/types';

const API_URL = import.meta.env.VITE_API_URL ?? '';

const ACCESS_KEY = 'tracker.accessToken';
const REFRESH_KEY = 'tracker.refreshToken';

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
    public readonly body?: unknown,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

/**
 * Guarda os tokens.
 *
 * O access token é curto e fica também em memória para evitar uma leitura de
 * localStorage por requisição; o refresh token é o que sobrevive ao reload.
 */
class TokenStore {
  private access: string | null = null;

  constructor() {
    this.access = safeRead(ACCESS_KEY);
  }

  get accessToken(): string | null {
    return this.access;
  }

  get refreshToken(): string | null {
    return safeRead(REFRESH_KEY);
  }

  save(tokens: AuthTokens): void {
    this.access = tokens.accessToken;
    safeWrite(ACCESS_KEY, tokens.accessToken);
    safeWrite(REFRESH_KEY, tokens.refreshToken);
  }

  clear(): void {
    this.access = null;
    safeRemove(ACCESS_KEY);
    safeRemove(REFRESH_KEY);
  }
}

function safeRead(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function safeWrite(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    /* modo privado ou storage bloqueado: a sessão vale só para esta aba */
  }
}

function safeRemove(key: string): void {
  try {
    window.localStorage.removeItem(key);
  } catch {
    /* nada a fazer */
  }
}

export const tokens = new TokenStore();

type Listener = () => void;
const unauthorizedListeners = new Set<Listener>();

/** Avisa a aplicação quando a sessão morreu de vez. */
export function onUnauthorized(listener: Listener): () => void {
  unauthorizedListeners.add(listener);
  return () => unauthorizedListeners.delete(listener);
}

/** A API pediu o aceite do contrato (o portão do contrato escuta). */
export const CONTRACT_REQUIRED_EVENT = 'farbo:contract-required';

function notifyUnauthorized(): void {
  unauthorizedListeners.forEach((listener) => listener());
}

// Um refresh de cada vez: várias requisições que falham juntas devem esperar
// a mesma renovação, não disparar uma cada.
let refreshInFlight: Promise<boolean> | null = null;

async function refreshSession(): Promise<boolean> {
  const refreshToken = tokens.refreshToken;
  if (!refreshToken) return false;

  refreshInFlight ??= (async () => {
    try {
      const response = await fetch(`${API_URL}/api/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refreshToken }),
      });
      if (!response.ok) {
        tokens.clear();
        return false;
      }
      tokens.save((await response.json()) as AuthTokens);
      return true;
    } catch {
      return false;
    } finally {
      refreshInFlight = null;
    }
  })();

  return refreshInFlight;
}

interface RequestOptions extends Omit<RequestInit, 'body'> {
  body?: unknown;
  /** Uso interno: evita laço infinito de renovação. */
  retrying?: boolean;
  /** Rotas públicas (login/refresh) não mandam Authorization. */
  anonymous?: boolean;
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { body, retrying, anonymous, headers, ...rest } = options;

  const finalHeaders = new Headers(headers);
  // Arquivo (FormData): o navegador monta o Content-Type com a fronteira.
  const isForm = typeof FormData !== 'undefined' && body instanceof FormData;
  if (body !== undefined && !isForm) {
    finalHeaders.set('Content-Type', 'application/json');
  }
  if (!anonymous && tokens.accessToken) {
    finalHeaders.set('Authorization', `Bearer ${tokens.accessToken}`);
  }

  const response = await fetch(`${API_URL}${path}`, {
    ...rest,
    headers: finalHeaders,
    body: body === undefined ? undefined : isForm ? body : JSON.stringify(body),
  });

  if (response.status === 401 && !anonymous && !retrying) {
    if (await refreshSession()) {
      return request<T>(path, { ...options, retrying: true });
    }
    tokens.clear();
    notifyUnauthorized();
    throw new ApiError(401, 'sessão expirada');
  }

  if (response.status === 204) {
    return undefined as T;
  }

  const payload = await parseBody(response);

  if (!response.ok) {
    const message =
      typeof payload === 'object' && payload !== null && 'error' in payload
        ? String((payload as { error: unknown }).error)
        : `falha na requisição (${response.status})`;
    // O cliente ainda não aceitou o contrato: a tela do contrato aparece.
    if ((payload as { code?: string } | undefined)?.code === 'CONTRACT_REQUIRED' && typeof window !== 'undefined') {
      window.dispatchEvent(new Event(CONTRACT_REQUIRED_EVENT));
    }
    throw new ApiError(response.status, message, payload);
  }

  return payload as T;
}

async function parseBody(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) return undefined;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

/** Busca um arquivo protegido (o link comum não leva o token). */
async function fetchFile(path: string, retrying = false): Promise<{ blob: Blob; filename: string }> {
  const headers = new Headers();
  if (tokens.accessToken) headers.set('Authorization', `Bearer ${tokens.accessToken}`);
  const response = await fetch(`${API_URL}${path}`, { headers });
  if (response.status === 401 && !retrying) {
    if (await refreshSession()) return fetchFile(path, true);
    tokens.clear();
    notifyUnauthorized();
    throw new ApiError(401, 'sessão expirada');
  }
  if (!response.ok) {
    const payload = await parseBody(response);
    const message =
      typeof payload === 'object' && payload !== null && 'error' in payload
        ? String((payload as { error: unknown }).error)
        : `falha ao baixar (${response.status})`;
    throw new ApiError(response.status, message, payload);
  }
  const disposition = response.headers.get('Content-Disposition') ?? '';
  const match = /filename\*=utf-8''([^;]+)/i.exec(disposition) ?? /filename="?([^";]+)"?/i.exec(disposition);
  return { blob: await response.blob(), filename: match ? decodeURIComponent(match[1]) : 'arquivo' };
}

/** O arquivo protegido como Blob (para mostrar uma imagem, por exemplo). */
export async function fetchBlob(path: string): Promise<Blob> {
  return (await fetchFile(path)).blob;
}

/** Baixa um arquivo protegido com o nome que o servidor mandou. */
export async function download(path: string): Promise<void> {
  const { blob, filename } = await fetchFile(path);
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) => request<T>(path, { method: 'POST', body }),
  patch: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PATCH', body }),
  put: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PUT', body }),
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
};

/** Monta a URL do WebSocket com o token na query (o navegador não permite header). */
export function websocketUrl(): string {
  const configured = import.meta.env.VITE_WS_URL;
  const base =
    configured ??
    (API_URL
      ? API_URL.replace(/^http/, 'ws')
      : `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${window.location.host}`);

  const token = tokens.accessToken ?? '';
  return `${base}/ws?token=${encodeURIComponent(token)}`;
}

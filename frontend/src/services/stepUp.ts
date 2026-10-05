import { api, ApiError, request } from '@/api/client';
import type { AuthTokens } from '@/types';

/**
 * Confirmação extra antes de ações sensíveis (desligar o motor): a biometria
 * do aparelho (WebAuthn: Face ID, digital) ou, se ela falhar, a senha. O
 * servidor devolve um comprovante de uso único, que vai junto com a ação.
 */

/** As ações que pedem a confirmação: bloquear, desbloquear e dar acesso a um veículo. */
export type StepUpPurpose = 'engine_cut' | 'engine_resume' | 'vehicle_share' | 'supplier_pix';

export interface StepUpGrant {
  token: string;
  method: 'biometric' | 'password';
  expiresAt: string;
}

export interface BiometricDevice {
  id: number;
  credentialId: string;
  name: string;
  createdAt: string;
  lastUsedAt: string | null;
}

interface CreationOptionsJSON {
  challengeId: string;
  challenge: string;
  rp: { id: string; name: string };
  user: { id: string; name: string; displayName: string };
  pubKeyCredParams: { type: 'public-key'; alg: number }[];
  timeout: number;
  excludeCredentials: string[];
}

interface RequestOptionsJSON {
  challengeId: string;
  challenge: string;
  rpId: string;
  timeout: number;
  allowCredentials: string[];
}

/** Por que a biometria não deu: não cadastrada aqui, ou recusada/cancelada. */
export class BiometricError extends Error {
  constructor(public readonly reason: 'not-enrolled' | 'failed' | 'unsupported') {
    super(reason);
    this.name = 'BiometricError';
  }
}

// --- base64url ------------------------------------------------------------------

export function toBytes(value: string): Uint8Array {
  const base64 = value.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((value.length + 3) % 4);
  const binary = atob(base64);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
}

export function toBase64url(value: ArrayBuffer | Uint8Array): string {
  const bytes = value instanceof Uint8Array ? value : new Uint8Array(value);
  let binary = '';
  bytes.forEach((b) => (binary += String.fromCharCode(b)));
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

// --- O aparelho -----------------------------------------------------------------

/** O aparelho tem biometria (ou o código do aparelho) que o site pode usar. */
export async function biometricAvailable(): Promise<boolean> {
  try {
    return (
      window.isSecureContext &&
      typeof window.PublicKeyCredential !== 'undefined' &&
      (await PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable())
    );
  } catch {
    return false;
  }
}

const isApple = () =>
  /iPhone|iPad|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);

/** Como chamar a biometria na tela: "Face ID" no iPhone, "biometria" nos outros. */
export function biometricLabels() {
  return isApple()
    ? { name: 'Face ID', withArticle: 'o Face ID', enabled: 'Face ID ativado', disabled: 'Face ID desativado' }
    : { name: 'Biometria', withArticle: 'a biometria', enabled: 'Biometria ativada', disabled: 'Biometria desativada' };
}

function deviceName(): string {
  const ua = navigator.userAgent;
  if (/iPhone/.test(ua)) return 'iPhone';
  if (/iPad/.test(ua) || isApple()) return 'iPad';
  if (/Android/.test(ua)) return 'Celular Android';
  if (/Macintosh/.test(ua)) return 'Mac';
  if (/Windows/.test(ua)) return 'Computador Windows';
  return 'Este aparelho';
}

// A biometria cadastrada neste aparelho (cada aparelho tem a sua).
const localKey = (userId: string) => `farbo.biometria.${userId}`;

export function deviceCredential(userId: string): string | null {
  try {
    return localStorage.getItem(localKey(userId));
  } catch {
    return null;
  }
}

function rememberCredential(userId: string, credentialId: string | null) {
  try {
    if (credentialId) localStorage.setItem(localKey(userId), credentialId);
    else localStorage.removeItem(localKey(userId));
  } catch {
    /* sem storage: na próxima vez, pede a senha */
  }
}

// A conta que entra com a biometria neste aparelho (o login ainda não sabe
// quem é): a credencial e como mostrar a pessoa na tela de entrada.
export interface BiometricAccount {
  credentialId: string;
  userId: string;
  email: string;
  name: string;
}

const LOGIN_KEY = 'farbo.biometria.login';

export function biometricLoginAccount(): BiometricAccount | null {
  try {
    const raw = localStorage.getItem(LOGIN_KEY);
    const account = raw ? (JSON.parse(raw) as BiometricAccount) : null;
    return account?.credentialId ? account : null;
  } catch {
    return null;
  }
}

function rememberLoginAccount(account: BiometricAccount | null) {
  try {
    if (account) localStorage.setItem(LOGIN_KEY, JSON.stringify(account));
    else localStorage.removeItem(LOGIN_KEY);
  } catch {
    /* sem storage: entra com a senha */
  }
}

// "Agora não" na oferta depois do login com senha: não pergunta de novo.
const offerKey = (userId: string) => `farbo.biometria.oferta.${userId}`;

export function biometricOfferDismissed(userId: string): boolean {
  try {
    return localStorage.getItem(offerKey(userId)) === 'nao';
  } catch {
    return false;
  }
}

export function dismissBiometricOffer(userId: string) {
  try {
    localStorage.setItem(offerKey(userId), 'nao');
  } catch {
    /* sem storage: pergunta de novo no próximo login */
  }
}

// --- Cadastro -------------------------------------------------------------------

export const listBiometrics = () =>
  api.get<{ rpId: string; credentials: BiometricDevice[] }>('/api/step-up/biometrics');

/**
 * Cadastra a biometria deste aparelho (pede a senha da conta). Ela passa a
 * servir para entrar no app e para confirmar o bloqueio do motor.
 */
export async function enrollBiometric(user: { id: string; email: string; name: string }, password: string): Promise<void> {
  const userId = user.id;
  const options = await api.post<CreationOptionsJSON>('/api/step-up/biometrics/options', { password });
  let credential: PublicKeyCredential | null;
  try {
    credential = (await navigator.credentials.create({
      publicKey: {
        challenge: toBytes(options.challenge),
        rp: options.rp,
        user: { id: toBytes(options.user.id), name: options.user.name, displayName: options.user.displayName },
        pubKeyCredParams: options.pubKeyCredParams,
        timeout: options.timeout,
        excludeCredentials: options.excludeCredentials.map((id) => ({ type: 'public-key' as const, id: toBytes(id) })),
        // Só a biometria do próprio aparelho, sempre verificando a pessoa.
        authenticatorSelection: { authenticatorAttachment: 'platform', userVerification: 'required', residentKey: 'discouraged' },
        attestation: 'none',
      },
    })) as PublicKeyCredential | null;
  } catch (error) {
    if (error instanceof DOMException && error.name === 'InvalidStateError') {
      throw new Error('Este aparelho já tem a biometria cadastrada nesta conta. Remova a antiga e cadastre de novo.');
    }
    throw new BiometricError('failed');
  }
  if (!credential) throw new BiometricError('failed');
  const response = credential.response as AuthenticatorAttestationResponse;
  const publicKey = response.getPublicKey?.();
  const authenticatorData = response.getAuthenticatorData?.();
  const algorithm = response.getPublicKeyAlgorithm?.();
  if (!publicKey || !authenticatorData || algorithm === undefined) {
    throw new BiometricError('unsupported');
  }
  const credentialId = toBase64url(credential.rawId);
  await api.post('/api/step-up/biometrics', {
    challengeId: options.challengeId,
    credentialId,
    clientDataJSON: toBase64url(response.clientDataJSON),
    authenticatorData: toBase64url(authenticatorData),
    publicKey: toBase64url(publicKey),
    algorithm,
    name: deviceName(),
  });
  rememberCredential(userId, credentialId);
  rememberLoginAccount({ credentialId, userId, email: user.email, name: user.name });
}

/** Remove a biometria (deste aparelho, se for ela). */
export async function removeBiometric(userId: string, device: BiometricDevice): Promise<void> {
  await api.delete(`/api/step-up/biometrics/${device.id}`);
  if (deviceCredential(userId) === device.credentialId) rememberCredential(userId, null);
  if (biometricLoginAccount()?.credentialId === device.credentialId) rememberLoginAccount(null);
}

// --- Entrar com a biometria -----------------------------------------------------

/** Entra com a biometria deste aparelho (devolve a sessão, como o login). */
export async function biometricLogin(): Promise<AuthTokens> {
  const account = biometricLoginAccount();
  if (!account) throw new BiometricError('not-enrolled');

  let options: RequestOptionsJSON;
  try {
    // Rota pública: anonymous evita que um 401 vire "sessão expirada".
    options = await request<RequestOptionsJSON>('/api/auth/biometric/options', {
      method: 'POST',
      body: { credentialId: account.credentialId },
      anonymous: true,
    });
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      // Removida em Conta ou em outro aparelho: volta para a senha.
      rememberLoginAccount(null);
      rememberCredential(account.userId, null);
      throw new BiometricError('not-enrolled');
    }
    throw error;
  }

  let assertion: PublicKeyCredential | null;
  try {
    assertion = (await navigator.credentials.get({
      publicKey: {
        challenge: toBytes(options.challenge),
        rpId: options.rpId,
        timeout: options.timeout,
        allowCredentials: [{ type: 'public-key', id: toBytes(account.credentialId), transports: ['internal'] }],
        userVerification: 'required',
      },
    })) as PublicKeyCredential | null;
  } catch {
    throw new BiometricError('failed');
  }
  if (!assertion) throw new BiometricError('failed');
  const response = assertion.response as AuthenticatorAssertionResponse;
  try {
    return await request<AuthTokens>('/api/auth/biometric', {
      method: 'POST',
      anonymous: true,
      body: {
        challengeId: options.challengeId,
        credentialId: toBase64url(assertion.rawId),
        clientDataJSON: toBase64url(response.clientDataJSON),
        authenticatorData: toBase64url(response.authenticatorData),
        signature: toBase64url(response.signature),
      },
    });
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) throw new BiometricError('failed');
    throw error;
  }
}

// --- Confirmação ------------------------------------------------------------------

/** Confirma com a biometria deste aparelho. */
export async function biometricGrant(userId: string, purpose: StepUpPurpose): Promise<StepUpGrant> {
  const local = deviceCredential(userId);
  if (!local) throw new BiometricError('not-enrolled');

  let options: RequestOptionsJSON;
  try {
    options = await api.post<RequestOptionsJSON>('/api/step-up/options', { purpose });
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      rememberCredential(userId, null); // removida em outro aparelho
      throw new BiometricError('not-enrolled');
    }
    throw error;
  }
  if (!options.allowCredentials.includes(local)) {
    rememberCredential(userId, null);
    throw new BiometricError('not-enrolled');
  }

  let assertion: PublicKeyCredential | null;
  try {
    assertion = (await navigator.credentials.get({
      publicKey: {
        challenge: toBytes(options.challenge),
        rpId: options.rpId,
        timeout: options.timeout,
        allowCredentials: [{ type: 'public-key', id: toBytes(local), transports: ['internal'] }],
        userVerification: 'required',
      },
    })) as PublicKeyCredential | null;
  } catch {
    throw new BiometricError('failed'); // cancelou, não reconheceu ou esgotou o tempo
  }
  if (!assertion) throw new BiometricError('failed');
  const response = assertion.response as AuthenticatorAssertionResponse;
  try {
    return await api.post<StepUpGrant>('/api/step-up/biometric', {
      purpose,
      challengeId: options.challengeId,
      credentialId: toBase64url(assertion.rawId),
      clientDataJSON: toBase64url(response.clientDataJSON),
      authenticatorData: toBase64url(response.authenticatorData),
      signature: toBase64url(response.signature),
    });
  } catch (error) {
    if (error instanceof ApiError && error.status === 403) throw new BiometricError('failed');
    throw error;
  }
}

/** Confirma com a senha da conta (403 WRONG_PASSWORD se errada). */
export const passwordGrant = (purpose: StepUpPurpose, password: string) =>
  api.post<StepUpGrant>('/api/step-up/password', { purpose, password });

/** Código do erro da API (STEP_UP_REQUIRED, WRONG_PASSWORD...). */
export function errorCode(error: unknown): string | undefined {
  if (!(error instanceof ApiError)) return undefined;
  const body = error.body as { code?: string } | undefined;
  return body?.code;
}

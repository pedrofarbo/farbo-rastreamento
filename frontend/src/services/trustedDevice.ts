/**
 * "Confiar neste aparelho por 30 dias": o token que o servidor devolve fica
 * neste navegador, por e-mail (mais de uma conta pode entrar no mesmo
 * aparelho). No próximo login, a senha basta.
 */
const KEY = 'tracker.trustedDevices';

function read(): Record<string, string> {
  try {
    const raw = localStorage.getItem(KEY);
    return raw ? (JSON.parse(raw) as Record<string, string>) : {};
  } catch {
    return {};
  }
}

function write(map: Record<string, string>) {
  try {
    localStorage.setItem(KEY, JSON.stringify(map));
  } catch {
    /* sem storage: só não lembra */
  }
}

const keyOf = (email: string) => email.trim().toLowerCase();

export function trustedDeviceFor(email: string): string {
  return read()[keyOf(email)] ?? '';
}

export function rememberTrustedDevice(email: string, token: string) {
  const map = read();
  map[keyOf(email)] = token;
  write(map);
}

export function forgetTrustedDevice(email: string) {
  const map = read();
  delete map[keyOf(email)];
  write(map);
}

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';

import { authApi } from '@/api/resources';
import { ApiError, onUnauthorized, tokens } from '@/api/client';
import { biometricLogin } from '@/services/stepUp';
import type { User } from '@/types';

interface AuthContextValue {
  user: User | null;
  loading: boolean;
  /** Entra com e-mail e senha; devolve quem entrou. */
  login: (email: string, password: string) => Promise<User>;
  /** Entra com a biometria deste aparelho (Face ID, digital). */
  loginWithBiometric: () => Promise<void>;
  logout: () => Promise<void>;
  /** Troca os dados de quem está conectado (depois de salvar Meus dados). */
  updateUser: (user: User) => void;
  canSendCommands: boolean;
  canManage: boolean;
  /** Equipe que opera os pedidos (admin e operador). */
  canOperate: boolean;
  /** Cliente final: vê só os próprios veículos e faturas. */
  isCustomer: boolean;
  /** Equipe da central (admin, operador, visualização). */
  isStaff: boolean;
}

const AuthContext = createContext<AuthContextValue | null>(null);

// Último usuário conhecido: sem rede (app aberto offline, oscilação ao
// abrir o painel) a sessão continua com ele em vez de cair no login.
const USER_KEY = 'tracker.user';

function cachedUser(): User | null {
  try {
    const raw = localStorage.getItem(USER_KEY);
    return raw ? (JSON.parse(raw) as User) : null;
  } catch {
    return null;
  }
}

function rememberUser(user: User | null) {
  try {
    if (user) localStorage.setItem(USER_KEY, JSON.stringify(user));
    else localStorage.removeItem(USER_KEY);
  } catch {
    /* sem storage: só não lembra */
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  // Recupera a sessão ao abrir o app: se o refresh token ainda vale, o
  // cliente renova sozinho na primeira chamada.
  useEffect(() => {
    let active = true;

    (async () => {
      if (!tokens.accessToken && !tokens.refreshToken) {
        setLoading(false);
        return;
      }
      try {
        const me = await authApi.me();
        rememberUser(me);
        if (active) setUser(me);
      } catch (error) {
        if (error instanceof ApiError && (error.status === 401 || error.status === 403)) {
          // O servidor recusou a sessão: acabou.
          tokens.clear();
          rememberUser(null);
        } else if (active) {
          // Sem rede ou servidor fora: segue com a última sessão conhecida;
          // o próximo pedido que chegar ao servidor confere o token.
          setUser(cachedUser());
        }
      } finally {
        if (active) setLoading(false);
      }
    })();

    return () => {
      active = false;
    };
  }, []);

  // O cliente avisa quando a renovação falhou de vez.
  useEffect(
    () =>
      onUnauthorized(() => {
        rememberUser(null);
        setUser(null);
      }),
    [],
  );

  const login = useCallback(async (email: string, password: string) => {
    const result = await authApi.login(email, password);
    tokens.save(result);
    rememberUser(result.user);
    setUser(result.user);
    return result.user;
  }, []);

  const loginWithBiometric = useCallback(async () => {
    const result = await biometricLogin();
    tokens.save(result);
    rememberUser(result.user);
    setUser(result.user);
  }, []);

  const logout = useCallback(async () => {
    const refreshToken = tokens.refreshToken;
    if (refreshToken) {
      try {
        await authApi.logout(refreshToken);
      } catch {
        /* a sessão local termina de qualquer forma */
      }
    }
    tokens.clear();
    rememberUser(null);
    setUser(null);
  }, []);

  const updateUser = useCallback((next: User) => {
    rememberUser(next);
    setUser(next);
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      user,
      loading,
      login,
      loginWithBiometric,
      logout,
      updateUser,
      // O cliente também comanda (bloqueio, desbloqueio, posição), mas só os
      // próprios veículos — a API confere o dono.
      canSendCommands:
        user?.role === 'admin' || user?.role === 'operator' || user?.role === 'customer',
      canManage: user?.role === 'admin',
      canOperate: user?.role === 'admin' || user?.role === 'operator',
      isCustomer: user?.role === 'customer',
      isStaff: user !== null && user.role !== 'customer',
    }),
    [user, loading, login, loginWithBiometric, logout, updateUser],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth precisa estar dentro de AuthProvider');
  }
  return context;
}

import { Suspense } from 'react';
import { useQuery } from '@tanstack/react-query';
import { NavLink, Outlet } from 'react-router-dom';

import { leadsApi, meApi, whatsappApi } from '@/api/resources';
import { Spinner } from '@/components/ui/Spinner';
import { useRealtime } from '@/hooks/useRealtime';
import { useAuth } from '@/stores/AuthContext';
import { useTheme } from '@/hooks/useTheme';

import styles from './AppShell.module.css';

const ROLE_LABELS: Record<string, string> = {
  admin: 'Administrador',
  operator: 'Operador',
  viewer: 'Visualização',
  customer: 'Cliente',
};

export function AppShell() {
  const { user, logout, canManage, canOperate, isCustomer } = useAuth();

  // Para o cliente, a conta alimenta o aviso de fatura vencida no menu.
  const account = useQuery({
    queryKey: ['me', 'account'],
    queryFn: meApi.account,
    enabled: isCustomer,
    refetchInterval: 5 * 60_000,
  });
  const overdue = account.data?.overdueInvoices ?? 0;
  // Para a central, as conversas do WhatsApp esperando alguém da equipe.
  const whatsapp = useQuery({
    queryKey: ['whatsapp', 'status'],
    queryFn: whatsappApi.status,
    enabled: canOperate,
    refetchInterval: 30_000,
  });
  const waiting = whatsapp.data?.attention ?? 0;
  // Para o admin, os pré-clientes que ninguém atendeu ainda.
  const leadStats = useQuery({
    queryKey: ['leads', 'stats'],
    queryFn: leadsApi.stats,
    enabled: canManage,
    refetchInterval: 60_000,
  });
  const newLeads = leadStats.data?.new ?? 0;
  const { connected } = useRealtime();
  const { theme, toggle } = useTheme();

  const navClass = ({ isActive }: { isActive: boolean }) =>
    `${styles.navLink} ${isActive ? styles.navActive : ''}`;

  return (
    <div className={styles.shell}>
      {/* O cabeçalho fica escuro nos dois temas, como a barra da landing: a
          logo tem letras brancas. */}
      <header className={styles.header} data-theme="dark">
        <NavLink to="/dashboard" className={styles.brand}>
          {/* No celular fica só o símbolo, para sobrar espaço para o menu. */}
          <picture>
            <source media="(max-width: 560px)" srcSet="/assets/logo-mark.png" />
            <img src="/assets/logo-header.png" alt="Farbo Rastreadores" className={styles.logo} />
          </picture>
        </NavLink>

        <nav className={styles.nav}>
          {isCustomer ? (
            <>
              <NavLink to="/dashboard" className={navClass}>
                Mapa
              </NavLink>
              <NavLink to="/meus-veiculos" className={navClass}>
                Meus veículos
              </NavLink>
              <NavLink to="/faturas" className={navClass}>
                Faturas
                {overdue > 0 && (
                  <span className={styles.navBadge} title={`${overdue} fatura(s) vencida(s)`}>
                    {overdue}
                  </span>
                )}
              </NavLink>
              <NavLink to="/alertas" className={navClass}>
                Alertas
              </NavLink>
              {/* O app do cliente (PWA) é outra página: link comum, não do router. */}
              <a href="/app/" className={styles.navLink}>
                App no celular
              </a>
            </>
          ) : (
            <>
              <NavLink to="/dashboard" className={navClass}>
                Painel
              </NavLink>
              <NavLink to="/eventos" className={navClass}>
                Eventos
              </NavLink>
              <NavLink to="/cercas" className={navClass}>
                Cercas
              </NavLink>
              {canOperate && (
                <>
                  <NavLink to="/pedidos" className={navClass}>
                    Pedidos
                  </NavLink>
                  <NavLink to="/atendimento" className={navClass}>
                    Atendimento
                    {waiting > 0 && (
                      <span className={styles.navBadge} title={`${waiting} conversa(s) esperando a equipe`}>
                        {waiting}
                      </span>
                    )}
                  </NavLink>
                </>
              )}
              {canManage && (
                <>
                  <NavLink to="/clientes" className={navClass}>
                    Clientes
                    {newLeads > 0 && (
                      <span className={styles.navBadge} title={`${newLeads} pré-cliente(s) novo(s)`}>
                        {newLeads}
                      </span>
                    )}
                  </NavLink>
                  <NavLink to="/prestadores" className={navClass}>
                    Prestadores
                  </NavLink>
                  <NavLink to="/dispositivos" className={navClass}>
                    Rastreadores
                  </NavLink>
                  <NavLink to="/diagnostico" className={navClass}>
                    Diagnóstico
                  </NavLink>
                </>
              )}
            </>
          )}
        </nav>

        <div className={styles.right}>
          <div
            className={`${styles.connection} ${connected ? styles.connectionLive : ''}`}
            title={
              connected
                ? 'Recebendo atualizações em tempo real'
                : 'Sem conexão em tempo real; reconectando'
            }
          >
            <span className={`${styles.dot} ${connected ? styles.dotLive : ''}`} />
            <span>{connected ? 'Ao vivo' : 'Reconectando'}</span>
          </div>

          {user && (
            <div className={styles.user}>
              <span className={styles.userName}>{user.name || user.email}</span>
              <span className={styles.userRole}>{ROLE_LABELS[user.role] ?? user.role}</span>
            </div>
          )}

          <button
            type="button"
            className={styles.themeButton}
            onClick={toggle}
            title="Alternar tema claro/escuro"
          >
            {theme === 'dark' ? '☾' : '☀'}
          </button>

          <button type="button" className={styles.logoutButton} onClick={() => void logout()}>
            Sair
          </button>
        </div>
      </header>

      <main className={styles.content}>
        {/* As páginas carregam sob demanda: o menu fica e só o conteúdo espera. */}
        <Suspense fallback={<Spinner label="Carregando" />}>
          <Outlet />
        </Suspense>
      </main>
    </div>
  );
}

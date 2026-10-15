import { Suspense, useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { NavLink, Outlet, useLocation } from 'react-router-dom';

import { financeApi, leadsApi, meApi, whatsappApi } from '@/api/resources';
import { Spinner } from '@/components/ui/Spinner';
import { SUPPORT_SCREEN } from '@/config/contact';
import { useRealtime } from '@/hooks/useRealtime';
import { ROLE_LABELS } from '@/services/roles';
import { useAuth } from '@/stores/AuthContext';
import { useTheme } from '@/hooks/useTheme';

import styles from './AppShell.module.css';

/** Os ícones do menu lateral (celular), no traço do resto do painel. */
const ICONS: Record<string, ReactNode> = {
  map: <path d="M9 4 3 6v14l6-2 6 2 6-2V4l-6 2-6-2Zm0 0v14m6-12v14" />,
  car: (
    <>
      <path d="M5 17H3v-5l2-5h14l2 5v5h-2" />
      <path d="M3 12h18" />
      <circle cx="7.5" cy="17" r="2" />
      <circle cx="16.5" cy="17" r="2" />
    </>
  ),
  receipt: <path d="M6 2h12v20l-3-2-3 2-3-2-3 2V2Zm3 6h6m-6 4h6m-6 4h4" />,
  bell: <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9m4.3 13a1.94 1.94 0 0 0 3.4 0" />,
  phone: (
    <>
      <rect x="6" y="2" width="12" height="20" rx="2" />
      <path d="M11 18h2" />
    </>
  ),
  activity: <path d="M22 12h-4l-3 9L9 3l-3 9H2" />,
  chart: <path d="M3 21h18M7 17v-5m5 5V7m5 10v-8" />,
  fence: <path d="M12 2 3 7v10l9 5 9-5V7l-9-5Z" />,
  package: <path d="M21 8 12 3 3 8v8l9 5 9-5V8ZM3 8l9 5 9-5m-9 5v8" />,
  chat: <path d="M21 12a8 8 0 0 1-11.6 7.1L4 21l1.9-5.4A8 8 0 1 1 21 12Z" />,
  users: (
    <>
      <circle cx="9" cy="8" r="3.5" />
      <path d="M2 21a7 7 0 0 1 14 0m1-14.5a3.5 3.5 0 0 1 0 7M22 21a7 7 0 0 0-4-6.3" />
    </>
  ),
  building: <path d="M4 21V5l8-3v19M12 9l8 3v9M2 21h20M8 8v.01M8 12v.01M8 16v.01M16 15v.01M16 18v.01" />,
  wrench: <path d="M14.7 6.3a4 4 0 0 0 5 5L21 13l-8 8-3-3 1.3-1.3a4 4 0 0 0-5-5L3 10l8-8 3 3-1.3 1.3Z" />,
  chip: (
    <>
      <rect x="6" y="6" width="12" height="12" rx="2" />
      <path d="M9 2v4m6-4v4M9 18v4m6-4v4M2 9h4m-4 6h4m12-6h4m-4 6h4" />
    </>
  ),
  pulse: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M7 12h2l2-4 2 8 2-4h2" />
    </>
  ),
  userCog: (
    <>
      <circle cx="10" cy="8" r="4" />
      <path d="M3 21a7 7 0 0 1 11-5.7M18 15v2m0 4v.01M15 18h2m2 0h2" />
    </>
  ),
};

function Icon({ name }: { name: string }) {
  return (
    <svg className={styles.navIcon} width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {ICONS[name]}
    </svg>
  );
}

export function AppShell() {
  const { user, logout, canManage, canOperate, isCustomer, membership, isAccountOwner, canSeeBilling } = useAuth();

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
    enabled: canOperate && SUPPORT_SCREEN,
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
  // Para o admin, as contas da empresa vencidas e as que vencem hoje.
  const financeAlerts = useQuery({
    queryKey: ['finance', 'alerts'],
    queryFn: financeApi.alerts,
    enabled: canManage,
    refetchInterval: 5 * 60_000,
  });
  const billsDue = (financeAlerts.data?.overdue ?? 0) + (financeAlerts.data?.dueToday ?? 0);
  const { connected } = useRealtime();
  const { theme, toggle } = useTheme();

  // No celular, o menu é a barra lateral: abre no ☰, fecha no ×, fora dela,
  // no Esc e ao trocar de página.
  const [menuOpen, setMenuOpen] = useState(false);
  const menuButton = useRef<HTMLButtonElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const location = useLocation();
  useEffect(() => setMenuOpen(false), [location.pathname]);
  useEffect(() => {
    if (!menuOpen) return;
    closeButton.current?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setMenuOpen(false);
        menuButton.current?.focus();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [menuOpen]);
  // O ☰ ganha um ponto quando algum item do menu tem aviso.
  const pending = (canSeeBilling ? overdue : 0) + (canOperate ? waiting : 0) + (canManage ? newLeads + billsDue : 0);
  // Conta empresa: quem é da equipe vê de quem é a conta, o papel e se está restrito a grupos.
  const accountLabel = membership
    ? `Conta de ${membership.ownerName} · ${membership.role === 'manager' ? 'Gestor' : 'Visualizador'}${membership.restricted ? ' · alguns grupos' : ''}`
    : null;

  const navClass = ({ isActive }: { isActive: boolean }) =>
    `${styles.navLink} ${isActive ? styles.navActive : ''}`;

  return (
    <div className={styles.shell}>
      {/* O cabeçalho fica escuro nos dois temas, como a barra da landing: a
          logo tem letras brancas. */}
      <header className={styles.header} data-theme="dark">
        <button
          ref={menuButton}
          type="button"
          className={styles.menuButton}
          aria-label={pending > 0 ? `Abrir o menu (${pending} aviso(s))` : 'Abrir o menu'}
          aria-expanded={menuOpen}
          aria-controls="menu-principal"
          onClick={() => setMenuOpen(true)}
        >
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
            <path d="M4 7h16M4 12h16M4 17h16" />
          </svg>
          {pending > 0 && <span className={styles.menuDot} />}
        </button>

        <NavLink to="/dashboard" className={styles.brand}>
          <img src="/assets/logo-header.png" alt="Farbo Rastreadores" className={styles.logo} />
        </NavLink>

        {menuOpen && <div className={styles.backdrop} onClick={() => setMenuOpen(false)} aria-hidden="true" />}

        <nav id="menu-principal" className={`${styles.nav} ${menuOpen ? styles.navOpen : ''}`} aria-label="Menu principal">
          {/* Só no celular: o topo da barra lateral, com quem está logado. */}
          <div className={styles.drawerTop}>
            <img src="/assets/logo-header.png" alt="" className={styles.drawerLogo} />
            <button
              ref={closeButton}
              type="button"
              className={styles.closeButton}
              aria-label="Fechar o menu"
              onClick={() => {
                setMenuOpen(false);
                menuButton.current?.focus();
              }}
            >
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
                <path d="M6 6l12 12M18 6 6 18" />
              </svg>
            </button>
          </div>
          {user && (
            <div className={styles.drawerUser}>
              <strong>{user.name || user.email}</strong>
              <span>{accountLabel ?? ROLE_LABELS[user.role] ?? user.role}</span>
            </div>
          )}
          <div className={styles.navItems}>
          {isCustomer ? (
            <>
              <NavLink to="/dashboard" className={navClass}>
                <Icon name="map" />
                Mapa
              </NavLink>
              <NavLink to="/meus-veiculos" className={navClass}>
                <Icon name="car" />
                Meus veículos
              </NavLink>
              {canSeeBilling && (
                <NavLink to="/faturas" className={navClass}>
                  <Icon name="receipt" />
                  Faturas
                  {overdue > 0 && (
                    <span className={styles.navBadge} title={`${overdue} fatura(s) vencida(s)`}>
                      {overdue}
                    </span>
                  )}
                </NavLink>
              )}
              <NavLink to="/alertas" className={navClass}>
                <Icon name="bell" />
                Alertas
              </NavLink>
              <NavLink to="/cercas" className={navClass}>
                <Icon name="fence" />
                Cercas
              </NavLink>
              <NavLink to="/relatorios" className={navClass}>
                <Icon name="chart" />
                Relatórios
              </NavLink>
              <NavLink to="/manutencao" className={navClass}>
                <Icon name="wrench" />
                Manutenção
              </NavLink>
              {/* Conta empresa: quem da empresa acessa a conta (só o titular). */}
              {isAccountOwner && (
                <NavLink to="/equipe" className={navClass}>
                  <Icon name="users" />
                  Equipe
                </NavLink>
              )}
              {/* O app do cliente (PWA) é outra página: link comum, não do router. */}
              <a href="/app/" className={styles.navLink}>
                <Icon name="phone" />
                App no celular
              </a>
            </>
          ) : (
            <>
              <NavLink to="/dashboard" className={navClass}>
                <Icon name="map" />
                Painel
              </NavLink>
              <NavLink to="/eventos" className={navClass}>
                <Icon name="activity" />
                Eventos
              </NavLink>
              <NavLink to="/cercas" className={navClass}>
                <Icon name="fence" />
                Cercas
              </NavLink>
              {canOperate && (
                <>
                  <NavLink to="/pedidos" className={navClass}>
                    <Icon name="package" />
                    Pedidos
                  </NavLink>
                  {SUPPORT_SCREEN && (
                    <NavLink to="/atendimento" className={navClass}>
                      <Icon name="chat" />
                      Atendimento
                      {waiting > 0 && (
                        <span className={styles.navBadge} title={`${waiting} conversa(s) esperando a equipe`}>
                          {waiting}
                        </span>
                      )}
                    </NavLink>
                  )}
                </>
              )}
              {canManage && (
                <>
                  <NavLink to="/clientes" className={navClass}>
                    <Icon name="users" />
                    Clientes
                    {newLeads > 0 && (
                      <span className={styles.navBadge} title={`${newLeads} pré-cliente(s) novo(s)`}>
                        {newLeads}
                      </span>
                    )}
                  </NavLink>
                  <NavLink to="/empresa" className={navClass}>
                    <Icon name="building" />
                    Empresa
                    {billsDue > 0 && (
                      <span className={styles.navBadge} title={`${billsDue} conta(s) vencida(s) ou vencendo hoje`}>
                        {billsDue}
                      </span>
                    )}
                  </NavLink>
                  <NavLink to="/prestadores" className={navClass}>
                    <Icon name="wrench" />
                    Prestadores
                  </NavLink>
                  <NavLink to="/dispositivos" className={navClass}>
                    <Icon name="chip" />
                    Rastreadores
                  </NavLink>
                  <NavLink to="/diagnostico" className={navClass}>
                    <Icon name="pulse" />
                    Diagnóstico
                  </NavLink>
                  <NavLink to="/usuarios" className={navClass}>
                    <Icon name="userCog" />
                    Usuários
                  </NavLink>
                </>
              )}
            </>
          )}
          </div>

          {/* Só no celular: a conta e o tema no pé da barra lateral. */}
          <div className={styles.drawerFooter}>
            <NavLink to="/seguranca" className={navClass}>
              <svg className={styles.navIcon} width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <rect x="4" y="11" width="16" height="10" rx="2" />
                <path d="M8 11V7a4 4 0 0 1 8 0v4" />
              </svg>
              Segurança da conta
            </NavLink>
            <button type="button" className={styles.navLink} onClick={toggle}>
              <span className={styles.navIcon} aria-hidden="true">
                {theme === 'dark' ? '☾' : '☀'}
              </span>
              {theme === 'dark' ? 'Tema claro' : 'Tema escuro'}
            </button>
            <button type="button" className={`${styles.navLink} ${styles.drawerLogout}`} onClick={() => void logout()}>
              <svg className={styles.navIcon} width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4m7 14 5-5-5-5m5 5H9" />
              </svg>
              Sair
            </button>
          </div>
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
            <NavLink to="/seguranca" className={styles.user} title="Segurança da conta">
              <span className={styles.userName}>{user.name || user.email}</span>
              <span className={styles.userRole}>{accountLabel ?? ROLE_LABELS[user.role] ?? user.role}</span>
            </NavLink>
          )}

          {/* A segurança da conta (a verificação em duas etapas). No
              celular, ela, o tema e o Sair ficam no pé da barra lateral. */}
          <NavLink
            to="/seguranca"
            className={`${styles.themeButton} ${styles.desktopOnly}`}
            title="Segurança da conta"
            aria-label="Segurança da conta"
          >
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <rect x="4" y="11" width="16" height="10" rx="2" />
              <path d="M8 11V7a4 4 0 0 1 8 0v4" />
            </svg>
          </NavLink>

          <button
            type="button"
            className={`${styles.themeButton} ${styles.desktopOnly}`}
            onClick={toggle}
            title="Alternar tema claro/escuro"
          >
            {theme === 'dark' ? '☾' : '☀'}
          </button>

          <button type="button" className={`${styles.logoutButton} ${styles.desktopOnly}`} onClick={() => void logout()}>
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

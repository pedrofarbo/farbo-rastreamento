import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { NavLink, Outlet, useLocation, useNavigationType } from 'react-router-dom';

import { meApi } from '@/api/resources';
import { useRealtime } from '@/hooks/useRealtime';

import { BellIcon, CarIcon, MapIcon, ReceiptIcon, UserIcon } from './icons';
import { applyUpdate, usePwa } from './pwa';
import { PULL_THRESHOLD, usePullToRefresh } from './usePullToRefresh';
import styles from './AppLayout.module.css';

const TABS = [
  { to: '/mapa', label: 'Mapa', icon: MapIcon },
  { to: '/meus-veiculos', label: 'Veículos', icon: CarIcon },
  { to: '/alertas', label: 'Alertas', icon: BellIcon },
  { to: '/faturas', label: 'Faturas', icon: ReceiptIcon },
  { to: '/conta', label: 'Conta', icon: UserIcon },
];

/** Tela que ocupa tudo, por baixo das barras (o mapa). */
const FULL_BLEED = new Set(['/mapa']);

function useOnline(): boolean {
  const [online, setOnline] = useState(navigator.onLine);
  useEffect(() => {
    const on = () => setOnline(true);
    const off = () => setOnline(false);
    window.addEventListener('online', on);
    window.addEventListener('offline', off);
    return () => {
      window.removeEventListener('online', on);
      window.removeEventListener('offline', off);
    };
  }, []);
  return online;
}

/** Profundidade da tela: as abas são 0; veículo e cercas, telas de dentro. */
function depth(path: string): number {
  if (path.startsWith('/cercas/')) return 2;
  if (path === '/cercas' || path.startsWith('/veiculos/')) return 1;
  return 0;
}

/**
 * Animação de entrada da tela: entrar numa tela de dentro (veículo, cercas)
 * desliza da direita, voltar desliza da esquerda (como a navegação do iOS);
 * trocar de aba só esmaece.
 */
function transitionFor(path: string, previous: string | null, navigation: string): 'push' | 'pop' | 'fade' | 'none' {
  if (previous === null) return 'none';
  if (depth(path) > depth(previous) && navigation !== 'POP') return 'push';
  if (depth(path) < depth(previous) && navigation === 'POP') return 'pop';
  return 'fade';
}

/** As cercas moram dentro da aba Alertas. */
const tabOf = (path: string) => (path.startsWith('/cercas') ? '/alertas' : path);

const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/** Moldura do app: barra do topo e abas translúcidas, com o conteúdo por baixo. */
export function AppLayout() {
  const online = useOnline();
  const { connected } = useRealtime();
  const { updateReady } = usePwa();
  const queryClient = useQueryClient();
  const location = useLocation();
  const navigation = useNavigationType();
  const account = useQuery({ queryKey: ['me', 'account'], queryFn: meApi.account, staleTime: 60_000 });
  const overdue = account.data?.overdueInvoices ?? 0;
  const live = online && connected;
  const liveLabel = live ? 'Ao vivo' : online ? 'Conectando…' : 'Offline';

  const rootRef = useRef<HTMLDivElement>(null);
  const headerRef = useRef<HTMLElement>(null);
  const scrollRef = useRef<HTMLElement>(null);
  const fullBleed = FULL_BLEED.has(location.pathname);

  // A altura real do topo (varia com os avisos) vira --app-top: o conteúdo
  // começa logo abaixo dele e a lista do mapa para ali.
  useLayoutEffect(() => {
    const root = rootRef.current;
    const header = headerRef.current;
    if (!root || !header) return;
    const update = () => root.style.setProperty('--app-top', `${header.offsetHeight}px`);
    update();
    const observer = new ResizeObserver(update);
    observer.observe(header);
    return () => observer.disconnect();
  }, []);

  // Tela nova começa do topo (o contêiner é novo a cada rota).
  const previous = useRef<string | null>(null);
  // Uma vez por rota: re-render na mesma tela (tempo real, ?mapa=tela-cheia)
  // não pode trocar a animação e fazer a página esmaecer de novo.
  const transition = useMemo(
    () => transitionFor(location.pathname, previous.current, navigation),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [location.pathname],
  );
  useEffect(() => {
    previous.current = location.pathname;
  }, [location.pathname]);

  const refresh = useCallback(() => Promise.all([queryClient.invalidateQueries(), wait(700)]), [queryClient]);
  const pull = usePullToRefresh(scrollRef, refresh, !fullBleed, location.pathname);
  const progress = Math.min(1, pull.offset / PULL_THRESHOLD);

  return (
    <div ref={rootRef} className={styles.app}>
      <header ref={headerRef} className={styles.header}>
        <div className={styles.topbar}>
          {/* A logo completa, centralizada; o ícone só entra quando a tela é
              estreita demais para ela. */}
          <picture className={styles.logo}>
            <source media="(max-width: 259px)" srcSet="/assets/logo-mark.png" />
            <img src="/assets/logo-header.png" alt="Farbo Rastreadores" />
          </picture>
          <span className={`${styles.live} ${live ? styles.liveOn : ''}`} title={liveLabel} aria-label={liveLabel}>
            <span className={styles.liveLabel}>{liveLabel}</span>
          </span>
        </div>

        {!online && (
          <div className={styles.banner} role="status">
            Sem internet: você está vendo os últimos dados guardados neste celular.
          </div>
        )}
        {account.data?.suspended && (
          <NavLink to="/faturas" className={`${styles.banner} ${styles.bannerDanger}`}>
            Acesso suspenso por fatura vencida. Toque para pagar e voltar a ver o mapa.
          </NavLink>
        )}
        {updateReady && (
          <button type="button" className={`${styles.banner} ${styles.bannerAccent}`} onClick={applyUpdate}>
            Nova versão do app disponível — toque para atualizar
          </button>
        )}
      </header>

      {!fullBleed && (
        <div
          className={`${styles.pull} ${pull.refreshing ? styles.pullSpinning : ''} ${pull.pulling ? styles.pullDragging : ''}`}
          style={{ opacity: pull.refreshing ? 1 : progress, transform: `translate(-50%, ${pull.offset - 44}px) rotate(${progress * 270}deg)` }}
          aria-hidden={!pull.refreshing}
          role="status"
          aria-label={pull.refreshing ? 'Atualizando' : undefined}
        />
      )}

      <main
        ref={scrollRef}
        key={location.pathname}
        className={`${styles.content} ${fullBleed ? styles.fullBleed : ''} ${styles[transition] ?? ''}`}
      >
        <div
          className={`${styles.page} ${pull.pulling ? styles.pageDragging : ''}`}
          style={pull.offset ? { transform: `translateY(${pull.offset}px)` } : undefined}
        >
          <Outlet />
        </div>
      </main>

      <nav className={styles.tabbar} aria-label="Navegação principal">
        {TABS.map(({ to, label, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            className={({ isActive }) =>
              `${styles.tab} ${isActive || tabOf(location.pathname) === to ? styles.tabActive : ''}`
            }
          >
            <span className={styles.tabIcon}>
              <Icon />
              {to === '/faturas' && overdue > 0 && (
                <span className={styles.dot} aria-label={`${overdue} fatura(s) vencida(s)`} />
              )}
            </span>
            <span className={styles.tabLabel}>{label}</span>
          </NavLink>
        ))}
      </nav>
    </div>
  );
}

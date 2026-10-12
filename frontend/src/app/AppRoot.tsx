import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Navigate, Outlet, Route, Routes } from 'react-router-dom';

import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { ToastProvider } from '@/components/ui/Toast';
import { RealtimeProvider } from '@/hooks/useRealtime';
import { InvoicesPage } from '@/pages/customer/InvoicesPage';
import { MaintenancePage } from '@/pages/customer/MaintenancePage';
import { MyVehiclesPage } from '@/pages/customer/MyVehiclesPage';
import { ReportsPage } from '@/pages/customer/ReportsPage';
import { TheftReportPage } from '@/pages/TheftReportPage';
import { ContractGate } from '@/components/contract/ContractGate';
import { AuthProvider, useAuth } from '@/stores/AuthContext';

import { AppLayout } from './AppLayout';
import { AccountScreen } from './screens/AccountScreen';
import { ProfileScreen } from './screens/ProfileScreen';
import { TeamScreen } from './screens/TeamScreen';
import { SecurityScreen } from './screens/SecurityScreen';
import { AlertsScreen } from './screens/AlertsScreen';
import { FenceScreen } from './screens/FenceScreen';
import { FencesScreen } from './screens/FencesScreen';
import { LoginScreen } from './screens/LoginScreen';
import { MapScreen } from './screens/MapScreen';
import { VehicleScreen } from './screens/VehicleScreen';
import styles from './screens/Screen.module.css';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // O WebSocket mantém os dados frescos; ao voltar para o app, confere.
      refetchOnWindowFocus: true,
      retry: 1,
      staleTime: 30_000,
    },
  },
});

/** O app é do cliente; a equipe usa o painel. */
function RequireCustomer() {
  const { user, loading, isCustomer, logout } = useAuth();
  if (loading) return <Spinner label="Carregando" />;
  if (!user) return <Navigate to="/login" replace />;
  if (!isCustomer) {
    return (
      <div className={styles.login}>
        <h1 className={styles.title}>Este app é dos clientes</h1>
        <p className={styles.lead}>Com a conta da central, use o painel completo.</p>
        <Button onClick={() => window.location.assign('/dashboard')} block>
          Abrir o painel
        </Button>
        <Button variant="ghost" onClick={() => logout()} block>
          Sair
        </Button>
      </div>
    );
  }
  // Sem o aceite do contrato, o contrato vem antes de tudo.
  return (
    <ContractGate>
      <AppLayout />
    </ContractGate>
  );
}

/** Conta empresa: operate deixa o Gestor (com todos os veículos) passar; sem ele, só o titular. */
function RequireAccount({ operate = false }: { operate?: boolean }) {
  const { isAccountOwner, canSeeBilling } = useAuth();
  if (!(operate ? canSeeBilling : isAccountOwner)) return <Navigate to="/conta" replace />;
  return <Outlet />;
}

export function AppRoot() {
  return (
    <QueryClientProvider client={queryClient}>
      {/* Tudo sob /app: os links das telas reaproveitadas do painel
          (/veiculos/…, /faturas) caem nas rotas do app. */}
      <BrowserRouter basename="/app">
        <AuthProvider>
          <RealtimeProvider>
            <ToastProvider>
              <Routes>
                <Route path="/login" element={<LoginScreen />} />
                <Route element={<RequireCustomer />}>
                  {/* O mapa tem rota própria: com o basename, "/" viraria a URL
                      /app (sem barra), fora do escopo /app/ do service worker —
                      recarregar sem internet ou abrir pelo ícone sairia do app. */}
                  <Route index element={<Navigate to="/mapa" replace />} />
                  <Route path="mapa" element={<MapScreen />} />
                  <Route path="veiculos/:id" element={<VehicleScreen />} />
                  <Route path="relatorio-roubo/:id" element={<TheftReportPage />} />
                  <Route path="meus-veiculos" element={<MyVehiclesPage />} />
                  {/* Manutenção e Relatórios abrem pela Conta: o voltar leva a ela. */}
                  <Route path="relatorios" element={<ReportsPage backTo="/conta" />} />
                  <Route path="manutencao" element={<MaintenancePage backTo="/conta" />} />
                  <Route path="alertas" element={<AlertsScreen />} />
                  <Route path="cercas" element={<FencesScreen />} />
                  <Route path="cercas/nova" element={<FenceScreen />} />
                  <Route path="cercas/:id" element={<FenceScreen />} />
                  <Route path="conta" element={<AccountScreen />} />
                  <Route path="conta/seguranca" element={<SecurityScreen />} />
                  {/* Conta empresa: faturas do titular e do Gestor; cadastro e
                      equipe, só do titular. */}
                  <Route element={<RequireAccount operate />}>
                    <Route path="faturas" element={<InvoicesPage />} />
                  </Route>
                  <Route element={<RequireAccount />}>
                    <Route path="conta/dados" element={<ProfileScreen />} />
                    <Route path="conta/equipe" element={<TeamScreen />} />
                  </Route>
                  <Route path="dashboard" element={<Navigate to="/mapa" replace />} />
                  <Route path="*" element={<Navigate to="/mapa" replace />} />
                </Route>
              </Routes>
            </ToastProvider>
          </RealtimeProvider>
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

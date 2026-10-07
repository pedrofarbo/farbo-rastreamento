import { lazy, Suspense } from 'react';
import type { ComponentType } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Navigate, Outlet, Route, Routes } from 'react-router-dom';

import { ContractGate } from '@/components/contract/ContractGate';
import { Spinner } from '@/components/ui/Spinner';
import { ToastProvider } from '@/components/ui/Toast';
import { RealtimeProvider } from '@/hooks/useRealtime';
import { LandingPage } from '@/pages/LandingPage';
import { AuthProvider, useAuth } from '@/stores/AuthContext';

/**
 * Só a landing vai no pacote inicial: quem chega pelo site (quase sempre no
 * celular) não baixa o mapa, os comandos e o resto do painel. Cada página do
 * painel carrega quando é aberta.
 */
function page<K extends string>(load: () => Promise<Record<K, ComponentType>>, name: K) {
  return lazy(() => load().then((module) => ({ default: module[name] })));
}

const AppShell = page(() => import('@/components/layout/AppShell'), 'AppShell');
const CustomerDetailsPage = page(() => import('@/pages/admin/CustomerDetailsPage'), 'CustomerDetailsPage');
const CustomersPage = page(() => import('@/pages/admin/CustomersPage'), 'CustomersPage');
const InstallersPage = page(() => import('@/pages/admin/InstallersPage'), 'InstallersPage');
const OrdersPage = page(() => import('@/pages/admin/OrdersPage'), 'OrdersPage');
const SupportPage = page(() => import('@/pages/admin/SupportPage'), 'SupportPage');
const UsersPage = page(() => import('@/pages/admin/UsersPage'), 'UsersPage');
const CompanyPage = page(() => import('@/pages/admin/company/CompanyPage'), 'CompanyPage');
const TermsPage = page(() => import('@/pages/legal/TermsPage'), 'TermsPage');
const PrivacyPage = page(() => import('@/pages/legal/PrivacyPage'), 'PrivacyPage');
const EventSignupPage = page(() => import('@/pages/EventSignupPage'), 'EventSignupPage');
const ReferralSignupPage = page(() => import('@/pages/ReferralSignupPage'), 'ReferralSignupPage');
const PartnerPage = page(() => import('@/pages/PartnerPage'), 'PartnerPage');
const PublicTheftPage = page(() => import('@/pages/PublicTheftPage'), 'PublicTheftPage');
const TheftReportPage = page(() => import('@/pages/TheftReportPage'), 'TheftReportPage');
const PublicPayPage = page(() => import('@/pages/PublicPayPage'), 'PublicPayPage');
const ContractPage = page(() => import('@/pages/legal/ContractPage'), 'ContractPage');
const AlertsPage = page(() => import('@/pages/customer/AlertsPage'), 'AlertsPage');
const InvoicesPage = page(() => import('@/pages/customer/InvoicesPage'), 'InvoicesPage');
const MyVehiclesPage = page(() => import('@/pages/customer/MyVehiclesPage'), 'MyVehiclesPage');
const DashboardPage = page(() => import('@/pages/DashboardPage'), 'DashboardPage');
const DevicesPage = page(() => import('@/pages/DevicesPage'), 'DevicesPage');
const DiagnosticsPage = page(() => import('@/pages/DiagnosticsPage'), 'DiagnosticsPage');
const EventsPage = page(() => import('@/pages/EventsPage'), 'EventsPage');
const ForgotPasswordPage = page(() => import('@/pages/ForgotPasswordPage'), 'ForgotPasswordPage');
const GeofencesPage = page(() => import('@/pages/GeofencesPage'), 'GeofencesPage');
const LoginPage = page(() => import('@/pages/LoginPage'), 'LoginPage');
const ResetPasswordPage = page(() => import('@/pages/ResetPasswordPage'), 'ResetPasswordPage');
const VehicleDetailsPage = page(() => import('@/pages/VehicleDetailsPage'), 'VehicleDetailsPage');

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // O WebSocket já mantém os dados frescos; o refetch é rede de segurança.
      refetchOnWindowFocus: false,
      retry: 1,
      staleTime: 30_000,
    },
  },
});

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AuthProvider>
          <RealtimeProvider>
            <ToastProvider>
              <Suspense fallback={<Spinner label="Carregando" />}>
              <Routes>
                <Route path="/" element={<LandingPage />} />
                <Route path="/landing" element={<LandingPage />} />
                <Route path="/login" element={<LoginPage />} />
                <Route path="/esqueci-senha" element={<ForgotPasswordPage />} />
                <Route path="/redefinir-senha" element={<ResetPasswordPage />} />
                <Route path="/termos-de-uso" element={<TermsPage />} />
                <Route path="/politica-de-privacidade" element={<PrivacyPage />} />
                <Route path="/contrato" element={<ContractPage />} />
                {/* Cadastro no pré-lançamento em eventos (o QR Code do estande). */}
                <Route path="/evento" element={<EventSignupPage />} />
                <Route path="/evento/:evento" element={<EventSignupPage />} />
                {/* Afiliados: o link de cadastro e a página do afiliado (link secreto). */}
                <Route path="/indicacao/:codigo" element={<ReferralSignupPage />} />
                <Route path="/parceiro/:token" element={<PartnerPage />} />
                {/* Modo roubo: a posição ao vivo pelo link (sem login), para a polícia. */}
                <Route path="/localizar/:token" element={<PublicTheftPage />} />
                {/* O link de pagamento dos lembretes de fatura (sem login). */}
                <Route path="/pagar/:token" element={<PublicPayPage />} />

                <Route element={<RequireAuth />}>
                  {/* O relatório para o boletim de ocorrência: sem o menu (vai para a impressora). */}
                  <Route path="relatorio-roubo/:id" element={<TheftReportPage />} />
                  <Route element={<AppShell />}>
                    {/* Comuns: a API filtra o que o cliente vê. */}
                    <Route path="dashboard" element={<DashboardPage />} />
                    <Route path="veiculos/:id" element={<VehicleDetailsPage />} />

                    <Route element={<RequireCustomer />}>
                      <Route path="meus-veiculos" element={<MyVehiclesPage />} />
                      <Route path="faturas" element={<InvoicesPage />} />
                      <Route path="alertas" element={<AlertsPage />} />
                    </Route>

                    <Route element={<RequireStaff />}>
                      <Route path="eventos" element={<EventsPage />} />
                      <Route path="cercas" element={<GeofencesPage />} />
                    </Route>

                    <Route element={<RequireOperator />}>
                      <Route path="pedidos" element={<OrdersPage />} />
                      <Route path="atendimento" element={<SupportPage />} />
                    </Route>

                    <Route element={<RequireAdmin />}>
                      <Route path="dispositivos" element={<DevicesPage />} />
                      <Route path="diagnostico" element={<DiagnosticsPage />} />
                      <Route path="clientes" element={<CustomersPage />} />
                      <Route path="clientes/:id" element={<CustomerDetailsPage />} />
                      <Route path="prestadores" element={<InstallersPage />} />
                      <Route path="empresa" element={<CompanyPage />} />
                      <Route path="usuarios" element={<UsersPage />} />
                    </Route>
                  </Route>
                </Route>

                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
              </Suspense>
            </ToastProvider>
          </RealtimeProvider>
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

function RequireAuth() {
  const { user, loading } = useAuth();

  if (loading) return <Spinner label="Carregando" />;
  if (!user) return <Navigate to="/login" replace />;
  // Cliente sem o aceite do contrato vê o contrato antes de tudo.
  return (
    <ContractGate>
      <Outlet />
    </ContractGate>
  );
}

/**
 * As telas de cadastro e diagnóstico ficam fora do alcance de quem só
 * visualiza. A API repete a verificação: isto aqui é conveniência, não
 * segurança.
 */
function RequireAdmin() {
  const { canManage } = useAuth();
  if (!canManage) return <Navigate to="/dashboard" replace />;
  return <Outlet />;
}

/** Telas operacionais da central (eventos de toda a frota, cercas). */
/** Fila de pedidos: admin e operador. */
function RequireOperator() {
  const { canOperate } = useAuth();
  if (!canOperate) return <Navigate to="/dashboard" replace />;
  return <Outlet />;
}

function RequireStaff() {
  const { isStaff } = useAuth();
  if (!isStaff) return <Navigate to="/dashboard" replace />;
  return <Outlet />;
}

/** Área do cliente: veículos próprios e faturas. */
function RequireCustomer() {
  const { isCustomer } = useAuth();
  if (!isCustomer) return <Navigate to="/dashboard" replace />;
  return <Outlet />;
}

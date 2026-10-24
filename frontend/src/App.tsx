import { lazy, Suspense } from 'react';
import type { ComponentType } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Navigate, Outlet, Route, Routes } from 'react-router-dom';

import { ContractGate } from '@/components/contract/ContractGate';
import { Spinner } from '@/components/ui/Spinner';
import { ToastProvider } from '@/components/ui/Toast';
import { SUPPORT_SCREEN } from '@/config/contact';
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
const SecurityPage = page(() => import('@/pages/SecurityPage'), 'SecurityPage');
const CompanyPage = page(() => import('@/pages/admin/company/CompanyPage'), 'CompanyPage');
const TermsPage = page(() => import('@/pages/legal/TermsPage'), 'TermsPage');
const PrivacyPage = page(() => import('@/pages/legal/PrivacyPage'), 'PrivacyPage');
const EventSignupPage = page(() => import('@/pages/EventSignupPage'), 'EventSignupPage');
const ReferralSignupPage = page(() => import('@/pages/ReferralSignupPage'), 'ReferralSignupPage');
const TeamPage = page(() => import('@/pages/customer/TeamPage'), 'TeamPage');
const PartnerPage = page(() => import('@/pages/PartnerPage'), 'PartnerPage');
const PartnerFlyerPage = page(() => import('@/pages/PartnerFlyerPage'), 'PartnerFlyerPage');
const PublicTheftPage = page(() => import('@/pages/PublicTheftPage'), 'PublicTheftPage');
const PublicLivePage = page(() => import('@/pages/PublicLivePage'), 'PublicLivePage');
const PublicRoutePage = page(() => import('@/pages/PublicRoutePage'), 'PublicRoutePage');
const InstallerPage = page(() => import('@/pages/InstallerPage'), 'InstallerPage');
const InstallationsAgendaPage = page(() => import('@/pages/admin/InstallationsAgendaPage'), 'InstallationsAgendaPage');
const OrgInstallationsPage = page(() => import('@/pages/org/OrgInstallationsPage'), 'OrgInstallationsPage');
const TheftReportPage = page(() => import('@/pages/TheftReportPage'), 'TheftReportPage');
const ClaimReportPage = page(() => import('@/pages/ClaimReportPage'), 'ClaimReportPage');
const PublicPayPage = page(() => import('@/pages/PublicPayPage'), 'PublicPayPage');
const InstallGuidePage = page(() => import('@/pages/InstallGuidePage'), 'InstallGuidePage');
const ContractPage = page(() => import('@/pages/legal/ContractPage'), 'ContractPage');
const AlertsPage = page(() => import('@/pages/customer/AlertsPage'), 'AlertsPage');
const InvoicesPage = page(() => import('@/pages/customer/InvoicesPage'), 'InvoicesPage');
const ReportsPage = page(() => import('@/pages/customer/ReportsPage'), 'ReportsPage');
const MaintenancePage = page(() => import('@/pages/customer/MaintenancePage'), 'MaintenancePage');
const FleetOpsPage = page(() => import('@/pages/customer/FleetOpsPage'), 'FleetOpsPage');
const MyIntegrationsPage = page(() => import('@/pages/IntegrationsPage'), 'MyIntegrationsPage');
const OrgIntegrationsPage = page(() => import('@/pages/IntegrationsPage'), 'OrgIntegrationsPage');
const CustomerReportsPage = page(() => import('@/pages/customer/ReportsPage'), 'CustomerReportsPage');
const MyVehiclesPage = page(() => import('@/pages/customer/MyVehiclesPage'), 'MyVehiclesPage');
const DashboardPage = page(() => import('@/pages/DashboardPage'), 'DashboardPage');
const DevicesPage = page(() => import('@/pages/DevicesPage'), 'DevicesPage');
const DiagnosticsPage = page(() => import('@/pages/DiagnosticsPage'), 'DiagnosticsPage');
const EventsPage = page(() => import('@/pages/EventsPage'), 'EventsPage');
const ForgotPasswordPage = page(() => import('@/pages/ForgotPasswordPage'), 'ForgotPasswordPage');
const CustomerFencesPage = page(() => import('@/pages/customer/CustomerFencesPage'), 'CustomerFencesPage');
const CentralFencesPage = page(() => import('@/pages/customer/CustomerFencesPage'), 'CentralFencesPage');
const LoginPage = page(() => import('@/pages/LoginPage'), 'LoginPage');
const ResetPasswordPage = page(() => import('@/pages/ResetPasswordPage'), 'ResetPasswordPage');
const VehicleDetailsPage = page(() => import('@/pages/VehicleDetailsPage'), 'VehicleDetailsPage');
const OrgAssociatesPage = page(() => import('@/pages/org/OrgAssociatesPage'), 'OrgAssociatesPage');
const OrgAssociatePage = page(() => import('@/pages/org/OrgAssociatePage'), 'OrgAssociatePage');
const OrgTeamPage = page(() => import('@/pages/org/OrgTeamPage'), 'OrgTeamPage');
const OrgRecoveryPage = page(() => import('@/pages/org/OrgRecoveryPage'), 'OrgRecoveryPage');
const OrgAnnouncementsPage = page(() => import('@/pages/org/OrgAnnouncementsPage'), 'OrgAnnouncementsPage');
const OrgClaimsPage = page(() => import('@/pages/org/OrgClaimsPage'), 'OrgClaimsPage');
const OrgClaimPage = page(() => import('@/pages/org/OrgClaimsPage'), 'OrgClaimPage');
const OrgInspectionsPage = page(() => import('@/pages/org/OrgInspectionsPage'), 'OrgInspectionsPage');
const OrgInspectionPage = page(() => import('@/pages/org/OrgInspectionsPage'), 'OrgInspectionPage');
const OrgInvoicesPage = page(() => import('@/pages/org/OrgInvoicesPage'), 'OrgInvoicesPage');
const OrganizationsPage = page(() => import('@/pages/admin/OrganizationsPage'), 'OrganizationsPage');
const OrganizationDetailsPage = page(() => import('@/pages/admin/OrganizationDetailsPage'), 'OrganizationDetailsPage');

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
                <Route path="/parceiro/:token/flyer" element={<PartnerFlyerPage />} />
                {/* Modo roubo: a posição ao vivo pelo link (sem login), para a polícia. */}
                <Route path="/localizar/:token" element={<PublicTheftPage />} />
                {/* A localização ao vivo por um tempo, pelo link que o cliente mandou (sem login). */}
                <Route path="/ao-vivo/:token" element={<PublicLivePage />} />
                {/* O link do passageiro: onde o ônibus da linha está (sem login). */}
                <Route path="/linha/:token" element={<PublicRoutePage />} />
                {/* O link do instalador: o passo a passo da instalação (sem login). */}
                <Route path="/instalador/:token" element={<InstallerPage />} />
                {/* O link de pagamento dos lembretes de fatura (sem login). */}
                <Route path="/pagar/:token" element={<PublicPayPage />} />
                {/* A instalação do J16 (o QR Code do folheto da embalagem). */}
                <Route path="/instalacao" element={<InstallGuidePage />} />

                <Route element={<RequireAuth />}>
                  {/* O relatório para o boletim de ocorrência: sem o menu (vai para a impressora). */}
                  <Route path="relatorio-roubo/:id" element={<TheftReportPage />} />
                  {/* O relatório de sinistro (o seguro, a perícia): o trajeto e a velocidade em volta de um momento. */}
                  <Route path="relatorio-sinistro/:id" element={<ClaimReportPage />} />
                  <Route element={<AppShell />}>
                    {/* Comuns: a API filtra o que o cliente vê. */}
                    <Route path="dashboard" element={<DashboardPage />} />
                    <Route path="veiculos/:id" element={<VehicleDetailsPage />} />
                    {/* Segurança da conta: a verificação em duas etapas (todos). */}
                    <Route path="seguranca" element={<SecurityPage />} />

                    <Route element={<RequireCustomer />}>
                      <Route path="meus-veiculos" element={<MyVehiclesPage />} />
                      <Route path="alertas" element={<AlertsPage />} />
                      <Route path="relatorios" element={<ReportsPage />} />
                      <Route path="manutencao" element={<MaintenancePage />} />
                      <Route path="frota" element={<FleetOpsPage />} />
                      {/* Conta empresa: faturas do titular e do Gestor; a equipe, só do titular. */}
                      <Route element={<RequireAccountOperator />}>
                        <Route path="faturas" element={<InvoicesPage />} />
                      </Route>
                      <Route element={<RequireAccountOwner />}>
                        <Route path="equipe" element={<TeamPage />} />
                        <Route path="integracoes" element={<MyIntegrationsPage />} />
                      </Route>
                    </Route>

                    {/* O painel da associação ou seguradora (o mapa e os veículos são os comuns). */}
                    <Route element={<RequireOrgMember />}>
                      <Route path="associados" element={<OrgAssociatesPage />} />
                      <Route path="associados/:id" element={<OrgAssociatePage />} />
                      <Route element={<RequireOrgAdmin />}>
                        <Route path="organizacao/equipe" element={<OrgTeamPage />} />
                        <Route path="organizacao/faturas" element={<OrgInvoicesPage />} />
                        <Route path="organizacao/integracoes" element={<OrgIntegrationsPage />} />
                      </Route>
                      <Route path="organizacao/relatorios" element={<ReportsPage />} />
                      <Route path="organizacao/recuperacao" element={<OrgRecoveryPage />} />
                      <Route path="organizacao/comunicados" element={<OrgAnnouncementsPage />} />
                      <Route path="organizacao/ocorrencias" element={<OrgClaimsPage />} />
                      <Route path="organizacao/ocorrencias/:id" element={<OrgClaimPage />} />
                      <Route path="organizacao/vistorias" element={<OrgInspectionsPage />} />
                      <Route path="organizacao/vistorias/:id" element={<OrgInspectionPage />} />
                      <Route path="organizacao/instalacoes" element={<OrgInstallationsPage />} />
                    </Route>

                    <Route element={<RequireStaff />}>
                      <Route path="eventos" element={<EventsPage />} />
                    </Route>
                    {/* Cercas: as do cliente (dele, para os veículos dele) ou as da central. */}
                    <Route path="cercas" element={<FencesRoute />} />

                    <Route element={<RequireOperator />}>
                      <Route path="pedidos" element={<OrdersPage />} />
                      <Route path="agenda" element={<InstallationsAgendaPage />} />
                      <Route path="atendimento" element={SUPPORT_SCREEN ? <SupportPage /> : <Navigate to="/dashboard" replace />} />
                    </Route>

                    <Route element={<RequireAdmin />}>
                      <Route path="dispositivos" element={<DevicesPage />} />
                      <Route path="diagnostico" element={<DiagnosticsPage />} />
                      <Route path="clientes" element={<CustomersPage />} />
                      <Route path="clientes/:id" element={<CustomerDetailsPage />} />
                      <Route path="clientes/:id/relatorios" element={<CustomerReportsPage />} />
                      <Route path="associacoes" element={<OrganizationsPage />} />
                      <Route path="associacoes/:id" element={<OrganizationDetailsPage />} />
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

/** As cercas no mesmo endereço: o cliente vê as dele; a central, as da central. */
function FencesRoute() {
  const { isCustomer, isStaff } = useAuth();
  if (isCustomer) return <CustomerFencesPage />;
  if (isStaff) return <CentralFencesPage />;
  return <Navigate to="/dashboard" replace />;
}

/** Área do cliente: veículos próprios e faturas. */
function RequireCustomer() {
  const { isCustomer } = useAuth();
  if (!isCustomer) return <Navigate to="/dashboard" replace />;
  return <Outlet />;
}

/** Conta empresa: as faturas são do titular e do Gestor com acesso a todos os veículos. */
function RequireAccountOperator() {
  const { canSeeBilling } = useAuth();
  if (!canSeeBilling) return <Navigate to="/dashboard" replace />;
  return <Outlet />;
}

/** O painel da associação ou seguradora. */
function RequireOrgMember() {
  const { isOrgMember } = useAuth();
  if (!isOrgMember) return <Navigate to="/dashboard" replace />;
  return <Outlet />;
}

/** A equipe e as faturas da organização: só o Administrador dela. */
function RequireOrgAdmin() {
  const { isOrgAdmin } = useAuth();
  if (!isOrgAdmin) return <Navigate to="/associados" replace />;
  return <Outlet />;
}

/** Conta empresa: o que é só do titular (a equipe). */
function RequireAccountOwner() {
  const { isAccountOwner } = useAuth();
  if (!isAccountOwner) return <Navigate to="/dashboard" replace />;
  return <Outlet />;
}

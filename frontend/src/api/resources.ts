import type { AssociateImportRow } from '@/services/associateImport';
import type { FleetImportRow } from '@/services/fleetImport';
import type { TeamRole } from '@/services/roles';

import { api, apiUrl, download, fetchBlob, request, tokens } from './client';
import type {
  CancelOrderResult,
  Chip,
  AccountMember,
  AwaitingInstall,
  AwaitingUninstall,
  CheckoutInput,
  CheckoutOffer,
  CheckoutResult,
  OrderOptions,
  BusRoute,
  BusRouteInput,
  BusTrip,
  InstallCheck,
  InstallationAppointment,
  InstallationInput,
  InstallationSlot,
  PublicInstallView,
  InstallerLink,
  InstallerLinkInput,
  PublicInstallerLinkView,
  PublicRouteView,
  RouteDay,
  FleetImportResult,
  MaintenanceItem,
  MaintenanceKind,
  MaintenanceRecord,
  MaintenanceStatus,
  Odometer,
  ReportFrequency,
  ReportSchedule,
  ReportSchedules,
  MemberRole,
  VehicleKind,
  PriceAdjustmentOverview,
  LoginResponse,
  TeamMember,
  TwoFactorEnrollment,
  TwoFactorMethod,
  TwoFactorStatus,
  Affiliate,
  AffiliateClosingLine,
  AffiliateInput,
  AffiliatePayout,
  AffiliateSettings,
  CustomerReferral,
  PartnerReport,
  PixInfo,
  PixPlan,
  PixTransfer,
  ShippingQuoteView,
  TheftView,
  PublicTheftView,
  LiveLink,
  LiveLinksView,
  PublicLiveView,
  InvoiceReminder,
  PublicInvoice,
  ContractDocument,
  ContractStatus,
  SmsSession,
  SmsSetupView,
  PublicAffiliate,
  CashFlow,
  CustomerPaymentMethod,
  CustomerPayments,
  GatewayBalance,
  DreMonth,
  EntryKind,
  FinanceAlerts,
  FinanceAttachment,
  FinanceCategory,
  FinanceEntry,
  FinanceOverview,
  FinanceRecurrence,
  FinanceSettings,
  PaymentMethod,
  StockItem,
  StockMovement,
  StockMoveType,
  Supplier,
  AlertSettings,
  AlertSettingsInput,
  PushStatus,
  AuditEntry,
  ConnectionInfo,
  Catalog,
  CustomerAccount,
  CustomerDetail,
  CustomerSummary,
  CustomerFulfillment,
  DeliveryAddress,
  Fulfillment,
  FulfillmentTrack,
  HistoryRetention,
  DateOnly,
  Device,
  DeviceCommand,
  EngineCutCheck,
  DeviceState,
  Geofence,
  Invoice,
  PixCharge,
  CardPayInput,
  CardView,
  Position,
  PositionHistory,
  ProtocolDescriptor,
  ProvisioningCommand,
  RawPacket,
  ShippingBalance,
  ShippingIntegration,
  ShippingTopUp,
  ShippingTopUpMethod,
  ShippingQuote,
  Subscription,
  Installer,
  PublicInstaller,
  TrackerOrderResult,
  User,
  VehicleShare,
  Vehicle,
  VehicleDirectoryFilter,
  VehicleDirectoryPage,
  VehicleEvent,
  VehicleView,
  ConversationMode,
  WhatsAppConversation,
  WhatsAppConversationDetails,
  WhatsAppMessage,
  WhatsAppStatus,
  Lead,
  LeadInput,
  LeadStatus,
  MRR,
  EngineHistoryItem,
  AuthSession,
  NotificationInbox,
  WaitlistEntry,
  WaitlistInput,
  PromoStatus,
  PromoUsage,
  LandingAnalytics,
  InfraStatus,
  SystemLogPage,
  SmsUsage,
  AccountPlan,
  ReportDailyRow,
  ReportOverspeed,
  ReportSummaryRow,
  ReportUsageRow,
  ReportDrivingRow,
  ReportTrip,
  ReportVehicle,
  FleetSettings,
  VehicleGroup,
  UnknownAttempt,
  Associate,
  AssociateDetail,
  ClosingMonth,
  EquipmentTerms,
  FiscalNote,
  AssociateInput,
  OrgInvoice,
  OrgEstimate,
  OrgMember,
  OrgOverview,
  OrgRecoveries,
  Claim,
  ClaimInput,
  ClaimStatus,
  Inspection,
  InspectionSlot,
  ReferralProgram,
  FleetDriver,
  FleetDriverInput,
  FleetExpense,
  FleetExpenseInput,
  FleetCostRow,
  TrafficFine,
  TrafficFineInput,
  Rental,
  RentalInput,
  JourneyRow,
  ApiKey,
  Webhook,
  OrgAnnouncement,
  OrgAnnouncements,
  OrgRole,
  Organization,
  OrganizationDetail,
  OrganizationInput,
} from '@/types';

// ---------------------------------------------------------------------------
// Autenticação
// ---------------------------------------------------------------------------

export const authApi = {
  // Login e logout são rotas públicas: um 401 no login é senha errada, não
  // sessão vencida. Sem anonymous, o cliente tentaria renovar a sessão e
  // trocaria "e-mail ou senha inválidos" por "sessão expirada".
  login: (email: string, password: string, deviceToken = '') =>
    request<LoginResponse>('/api/auth/login', {
      method: 'POST',
      body: { email, password, deviceToken },
      anonymous: true,
    }),
  logout: (refreshToken: string) =>
    request<void>('/api/auth/logout', { method: 'POST', body: { refreshToken }, anonymous: true }),
  me: () => api.get<User>('/api/auth/me'),
  /** Os aparelhos com a conta aberta. */
  sessions: () => api.get<{ sessions: AuthSession[] }>('/api/auth/sessions'),
  /** Sai dos outros aparelhos; este ganha um token de acesso novo (o antigo é de antes do corte). */
  signOutOthers: async () => {
    const out = await api.post<{ accessToken: string; expiresAt: string; signedOut: number }>('/api/auth/sessions/sign-out-others');
    tokens.saveAccess(out.accessToken);
    return out;
  },

  // Redefinição de senha: rotas públicas, sem sessão (anonymous evita mandar
  // um token velho e disparar a renovação de sessão num 401).
  forgotPassword: (email: string) =>
    request<{ message: string }>('/api/auth/forgot-password', {
      method: 'POST',
      body: { email },
      anonymous: true,
    }),
  validateResetToken: (token: string) =>
    request<void>('/api/auth/reset-password/validate', {
      method: 'POST',
      body: { token },
      anonymous: true,
    }),
  resetPassword: (token: string, password: string) =>
    request<void>('/api/auth/reset-password', {
      method: 'POST',
      body: { token, password },
      anonymous: true,
    }),
};

// ---------------------------------------------------------------------------
// Equipe da central (usuários do painel)
// ---------------------------------------------------------------------------

export interface TeamUserInput {
  email: string;
  name: string;
  role: TeamRole;
  /** Em branco: vai o convite por e-mail para a pessoa criar a senha. */
  password: string;
}

export interface TeamUserUpdate {
  name: string;
  role: TeamRole;
  active: boolean;
}

/** O reajuste anual pelo IPCA (admin): o próximo, os anteriores e o veto. */
export const priceAdjustmentsApi = {
  overview: () => api.get<PriceAdjustmentOverview>('/api/price-adjustments'),
  cancel: (year: number) => api.post<void>(`/api/price-adjustments/${year}/cancel`),
};

/**
 * Verificação em duas etapas. O login (sem sessão ainda): o código, o
 * reenvio e a ativação obrigatória da equipe. A conta (Segurança): ativar,
 * trocar de método, códigos de recuperação, aparelhos e desativar.
 */
export const twoFactorApi = {
  verify: (challenge: string, code: string, trustDevice: boolean) =>
    request<LoginResponse>('/api/auth/2fa/verify', {
      method: 'POST', body: { challenge, code, trustDevice }, anonymous: true,
    }),
  resend: (challenge: string) =>
    request<void>('/api/auth/2fa/resend', { method: 'POST', body: { challenge }, anonymous: true }),
  setupEmail: (challenge: string, code: string) =>
    request<TwoFactorEnrollment>('/api/auth/2fa/setup/email', {
      method: 'POST', body: { challenge, code }, anonymous: true,
    }),
  setupConfirm: (challenge: string, code: string, trustDevice: boolean) =>
    request<LoginResponse>('/api/auth/2fa/setup/confirm', {
      method: 'POST', body: { challenge, code, trustDevice }, anonymous: true,
    }),

  status: () => api.get<TwoFactorStatus>('/api/security/two-factor'),
  start: (method: TwoFactorMethod, password: string) =>
    api.post<TwoFactorEnrollment>('/api/security/two-factor/start', { method, password }),
  confirm: (code: string) =>
    api.post<{ recoveryCodes: string[]; method: TwoFactorMethod }>('/api/security/two-factor/confirm', { code }),
  /** O código por e-mail para confirmar uma mudança (de quem usa o e-mail). */
  sendCode: () => api.post<void>('/api/security/two-factor/code'),
  disable: (password: string, code: string) => api.post<void>('/api/security/two-factor/disable', { password, code }),
  recoveryCodes: (password: string, code: string) =>
    api.post<{ recoveryCodes: string[] }>('/api/security/two-factor/recovery-codes', { password, code }),
  /** Sem id: todos os aparelhos. */
  revokeDevice: (id?: string) => api.delete<void>(`/api/security/two-factor/devices${id ? `/${id}` : ''}`),
  /** Admin: redefine a verificação de alguém da equipe ou de um cliente. */
  resetUser: (id: string) => api.post<void>(`/api/users/${id}/two-factor/reset`),
  resetCustomer: (id: string) => api.post<void>(`/api/customers/${id}/two-factor/reset`),
};

export const usersApi = {
  /** Só a equipe: os clientes ficam em Clientes. */
  list: () => api.get<TeamMember[]>('/api/users'),
  create: (input: TeamUserInput) => api.post<User>('/api/users', input),
  update: (id: string, input: TeamUserUpdate) => api.patch<User>(`/api/users/${id}`, input),
  /** Reenvia o link para criar a senha (o anterior deixa de valer). */
  invite: (id: string) => api.post<{ message: string }>(`/api/users/${id}/invite`),
};

// ---------------------------------------------------------------------------
// Veículos
// ---------------------------------------------------------------------------

export interface VehicleInput {
  name: string;
  /** Carro ou moto; ausente na edição mantém o tipo atual (na criação, carro). */
  kind?: VehicleKind;
  plate?: string;
  brand?: string;
  model?: string;
  year?: number | null;
  color?: string;
  speedLimitKmh?: number | null;
  deviceId?: string | null;
  /** O grupo da conta ("" tira do grupo); ausente, a edição mantém. */
  groupId?: string;
  /** Os campos personalizados; ausentes, a edição mantém. */
  custom1?: string;
  custom2?: string;
}

/** Monta o corpo de edição a partir do veículo atual (a API substitui todos os campos). */
export function vehicleInputFrom(vehicle: Vehicle, changes: Partial<VehicleInput> = {}): VehicleInput {
  return {
    name: vehicle.name,
    kind: vehicle.kind,
    plate: vehicle.plate,
    brand: vehicle.brand,
    model: vehicle.model,
    year: vehicle.year,
    color: vehicle.color,
    speedLimitKmh: vehicle.speedLimitKmh,
    deviceId: vehicle.deviceId,
    groupId: vehicle.groupId ?? '',
    custom1: vehicle.custom1 ?? '',
    custom2: vehicle.custom2 ?? '',
    ...changes,
  };
}

/** A organização da frota da conta: os grupos e os nomes dos campos personalizados. */
export const fleetApi = {
  get: () => api.get<FleetSettings>('/api/me/fleet'),
  createGroup: (name: string) => api.post<VehicleGroup>('/api/me/fleet/groups', { name }),
  renameGroup: (id: string, name: string) => api.patch<void>(`/api/me/fleet/groups/${id}`, { name }),
  deleteGroup: (id: string) => api.delete<void>(`/api/me/fleet/groups/${id}`),
  setLabels: (labels: { label1: string; label2: string }) =>
    api.put<{ label1: string; label2: string }>('/api/me/fleet/labels', labels),
};

export const vehiclesApi = {
  list: () => api.get<VehicleView[]>('/api/vehicles'),
  /** Central (administrador): os veículos de todas as contas, paginados e filtrados. */
  directory: (filter: VehicleDirectoryFilter, page: number, pageSize: number) => {
    const query = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
    for (const [key, value] of Object.entries(filter)) if (value.trim()) query.set(key, value.trim());
    return api.get<VehicleDirectoryPage>(`/api/vehicle-directory?${query}`);
  },
  get: (id: string) => api.get<VehicleView>(`/api/vehicles/${id}`),
  create: (input: VehicleInput) => api.post<Vehicle>('/api/vehicles', input),
  update: (id: string, input: VehicleInput) => api.patch<Vehicle>(`/api/vehicles/${id}`, input),
  /** Admin: exceção do prazo do histórico (nulo segue o cliente). */
  setHistoryRetention: (id: string, days: HistoryRetention | null) =>
    api.put<{ effectiveDays: number }>(`/api/vehicles/${id}/history-retention`, { days }),
  remove: (id: string) => api.delete<void>(`/api/vehicles/${id}`),

  position: (id: string) =>
    api.get<{ position: Position | null; state: DeviceState | null; message?: string }>(
      `/api/vehicles/${id}/position`,
    ),

  positions: (id: string, params: { from: string; to: string; limit?: number; simplify?: boolean }) => {
    const query = new URLSearchParams({ from: params.from, to: params.to });
    if (params.limit) query.set('limit', String(params.limit));
    if (params.simplify === false) query.set('simplify', 'false');
    return api.get<PositionHistory>(`/api/vehicles/${id}/positions?${query}`);
  },

  events: (id: string, params: { from?: string; to?: string; limit?: number } = {}) => {
    const query = new URLSearchParams();
    if (params.from) query.set('from', params.from);
    if (params.to) query.set('to', params.to);
    if (params.limit) query.set('limit', String(params.limit));
    return api.get<VehicleEvent[]>(`/api/vehicles/${id}/events?${query}`);
  },

  /** O rastreio de bloqueio e desbloqueio do veículo. */
  engineHistory: (id: string) => api.get<EngineHistoryItem[]>(`/api/vehicles/${id}/engine-history`),
  commands: (id: string, limit = 50) =>
    api.get<DeviceCommand[]>(`/api/vehicles/${id}/commands?limit=${limit}`),
};

// ---------------------------------------------------------------------------
// Comandos
// ---------------------------------------------------------------------------

export const commandsApi = {
  /** O cliente manda o comprovante da confirmação (biometria ou senha). */
  engineCut: (vehicleId: string, stepUpToken?: string) =>
    request<DeviceCommand>(`/api/vehicles/${vehicleId}/commands/engine-cut`, {
      method: 'POST',
      headers: stepUpToken ? { 'X-Step-Up-Token': stepUpToken } : undefined,
    }),
  engineCutCheck: (vehicleId: string) =>
    api.get<EngineCutCheck>(`/api/vehicles/${vehicleId}/commands/engine-cut/check`),
  /** Desbloquear também pede a confirmação do cliente. */
  engineResume: (vehicleId: string, stepUpToken?: string) =>
    request<DeviceCommand>(`/api/vehicles/${vehicleId}/commands/engine-resume`, {
      method: 'POST',
      headers: stepUpToken ? { 'X-Step-Up-Token': stepUpToken } : undefined,
    }),
  requestPosition: (vehicleId: string) =>
    api.post<DeviceCommand>(`/api/vehicles/${vehicleId}/commands/request-position`),
  requestStatus: (vehicleId: string) =>
    api.post<DeviceCommand>(`/api/vehicles/${vehicleId}/commands/request-status`),
  generic: (vehicleId: string, body: { command: string; params?: Record<string, string>; raw?: string }) =>
    api.post<DeviceCommand>(`/api/vehicles/${vehicleId}/commands`, body),
};

// ---------------------------------------------------------------------------
// Modo roubo
// ---------------------------------------------------------------------------

export const theftApi = {
  get: (vehicleId: string) => api.get<TheftView>(`/api/vehicles/${vehicleId}/theft`),
  /** Liga (sem confirmação extra: na hora do roubo, cada segundo conta). */
  activate: (vehicleId: string) => api.post<TheftView>(`/api/vehicles/${vehicleId}/theft`),
  /** Desliga: só o dono, com a biometria ou a senha. */
  end: (vehicleId: string, outcome: 'RECOVERED' | 'CANCELLED', stepUpToken: string) =>
    request<TheftView>(`/api/vehicles/${vehicleId}/theft/end`, {
      method: 'POST',
      body: { outcome },
      headers: { 'X-Step-Up-Token': stepUpToken },
    }),
  /** O link público (sem login). */
  publicView: (token: string) =>
    request<PublicTheftView>(`/api/public/theft/${encodeURIComponent(token)}`, { anonymous: true }),
};

// ---------------------------------------------------------------------------
// Localização ao vivo por um tempo (o link que o cliente manda)
// ---------------------------------------------------------------------------

export const liveLinksApi = {
  list: (vehicleId: string) => api.get<LiveLinksView>(`/api/vehicles/${vehicleId}/live-links`),
  /** Gera o link (com a biometria ou a senha). */
  create: (vehicleId: string, input: { hours: number; label: string }, stepUpToken: string) =>
    request<LiveLink>(`/api/vehicles/${vehicleId}/live-links`, { method: 'POST', body: input, headers: withStepUp(stepUpToken) }),
  revoke: (vehicleId: string, id: string) => api.delete<void>(`/api/vehicles/${vehicleId}/live-links/${id}`),
  /** O link público (sem login). */
  publicView: (token: string) =>
    request<PublicLiveView>(`/api/public/live/${encodeURIComponent(token)}`, { anonymous: true }),
};

// ---------------------------------------------------------------------------
// Acessos de terceiros (o cliente compartilha um veículo dele)
// ---------------------------------------------------------------------------

export interface VehicleShareInput {
  vehicleId: string;
  name: string;
  email: string;
  canBlock: boolean;
}

const withStepUp = (token?: string) => (token ? { 'X-Step-Up-Token': token } : undefined);

/** A equipe da conta empresa (só o titular). */
export const teamApi = {
  list: () => api.get<AccountMember[]>('/api/me/team'),
  /** Quantas pessoas a equipe pode ter (cresce com a frota) e quantas tem. */
  limit: () => api.get<{ limit: number; used: number }>('/api/me/team/limit'),
  /** Põe alguém na equipe: pede a confirmação extra (biometria ou senha). Groups vazio: todos os veículos. */
  add: (input: { name: string; email: string; role: MemberRole; groups: string[] }, stepUpToken: string) =>
    request<AccountMember>('/api/me/team', { method: 'POST', body: input, headers: withStepUp(stepUpToken) }),
  setRole: (userId: string, role: MemberRole) => api.patch<AccountMember>(`/api/me/team/${userId}`, { role }),
  /** Os veículos que a pessoa vê: com grupos, só os deles; vazio, todos. */
  setGroups: (userId: string, groups: string[]) => api.patch<AccountMember>(`/api/me/team/${userId}`, { groups }),
  remove: (userId: string) => api.delete<void>(`/api/me/team/${userId}`),
};

/** Os dados de quem entra na equipe de uma organização. */
export interface OrgMemberInput {
  name: string;
  email: string;
  role: OrgRole;
}

/** O que a equipe de uma organização faz: no painel dela, ou pela central (sem a confirmação extra). */
export interface OrgTeamApi {
  list: () => Promise<OrgMember[]>;
  add: (input: OrgMemberInput, stepUpToken?: string) => Promise<OrgMember>;
  setRole: (userId: string, role: OrgRole) => Promise<OrgMember>;
  remove: (userId: string) => Promise<OrgMember>;
  invite: (userId: string) => Promise<{ message: string }>;
}

/** O painel da associação ou seguradora (o mapa e os veículos vêm de vehiclesApi). */
export const orgApi = {
  overview: () => api.get<OrgOverview>('/api/org/'),
  associates: () => api.get<Associate[]>('/api/org/associates'),
  announcements: () => api.get<OrgAnnouncements>('/api/org/announcements'),
  /** Manda o comunicado aos associados ativos (no app e, se pedido, por e-mail). */
  announce: (input: { title: string; body: string; email: boolean }) =>
    api.post<OrgAnnouncement>('/api/org/announcements', input),
  /** Os associados (e veículos) por planilha; dryRun só confere. Problemas: 422 com problems (row = posição). */
  importAssociates: (rows: AssociateImportRow[], dryRun: boolean) =>
    request<{ dryRun: boolean; associates: number; vehicles: number }>('/api/org/associates/import', {
      method: 'POST',
      body: { rows, dryRun },
    }),
  claims: (status = '') => api.get<Claim[]>(`/api/org/claims${status ? `?situacao=${status}` : ''}`),
  claim: (id: string) => api.get<Claim>(`/api/org/claims/${id}`),
  setClaimStatus: (id: string, status: ClaimStatus, note: string) =>
    api.post<Claim>(`/api/org/claims/${id}/status`, { status, note }),
  claimNote: (id: string, note: string) => api.post<Claim>(`/api/org/claims/${id}/notes`, { note }),
  claimFileUrl: (id: string, fileId: string) => `/api/org/claims/${id}/files/${fileId}`,
  downloadClaimFile: (id: string, fileId: string) => download(`/api/org/claims/${id}/files/${fileId}`),
  inspections: (status = '') => api.get<Inspection[]>(`/api/org/inspections${status ? `?situacao=${status}` : ''}`),
  inspection: (id: string) => api.get<Inspection>(`/api/org/inspections/${id}`),
  requestInspection: (vehicleId: string, note: string) => api.post<Inspection>('/api/org/inspections', { vehicleId, note }),
  reviewInspection: (id: string, approve: boolean, note: string) =>
    api.post<Inspection>(`/api/org/inspections/${id}/review`, { approve, note }),
  inspectionPhotoUrl: (id: string, slot: InspectionSlot) => `/api/org/inspections/${id}/photos/${slot}`,
  /** O painel de recuperação: os modos roubo dos veículos dos associados. */
  recoveries: (months = 12) => api.get<OrgRecoveries>(`/api/org/recoveries?meses=${months}`),
  associate: (id: string) => api.get<AssociateDetail>(`/api/org/associates/${id}`),
  createAssociate: (input: AssociateInput) => api.post<Associate>('/api/org/associates', input),
  updateAssociate: (id: string, input: AssociateInput) => api.patch<Associate>(`/api/org/associates/${id}`, input),
  inviteAssociate: (id: string) => api.post<{ message: string }>(`/api/org/associates/${id}/invite`),
  /** O veículo do associado e o pedido de instalação (na fila da central). */
  registerVehicle: (associateId: string, input: VehicleInput) =>
    api.post<Vehicle>(`/api/org/associates/${associateId}/vehicles`, input),
  team: {
    list: () => api.get<OrgMember[]>('/api/org/team'),
    /** Pede a confirmação extra (biometria ou senha) de quem põe. */
    add: (input: OrgMemberInput, stepUpToken?: string) =>
      request<OrgMember>('/api/org/team', { method: 'POST', body: input, headers: withStepUp(stepUpToken) }),
    setRole: (userId: string, role: OrgRole) => api.patch<OrgMember>(`/api/org/team/${userId}`, { role }),
    remove: (userId: string) => api.delete<OrgMember>(`/api/org/team/${userId}`),
    invite: (userId: string) => api.post<{ message: string }>(`/api/org/team/${userId}/invite`),
  } satisfies OrgTeamApi,
  invoices: () => api.get<{ invoices: OrgInvoice[]; estimate: OrgEstimate; onlinePayment: boolean }>('/api/org/invoices'),
  /** Pede à central a retirada do rastreador do veículo do associado (o motivo vai junto). */
  requestUninstall: (vehicleId: string, note: string) => api.post<void>(`/api/org/vehicles/${vehicleId}/uninstall`, { note }),
  cancelUninstall: (vehicleId: string) => api.delete<void>(`/api/org/vehicles/${vehicleId}/uninstall`),
  /** As instalações e desinstalações agendadas dos veículos dos associados (de hoje a 7 dias, ou o período). */
  installations: (from?: string, to?: string) =>
    api.get<InstallationAppointment[]>(`/api/org/installations${from && to ? `?de=${from}&ate=${to}` : ''}`),
  /** As notas fiscais de cada mês. */
  fiscalNotes: () => api.get<FiscalNote[]>('/api/org/fiscal-notes'),
  downloadNote: (id: string) => download(`/api/org/fiscal-notes/${id}/file`),
  /** O contrato da janela de pagamento, para as faturas da organização. */
  pix: {
    invoicePix: (id: string) => api.post<PixCharge>(`/api/org/invoices/${id}/pix`),
    invoiceBoleto: (id: string) => api.post<PixCharge>(`/api/org/invoices/${id}/boleto`),
    charge: (id: string) => api.get<PixCharge>(`/api/org/charges/${id}`),
    simulateCharge: (id: string) => api.post<PixCharge>(`/api/org/charges/${id}/simulate`),
  } satisfies PixApi,
};

/** O fechamento do mês e as notas fiscais (central). */
export const closingApi = {
  /** O mês (AAAA-MM); sem ele, o mês passado. */
  month: (month?: string) => api.get<ClosingMonth>(`/api/closing/${month ? `?mes=${encodeURIComponent(month)}` : ''}`),
  exportCsv: (month: string) => download(`/api/closing/export.csv?mes=${encodeURIComponent(month)}`),
  /** Registra (ou troca) a nota; sem arquivo, fica o que já tinha. */
  saveNote: (input: { customerId: string; month: string; number: string; issuedOn: string; amountCents: number; file?: File | null }) => {
    const form = new FormData();
    form.append('customerId', input.customerId);
    form.append('month', input.month);
    form.append('number', input.number);
    form.append('issuedOn', input.issuedOn);
    form.append('amountCents', String(input.amountCents));
    if (input.file) form.append('file', input.file);
    return request<FiscalNote>('/api/closing/notes', { method: 'POST', body: form });
  },
  removeNote: (id: string) => api.delete<void>(`/api/closing/notes/${id}`),
  downloadNote: (id: string) => download(`/api/closing/notes/${id}/file`),
};

/** As associações e seguradoras, pela central. */
export const organizationsApi = {
  list: () => api.get<Organization[]>('/api/organizations'),
  create: (input: OrganizationInput & { admin?: { name: string; email: string } }) =>
    api.post<{ organization: Organization; adminError?: string }>('/api/organizations', input),
  get: (id: string) => api.get<OrganizationDetail>(`/api/organizations/${id}`),
  update: (id: string, input: OrganizationInput) => api.patch<Organization>(`/api/organizations/${id}`, input),
  team: (id: string): OrgTeamApi => ({
    list: async () => (await api.get<OrganizationDetail>(`/api/organizations/${id}`)).memberList,
    add: (input: OrgMemberInput) => api.post<OrgMember>(`/api/organizations/${id}/members`, input),
    setRole: (userId: string, role: OrgRole) => api.patch<OrgMember>(`/api/organizations/${id}/members/${userId}`, { role }),
    remove: (userId: string) => api.delete<OrgMember>(`/api/organizations/${id}/members/${userId}`),
    invite: (userId: string) => api.post<{ message: string }>(`/api/organizations/${id}/members/${userId}/invite`),
  }),
  createAssociate: (id: string, input: AssociateInput) => api.post<Associate>(`/api/organizations/${id}/associates`, input),
  /** Os associados (e veículos) por planilha, dentro da organização; dryRun só confere. */
  importAssociates: (id: string, rows: AssociateImportRow[], dryRun: boolean) =>
    request<{ dryRun: boolean; associates: number; vehicles: number }>(`/api/organizations/${id}/associates/import`, {
      method: 'POST',
      body: { rows, dryRun },
    }),
  registerVehicle: (id: string, associateId: string, input: VehicleInput) =>
    api.post<Vehicle>(`/api/organizations/${id}/associates/${associateId}/vehicles`, input),
  /** Lança já a fatura do próximo vencimento. */
  generateInvoice: (id: string) => api.post<OrgInvoice>(`/api/organizations/${id}/invoices/generate`),
};

/** A contratação rápida pelo site (sem login): a oferta, o frete até o CEP e o pedido com Pix. */
export const checkoutApi = {
  offer: () => request<CheckoutOffer>('/api/public/checkout', { anonymous: true }),
  shipping: (address: DeliveryAddress) =>
    request<ShippingQuoteView>('/api/public/checkout/shipping', { method: 'POST', body: address, anonymous: true }),
  place: (input: CheckoutInput) =>
    request<CheckoutResult>('/api/public/checkout', { method: 'POST', body: input, anonymous: true }),
};

/** A agenda de instalação (central). */
export const installationsApi = {
  /** De hoje a 7 dias, ou o período (AAAA-MM-DD). */
  list: (from?: string, to?: string) =>
    api.get<InstallationAppointment[]>(`/api/installations${from && to ? `?de=${from}&ate=${to}` : ''}`),
  awaiting: () => api.get<AwaitingInstall[]>('/api/installations/awaiting'),
  /** Os veículos de associado à espera da retirada do rastreador (pedido da associação ou associado desligado). */
  awaitingUninstall: () => api.get<AwaitingUninstall[]>('/api/installations/awaiting-uninstall'),
  create: (input: InstallationInput) => api.post<InstallationAppointment>('/api/installations', input),
  reschedule: (id: string, input: InstallationInput) => api.patch<InstallationAppointment>(`/api/installations/${id}`, input),
  /** Cancela ou marca que o cliente não estava. */
  close: (id: string, status: 'canceled' | 'no_show') =>
    api.post<InstallationAppointment>(`/api/installations/${id}/close`, { status }),
  photoUrl: (id: string, slot: InstallationSlot) => `/api/installations/${id}/photos/${slot}`,
};

/** O link do instalador (sem login). */
export const installLinkApi = {
  view: (token: string) => request<PublicInstallView>(`/api/public/install/${encodeURIComponent(token)}`, { anonymous: true }),
  arrive: (token: string) =>
    request<PublicInstallView>(`/api/public/install/${encodeURIComponent(token)}/arrive`, { method: 'POST', anonymous: true }),
  check: (token: string, imei: string) =>
    request<InstallCheck>(`/api/public/install/${encodeURIComponent(token)}/check`, {
      method: 'POST',
      body: { imei },
      anonymous: true,
    }),
  savePhoto: (token: string, slot: InstallationSlot, file: Blob) => {
    const form = new FormData();
    form.append('file', file, `${slot}.jpg`);
    return request<void>(`/api/public/install/${encodeURIComponent(token)}/photos/${slot}`, {
      method: 'PUT',
      body: form,
      anonymous: true,
    });
  },
  photoUrl: (token: string, slot: InstallationSlot, version = '') =>
    apiUrl(`/api/public/install/${encodeURIComponent(token)}/photos/${slot}${version ? `?v=${version}` : ''}`),
  complete: (token: string, imei: string, note: string) =>
    request<PublicInstallView>(`/api/public/install/${encodeURIComponent(token)}/complete`, {
      method: 'POST',
      body: { imei, note },
      anonymous: true,
    }),
};

/** A instalação avulsa: o link do rastreador escolhido, sem agendamento (admin e operador). */
export const installerLinksApi = {
  list: () => api.get<InstallerLink[]>('/api/installer-links'),
  create: (input: InstallerLinkInput) => api.post<InstallerLink>('/api/installer-links', input),
  /** Encerra o link: o instalador não abre mais. */
  close: (id: string) => api.post<InstallerLink>(`/api/installer-links/${id}/close`),
};

/** O link da instalação avulsa (sem login): o teste do sinal e a conclusão. */
export const installerLinkPublicApi = {
  view: (token: string) =>
    request<PublicInstallerLinkView>(`/api/public/installer-links/${encodeURIComponent(token)}`, { anonymous: true }),
  check: (token: string) =>
    request<InstallCheck>(`/api/public/installer-links/${encodeURIComponent(token)}/check`, { method: 'POST', anonymous: true }),
  complete: (token: string, note: string) =>
    request<PublicInstallerLinkView>(`/api/public/installer-links/${encodeURIComponent(token)}/complete`, {
      method: 'POST',
      body: { note },
      anonymous: true,
    }),
};

/** Os itinerários da conta (as linhas de ônibus) e o link do passageiro. */
export const busRoutesApi = {
  list: () => api.get<BusRoute[]>('/api/me/routes'),
  get: (id: string) => api.get<BusRoute>(`/api/me/routes/${id}`),
  /** As viagens do dia (AAAA-MM-DD; sem ele, hoje) com o progresso de cada uma. */
  day: (id: string, day?: string) => api.get<RouteDay>(`/api/me/routes/${id}/day${day ? `?dia=${day}` : ''}`),
  save: (id: string | null, input: BusRouteInput) =>
    id ? api.put<BusRoute>(`/api/me/routes/${id}`, input) : api.post<BusRoute>('/api/me/routes', input),
  remove: (id: string) => api.delete<void>(`/api/me/routes/${id}`),
  /** O veículo na linha na hora pedida (e nos dias seguintes, com days). */
  createTrips: (id: string, input: { vehicleId: string; departsAt: string; days: number }) =>
    api.post<BusTrip[]>(`/api/me/routes/${id}/trips`, input),
  cancelTrip: (tripId: string) => api.post<void>(`/api/me/trips/${tripId}/cancel`),
  publicView: (token: string) =>
    request<PublicRouteView>(`/api/public/routes/${encodeURIComponent(token)}`, { anonymous: true }),
};

export const sharesApi = {
  /** Os acessos que o cliente deu, de todos os veículos dele. */
  list: () => api.get<VehicleShare[]>('/api/me/shares'),
  /** Pede a confirmação (biometria ou senha) do dono. */
  create: (input: VehicleShareInput, stepUpToken: string) =>
    request<VehicleShare>('/api/me/shares', { method: 'POST', body: input, headers: withStepUp(stepUpToken) }),
  /** Liberar o bloqueio pede a confirmação; tirar, não. */
  setCanBlock: (id: string, canBlock: boolean, stepUpToken?: string) =>
    request<VehicleShare>(`/api/me/shares/${id}`, { method: 'PATCH', body: { canBlock }, headers: withStepUp(stepUpToken) }),
  /** O dono tira o acesso, ou quem recebeu deixa de acompanhar. */
  remove: (id: string) => api.delete<void>(`/api/me/shares/${id}`),
};

// ---------------------------------------------------------------------------
// Dispositivos
// ---------------------------------------------------------------------------

/**
 * Cadastro/edição de rastreador. As senhas são só de escrita: na edição,
 * senha vazia (ou ausente) mantém a atual e `clear*` apaga. Sem
 * `commandOverrides` os textos atuais ficam como estão.
 */
export interface DeviceInput {
  imei: string;
  /** Conferido na etiqueta: o IMEI suspeito (16 dígitos, verificador que não bate) está certo. */
  imeiConfirmed?: boolean;
  model?: string;
  manufacturer?: string;
  protocol?: string;
  firmware?: string;
  phoneNumber?: string;
  /** O ICCID do chip; o servidor guarda só os dígitos (a marcação "SP" da etiqueta sai). */
  iccid?: string;
  apn?: string;
  apnUser?: string;
  apnPassword?: string;
  clearApnPassword?: boolean;
  serverHost?: string;
  serverPort?: number | null;
  reportIntervalSeconds?: number | null;
  heartbeatIntervalSeconds?: number | null;
  commandPassword?: string;
  clearCommandPassword?: boolean;
  commandOverrides?: Record<string, string>;
  notes?: string;
  /** O chip escolhido no estoque: a linha, o ICCID, o APN e a senha vêm dele. */
  chipId?: string | null;
}

/** O cadastro de um chip do estoque. A senha do APN é só de escrita (em branco mantém). */
export interface ChipInput {
  iccid: string;
  phoneNumber: string;
  carrier: string;
  apn: string;
  apnUser: string;
  apnPassword?: string;
  clearApnPassword?: boolean;
  notes: string;
}

/** A importação de um lote de chips: os recusados voltam com a posição na lista. */
export interface ChipBulkResult {
  total: number;
  created: number;
  errors: { index: number; iccid: string; error: string }[];
}

/** O estoque de chips (administrador). */
export const chipsApi = {
  list: () => api.get<Chip[]>('/api/chips'),
  create: (input: ChipInput) => api.post<Chip>('/api/chips', input),
  createBulk: (chips: ChipInput[]) => api.post<ChipBulkResult>('/api/chips/bulk', { chips }),
  update: (id: string, input: ChipInput) => api.patch<Chip>(`/api/chips/${id}`, input),
  remove: (id: string) => api.delete<void>(`/api/chips/${id}`),
};

/**
 * Formulário de edição a partir do rastreador lido. Copia campo a campo só o
 * que é editável: as senhas começam vazias (a API não as devolve; vazio
 * mantém a atual) e os overrides ficam de fora (ausente mantém os atuais).
 */
export function deviceInputFrom(device: Device): DeviceInput {
  return {
    imei: device.imei,
    model: device.model,
    manufacturer: device.manufacturer,
    protocol: device.protocol,
    firmware: device.firmware,
    phoneNumber: device.phoneNumber,
    iccid: device.iccid,
    apn: device.apn,
    apnUser: device.apnUser,
    apnPassword: '',
    serverHost: device.serverHost,
    serverPort: device.serverPort,
    reportIntervalSeconds: device.reportIntervalSeconds,
    heartbeatIntervalSeconds: device.heartbeatIntervalSeconds,
    commandPassword: '',
    notes: device.notes,
  };
}

export interface DeviceBulkError {
  index: number;
  imei: string;
  error: string;
}

export interface DeviceBulkResult {
  total: number;
  created: number;
  devices: Device[];
  errors: DeviceBulkError[];
}

export const devicesApi = {
  list: () => api.get<Device[]>('/api/devices'),
  get: (id: string) => api.get<Device>(`/api/devices/${id}`),
  create: (input: DeviceInput) => api.post<Device>('/api/devices', input),
  createBulk: (devices: DeviceInput[]) =>
    api.post<DeviceBulkResult>('/api/devices/bulk', { devices }),
  update: (id: string, input: DeviceInput) => api.patch<Device>(`/api/devices/${id}`, input),
  remove: (id: string) => api.delete<void>(`/api/devices/${id}`),
  /** Os rastreadores que tentaram conectar sem cadastro nos últimos dias. */
  unknown: () => api.get<UnknownAttempt[]>('/api/devices/unknown'),
  status: (id: string) =>
    api.get<{
      deviceId: string;
      status: string;
      lastSeenAt: string | null;
      protocol: string;
      connected: boolean;
      state: DeviceState | null;
      connection?: ConnectionInfo;
      lastPosition?: Position;
    }>(`/api/devices/${id}/status`),
  provisioning: (id: string) =>
    api.get<{ device: Device; commands: ProvisioningCommand[]; warning: string }>(
      `/api/devices/${id}/provisioning`,
    ),
};

// ---------------------------------------------------------------------------
// Cercas, eventos e diagnóstico
// ---------------------------------------------------------------------------

export interface GeofenceInput {
  name: string;
  shape?: 'circle' | 'polygon';
  latitude: number;
  longitude: number;
  radiusMeters: number;
  polygon?: [number, number][] | null;
  scheduleDays?: number[];
  scheduleStart?: number | null;
  scheduleEnd?: number | null;
  active?: boolean;
  /** Cerca do cliente: os veículos dele que ela vigia (ao menos um). */
  vehicleIds?: string[];
  notifyEnter?: boolean;
  notifyExit?: boolean;
}

export const geofencesApi = {
  list: () => api.get<Geofence[]>('/api/geofences'),
  /** Quantas cercas a conta pode ter (cresce com a frota) e quantas tem; null: sem limite (a central). */
  limit: () => api.get<{ limit: number | null; used: number }>('/api/geofences/limit'),
  create: (input: GeofenceInput) => api.post<Geofence>('/api/geofences', input),
  update: (id: string, input: GeofenceInput) => api.patch<Geofence>(`/api/geofences/${id}`, input),
  remove: (id: string) => api.delete<void>(`/api/geofences/${id}`),
};

/** O período e o veículo de um relatório (dias AAAA-MM-DD). */
export interface ReportQuery {
  from: string;
  to: string;
  vehicleId?: string;
  /** Só os veículos de um grupo da conta. */
  groupId?: string;
  /** A central vendo os relatórios de um cliente. */
  customerId?: string;
  /** A associação vendo os de um associado só. */
  associateId?: string;
}

function reportPath(kind: string, q: ReportQuery, csv = false): string {
  const params = new URLSearchParams({ de: q.from, ate: q.to });
  if (q.vehicleId) params.set('veiculo', q.vehicleId);
  if (q.groupId) params.set('grupo', q.groupId);
  if (q.customerId) params.set('cliente', q.customerId);
  if (q.associateId) params.set('associado', q.associateId);
  if (csv) params.set('formato', 'csv');
  return `/api/reports/${kind}?${params}`;
}

export type ReportKind = 'summary' | 'usage' | 'driving' | 'journey' | 'daily' | 'trips' | 'overspeed';

/** Os relatórios da frota da conta; download baixa a planilha (CSV). */
export const reportsApi = {
  summary: (q: ReportQuery) => api.get<{ from: string; to: string; rows: ReportSummaryRow[] }>(reportPath('summary', q)),
  usage: (q: ReportQuery) => api.get<{ from: string; to: string; rows: ReportUsageRow[] }>(reportPath('usage', q)),
  driving: (q: ReportQuery) =>
    api.get<{ from: string; to: string; rows: ReportDrivingRow[]; minScoreKm: number }>(reportPath('driving', q)),
  journey: (q: ReportQuery, kind: 'carga' | 'passageiros') =>
    api.get<{ from: string; to: string; vehicles: ReportVehicle[]; rows: JourneyRow[]; continuousLimitSeconds: number }>(
      `${reportPath('journey', q)}&tipo=${kind}`,
    ),
  costs: (q: ReportQuery) =>
    api.get<{ from: string; to: string; vehicles: ReportVehicle[]; rows: FleetCostRow[] }>(reportPath('costs' as ReportKind, q)),
  daily: (q: ReportQuery) =>
    api.get<{ from: string; to: string; vehicles: ReportVehicle[]; rows: ReportDailyRow[] }>(reportPath('daily', q)),
  trips: (q: ReportQuery) =>
    api.get<{ from: string; to: string; vehicle: ReportVehicle; trips: ReportTrip[]; availableFrom: string }>(
      reportPath('trips', q),
    ),
  overspeed: (q: ReportQuery) =>
    api.get<{ from: string; to: string; vehicles: ReportVehicle[]; rows: ReportOverspeed[]; truncated: boolean }>(
      reportPath('overspeed', q),
    ),
  download: (kind: ReportKind, q: ReportQuery) => download(reportPath(kind, q, true)),
};

/** O hodômetro, o horímetro e a manutenção dos veículos. */
export const maintenanceApi = {
  /** As manutenções da frota (os veículos que a pessoa vê), das mais urgentes. */
  list: () => api.get<{ items: MaintenanceItem[]; counts: Partial<Record<MaintenanceStatus, number>> }>('/api/me/maintenance'),
  create: (input: MaintenanceInput & { vehicleIds: string[] }) => api.post<{ created: number }>('/api/me/maintenance', input),
  update: (id: string, input: MaintenanceInput) => api.patch<void>(`/api/me/maintenance/${id}`, input),
  remove: (id: string) => api.delete<void>(`/api/me/maintenance/${id}`),
  /** Registra feitas: com uma, dá para informar o km e as horas; com várias, vale o hodômetro e o horímetro de cada veículo. */
  done: (input: { itemIds: string[]; date?: string; km?: number | null; hours?: number | null; notes?: string }) =>
    api.post<{ done: number }>('/api/me/maintenance/done', input),
  vehicle: (vehicleId: string) =>
    api.get<{ odometer: Odometer; items: MaintenanceItem[]; records: MaintenanceRecord[] }>(`/api/vehicles/${vehicleId}/maintenance`),
  setOdometer: (vehicleId: string, km: number) => api.put<Odometer>(`/api/vehicles/${vehicleId}/odometer`, { km }),
  setEngineHours: (vehicleId: string, hours: number) => api.put<Odometer>(`/api/vehicles/${vehicleId}/engine-hours`, { hours }),
};

export interface MaintenanceInput {
  /** Sem ele, manutenção. */
  kind?: MaintenanceKind;
  name: string;
  /** Documento: o próximo vencimento (no lugar da última vez). */
  dueDate?: string;
  everyKm: number | null;
  everyMonths: number | null;
  /** Horas de motor. */
  everyHours: number | null;
  /** Vazio: hoje. */
  lastDate?: string;
  /** Só com um veículo; sem ele, o hodômetro de agora. */
  lastKm?: number | null;
  /** Só com um veículo; sem elas, o horímetro de agora. */
  lastHours?: number | null;
}

/** O relatório da frota por e-mail: os agendamentos de quem está logado. */
export const reportSchedulesApi = {
  list: () => api.get<ReportSchedules>('/api/me/report-schedules'),
  create: (input: ReportScheduleInput) => api.post<ReportSchedule>('/api/me/report-schedules', input),
  remove: (id: string) => api.delete<void>(`/api/me/report-schedules/${id}`),
};

export interface ReportScheduleInput {
  frequency: ReportFrequency;
  /** Só um grupo da conta (ausente: a conta toda). */
  groupId?: string | null;
  extraRecipients: string[];
}

export const eventsApi = {
  recent: (limit = 100) => api.get<VehicleEvent[]>(`/api/events?limit=${limit}`),
};

export const geocodingApi = {
  reverse: (lat: number, lon: number) =>
    api.get<{ address: string }>(`/api/geocoding/reverse?lat=${lat}&lon=${lon}`),
};

export const diagnosticsApi = {
  protocols: () => api.get<ProtocolDescriptor[]>('/api/protocols'),
  connections: () =>
    api.get<{ count: number; connections: ConnectionInfo[] }>('/api/diagnostics/connections'),
  rawPackets: (limit = 100) =>
    api.get<{ packets: RawPacket[]; hint: string }>(`/api/diagnostics/raw-packets?limit=${limit}`),
  auditLogs: (limit = 100) => api.get<AuditEntry[]>(`/api/diagnostics/audit-logs?limit=${limit}`),
};

// ---------------------------------------------------------------------------
// Clientes, assinaturas e faturas
// ---------------------------------------------------------------------------

export interface SubscriptionInput {
  planName: string;
  priceCents: number;
  dueDay: number;
  firstDueDate?: DateOnly | null;
}

export interface CustomerInput {
  name: string;
  email: string;
  phone: string;
  document: string;
  /** Vazio manda o convite por e-mail para o cliente criar a senha. */
  password: string;
  /** Pré-cliente de onde veio o cadastro: passa a convertido. */
  leadId?: string;
}

export interface InvoiceInput {
  description: string;
  amountCents: number;
  dueDate: DateOnly;
  paymentUrl: string;
  pixCode: string;
}

/** O saldo do rastreador parcelado no encerramento: cobrar numa fatura só ou dispensar. */
export type EquipmentBalance = 'CHARGE' | 'WAIVE';

/** Administração (perfil admin). */
export const customersApi = {
  /** O MRR de hoje e dos últimos meses. */
  mrr: () => api.get<MRR>('/api/customers/mrr'),
  /** A equipe da conta empresa do cliente (só leitura, para a central). */
  team: (id: string) => api.get<AccountMember[]>(`/api/customers/${id}/team`),
  /** A organização da frota do cliente: grupos e campos personalizados. */
  fleet: (id: string) => api.get<FleetSettings>(`/api/customers/${id}/fleet`),
  list: () => api.get<CustomerSummary[]>('/api/customers'),
  /** Alertas do cliente: o que ele escolheu e o que foi enviado. */
  alerts: (id: string) => api.get<AlertSettings>(`/api/customers/${id}/alerts`),
  saveAlerts: (id: string, input: AlertSettingsInput) =>
    api.put<AlertSettings>(`/api/customers/${id}/alerts`, input),
  get: (id: string) => api.get<CustomerDetail>(`/api/customers/${id}`),
  create: (input: CustomerInput) => api.post<CustomerDetail>('/api/customers', input),
  update: (id: string, input: { name: string; phone: string; document: string; active: boolean }) =>
    api.patch<CustomerDetail>(`/api/customers/${id}`, input),
  invite: (id: string) => api.post<{ message: string }>(`/api/customers/${id}/invite`),
  /** O link do convite, para mandar sem o e-mail; derruba o anterior. */
  inviteLink: (id: string) => api.post<{ url: string; expiresAt: string }>(`/api/customers/${id}/invite-link`),
  /** Os aparelhos com a conta do cliente aberta. */
  sessions: (id: string) => api.get<{ sessions: AuthSession[] }>(`/api/customers/${id}/sessions`),
  /** Desconecta todas as sessões do cliente (na hora). */
  signOut: (id: string) => api.post<{ signedOut: number }>(`/api/customers/${id}/sessions/sign-out`),
  /** A senha nova definida pela central (com a confirmação de quem faz): as sessões dele caem. */
  setPassword: (id: string, password: string, stepUpToken: string) =>
    request<void>(`/api/customers/${id}/password`, { method: 'POST', body: { password }, headers: withStepUp(stepUpToken) }),
  saveAddress: (id: string, input: DeliveryAddress) =>
    api.put<DeliveryAddress>(`/api/customers/${id}/address`, input),
  /** Prazo do histórico de todos os veículos do cliente (nulo = padrão da central). */
  setHistoryRetention: (id: string, days: HistoryRetention | null) =>
    api.put<{ effectiveDays: number }>(`/api/customers/${id}/history-retention`, { days }),
  /** Quem indicou o cliente (null tira a indicação). */
  /** Define o plano do cliente: vale para todo veículo novo (as assinaturas atuais não mudam). */
  setPlan: (id: string, input: SubscriptionInput & Partial<EquipmentTerms>) => api.put<AccountPlan>(`/api/customers/${id}/plan`, input),
  /** Volta o cliente ao plano padrão (o da assinatura ativa, ou o do catálogo). */
  clearPlan: (id: string) => api.delete<void>(`/api/customers/${id}/plan`),
  /** A associação ou seguradora que paga o rastreamento do cliente (null: nenhuma). */
  setOrganization: (id: string, organizationId: string | null) =>
    api.put<{ organization: Organization | null }>(`/api/customers/${id}/organization`, { organizationId }),
  setAffiliate: (id: string, affiliateId: string | null) =>
    api.put<{ affiliate: CustomerReferral | null }>(`/api/customers/${id}/affiliate`, { affiliateId }),

  updateSubscription: (id: string, input: { planName: string; priceCents: number }) =>
    api.patch<Subscription>(`/api/subscriptions/${id}`, input),
  /**
   * Encerra a assinatura. Com o rastreador parcelado e parcelas por pagar, diz
   * o que fazer com o saldo: CHARGE (uma fatura só) ou WAIVE (dispensar).
   */
  cancelSubscription: (id: string, equipmentBalance?: EquipmentBalance, waiveUninstall = false) =>
    api.post<Subscription & { balanceInvoice: Invoice | null; uninstallInvoice: Invoice | null }>(
      `/api/subscriptions/${id}/cancel`,
      equipmentBalance || waiveUninstall
        ? { ...(equipmentBalance ? { equipmentBalance } : {}), ...(waiveUninstall ? { waiveUninstall } : {}) }
        : undefined,
    ),

  createInvoice: (customerId: string, input: InvoiceInput) =>
    api.post<Invoice>(`/api/customers/${customerId}/invoices`, input),
  updateInvoice: (id: string, input: { paymentUrl: string; pixCode: string }) =>
    api.patch<Invoice>(`/api/invoices/${id}`, input),
  payInvoice: (id: string) => api.post<Invoice>(`/api/invoices/${id}/pay`),
  /** Manda agora um lembrete da fatura (e-mail e push, com o link de pagamento). */
  remindInvoice: (id: string) => api.post<InvoiceReminder>(`/api/invoices/${id}/remind`),
  cancelInvoice: (id: string) => api.post<Invoice>(`/api/invoices/${id}/cancel`),
  /** Muda o vencimento de uma fatura em aberto (hoje ou depois). */
  changeInvoiceDueDate: (id: string, dueDate: string) => api.post<Invoice>(`/api/invoices/${id}/due-date`, { dueDate }),
  /**
   * Parcela o rastreador de uma fatura avulsa ainda não paga (pedido feito à
   * vista): equipmentCents é a parte da fatura que é o rastreador, que vai
   * para as mensalidades; o resto (frete) fica na fatura.
   */
  financeInvoice: (id: string, input: { subscriptionId: string; installments: number; equipmentCents: number }) =>
    api.post<{ invoice: Invoice; subscription: Subscription }>(`/api/invoices/${id}/installments`, input),
  /** Muda o dia de vencimento da assinatura (as faturas em aberto não vencidas vão junto). */
  changeDueDay: (subscriptionId: string, dueDay: number) =>
    api.post<Subscription>(`/api/subscriptions/${subscriptionId}/due-day`, { dueDay }),

  invoicePix: (invoiceId: string) => api.post<PixCharge>(`/api/invoices/${invoiceId}/pix`),
  invoiceBoleto: (invoiceId: string) => api.post<PixCharge>(`/api/invoices/${invoiceId}/boleto`),
  charge: (id: string) => api.get<PixCharge>(`/api/charges/${id}`),
  /** O cartão salvo do cliente: a central só desliga o débito automático ou apaga. */
  disableAutoDebit: (customerId: string) => api.put<unknown>(`/api/customers/${customerId}/card/auto-debit`, { enabled: false }),
  removeCard: (customerId: string) => api.delete<void>(`/api/customers/${customerId}/card`),
  simulateCharge: (id: string) => api.post<PixCharge>(`/api/charges/${id}/simulate`),
  refundCharge: (id: string, reason: string) =>
    api.post<PixCharge>(`/api/charges/${id}/refund`, { reason }),

  /** Novo veículo: veículo → rastreador (fatura do equipamento) → assinatura. */
  orderTracker: (customerId: string, input: AdminTrackerOrder) =>
    api.post<TrackerOrderResult>(`/api/customers/${customerId}/trackers`, input),
  /** A frota por planilha: cada linha vira um veículo com a assinatura; o equipamento, uma fatura só. */
  importFleet: (customerId: string, input: FleetImportRequest) =>
    api.post<FleetImportResult>(`/api/customers/${customerId}/trackers/import`, input),
  /** As formas de entrega até o endereço do cliente (pedido pela central). */
  shippingQuote: (customerId: string) => api.get<ShippingQuoteView>(`/api/customers/${customerId}/shipping-quote`),
  /** Se o cliente pode contratar com a promoção de pré-lançamento. */
  launchPromo: (id: string) => api.get<PromoStatus>(`/api/customers/${id}/launch-promo`),
  /** Libera a promoção de pré-lançamento para quem não está na lista (vale como se estivesse). */
  grantLaunchPromo: (id: string) => api.post<PromoStatus>(`/api/customers/${id}/launch-promo/grant`),
  /** Retira a liberação (o que já foi contratado com a promoção continua). */
  revokeLaunchPromo: (id: string) => api.delete<PromoStatus>(`/api/customers/${id}/launch-promo/grant`),
  /** Assinatura antiga sem veículo: informa qual veículo ela cobre. */
  attachVehicle: (customerId: string, subscriptionId: string, input: VehicleInput) =>
    api.post<Vehicle>(`/api/customers/${customerId}/subscriptions/${subscriptionId}/vehicle`, input),
  /** Nova assinatura para um veículo que ficou sem nenhuma ativa. */
  reactivate: (customerId: string, vehicleId: string, plan: SubscriptionInput) =>
    api.post<Subscription>(`/api/customers/${customerId}/vehicles/${vehicleId}/subscription`, plan),
};

export interface AdminTrackerOrder {
  vehicle: VehicleInput;
  /** Zero não cobra (aparelho do cliente, cortesia, comodato). */
  equipmentCents: number;
  plan: SubscriptionInput;
  /** Aplica a promoção de pré-lançamento (o cliente precisa ter direito). */
  launchPromo?: boolean;
  /** O frete cobrado do cliente; 0 ou ausente: sem frete (entrega em mãos). */
  shippingServiceId?: number;
  /** O equipamento parcelado sem juros (ausente ou 1: à vista). */
  installments?: number;
  /** O rastreador comprado instalado pelos instaladores da Farbo, no preço das opções do pedido. */
  farboInstall?: boolean;
}

export interface FleetImportRequest {
  rows: FleetImportRow[];
  /** O valor de cada aparelho; zero não cobra. */
  equipmentCents: number;
  plan: SubscriptionInput;
  /** Só confere: nada é gravado. */
  dryRun?: boolean;
}

export interface InstallerInput {
  name: string;
  city: string;
  serviceArea: string;
  whatsapp: string;
  servesMoto: boolean;
  servesCar: boolean;
  priceMotoCents: number | null;
  priceCarCents: number | null;
  description: string;
  active: boolean;
}

/** Prestadores de instalação (perfil admin). */
export const installersApi = {
  list: () => api.get<Installer[]>('/api/installers'),
  create: (input: InstallerInput) => api.post<Installer>('/api/installers', input),
  update: (id: string, input: InstallerInput) => api.patch<Installer>(`/api/installers/${id}`, input),
  remove: (id: string) => api.delete<void>(`/api/installers/${id}`),
};

/** Pedidos: o chip M2M e o rastreador até o cliente (admin e operador). */
export const fulfillmentsApi = {
  list: (all = false) => api.get<Fulfillment[]>(`/api/fulfillments${all ? '?status=all' : ''}`),
  get: (id: string) => api.get<Fulfillment>(`/api/fulfillments/${id}`),
  change: (
    id: string,
    input: { track: FulfillmentTrack; status: string; note?: string; deviceId?: string | null },
  ) => api.post<Fulfillment>(`/api/fulfillments/${id}/status`, input),
  sync: (id: string) => api.post<Fulfillment>(`/api/fulfillments/${id}/shipping/sync`),
  /** Admin: cotação e compra da etiqueta (debita o saldo do Melhor Envios). */
  quote: (id: string) => api.post<ShippingQuote[]>(`/api/fulfillments/${id}/shipping/quote`),
  buyLabel: (id: string, serviceId: number) =>
    api.post<Fulfillment>(`/api/fulfillments/${id}/shipping/label`, { serviceId }),
  /** A etiqueta comprada, em PDF (409 LABEL_NOT_PDF com o link, se vier a página de impressão). */
  downloadLabel: (id: string) => download(`/api/fulfillments/${id}/shipping/label.pdf`),
  /** Admin: cancela o pedido antes do envio (o veículo sai da conta). */
  cancel: (id: string, reason: string) => api.post<CancelOrderResult>(`/api/fulfillments/${id}/cancel`, { reason }),
};

/** Atendimento pelo WhatsApp: conversas da IA e da equipe (admin e operador). */
export const whatsappApi = {
  status: () => api.get<WhatsAppStatus>('/api/whatsapp'),
  list: (attention = false) =>
    api.get<WhatsAppConversation[]>(`/api/whatsapp/conversations${attention ? '?filter=attention' : ''}`),
  get: (id: string) => api.get<WhatsAppConversationDetails>(`/api/whatsapp/conversations/${id}`),
  /** Responder assume a conversa: a IA para de responder. */
  send: (id: string, text: string) => api.post<WhatsAppMessage>(`/api/whatsapp/conversations/${id}/messages`, { text }),
  setMode: (id: string, mode: ConversationMode) =>
    api.put<WhatsAppConversation>(`/api/whatsapp/conversations/${id}/mode`, { mode }),
};

/** Conexão com o Melhor Envios (admin). */
export const shippingIntegrationApi = {
  get: () => api.get<ShippingIntegration>('/api/integrations/melhorenvio'),
  connect: () => api.post<{ url: string }>('/api/integrations/melhorenvio/connect'),
  disconnect: () => api.post<void>('/api/integrations/melhorenvio/disconnect'),
  /** O saldo da carteira (409 BALANCE_FORBIDDEN: conectar de novo para autorizar). */
  balance: () => api.get<ShippingBalance>('/api/integrations/melhorenvio/balance'),
  /** Gera o Pix ou o boleto para pôr saldo na carteira. */
  addBalance: (valueCents: number, method: ShippingTopUpMethod) =>
    api.post<ShippingTopUp>('/api/integrations/melhorenvio/balance', { valueCents, method }),
  /** A conta a pagar da recarga por Pix, para pagar pela AbacatePay (com o copia-e-cola colado, se preciso). */
  topUpEntry: (id: string, pixCode = '') =>
    api.post<FinanceEntry>(`/api/integrations/melhorenvio/balance/${id}/entry`, { pixCode }),
};

/** Rotas públicas, usadas também pela landing page (sem login). */
/** O contrato de prestação de serviços (aceite no primeiro acesso, com o CPF). */
export const contractApi = {
  mine: () => api.get<ContractStatus>('/api/me/contract'),
  accept: (version: string, document: string) =>
    api.post<ContractStatus>('/api/me/contract/accept', { version, document }),
  /** O texto em vigor, sem login (o contrato dos clientes ou o termo do associado). */
  current: (associate = false) =>
    request<ContractDocument>(`/api/public/contract${associate ? '?tipo=associado' : ''}`, { anonymous: true }),
};

/** No link de pagamento, a chave do Pix de todas as faturas do vencimento. */
export const GROUP_SUFFIX = ':todas';

/** O link de pagamento da fatura (sem login). */
export const payLinkApi = {
  invoice: (token: string) => request<PublicInvoice>(`/api/public/invoices/${encodeURIComponent(token)}`, { anonymous: true }),
  /** O contrato da janela de Pix, preso ao link. */
  pix: (token: string): PixApi => {
    const base = `/api/public/invoices/${encodeURIComponent(token)}`;
    return {
      // A chave termina em ":todas": o Pix de todas as faturas do vencimento.
      invoicePix: (key: string) =>
        request<PixCharge>(key.endsWith(GROUP_SUFFIX) ? `${base}/pix-group` : `${base}/pix`, { method: 'POST', anonymous: true }),
      invoiceBoleto: (key: string) =>
        request<PixCharge>(key.endsWith(GROUP_SUFFIX) ? `${base}/boleto-group` : `${base}/boleto`, { method: 'POST', anonymous: true }),
      charge: (id: string) => request<PixCharge>(`${base}/charges/${id}`, { anonymous: true }),
      simulateCharge: (id: string) => request<PixCharge>(`${base}/charges/${id}/simulate`, { method: 'POST', anonymous: true }),
    };
  },
};

export const publicApi = {
  installers: () => request<PublicInstaller[]>('/api/public/installers', { anonymous: true }),
  /** Cadastro de interesse da landing (vira pré-cliente). */
  createLead: (input: LeadInput) =>
    request<{ status: string }>('/api/public/leads', { method: 'POST', body: input, anonymous: true }),
  /** "Me avise quando lançar" (lista de lançamento). */
  joinLaunch: (input: WaitlistInput) =>
    request<{ status: string }>('/api/public/launch', { method: 'POST', body: input, anonymous: true }),
  /** O afiliado do link de indicação ("Indicado por @fulano"). */
  affiliate: (code: string) =>
    request<PublicAffiliate>(`/api/public/affiliates/${encodeURIComponent(code)}`, { anonymous: true }),
  /** A página do afiliado, pelo link secreto. */
  partner: (token: string) =>
    request<PartnerReport>(`/api/public/partner/${encodeURIComponent(token)}`, { anonymous: true }),
  /** O QR Code do link de indicação do afiliado (para o flyer): o endereço da imagem ou do arquivo. */
  partnerQrUrl: (token: string, format: 'png' | 'svg', download = false) =>
    apiUrl(`/api/public/partner/${encodeURIComponent(token)}/qr?format=${format}${download ? '&download=1' : ''}`),
};

export interface BulkSmsSetupResult {
  deviceId: string;
  imei?: string;
  success: boolean;
  sessionId?: string;
  status?: string;
  error?: string;
}

export interface BulkSmsSetupResponse {
  total: number;
  started: number;
  failed: number;
  results: BulkSmsSetupResult[];
}

/** Configuração do rastreador por SMS (SMS Market) na ativação. */
export const smsSetupApi = {
  get: (deviceId: string) => api.get<SmsSetupView>(`/api/devices/${deviceId}/sms-setup`),
  start: (deviceId: string, input: { fulfillmentId?: string | null; unlock?: boolean; query?: boolean; force?: boolean }) =>
    api.post<SmsSession>(`/api/devices/${deviceId}/sms-setup`, {
      fulfillmentId: input.fulfillmentId ?? null,
      unlock: Boolean(input.unlock),
      query: Boolean(input.query),
      force: Boolean(input.force),
    }),
  startBulk: (input: { deviceIds: string[]; unlock?: boolean; query?: boolean; force?: boolean }) =>
    api.post<BulkSmsSetupResponse>('/api/devices/bulk/sms-setup', {
      deviceIds: input.deviceIds,
      unlock: Boolean(input.unlock),
      query: Boolean(input.query),
      force: Boolean(input.force),
    }),
  cancel: (sessionId: string) => api.post<SmsSession>(`/api/sms-setup/${sessionId}/cancel`),
  /** O saldo na SMS Market e quantas ativações ele paga (o contador da página de rastreadores). */
  usage: () => api.get<SmsUsage>('/api/sms/usage'),
};

/** Programa de afiliados (admin). */
export const affiliatesApi = {
  list: () => api.get<Affiliate[]>('/api/affiliates'),
  create: (input: AffiliateInput) => api.post<Affiliate>('/api/affiliates', input),
  update: (id: string, input: AffiliateInput) => api.patch<Affiliate>(`/api/affiliates/${id}`, input),
  /** Troca o link secreto da página do afiliado (o antigo para de abrir). */
  renewReportLink: (id: string) => api.post<Affiliate>(`/api/affiliates/${id}/report-token`),
  settings: () => api.get<AffiliateSettings>('/api/affiliates/settings'),
  saveSettings: (input: { defaultCommissionCents: number; applyToAll: boolean; customerRewardCents?: number }) =>
    api.put<AffiliateSettings>('/api/affiliates/settings', input),
  /** month: AAAA-MM. */
  closing: (month: string) => api.get<AffiliateClosingLine[]>(`/api/affiliates/closing?month=${month}`),
  close: (input: { month: string; dueDate: string }) => api.post<AffiliatePayout[]>('/api/affiliates/closing', input),
  payouts: () => api.get<AffiliatePayout[]>('/api/affiliates/payouts'),
  undoPayout: (id: string) => api.delete<void>(`/api/affiliates/payouts/${id}`),
};

/** Infraestrutura: máquina, banco, backups e erros do servidor (admin). */
export const infraApi = {
  status: () => api.get<InfraStatus>('/api/infra/status'),
  logs: (q: { hours: number; level: '' | 'ERROR' | 'WARN'; search: string }) =>
    api.get<SystemLogPage>(
      `/api/infra/logs?${new URLSearchParams({ hours: String(q.hours), level: q.level, q: q.search, limit: '100' })}`,
    ),
};

/** Visitas da landing page (admin). */
export const analyticsApi = {
  /** origin: só os visitantes que chegaram por ela ("" = todas). */
  landing: (days: number, origin = '') =>
    api.get<LandingAnalytics>(`/api/analytics/landing?days=${days}&origem=${encodeURIComponent(origin)}`),
};

/** Pré-clientes (admin). */
export const leadsApi = {
  list: (status?: LeadStatus) => api.get<Lead[]>(`/api/leads${status ? `?status=${status}` : ''}`),
  stats: () => api.get<{ new: number }>('/api/leads/stats'),
  update: (id: string, input: { status: LeadStatus; notes: string }) => api.patch<Lead>(`/api/leads/${id}`, input),
  /** Lista de lançamento: quem pediu o aviso. */
  waitlist: () => api.get<WaitlistEntry[]>('/api/leads/waitlist'),
  removeFromWaitlist: (id: string) => api.delete<void>(`/api/leads/waitlist/${id}`),
  /** Tira o pré-cliente da lista de lançamento (perde a promoção; ele fica). */
  removeFromLaunch: (id: string) => api.delete<void>(`/api/leads/${id}/launch-list`),
  /** Apaga o pré-cliente e a inscrição na lista (pedido da pessoa, LGPD). */
  remove: (id: string) => api.delete<void>(`/api/leads/${id}`),
  /** Vagas da promoção de pré-lançamento. */
  promo: () => api.get<PromoUsage>('/api/leads/promo'),
  /** O QR Code de um link (o da tela do evento), para ver e para imprimir. */
  qrImage: (text: string) => fetchBlob(`/api/leads/qr?${new URLSearchParams({ text })}`),
  downloadQr: (text: string, name: string, format: 'svg' | 'png') =>
    download(`/api/leads/qr?${new URLSearchParams({ text, name, format })}`),
};

export const catalogApi = {
  get: () => api.get<Catalog>('/api/catalog'),
  /** As opções do pedido do cliente (central): o comodato e a instalação pelos instaladores da Farbo. */
  orderOptions: () => api.get<OrderOptions>('/api/order-options'),
  setOrderOptions: (input: Omit<OrderOptions, 'updatedAt'>) => api.put<OrderOptions>('/api/order-options', input),
};

/** Área do cliente (perfil customer). */
/** A gestão da frota da conta: motoristas, custos, multas e locações. */
export const fleetOpsApi = {
  drivers: () => api.get<FleetDriver[]>('/api/me/drivers'),
  createDriver: (input: FleetDriverInput) => api.post<FleetDriver>('/api/me/drivers', input),
  updateDriver: (id: string, input: FleetDriverInput) => api.patch<FleetDriver>(`/api/me/drivers/${id}`, input),
  /** Quem dirige o veículo a partir de agora (nulo: ninguém). */
  assignDriver: (vehicleId: string, driverId: string | null) =>
    api.put<void>(`/api/me/vehicles/${vehicleId}/driver`, { driverId }),
  expenses: (from: string, to: string) => api.get<FleetExpense[]>(`/api/me/expenses?de=${from}&ate=${to}`),
  createExpense: (input: FleetExpenseInput) => api.post<FleetExpense>('/api/me/expenses', input),
  deleteExpense: (id: string) => api.delete<void>(`/api/me/expenses/${id}`),
  fines: () => api.get<TrafficFine[]>('/api/me/fines'),
  createFine: (input: TrafficFineInput) => api.post<TrafficFine>('/api/me/fines', input),
  updateFine: (id: string, input: TrafficFineInput) => api.patch<TrafficFine>(`/api/me/fines/${id}`, input),
  deleteFine: (id: string) => api.delete<void>(`/api/me/fines/${id}`),
  rentals: () => api.get<Rental[]>('/api/me/rentals'),
  createRental: (input: RentalInput) => api.post<Rental>('/api/me/rentals', input),
  returnRental: (id: string, kmEnd: number | null) => api.post<Rental>(`/api/me/rentals/${id}/return`, { kmEnd }),
  extendRental: (id: string, endsAt: string) => api.post<Rental>(`/api/me/rentals/${id}/extend`, { endsAt }),
};

/** As integrações (chaves da API e webhooks): do titular (/me) ou da organização (/org). */
export function integrationsApi(base: '/api/me' | '/api/org') {
  return {
    keys: () => api.get<ApiKey[]>(`${base}/api-keys`),
    createKey: (name: string) => api.post<{ key: ApiKey; secret: string }>(`${base}/api-keys`, { name }),
    revokeKey: (id: string) => api.delete<void>(`${base}/api-keys/${id}`),
    webhooks: () => api.get<Webhook[]>(`${base}/webhooks`),
    createWebhook: (input: { url: string; events: string[] }) => api.post<Webhook>(`${base}/webhooks`, input),
    updateWebhook: (id: string, input: { url: string; events: string[]; active: boolean }) =>
      api.patch<Webhook>(`${base}/webhooks/${id}`, input),
    deleteWebhook: (id: string) => api.delete<void>(`${base}/webhooks/${id}`),
    testWebhook: (id: string) => api.post<Webhook>(`${base}/webhooks/${id}/test`),
  };
}

export const meApi = {
  /** Indique e ganhe: o link do cliente, quem veio por ele e os créditos. */
  referral: () => api.get<ReferralProgram>('/api/me/referral'),
  /** As ocorrências do associado (abertas por ele, com as fotos e o B.O.). */
  claims: () => api.get<Claim[]>('/api/me/claims'),
  claim: (id: string) => api.get<Claim>(`/api/me/claims/${id}`),
  openClaim: (input: ClaimInput) => api.post<Claim>('/api/me/claims', input),
  claimNote: (id: string, note: string) => api.post<Claim>(`/api/me/claims/${id}/notes`, { note }),
  addClaimFile: (id: string, kind: 'photo' | 'police_report' | 'document', file: Blob, name: string) => {
    const form = new FormData();
    form.append('kind', kind);
    form.append('file', file, name);
    return request<{ id: string }>(`/api/me/claims/${id}/files`, { method: 'POST', body: form });
  },
  claimFileUrl: (id: string, fileId: string) => `/api/me/claims/${id}/files/${fileId}`,
  /** As instalações agendadas dos veículos do cliente. */
  installations: () => api.get<InstallationAppointment[]>('/api/me/installations'),
  /** As vistorias pedidas ao associado. */
  inspections: () => api.get<Inspection[]>('/api/me/inspections'),
  inspection: (id: string) => api.get<Inspection>(`/api/me/inspections/${id}`),
  saveInspectionPhoto: (id: string, slot: InspectionSlot, file: Blob, position: { latitude: number; longitude: number } | null) => {
    const form = new FormData();
    form.append('file', file, `${slot}.jpg`);
    if (position) {
      form.append('latitude', String(position.latitude));
      form.append('longitude', String(position.longitude));
    }
    return request<unknown>(`/api/me/inspections/${id}/photos/${slot}`, { method: 'PUT', body: form });
  },
  inspectionPhotoUrl: (id: string, slot: InspectionSlot) => `/api/me/inspections/${id}/photos/${slot}`,
  submitInspection: (id: string) => api.post<Inspection>(`/api/me/inspections/${id}/submit`),
  /** As notas fiscais de cada mês. */
  fiscalNotes: () => api.get<FiscalNote[]>('/api/me/fiscal-notes'),
  downloadNote: (id: string) => download(`/api/me/fiscal-notes/${id}/file`),
  account: () => api.get<CustomerAccount>('/api/me/account'),
  subscriptions: () => api.get<Subscription[]>('/api/me/subscriptions'),
  invoices: () => api.get<Invoice[]>('/api/me/invoices'),
  /** A página seguinte do histórico: as fechadas que vencem antes de `before` (AAAA-MM-DD). */
  olderInvoices: (before: string) => api.get<Invoice[]>(`/api/me/invoices?antes=${encodeURIComponent(before)}`),

  invoicePix: (invoiceId: string) => api.post<PixCharge>(`/api/me/invoices/${invoiceId}/pix`),
  /** Um Pix só para várias faturas (a do mês da frota, ou as vencidas). */
  invoicesPix: (invoiceIds: string[]) => api.post<PixCharge>('/api/me/invoices/pix', { invoiceIds }),
  /** O boleto (com Pix junto) de uma fatura, ou um só para várias. */
  invoiceBoleto: (invoiceId: string) => api.post<PixCharge>(`/api/me/invoices/${invoiceId}/boleto`),
  invoicesBoleto: (invoiceIds: string[]) => api.post<PixCharge>('/api/me/invoices/boleto', { invoiceIds }),
  /** O cartão: pagar uma fatura ou várias (202: sem resposta do banco ainda; a tela acompanha). */
  invoiceCard: (invoiceId: string, input: CardPayInput) => api.post<PixCharge>(`/api/me/invoices/${invoiceId}/card`, input),
  invoicesCard: (invoiceIds: string[], input: CardPayInput) =>
    api.post<PixCharge>('/api/me/invoices/card', { ...input, invoiceIds }),
  /** O cartão da conta: ligado?, a chave pública, o cartão salvo e o pagador. */
  card: () => api.get<CardView>('/api/me/card'),
  setAutoDebit: (enabled: boolean) => api.put<CardView>('/api/me/card/auto-debit', { enabled }),
  removeCard: () => api.delete<void>('/api/me/card'),
  charge: (id: string) => api.get<PixCharge>(`/api/me/charges/${id}`),
  simulateCharge: (id: string) => api.post<PixCharge>(`/api/me/charges/${id}/simulate`),

  /** Endereço de entrega: obrigatório antes de contratar um rastreador. */
  saveAddress: (input: DeliveryAddress) => api.put<DeliveryAddress>('/api/me/address', input),
  /** Meus dados: nome, telefone e CPF/CNPJ (o e-mail é o login e não muda aqui). */
  updateProfile: (input: { name: string; phone: string; document: string }) => api.put<User>('/api/me/profile', input),
  /** Novo veículo; equipamento e plano vêm do catálogo e da conta do cliente. */
  /** launchPromo: o cliente confirmou com os preços da promoção que o catálogo mostrou. */
  orderTracker: (input: {
    vehicle: VehicleInput;
    launchPromo?: boolean;
    shippingServiceId?: number;
    /** Combinar a entrega com a central, em vez da transportadora. */
    arrangeDelivery?: boolean;
    /** O rastreador parcelado sem juros (ausente ou 1: à vista). */
    installments?: number;
    /** Comprado ou em comodato (quando a central oferece). */
    equipmentMode?: 'sale' | 'loan';
    /** O rastreador comprado instalado pelos instaladores da Farbo (no comodato, ela já é da Farbo). */
    farboInstall?: boolean;
  }) =>
    api.post<TrackerOrderResult>('/api/me/trackers', input),
  /** As formas de entrega até o endereço do cliente (preço e prazo). */
  shippingQuote: () => api.get<ShippingQuoteView>('/api/me/shipping-quote'),
  /** Acompanhamento do chip e do rastreador de cada pedido. */
  fulfillments: () => api.get<CustomerFulfillment[]>('/api/me/fulfillments'),
  /** Assinatura antiga sem veículo: o cliente informa qual veículo ela cobre. */
  attachVehicle: (subscriptionId: string, input: VehicleInput) =>
    api.post<Vehicle>(`/api/me/subscriptions/${subscriptionId}/vehicle`, input),
  /** Alertas por e-mail: escolhas, horário de vigilância e histórico. */
  alerts: () => api.get<AlertSettings>('/api/me/alerts'),
  saveAlerts: (input: AlertSettingsInput) => api.put<AlertSettings>('/api/me/alerts', input),
  /** Os e-mails a mais dos alertas da conta. */
  saveAlertRecipients: (emails: string[]) => api.put<AlertSettings>('/api/me/alerts/recipients', { emails }),
  /** Envia um e-mail de teste para o próprio cliente (um por minuto). */
  testAlerts: () => api.post<AlertSettings>('/api/me/alerts/test'),
  /** Notificações no celular (app): chave pública e aparelhos inscritos. */
  push: () => api.get<PushStatus>('/api/me/push'),
  subscribePush: (subscription: PushSubscriptionJSON) => api.post<void>('/api/me/push/subscriptions', subscription),
  unsubscribePush: (endpoint: string) => api.post<void>('/api/me/push/unsubscribe', { endpoint }),
  testPush: () => api.post<{ delivered: number }>('/api/me/push/test'),
  /** A área de notificações do app. */
  notifications: () => api.get<NotificationInbox>('/api/me/notifications'),
  /** Marca como lida uma notificação (id) ou todas. */
  readNotifications: (id?: number) => api.post<void>('/api/me/notifications/read', id === undefined ? undefined : { id }),
};

/** As operações que a janela de pagamento usa (cliente ou central). */
export interface PixApi {
  invoicePix: (invoiceId: string) => Promise<PixCharge>;
  invoiceBoleto: (invoiceId: string) => Promise<PixCharge>;
  /** O cartão (só o cliente logado: o formulário pede os dados do pagador). */
  invoiceCard?: (invoiceId: string, input: CardPayInput) => Promise<PixCharge>;
  card?: () => Promise<CardView>;
  charge: (id: string) => Promise<PixCharge>;
  simulateCharge: (id: string) => Promise<PixCharge>;
}

// ---------------------------------------------------------------------------
// Gestão da empresa (admin)
// ---------------------------------------------------------------------------

export interface EntryInput {
  kind: EntryKind;
  description: string;
  categoryId: string;
  supplierId: string | null;
  /** O total: com parcelas, é dividido e cada uma vence um mês depois. */
  amountCents: number;
  dueDate: string;
  installments: number;
  paymentCode: string;
  notes: string;
  /** Lançar já pago (nessa data e forma). */
  paidOn: string | null;
  paymentMethod: PaymentMethod;
}

export type EntryUpdate = Pick<
  EntryInput,
  'description' | 'categoryId' | 'supplierId' | 'amountCents' | 'dueDate' | 'paymentCode' | 'notes'
>;

export interface EntryFilter {
  kind: EntryKind;
  status: 'open' | 'overdue' | 'paid' | 'canceled' | 'all';
  from?: string;
  to?: string;
  category?: string;
  supplier?: string;
  q?: string;
}

export interface RecurrenceInput {
  kind: EntryKind;
  description: string;
  categoryId: string;
  supplierId: string | null;
  amountCents: number;
  firstDueDate: string;
  endsOn: string | null;
}

export type SupplierInput = Omit<Supplier, 'id' | 'createdAt'>;
export type CategoryInput = Pick<FinanceCategory, 'name' | 'group' | 'active'>;
export type StockItemInput = Pick<StockItem, 'name' | 'kind' | 'minQuantity' | 'active'>;

export interface MovementInput {
  itemId: string;
  type: StockMoveType;
  quantity: number;
  unitCostCents: number;
  occurredOn: string;
  supplierId: string | null;
  notes: string;
  /** A conta a pagar da compra (só na entrada). */
  payable: {
    categoryId: string;
    dueDate: string;
    installments: number;
    paidOn: string | null;
    paymentMethod: PaymentMethod;
  } | null;
}

export const financeApi = {
  alerts: () => api.get<FinanceAlerts>('/api/finance/alerts'),
  overview: () => api.get<FinanceOverview>('/api/finance/overview'),
  cashflow: (months = 12) => api.get<CashFlow>(`/api/finance/cashflow?months=${months}`),
  dre: (months = 12) => api.get<DreMonth[]>(`/api/finance/dre?months=${months}`),
  /** Os pagamentos dos clientes do mês ("2026-10"), com a busca e a forma. */
  customerPayments: (f: { month: string; q?: string; method?: CustomerPaymentMethod | '' }) => {
    const params = new URLSearchParams({ month: f.month });
    if (f.q) params.set('q', f.q);
    if (f.method) params.set('method', f.method);
    return api.get<CustomerPayments>(`/api/finance/customer-payments?${params}`);
  },
  saveSettings: (input: FinanceSettings) => api.put<FinanceSettings>('/api/finance/settings', input),

  entries: (f: EntryFilter) => {
    const params = new URLSearchParams({ kind: f.kind, status: f.status });
    for (const key of ['from', 'to', 'category', 'supplier', 'q'] as const) {
      const value = f[key];
      if (value) params.set(key, value);
    }
    return api.get<FinanceEntry[]>(`/api/finance/entries?${params}`);
  },
  createEntries: (input: EntryInput) => api.post<FinanceEntry[]>('/api/finance/entries', input),
  updateEntry: (id: string, input: EntryUpdate) => api.patch<FinanceEntry>(`/api/finance/entries/${id}`, input),
  pay: (id: string, input: { paidOn: string; paidCents: number; method: PaymentMethod }) =>
    api.post<FinanceEntry>(`/api/finance/entries/${id}/pay`, input),
  reopen: (id: string) => api.post<FinanceEntry>(`/api/finance/entries/${id}/reopen`),
  /** Pix pela AbacatePay: ligado, modo e saldo. */
  pixInfo: () => api.get<PixInfo>('/api/finance/pix'),
  /** O saldo na AbacatePay agora (disponível, a liberar e bloqueado). */
  balance: () => api.get<GatewayBalance>('/api/finance/balance'),
  /** Para onde e quanto vai sair (ou o problema), e os envios anteriores. */
  entryPix: (id: string) =>
    api.get<{ plan: PixPlan | null; problem: string; transfers: PixTransfer[] }>(`/api/finance/entries/${id}/pix`),
  /** Paga por Pix (pede o comprovante da confirmação da senha ou biometria). */
  sendPix: (id: string, stepUpToken: string) =>
    request<PixTransfer>(`/api/finance/entries/${id}/pix`, { method: 'POST', headers: { 'X-Step-Up-Token': stepUpToken } }),
  /** Conferido no painel da AbacatePay: o Pix sem resposta saiu ou não. */
  resolvePix: (transferId: string, input: { sent: boolean; providerId: string }) =>
    api.post<PixTransfer>(`/api/finance/pix/${transferId}/resolve`, input),
  cancel: (id: string) => api.post<FinanceEntry>(`/api/finance/entries/${id}/cancel`),
  removeEntry: (id: string) => api.delete<void>(`/api/finance/entries/${id}`),
  attach: (entryId: string, file: File) => {
    const form = new FormData();
    form.append('file', file);
    return request<FinanceAttachment>(`/api/finance/entries/${entryId}/attachments`, { method: 'POST', body: form });
  },
  downloadAttachment: (id: string) => download(`/api/finance/attachments/${id}`),
  removeAttachment: (id: string) => api.delete<void>(`/api/finance/attachments/${id}`),

  recurrences: () => api.get<FinanceRecurrence[]>('/api/finance/recurrences'),
  createRecurrence: (input: RecurrenceInput) => api.post<FinanceRecurrence>('/api/finance/recurrences', input),
  updateRecurrence: (id: string, input: Omit<RecurrenceInput, 'kind' | 'firstDueDate'>) =>
    api.patch<FinanceRecurrence>(`/api/finance/recurrences/${id}`, input),
  endRecurrence: (id: string) => api.post<FinanceRecurrence>(`/api/finance/recurrences/${id}/end`),

  categories: () => api.get<FinanceCategory[]>('/api/finance/categories'),
  saveCategory: (id: string | null, input: CategoryInput) =>
    id
      ? api.patch<FinanceCategory>(`/api/finance/categories/${id}`, input)
      : api.post<FinanceCategory>('/api/finance/categories', input),
  suppliers: () => api.get<Supplier[]>('/api/finance/suppliers'),
  saveSupplier: (id: string | null, input: SupplierInput) =>
    id ? api.patch<Supplier>(`/api/finance/suppliers/${id}`, input) : api.post<Supplier>('/api/finance/suppliers', input),

  stockItems: () =>
    api.get<{ items: StockItem[]; trackers: { withVehicle: number; withoutVehicle: number } }>('/api/finance/stock/items'),
  saveStockItem: (id: string | null, input: StockItemInput) =>
    id
      ? api.patch<StockItem>(`/api/finance/stock/items/${id}`, input)
      : api.post<StockItem>('/api/finance/stock/items', input),
  movements: (itemId?: string) =>
    api.get<StockMovement[]>(`/api/finance/stock/movements${itemId ? `?item=${itemId}` : ''}`),
  move: (input: MovementInput) =>
    api.post<{ movement: StockMovement; payables: FinanceEntry[] }>('/api/finance/stock/movements', input),
};


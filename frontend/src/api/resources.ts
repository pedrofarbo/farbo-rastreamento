import { api, request } from './client';
import type {
  AlertSettings,
  AlertSettingsInput,
  PushStatus,
  AuditEntry,
  AuthTokens,
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
  Position,
  PositionHistory,
  ProtocolDescriptor,
  ProvisioningCommand,
  RawPacket,
  ShippingIntegration,
  ShippingQuote,
  Subscription,
  Installer,
  PublicInstaller,
  TrackerOrderResult,
  User,
  Vehicle,
  VehicleEvent,
  VehicleView,
} from '@/types';

// ---------------------------------------------------------------------------
// Autenticação
// ---------------------------------------------------------------------------

export const authApi = {
  // Login e logout são rotas públicas: um 401 no login é senha errada, não
  // sessão vencida. Sem anonymous, o cliente tentaria renovar a sessão e
  // trocaria "e-mail ou senha inválidos" por "sessão expirada".
  login: (email: string, password: string) =>
    request<AuthTokens>('/api/auth/login', {
      method: 'POST',
      body: { email, password },
      anonymous: true,
    }),
  logout: (refreshToken: string) =>
    request<void>('/api/auth/logout', { method: 'POST', body: { refreshToken }, anonymous: true }),
  me: () => api.get<User>('/api/auth/me'),
  listUsers: () => api.get<User[]>('/api/users'),

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
  createUser: (input: { email: string; name: string; role: string; password: string }) =>
    api.post<User>('/api/users', input),
};

// ---------------------------------------------------------------------------
// Veículos
// ---------------------------------------------------------------------------

export interface VehicleInput {
  name: string;
  plate?: string;
  brand?: string;
  model?: string;
  year?: number | null;
  color?: string;
  speedLimitKmh?: number | null;
  deviceId?: string | null;
}

/** Monta o corpo de edição a partir do veículo atual (a API substitui todos os campos). */
export function vehicleInputFrom(vehicle: Vehicle, changes: Partial<VehicleInput> = {}): VehicleInput {
  return {
    name: vehicle.name,
    plate: vehicle.plate,
    brand: vehicle.brand,
    model: vehicle.model,
    year: vehicle.year,
    color: vehicle.color,
    speedLimitKmh: vehicle.speedLimitKmh,
    deviceId: vehicle.deviceId,
    ...changes,
  };
}

export const vehiclesApi = {
  list: () => api.get<VehicleView[]>('/api/vehicles'),
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
  engineResume: (vehicleId: string) =>
    api.post<DeviceCommand>(`/api/vehicles/${vehicleId}/commands/engine-resume`),
  requestPosition: (vehicleId: string) =>
    api.post<DeviceCommand>(`/api/vehicles/${vehicleId}/commands/request-position`),
  requestStatus: (vehicleId: string) =>
    api.post<DeviceCommand>(`/api/vehicles/${vehicleId}/commands/request-status`),
  generic: (vehicleId: string, body: { command: string; params?: Record<string, string>; raw?: string }) =>
    api.post<DeviceCommand>(`/api/vehicles/${vehicleId}/commands`, body),
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
  model?: string;
  manufacturer?: string;
  protocol?: string;
  firmware?: string;
  phoneNumber?: string;
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
}

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

export const devicesApi = {
  list: () => api.get<Device[]>('/api/devices'),
  get: (id: string) => api.get<Device>(`/api/devices/${id}`),
  create: (input: DeviceInput) => api.post<Device>('/api/devices', input),
  update: (id: string, input: DeviceInput) => api.patch<Device>(`/api/devices/${id}`, input),
  remove: (id: string) => api.delete<void>(`/api/devices/${id}`),
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
  latitude: number;
  longitude: number;
  radiusMeters: number;
  active?: boolean;
  /** Cerca do cliente: os veículos dele que ela vigia (ao menos um). */
  vehicleIds?: string[];
  notifyEnter?: boolean;
  notifyExit?: boolean;
}

export const geofencesApi = {
  list: () => api.get<Geofence[]>('/api/geofences'),
  create: (input: GeofenceInput) => api.post<Geofence>('/api/geofences', input),
  update: (id: string, input: GeofenceInput) => api.patch<Geofence>(`/api/geofences/${id}`, input),
  remove: (id: string) => api.delete<void>(`/api/geofences/${id}`),
};

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
}

export interface InvoiceInput {
  description: string;
  amountCents: number;
  dueDate: DateOnly;
  paymentUrl: string;
  pixCode: string;
}

/** Administração (perfil admin). */
export const customersApi = {
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
  saveAddress: (id: string, input: DeliveryAddress) =>
    api.put<DeliveryAddress>(`/api/customers/${id}/address`, input),
  /** Prazo do histórico de todos os veículos do cliente (nulo = padrão da central). */
  setHistoryRetention: (id: string, days: HistoryRetention | null) =>
    api.put<{ effectiveDays: number }>(`/api/customers/${id}/history-retention`, { days }),

  updateSubscription: (id: string, input: { planName: string; priceCents: number }) =>
    api.patch<Subscription>(`/api/subscriptions/${id}`, input),
  cancelSubscription: (id: string) => api.post<Subscription>(`/api/subscriptions/${id}/cancel`),

  createInvoice: (customerId: string, input: InvoiceInput) =>
    api.post<Invoice>(`/api/customers/${customerId}/invoices`, input),
  updateInvoice: (id: string, input: { paymentUrl: string; pixCode: string }) =>
    api.patch<Invoice>(`/api/invoices/${id}`, input),
  payInvoice: (id: string) => api.post<Invoice>(`/api/invoices/${id}/pay`),
  cancelInvoice: (id: string) => api.post<Invoice>(`/api/invoices/${id}/cancel`),

  invoicePix: (invoiceId: string) => api.post<PixCharge>(`/api/invoices/${invoiceId}/pix`),
  charge: (id: string) => api.get<PixCharge>(`/api/charges/${id}`),
  simulateCharge: (id: string) => api.post<PixCharge>(`/api/charges/${id}/simulate`),
  refundCharge: (id: string, reason: string) =>
    api.post<PixCharge>(`/api/charges/${id}/refund`, { reason }),

  /** Novo veículo: veículo → rastreador (fatura do equipamento) → assinatura. */
  orderTracker: (customerId: string, input: AdminTrackerOrder) =>
    api.post<TrackerOrderResult>(`/api/customers/${customerId}/trackers`, input),
  /** Assinatura antiga sem veículo: informa qual veículo ela cobre. */
  attachVehicle: (customerId: string, subscriptionId: string, input: VehicleInput) =>
    api.post<Vehicle>(`/api/customers/${customerId}/subscriptions/${subscriptionId}/vehicle`, input),
  /** Nova assinatura para um veículo que ficou sem nenhuma ativa. */
  reactivate: (customerId: string, vehicleId: string, plan: SubscriptionInput) =>
    api.post<Subscription>(`/api/customers/${customerId}/vehicles/${vehicleId}/subscription`, plan),
};

export interface AdminTrackerOrder {
  vehicle: VehicleInput;
  /** Zero não gera fatura (aparelho do cliente, cortesia). */
  equipmentCents: number;
  setupDueDate?: DateOnly | null;
  plan: SubscriptionInput;
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
};

/** Conexão com o Melhor Envios (admin). */
export const shippingIntegrationApi = {
  get: () => api.get<ShippingIntegration>('/api/integrations/melhorenvio'),
  connect: () => api.post<{ url: string }>('/api/integrations/melhorenvio/connect'),
  disconnect: () => api.post<void>('/api/integrations/melhorenvio/disconnect'),
};

/** Rotas públicas, usadas também pela landing page (sem login). */
export const publicApi = {
  installers: () => request<PublicInstaller[]>('/api/public/installers', { anonymous: true }),
};

export const catalogApi = {
  get: () => api.get<Catalog>('/api/catalog'),
};

/** Área do cliente (perfil customer). */
export const meApi = {
  account: () => api.get<CustomerAccount>('/api/me/account'),
  subscriptions: () => api.get<Subscription[]>('/api/me/subscriptions'),
  invoices: () => api.get<Invoice[]>('/api/me/invoices'),

  invoicePix: (invoiceId: string) => api.post<PixCharge>(`/api/me/invoices/${invoiceId}/pix`),
  charge: (id: string) => api.get<PixCharge>(`/api/me/charges/${id}`),
  simulateCharge: (id: string) => api.post<PixCharge>(`/api/me/charges/${id}/simulate`),

  /** Endereço de entrega: obrigatório antes de contratar um rastreador. */
  saveAddress: (input: DeliveryAddress) => api.put<DeliveryAddress>('/api/me/address', input),
  /** Novo veículo; equipamento e plano vêm do catálogo e da conta do cliente. */
  orderTracker: (input: { vehicle: VehicleInput }) =>
    api.post<TrackerOrderResult>('/api/me/trackers', input),
  /** Acompanhamento do chip e do rastreador de cada pedido. */
  fulfillments: () => api.get<CustomerFulfillment[]>('/api/me/fulfillments'),
  /** Assinatura antiga sem veículo: o cliente informa qual veículo ela cobre. */
  attachVehicle: (subscriptionId: string, input: VehicleInput) =>
    api.post<Vehicle>(`/api/me/subscriptions/${subscriptionId}/vehicle`, input),
  /** Alertas por e-mail: escolhas, horário de vigilância e histórico. */
  alerts: () => api.get<AlertSettings>('/api/me/alerts'),
  saveAlerts: (input: AlertSettingsInput) => api.put<AlertSettings>('/api/me/alerts', input),
  /** Envia um e-mail de teste para o próprio cliente (um por minuto). */
  testAlerts: () => api.post<AlertSettings>('/api/me/alerts/test'),
  /** Notificações no celular (app): chave pública e aparelhos inscritos. */
  push: () => api.get<PushStatus>('/api/me/push'),
  subscribePush: (subscription: PushSubscriptionJSON) => api.post<void>('/api/me/push/subscriptions', subscription),
  unsubscribePush: (endpoint: string) => api.post<void>('/api/me/push/unsubscribe', { endpoint }),
  testPush: () => api.post<{ delivered: number }>('/api/me/push/test'),
};

/** As operações de Pix que a janela de pagamento usa (cliente ou central). */
export interface PixApi {
  invoicePix: (invoiceId: string) => Promise<PixCharge>;
  charge: (id: string) => Promise<PixCharge>;
  simulateCharge: (id: string) => Promise<PixCharge>;
}

/**
 * Tipos espelhando o JSON da API.
 *
 * Nada aqui conhece TKSTAR, GT06 ou bytes: o frontend só enxerga a telemetria
 * já normalizada pelo backend.
 */

export type DeviceStatus = 'ONLINE' | 'STALE' | 'OFFLINE';

export type CommandStatus =
  | 'PENDING'
  | 'SENDING'
  | 'SENT'
  | 'ACKNOWLEDGED'
  | 'FAILED'
  | 'TIMEOUT'
  | 'REJECTED';

export type CommandType =
  | 'ENGINE_CUT'
  | 'ENGINE_RESUME'
  | 'REQUEST_POSITION'
  | 'REQUEST_STATUS'
  | 'SET_INTERVAL'
  | 'SET_HEARTBEAT'
  | 'SET_SERVER'
  | 'REBOOT'
  | 'CUSTOM';

/** admin, operator e viewer são a equipe da central; customer é o cliente final. */
export type UserRole = 'admin' | 'operator' | 'viewer' | 'customer';

export interface User {
  id: string;
  email: string;
  name: string;
  role: UserRole;
  phone: string;
  document: string;
  active: boolean;
  createdAt: string;
}

export interface AuthTokens {
  accessToken: string;
  refreshToken: string;
  expiresAt: string;
  user: User;
}

/**
 * Rastreador como a API devolve. As senhas (APN e de comando) são só de
 * escrita e nunca chegam aqui, nem para o admin: no lugar vêm os indicadores
 * `apnPasswordSet`/`commandPasswordSet` (só para o admin). Operador e
 * visualizador recebem só identificação e situação; os campos de
 * configuração chegam vazios. Nos overrides, a senha aparece como `***`.
 */
export interface Device {
  id: string;
  imei: string;
  model: string;
  manufacturer: string;
  protocol: string;
  firmware: string;
  phoneNumber: string;
  status: DeviceStatus;
  lastSeenAt: string | null;
  apn: string;
  apnUser: string;
  apnPasswordSet?: boolean;
  serverHost: string;
  serverPort: number | null;
  reportIntervalSeconds: number | null;
  heartbeatIntervalSeconds: number | null;
  commandPasswordSet?: boolean;
  commandOverrides: Record<string, string>;
  notes: string;
  createdAt: string;
  updatedAt: string;
}

export interface Position {
  id: number;
  deviceId: string;
  /** Instante informado pelo aparelho. */
  gpsTimestamp: string;
  /** Instante em que o servidor recebeu — diferente quando há buffer offline. */
  receivedAt: string;
  latitude: number;
  longitude: number;
  speedKmh: number;
  heading: number | null;
  altitude: number | null;
  gpsValid: boolean | null;
  satellites: number | null;
  hdop: number | null;
  acc: boolean | null;
  batteryVoltage: number | null;
  batteryPercent: number | null;
  gsmLevel: number | null;
  relayOn: boolean | null;
  protocol: string;
  source: 'gps' | 'heartbeat' | 'lbs';
  rawPayload?: string;
}

export interface DeviceState {
  deviceId: string;
  overspeed: boolean;
  acc: boolean | null;
  gpsValid: boolean | null;
  relayOn: boolean | null;
  batteryPercent: number | null;
  batteryVoltage: number | null;
  gsmLevel: number | null;
  insideFences: string[];
  lastHeartbeatAt: string | null;
  updatedAt: string;
}

export interface Vehicle {
  id: string;
  name: string;
  plate: string;
  brand: string;
  model: string;
  year: number | null;
  color: string;
  speedLimitKmh: number | null;
  deviceId: string | null;
  /** Cliente dono do veículo; nulo é veículo da central. */
  ownerId: string | null;
  /** Exceção do veículo (7, 14 ou 30 dias); nulo segue o cliente. */
  historyRetentionDays: HistoryRetention | null;
  createdAt: string;
  updatedAt: string;
}

/** Por quantos dias o histórico (trajeto e eventos) é guardado. */
export type HistoryRetention = 7 | 14 | 30;
export const HISTORY_RETENTION_OPTIONS: HistoryRetention[] = [7, 14, 30];

/** O que a lista do painel consome: veículo com o estado do rastreador junto. */
export interface VehicleView extends Vehicle {
  device: Device | null;
  lastPosition: Position | null;
  state: DeviceState | null;
  connected: boolean;
  /** Prazo que vale para o veículo (dele, do cliente ou o padrão da central). */
  historyDays: number;
  /**
   * O veículo é de outro cliente, que deu acesso a você: só a posição ao vivo
   * e, se liberado, o bloqueio de emergência (nunca o desbloqueio). Ausente
   * nos seus veículos e para a equipe.
   */
  shared?: SharedAccess | null;
}

export interface SharedAccess {
  shareId: string;
  ownerName: string;
  canBlock: boolean;
}

/** Um acesso dado pelo dono a outra pessoa (que entra com a própria conta). */
export interface VehicleShare {
  id: string;
  vehicleId: string;
  vehicleName: string;
  vehiclePlate: string;
  ownerName: string;
  guestId: string;
  guestName: string;
  guestEmail: string;
  canBlock: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface VehicleEvent {
  id: number;
  deviceId: string;
  type: string;
  timestamp: string;
  latitude: number | null;
  longitude: number | null;
  speedKmh: number | null;
  metadata: Record<string, unknown>;
  createdAt: string;
}

export interface DeviceCommand {
  id: string;
  deviceId: string;
  command: CommandType | string;
  payload: string;
  status: CommandStatus;
  correlationKey: number;
  requestedBy: string | null;
  requestedAt: string;
  sentAt: string | null;
  acknowledgedAt: string | null;
  timeoutAt: string | null;
  response: string;
  error: string;
  createdAt: string;
}

/** A regra de segurança do corte avaliada agora (sem enviar nada). */
export interface EngineCutCheck {
  allowed: boolean;
  /** NO_POSITION e STALE_POSITION se resolvem pedindo uma posição nova. */
  code?: 'READ_FAILED' | 'NO_POSITION' | 'STALE_POSITION' | 'TOO_FAST';
  reason?: string;
  positionAgeSeconds: number | null;
  maxPositionAgeSeconds: number;
}

export interface Geofence {
  id: string;
  name: string;
  latitude: number;
  longitude: number;
  radiusMeters: number;
  active: boolean;
  /** Cliente dono; nulo é cerca da central (vale para todos os veículos). */
  ownerId: string | null;
  /** Veículos do cliente vigiados pela cerca (vazio nas da central). */
  vehicleIds: string[];
  /** O cliente recebe aviso ao entrar/sair. */
  notifyEnter: boolean;
  notifyExit: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface PositionHistory {
  from: string;
  to: string;
  positions: Position[];
  /** Quantos pontos existem de fato no período. */
  total: number;
  returned: number;
  /** O backend devolveu 1 a cada sampleStep pontos. */
  sampled: boolean;
  sampleStep: number;
  /** Douglas-Peucker aplicado ao traçado. */
  simplified: boolean;
}

export interface ProtocolDescriptor {
  name: string;
  label: string;
  vendor: string;
  /** DOCUMENTED, ASSUMED ou UNKNOWN — ver docs/PROTOCOLS.md. */
  confidence: 'DOCUMENTED' | 'ASSUMED' | 'UNKNOWN';
  commands: CommandType[] | null;
  notes: string;
}

export interface RawPacket {
  id: number;
  remoteAddr: string;
  imei: string;
  protocol: string;
  reason: string;
  payloadHex: string;
  payloadAscii: string;
  byteCount: number;
  receivedAt: string;
}

export interface ConnectionInfo {
  imei: string;
  deviceId: string;
  protocol: string;
  remoteAddr: string;
  connectedAt: string;
  lastSeenAt: string;
}

export interface AuditEntry {
  id: number;
  userId: string | null;
  action: string;
  vehicleId: string | null;
  deviceId: string | null;
  result: string;
  ipAddress: string;
  metadata: Record<string, unknown>;
  createdAt: string;
}

export interface ProvisioningCommand {
  type: CommandType;
  description: string;
  /** Texto do comando com a senha trocada por `***` (ver `redacted`). */
  text: string;
  available: boolean;
  reason?: string;
  /** O texto levava a senha de comando: quem envia digita a senha no lugar do `***`. */
  redacted?: boolean;
}

/** Eventos recebidos pelo WebSocket (§18). */
export type RealtimeEventType =
  | 'position.updated'
  | 'device.online'
  | 'device.offline'
  | 'device.stale'
  | 'vehicle.event'
  | 'command.sent'
  | 'command.acknowledged'
  | 'command.failed'
  | 'engine.status.changed';

export interface RealtimeMessage<T = unknown> {
  type: RealtimeEventType;
  vehicleId?: string;
  deviceId?: string;
  timestamp: string;
  data?: T;
}

// ---------------------------------------------------------------------------
// Assinaturas e faturas
// ---------------------------------------------------------------------------

/** Data de calendário no formato AAAA-MM-DD (sem hora nem fuso). */
export type DateOnly = string;

/** Endereço de entrega do cliente: para onde vai o rastreador contratado. */
export interface DeliveryAddress {
  /** CEP só com os 8 dígitos. */
  zipCode: string;
  street: string;
  number: string;
  complement: string;
  district: string;
  city: string;
  /** Sigla da UF, ex.: "SP". */
  state: string;
}

export interface Subscription {
  id: string;
  customerId: string;
  planName: string;
  priceCents: number;
  dueDay: number;
  nextDueDate: DateOnly;
  status: 'ACTIVE' | 'CANCELED';
  canceledAt: string | null;
  /** Veículo que a assinatura cobre; nulo só em dados de antes do fluxo único. */
  vehicleId: string | null;
  /** Cópia do endereço de entrega na contratação; nula se não houve envio. */
  deliveryAddress: DeliveryAddress | null;
  /**
   * Promoção de pré-lançamento: as faturas que vencem antes de promoUntil
   * saem por promoPriceCents; depois, priceCents. Nulos: sem promoção.
   */
  promoPriceCents: number | null;
  promoUntil: DateOnly | null;
  createdAt: string;
  updatedAt: string;
}

export interface Invoice {
  id: string;
  customerId: string;
  subscriptionId: string | null;
  description: string;
  amountCents: number;
  dueDate: DateOnly;
  status: 'OPEN' | 'PAID' | 'CANCELED';
  /** Em aberto com vencimento no passado (calculado pelo backend). */
  overdue: boolean;
  daysOverdue: number;
  paidAt: string | null;
  /** Como foi quitada: MANUAL (baixa da central) ou PIX (confirmado no provedor). */
  paidVia: '' | 'MANUAL' | 'PIX';
  /** Pix que quitou a fatura, quando paidVia = PIX. */
  paidChargeId: string | null;
  paymentUrl: string;
  pixCode: string;
  createdAt: string;
  updatedAt: string;
}

/** Pix gerado para pagar uma fatura (AbacatePay, checkout transparente). */
export interface PixCharge {
  id: string;
  invoiceId: string;
  provider: string;
  providerChargeId: string;
  amountCents: number;
  /** Status do provedor: PENDING, PAID, EXPIRED, CANCELLED, REFUNDED, UNDER_DISPUTE… */
  status: string;
  /** Pix copia-e-cola. */
  brCode: string;
  /** Imagem do QR Code (data:image/png;base64,…). */
  qrCodeImage: string;
  /** Cobrança do ambiente de testes: dá para simular o pagamento. */
  devMode: boolean;
  expiresAt: string | null;
  paidAt: string | null;
  createdAt: string;
  invoiceStatus?: Invoice['status'];
  /** Estorno pedido pela central (em produção conclui depois, assíncrono). */
  refundRequestedAt: string | null;
  refundId: string;
  refundReason: string;
}

/** Pix que movimentou dinheiro (pago, estornado, em disputa), na ficha do cliente. */
export interface PixPayment extends PixCharge {
  invoiceDescription: string;
  /** Foi este Pix que quitou a fatura? Estorná-lo reabre a fatura. */
  settledInvoice: boolean;
}

/** Linha da lista de clientes da central. */
export interface CustomerSummary {
  id: string;
  name: string;
  email: string;
  phone: string;
  document: string;
  active: boolean;
  createdAt: string;
  activeSubscriptions: number;
  vehicleCount: number;
  /** Veículos de outros clientes que ele acompanha (acesso de terceiro). */
  sharedVehicles: number;
  openInvoices: number;
  overdueInvoices: number;
  openAmountCents: number;
  suspended: boolean;
}

export type ChipStatus = 'REQUESTED' | 'SHIPPED' | 'AT_BASE' | 'SEPARATED';
export type TrackerStatus =
  | 'AWAITING_SUPPLIER'
  | 'AT_BASE'
  | 'AWAITING_CHIP'
  | 'CONFIGURING'
  | 'CONFIGURED'
  | 'SHIPPED'
  | 'IN_TRANSIT'
  | 'DELIVERED';
export type FulfillmentTrack = 'CHIP' | 'TRACKER';

export interface FulfillmentEvent {
  id: number;
  track: FulfillmentTrack;
  status: string;
  note: string;
  /** Mudou sozinho (rastreio do Melhor Envios). */
  automatic: boolean;
  createdAt: string;
}

/** Acompanhamento de um veículo: o chip M2M e o rastreador até o cliente. */
export interface Fulfillment {
  id: string;
  customerId: string;
  customerName: string;
  vehicleId: string;
  vehicleName: string;
  vehiclePlate: string;
  subscriptionId: string | null;
  chipStatus: ChipStatus;
  trackerStatus: TrackerStatus;
  shippingOrderId: string | null;
  shippingProtocol: string;
  shippingService: string;
  shippingPriceCents: number | null;
  /** Status bruto do Melhor Envios (posted, delivered, undelivered...). */
  shippingStatus: string;
  trackingCode: string;
  labelUrl: string;
  events: FulfillmentEvent[];
  createdAt: string;
  updatedAt: string;
}

/** O que o cliente vê do acompanhamento. */
export interface CustomerFulfillment {
  id: string;
  vehicleId: string;
  vehicleName: string;
  chipStatus: ChipStatus;
  trackerStatus: TrackerStatus;
  carrier: string;
  trackingCode: string;
  events: FulfillmentEvent[];
  createdAt: string;
}

export interface ShippingQuote {
  serviceId: number;
  service: string;
  company: string;
  priceCents: number;
  deliveryDays: number;
  /** Preenchido quando o serviço não atende o trecho. */
  error: string;
}

export interface ShippingIntegration {
  configured: boolean;
  canConnect: boolean;
  staticToken: boolean;
  connected: boolean;
  sandbox: boolean;
  expiresAt: string | null;
  accountName: string;
  accountEmail: string;
  accountError: string;
  missingOrigin: string[];
  redirectUrl: string;
}

export interface CustomerDetail extends CustomerSummary {
  subscriptions: Subscription[];
  invoices: Invoice[];
  vehicles: VehicleView[];
  /** Pix online (AbacatePay) configurado no servidor. */
  onlinePayment: boolean;
  payments: PixPayment[];
  deliveryAddress: DeliveryAddress | null;
  fulfillments: Fulfillment[];
  /** Prazo do histórico no cliente; nulo = padrão da central. */
  historyRetentionDays: HistoryRetention | null;
  defaultHistoryDays: number;
}

/**
 * Preços de um rastreador novo (para o cliente, o plano é o que ele pagaria).
 * A instalação não entra: é paga direto ao prestador.
 */
export interface Catalog {
  planName: string;
  planPriceCents: number;
  defaultDueDay: number;
  equipmentName: string;
  equipmentPriceCents: number;
  /** Prazo, em dias, da fatura do equipamento. */
  setupDueDays: number;
  /** O cliente pode contratar com a promoção de pré-lançamento (nulo: não pode). */
  launchPromo?: PromoOffer | null;
}

/**
 * Promoção de pré-lançamento: rastreador e mensalidade dos primeiros meses.
 * No plano do Insanos MC (insanosPlanName), a mensalidade é
 * insanosMonthlyCents; no catálogo do cliente, monthlyCents já é a do plano dele.
 */
export interface PromoOffer {
  equipmentCents: number;
  monthlyCents: number;
  insanosMonthlyCents: number;
  insanosPlanName: string;
  months: number;
}

/** Se o cliente tem direito à promoção e, se não, por quê. */
export interface PromoStatus {
  eligible: boolean;
  reason: string;
  offer: PromoOffer;
}

/** Vagas da promoção de pré-lançamento. */
export interface PromoUsage {
  enabled: boolean;
  slots: number;
  used: number;
  offer: PromoOffer;
}

/** Prestador de instalação recomendado (a instalação é paga direto a ele). */
export interface PublicInstaller {
  id: string;
  name: string;
  city: string;
  serviceArea: string;
  /** Só dígitos, já com o 55 (para o link wa.me). */
  whatsapp: string;
  servesMoto: boolean;
  servesCar: boolean;
  priceMotoCents: number | null;
  priceCarCents: number | null;
  description: string;
}

/** Cadastro completo, visto pela central. */
export interface Installer extends PublicInstaller {
  /** Inativo não aparece na landing nem no painel do cliente. */
  active: boolean;
  createdAt: string;
  updatedAt: string;
}

/** O que a contratação de um rastreador criou. */
export interface TrackerOrderResult {
  vehicle: Vehicle;
  subscription: Subscription;
  /** Fatura do equipamento; nula quando nada foi cobrado. */
  setupInvoice: Invoice | null;
}

/** Resumo da conta que o cliente vê. */
export interface CustomerAccount {
  activeSubscriptions: number;
  vehicles: number;
  openInvoices: number;
  overdueInvoices: number;
  openAmountCents: number;
  suspended: boolean;
  suspendAfterDays: number;
  nextInvoice: Invoice | null;
  /** Pix online (AbacatePay) configurado no servidor. */
  onlinePayment: boolean;
  /** Nulo até o cliente cadastrar; sem ele, não dá para contratar rastreador. */
  deliveryAddress: DeliveryAddress | null;
}

// ---------------------------------------------------------------------------
// Alertas por e-mail
// ---------------------------------------------------------------------------

export type AlertKind =
  | 'SOS'
  | 'POWER_CUT'
  | 'TOWING'
  | 'IGNITION_GUARD'
  | 'OVERSPEED'
  | 'SIGNAL_LOST'
  | 'LOW_BATTERY'
  | 'ENGINE_BLOCK'
  | 'IGNITION';

export interface AlertKindInfo {
  kind: AlertKind;
  label: string;
  description: string;
  /** Alerta de segurança: também vai para a central. */
  security: boolean;
  /** Ligado para quem nunca mexeu nas preferências. */
  default: boolean;
}

export type AlertNotificationStatus = 'PENDING' | 'SENT' | 'FAILED';

export interface AlertNotification {
  id: number;
  vehicleId: string | null;
  vehicleName: string;
  plate: string;
  /** Tipo gravado no histórico: os do catálogo e ENGINE_BLOCKED, ENGINE_UNBLOCKED, GEOFENCE_ENTER, GEOFENCE_EXIT, TEST. */
  kind: string;
  occurredAt: string;
  status: AlertNotificationStatus;
  /** Ocorrências iguais seguradas que o e-mail resumiu. */
  suppressedCount: number;
  /** Em quantos celulares o alerta chegou como notificação. */
  pushSent: number;
  /** Complemento do tipo: o nome da cerca nos alertas de cerca. */
  detail: string;
  createdAt: string;
  sentAt: string | null;
}

export interface AlertSettings {
  /** Alertas ligados no servidor (ALERTS_ENABLED). */
  enabled: boolean;
  /** Há SMTP configurado; sem ele nenhum e-mail sai. */
  mailConfigured: boolean;
  email: string;
  kinds: AlertKind[];
  /** Horário de vigilância, "HH:MM". */
  guardStart: string;
  guardEnd: string;
  /** O usuário já salvou preferências (senão valem as padrão). */
  custom: boolean;
  suspended: boolean;
  cooldownMinutes: number;
  timezone: string;
  catalog: AlertKindInfo[];
  history: AlertNotification[];
}

export interface AlertSettingsInput {
  kinds: AlertKind[];
  guardStart: string;
  guardEnd: string;
}

// ---------------------------------------------------------------------------
// Notificações no celular (app do cliente)
// ---------------------------------------------------------------------------

export interface PushDevice {
  id: number;
  userAgent: string;
  createdAt: string;
  lastSuccessAt: string | null;
  endpoint: string;
}

export interface PushStatus {
  /** Push ligado no servidor. */
  enabled: boolean;
  /** applicationServerKey (VAPID) em base64url. */
  publicKey: string;
  devices: PushDevice[];
}

/** Atendimento pelo WhatsApp: quem responde a conversa. */
export type ConversationMode = 'BOT' | 'HUMAN';

export interface WhatsAppMessage {
  id: number;
  direction: 'IN' | 'OUT';
  /** CONTACT escreveu; BOT é a IA; AGENT, alguém da equipe. */
  author: 'CONTACT' | 'BOT' | 'AGENT';
  agentId: string | null;
  agentName: string;
  /** text, audio, image, video, document, sticker, location... */
  kind: string;
  body: string;
  /** Das enviadas: sent, delivered, read ou failed. */
  status: string;
  error: string;
  createdAt: string;
}

export interface WhatsAppConversation {
  id: string;
  waId: string;
  phone: string;
  contactName: string;
  customerId: string | null;
  customerName: string;
  mode: ConversationMode;
  handoffReason: string;
  /** Esperando alguém da equipe. */
  needsAttention: boolean;
  lastInboundAt: string | null;
  lastMessageAt: string;
  /** Dentro das 24 h em que o WhatsApp aceita resposta em texto livre. */
  windowOpen: boolean;
  createdAt: string;
  lastMessage?: WhatsAppMessage;
}

export interface WhatsAppConversationDetails extends WhatsAppConversation {
  messages: WhatsAppMessage[];
}

export interface WhatsAppStatus {
  configured: boolean;
  aiReady: boolean;
  model: string;
  attention: number;
}

/** Pré-cliente: NEW ninguém falou ainda; CONTACTED em conversa; CONVERTED virou cliente; DISCARDED não vai fechar. */
export type LeadStatus = 'NEW' | 'CONTACTED' | 'CONVERTED' | 'DISCARDED';

export type LeadVehicleType = '' | 'moto' | 'carro' | 'frota';

/** O que a landing manda no cadastro de interesse. */
export interface LeadInput {
  name: string;
  email: string;
  phone: string;
  city: string;
  plan: string;
  vehicleType: LeadVehicleType;
  vehicleCount: number;
  message: string;
  /** Aceitou ser contatado (LGPD). */
  consent: boolean;
  /** Entrar também na lista de pré-lançamento (aviso e promoção). */
  joinLaunch: boolean;
  /** Isca: escondido, só robô preenche. */
  website: string;
}

export interface Lead {
  id: string;
  name: string;
  email: string;
  phone: string;
  city: string;
  plan: string;
  vehicleType: LeadVehicleType;
  vehicleCount: number;
  message: string;
  status: LeadStatus;
  notes: string;
  /** O cliente cadastrado a partir dele. */
  customerId: string | null;
  /** O e-mail também está na lista de lançamento (e na promoção). */
  onLaunchList: boolean;
  source: string;
  consentAt: string;
  createdAt: string;
  updatedAt: string;
}

/** "Me avise quando lançar": o que a seção de pré-lançamento manda. */
export interface WaitlistInput {
  name: string;
  email: string;
  /** WhatsApp com DDD (obrigatório). */
  phone: string;
  consent: boolean;
  /** Isca: escondido, só robô preenche. */
  website: string;
}

/** Inscrição na lista de lançamento. */
export interface WaitlistEntry {
  id: string;
  name: string;
  email: string;
  /** WhatsApp; vazio em quem se inscreveu antes de ele ser obrigatório. */
  phone: string;
  consentAt: string;
  createdAt: string;
  updatedAt: string;
  /** Já é cliente (conta com este e-mail). */
  customerId: string | null;
  /** Contratou com a promoção de pré-lançamento. */
  promoClaimed: boolean;
}

/** Uma linha de ranking das visitas: visitantes (únicos por dia) e vezes. */
export interface AnalyticsCount {
  key: string;
  visitors: number;
  count: number;
}

/** As visitas da landing num período (sem cookies; ver o pacote analytics). */
export interface LandingAnalytics {
  from: string;
  to: string;
  visitors: number;
  pageviews: number;
  /** Visitantes nos últimos 10 minutos. */
  activeNow: number;
  leadOpens: number;
  leads: number;
  waitlist: number;
  installerOpens: number;
  days: { day: string; visitors: number; pageviews: number }[];
  referrers: AnalyticsCount[];
  campaigns: AnalyticsCount[];
  devices: AnalyticsCount[];
  browsers: AnalyticsCount[];
  systems: AnalyticsCount[];
  sections: AnalyticsCount[];
  clicks: AnalyticsCount[];
}

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

export type TwoFactorMethod = 'totp' | 'email';

/** O login que passou pela senha e espera o segundo fator (ou a ativação, a equipe). */
export interface TwoFactorChallenge {
  challenge: string;
  kind: 'verify' | 'setup';
  method: TwoFactorMethod | '';
  emailHint: string;
  expiresAt: string;
}

/** A resposta do login: a sessão aberta, ou o segundo fator que falta. */
export interface LoginResponse extends Partial<AuthTokens> {
  twoFactor?: TwoFactorChallenge;
  /** O aparelho confiável (quando pedido): dispensa o código por 30 dias. */
  deviceToken?: string;
  /** Os códigos de recuperação (na ativação). */
  recoveryCodes?: string[];
}

/** O que o app autenticador recebe para cadastrar a conta. */
export interface TwoFactorEnrollment {
  secret: string;
  otpauthUrl: string;
  /** O QR Code em SVG. */
  qrCode: string;
}

export interface TrustedDevice {
  id: string;
  userAgent: string;
  createdAt: string;
  lastUsedAt: string;
  expiresAt: string;
}

/** A verificação em duas etapas de quem está conectado. */
export interface TwoFactorStatus {
  enabled: boolean;
  method: TwoFactorMethod | '';
  required: boolean;
  enabledAt: string | null;
  methods: TwoFactorMethod[];
  recoveryCodesLeft: number;
  devices: TrustedDevice[];
  emailHint: string;
}

/** O resumo da verificação (listas da equipe e ficha do cliente). */
export interface TwoFactorBrief {
  enabled: boolean;
  method: TwoFactorMethod | '';
}

/** Alguém da equipe, com a verificação em duas etapas. */
export interface TeamMember extends User {
  twoFactor: TwoFactorBrief;
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
  /** O ICCID do chip (só dígitos; vazio se não informado). A equipe vê; o cliente, não. */
  iccid: string;
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

/** Carro ou moto: o mapa desenha um ou outro. */
export type VehicleKind = 'CAR' | 'MOTORCYCLE';

export interface Vehicle {
  id: string;
  name: string;
  kind: VehicleKind;
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
  /** O modo roubo ligado (ausente: desligado). */
  theft?: TheftBrief | null;
}

/** O modo roubo ligado, na lista de veículos. */
export interface TheftBrief {
  since: string;
  expiresAt: string;
}

/** Situação de um comando do modo roubo no rastreador. */
export type TheftCommandStatus = 'PENDING' | 'SENT' | 'SKIPPED';

/** O modo roubo de um veículo. */
export interface TheftMode {
  id: string;
  vehicleId: string;
  activatedByName: string;
  activatedAt: string;
  expiresAt: string;
  /** O intervalo curto chegou ao rastreador? (fora do ar: vai quando ele voltar) */
  boostStatus: TheftCommandStatus;
  /** O comando pela conexão do rastreador: SENT, ACKNOWLEDGED, TIMEOUT... */
  boostCommandStatus: string;
  /** Quando foi também por SMS (rastreador fora do ar na ativação). */
  boostSmsAt: string | null;
  endedAt: string | null;
  outcome: '' | 'RECOVERED' | 'CANCELLED' | 'EXPIRED';
  restoreStatus: '' | TheftCommandStatus;
}

/** O modo roubo na tela do veículo. */
export interface TheftView {
  mode: TheftMode | null;
  /** O link da posição ao vivo, sem login (para a polícia). */
  publicUrl: string;
  /** O dono, a equipe e quem pode bloquear o motor ligam. */
  canActivate: boolean;
  /** Só o dono (e a equipe) desliga. */
  canEnd: boolean;
  parkedSeconds: number;
  durationHours: number;
}

/** O que o link público mostra: o veículo e onde ele está. */
export interface PublicTheftView {
  vehicle: { name: string; plate: string; brand: string; model: string; year: number | null; color: string };
  activatedAt: string;
  expiresAt: string;
  position: {
    latitude: number;
    longitude: number;
    speedKmh: number;
    heading: number | null;
    gpsTimestamp: string;
    receivedAt: string;
  } | null;
  address: string;
  ignition: boolean | null;
  online: boolean;
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
  /**
   * Reajuste anual já avisado: as faturas que vencem a partir de
   * nextPriceFrom saem por nextPriceCents. Nulos: sem mudança agendada.
   */
  nextPriceCents: number | null;
  nextPriceFrom: DateOnly | null;
  /**
   * Rastreador parcelado sem juros: em quantas vezes (0: à vista) e até
   * quando a assinatura fica ativa (o vencimento da mensalidade com a última
   * parcela). O andamento: o valor parcelado, a parcela, quantas foram pagas
   * e quanto falta pagar (zeros quando à vista).
   */
  installments: number;
  commitmentUntil: DateOnly | null;
  equipmentCents: number;
  installmentCents: number;
  installmentsPaid: number;
  installmentsDueCents: number;
  createdAt: string;
  updatedAt: string;
}

/** Uma mensalidade reajustada no ano. */
export interface PriceAdjustmentItem {
  subscriptionId: string;
  customerId: string;
  customerName: string;
  vehicle: string;
  plan: string;
  oldPriceCents: number;
  newPriceCents: number;
  notified: boolean;
}

/** O reajuste anual pelo IPCA de um ano. */
export interface PriceAdjustment {
  year: number;
  period: string;
  rate: string;
  rateMillionths: number;
  effectiveFrom: DateOnly;
  status: 'NOTIFIED' | 'APPLIED' | 'CANCELED' | 'NO_CHANGE';
  subscriptions: number;
  notifiedAt: string;
  canceledAt: string | null;
  cancelUntil: DateOnly;
  canCancel: boolean;
  monthlyDiffCents: number;
  items: PriceAdjustmentItem[];
}

export interface PriceAdjustmentOverview {
  enabled: boolean;
  upcoming: { year: number; noticeDate: DateOnly; startDate: DateOnly; period: string } | null;
  adjustments: PriceAdjustment[];
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
  /** O frete escolhido no pedido (e cobrado do cliente); nulo se o pedido saiu sem frete. */
  quotedServiceId: number | null;
  quotedService: string;
  quotedPriceCents: number | null;
  quotedDays: number | null;
  /** O cliente combina a entrega com a central (sem frete nem etiqueta). */
  deliveryArranged: boolean;
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
  /** A entrega escolhida no pedido e o prazo (dias úteis). */
  deliveryService: string;
  deliveryDays: number | null;
  /** A entrega é combinada com a central. */
  deliveryArranged: boolean;
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

/** As formas de entrega para o pedido de um rastreador. */
export interface ShippingQuoteView {
  /** O frete é cotado (Melhor Envios ligado); sem isso, o pedido segue sem frete. */
  enabled: boolean;
  zipCode: string;
  /** Só os serviços que atendem o CEP, do mais barato ao mais caro. */
  quotes: ShippingQuote[];
  /** O que impediu a cotação (sem endereço, Melhor Envios fora). */
  problem: string;
  /** O endereço fica onde dá para combinar a entrega com a central (sem frete). */
  arrange: boolean;
}

/** O frete cobrado no pedido. */
export interface ShippingChoice {
  serviceId: number;
  name: string;
  priceCents: number;
  deliveryDays: number;
  /** Entrega combinada com a central (sem frete). */
  arranged: boolean;
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
  /** O painel do Melhor Envios (onde fica a carteira). */
  panelUrl: string;
  /** A recarga por Pix pode sair do saldo da AbacatePay. */
  payWithAbacate: boolean;
}

/** A carteira do Melhor Envios: o saldo que paga as etiquetas. */
export interface ShippingBalance {
  balanceCents: number;
  reservedCents: number;
  debtsCents: number;
  checkedAt: string;
  panelUrl: string;
}

export type ShippingTopUpMethod = 'pix' | 'boleto';

/** A cobrança gerada para pôr saldo na carteira (paga no Melhor Envios). */
export interface ShippingTopUp {
  id: string;
  providerId: string;
  protocol: string;
  status: string;
  method: ShippingTopUpMethod;
  valueCents: number;
  /** A página do Pix (QR Code) ou o PDF do boleto. */
  link: string;
  /** A linha digitável do boleto. */
  digitable: string;
  /** O Pix copia-e-cola, quando o Melhor Envios devolve. */
  pixCode: string;
  /** A conta a pagar, quando a recarga sai pela AbacatePay. */
  entryId: string | null;
  createdAt: string;
  panelUrl: string;
}

export interface CustomerDetail extends CustomerSummary {
  /** A verificação em duas etapas do cliente. */
  twoFactor: TwoFactorBrief;
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
  /** Quem indicou o cliente (programa de afiliados); nulo se ninguém. */
  affiliate: CustomerReferral | null;
  /** O último aceite do contrato (nulo: ainda não aceitou). */
  contract: ContractAcceptance | null;
  /** A versão do contrato em vigor. */
  contractVersion: string;
  /** O último lembrete de cada fatura (pelo id). */
  reminders: Record<string, InvoiceReminder>;
  /** O link de pagamento sem login das faturas em aberto (pelo id). */
  paymentLinks: Record<string, string>;
}

/** Um parágrafo (text) ou uma lista (items) do contrato. */
export interface ContractBlock {
  kind: 'p' | 'ul';
  text?: string;
  items?: string[];
}

/** Uma cláusula do contrato ("6. Permanência mínima e multa"). */
export interface ContractSection {
  title: string;
  blocks: ContractBlock[];
}

/** O contrato de prestação de serviços em vigor. */
export interface ContractDocument {
  version: string;
  effectiveDate: string;
  title: string;
  intro: ContractBlock[];
  sections: ContractSection[];
  /** O hash do texto: o aceite guarda qual texto foi aceito. */
  sha256: string;
  /** O que mudou desde a versão anterior. */
  changes: string;
}

/** O registro do aceite eletrônico. */
export interface ContractAcceptance {
  version: string;
  sha256: string;
  name: string;
  /** CPF/CNPJ (só os números). */
  document: string;
  ip: string;
  userAgent: string;
  acceptedAt: string;
}

/** O contrato do ponto de vista do cliente. */
export interface ContractStatus {
  contract: ContractDocument;
  /** Precisa aceitar esta versão para usar a plataforma. */
  required: boolean;
  /** O aceite desta versão (nulo: ainda não aceitou). */
  accepted: ContractAcceptance | null;
  /** O aceite de uma versão anterior (quem precisa aceitar de novo). */
  previous: ContractAcceptance | null;
  name: string;
  /** CPF/CNPJ do cadastro (vazio se ainda não informado). */
  taxId: string;
}

/** As etapas da régua de cobrança (e o lembrete pedido pela central). */
export type ReminderKind = 'ISSUED' | 'DUE_SOON' | 'DUE_TODAY' | 'OVERDUE' | 'SUSPENSION_SOON' | 'MANUAL';

/** Um lembrete de fatura que saiu (e-mail e push). */
export interface InvoiceReminder {
  invoiceId: string;
  kind: ReminderKind;
  emailed: boolean;
  /** Em quantos aparelhos o push chegou. */
  pushed: number;
  createdAt: string;
}

/** A fatura no link de pagamento (sem login): só o necessário para pagar. */
export interface PublicInvoice {
  firstName: string;
  description: string;
  amountCents: number;
  dueDate: string;
  status: 'OPEN' | 'PAID' | 'CANCELED';
  overdue: boolean;
  daysOverdue: number;
  paidAt: string | null;
  /** O Pix sai pela AbacatePay; sem isso, o link ou o Pix informados pela central. */
  onlinePayment: boolean;
  paymentUrl: string;
  pixCode: string;
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
  /** Em até quantas vezes sem juros o rastreador pode ser parcelado (1: só à vista). */
  equipmentMaxInstallments: number;
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
  /** Fatura do equipamento (com o frete); nula quando nada foi cobrado. */
  setupInvoice: Invoice | null;
  /** O frete cobrado; nulo se o pedido saiu sem frete. */
  shipping: ShippingChoice | null;
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
  /** O código do link de indicação do afiliado. */
  ref?: string;
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
  /** O cliente cadastrado a partir dele (ou a conta de cliente com o mesmo e-mail). */
  customerId: string | null;
  /** Está na lista de lançamento (o direito à promoção). */
  onLaunchList: boolean;
  /** landing (pré-cadastro), lancamento (a caixa da landing), evento ou indicacao. */
  source: string;
  consentAt: string;
  createdAt: string;
  updatedAt: string;
  /** O afiliado do link por onde chegou: o @ (ou o nome); vazio se nenhum. */
  affiliateId: string | null;
  referrer: string;
  /** O evento (QR Code) em que se inscreveu; vazio se não foi num. */
  event: string;
  /** Já contratou com a promoção de pré-lançamento. */
  promoClaimed: boolean;
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
  /** O evento da tela aberta pelo QR Code (/evento/<nome>). */
  event?: string;
  /** A cidade da instalação (a tela do evento pede). */
  city?: string;
  /** O código do link de indicação do afiliado. */
  ref?: string;
}

/** Inscrição na lista de lançamento. */
export interface WaitlistEntry {
  id: string;
  name: string;
  email: string;
  /** WhatsApp; vazio em quem se inscreveu antes de ele ser obrigatório. */
  phone: string;
  /** O evento em que se inscreveu (QR Code); vazio: pela landing. */
  event: string;
  /** A cidade da instalação; vazio quando não informou. */
  city: string;
  consentAt: string;
  createdAt: string;
  updatedAt: string;
  /** Já é cliente (conta com este e-mail). */
  customerId: string | null;
  /** Contratou com a promoção de pré-lançamento. */
  promoClaimed: boolean;
  /** O afiliado do link por onde chegou: o @ (ou o nome); vazio se nenhum. */
  affiliateId: string | null;
  referrer: string;
}

/** Uma linha de ranking das visitas: visitantes (únicos por dia) e vezes. */
export interface AnalyticsCount {
  key: string;
  visitors: number;
  count: number;
}

/** As visitas da landing num período (sem cookies; ver o pacote analytics). */
/** O funil de uma origem (Instagram, Google, direto...) no período. */
export interface AnalyticsOrigin {
  channel: string;
  visitors: number;
  leadOpens: number;
  leads: number;
  waitlist: number;
  /** Viraram contato: mandaram o pré-cadastro ou entraram na lista. */
  converted: number;
  clicked: number;
}

export interface LandingAnalytics {
  from: string;
  to: string;
  /** O resumo é só de quem veio por essa origem ("" = todas). */
  channel: string;
  visitors: number;
  pageviews: number;
  /** Visitantes nos últimos 10 minutos. */
  activeNow: number;
  leadOpens: number;
  leads: number;
  waitlist: number;
  installerOpens: number;
  days: { day: string; visitors: number; pageviews: number }[];
  /** O funil de cada origem, sempre de todas. */
  origins: AnalyticsOrigin[];
  campaigns: AnalyticsCount[];
  devices: AnalyticsCount[];
  browsers: AnalyticsCount[];
  systems: AnalyticsCount[];
  sections: AnalyticsCount[];
  clicks: AnalyticsCount[];
}

/** Último envio da cópia criptografada do banco para fora da VPS. */
export interface InfraOffsite {
  configured: boolean;
  ok: boolean;
  at: string;
  file: string;
  error: string;
}

/** O painel de infraestrutura (Diagnóstico → Servidor). */
export interface InfraStatus {
  host: {
    at: string;
    cpuPercent: number;
    cores: number;
    load1: number;
    load5: number;
    load15: number;
    memUsed: number;
    memTotal: number;
    diskUsed: number;
    diskTotal: number;
    /** Há quanto tempo a máquina está ligada (segundos). */
    hostUptime: number;
  };
  /** Um ponto por minuto, em porcentagem (recomeça quando o servidor reinicia). */
  history: { at: string; cpu: number; mem: number; disk: number }[];
  process: { startedAt: string; goVersion: string; goroutines: number; heapBytes: number; rssBytes: number };
  database: {
    enabled: boolean;
    ok: boolean;
    latencyMs: number;
    error?: string;
    sizeBytes: number;
    connections: number;
    maxConnections: number;
    tables: { name: string; sizeBytes: number; rows: number }[] | null;
  };
  redis: { enabled: boolean; ok: boolean; latencyMs: number; error?: string };
  backups: {
    available: boolean;
    count: number;
    totalBytes: number;
    latest: { name: string; at: string; sizeBytes: number } | null;
    /** A cópia fora da VPS (null antes da primeira volta do postgres-backup). */
    offsite: InfraOffsite | null;
  };
  logs: { errors24h: number; warnings24h: number; dropped: number; failed: number };
  live: { trackerConnections: number; realtimeClients: number; onlineDevices: number };
}

/** Um aviso ou erro do servidor. */
export interface SystemLogEntry {
  id: number;
  at: string;
  level: 'WARN' | 'ERROR';
  component: string;
  message: string;
  attrs: Record<string, unknown>;
}

export interface SystemLogPage {
  groups: { level: 'WARN' | 'ERROR'; component: string; message: string; count: number; lastAt: string }[];
  entries: SystemLogEntry[];
}

// ---------------------------------------------------------------------------
// Gestão da empresa (admin)
// ---------------------------------------------------------------------------

export type EntryKind = 'PAYABLE' | 'RECEIVABLE';
export type EntryStatus = 'OPEN' | 'PAID' | 'CANCELED';
export type PaymentMethod = '' | 'PIX' | 'BOLETO' | 'CARD' | 'TRANSFER' | 'CASH' | 'DEBIT';
/** A linha do resultado do mês (DRE) de cada categoria. */
export type DreGroup =
  | 'REVENUE'
  | 'OTHER_INCOME'
  | 'CAPITAL_IN'
  | 'TAX'
  | 'COST'
  | 'OPERATING'
  | 'FINANCIAL'
  | 'INVESTMENT'
  | 'CAPITAL_OUT';

export interface FinanceCategory {
  id: string;
  name: string;
  kind: 'EXPENSE' | 'INCOME';
  group: DreGroup;
  active: boolean;
}

export interface Supplier {
  id: string;
  name: string;
  document: string;
  email: string;
  phone: string;
  pixKey: string;
  /** Tipo da chave Pix; vazio quando o servidor deduz pelo formato. */
  pixKeyType: '' | PixKeyType;
  notes: string;
  active: boolean;
  createdAt: string;
}

/** Tipos de chave Pix (BR_CODE: o Pix copia-e-cola da conta). */
export type PixKeyType = 'CPF' | 'CNPJ' | 'PHONE' | 'EMAIL' | 'RANDOM' | 'BR_CODE';

/**
 * Um Pix a fornecedor pela AbacatePay. SENDING: enviando; COMPLETE: saiu;
 * FAILED: recusado ou falhou (a conta segue em aberto); UNKNOWN: sem
 * resposta — conferir no painel da AbacatePay.
 */
export interface PixTransfer {
  id: string;
  entryId: string;
  providerId: string;
  status: 'SENDING' | 'COMPLETE' | 'FAILED' | 'UNKNOWN';
  /** O que o fornecedor deve receber (o valor da conta). */
  amountCents: number;
  /** O que foi enviado pela AbacatePay: a conta + a tarifa (ela desconta a tarifa do envio). */
  sentCents: number;
  /** O que chegou ao fornecedor (enviado menos a tarifa cobrada); 0 antes de concluir. */
  deliveredCents: number;
  feeCents: number;
  key: string;
  keyType: PixKeyType;
  receiptUrl: string;
  devMode: boolean;
  error: string;
  createdAt: string;
  completedAt: string | null;
}

/** O Pix pela AbacatePay: ligado, modo e saldo. */
export interface PixInfo {
  enabled: boolean;
  devMode: boolean;
  availableCents: number | null;
  balanceError: string;
}

/** Para onde e quanto sai, antes de confirmar. */
export interface PixPlan {
  entryId: string;
  description: string;
  supplierName: string;
  amountCents: number;
  /** code: o Pix copia-e-cola da conta; key: a chave do fornecedor. */
  source: 'code' | 'key';
  key: string;
  keyType: PixKeyType;
  /** O recebedor como está no copia-e-cola. */
  recipient: string;
  /** A tarifa da AbacatePay (paga pela empresa) e o que sai do saldo (a conta + a tarifa). */
  feeCents: number;
  sendCents: number;
}

export interface FinanceAttachment {
  id: string;
  entryId: string;
  filename: string;
  contentType: string;
  sizeBytes: number;
  createdAt: string;
}

/** Uma conta a pagar ou uma receita avulsa. */
export interface FinanceEntry {
  id: string;
  kind: EntryKind;
  description: string;
  categoryId: string;
  categoryName: string;
  group: DreGroup;
  supplierId: string | null;
  supplierName: string;
  amountCents: number;
  dueDate: DateOnly;
  status: EntryStatus;
  paidOn: DateOnly | null;
  paidCents: number | null;
  /** Em aberto com o vencimento no passado. */
  overdue: boolean;
  paymentMethod: PaymentMethod;
  paymentCode: string;
  notes: string;
  recurrenceId: string | null;
  installment: number | null;
  installments: number | null;
  stockMovementId: string | null;
  attachments: FinanceAttachment[];
  /** O último Pix pela AbacatePay (nulo se nunca houve). */
  pix: PixTransfer | null;
  createdAt: string;
  updatedAt: string;
}

export interface FinanceRecurrence {
  id: string;
  kind: EntryKind;
  description: string;
  categoryId: string;
  categoryName: string;
  supplierId: string | null;
  supplierName: string;
  amountCents: number;
  dueDay: number;
  nextDueDate: DateOnly;
  endsOn: DateOnly | null;
  active: boolean;
  createdAt: string;
}

export interface FinanceSum {
  count: number;
  cents: number;
}

export interface CashMonth {
  month: string;
  invoicesCents: number;
  otherInCents: number;
  inCents: number;
  /** Tudo o que saiu (contas pagas e tarifas). */
  outCents: number;
  /** A parte das saídas que são tarifas e taxas (AbacatePay, bancárias). */
  feesCents: number;
  netCents: number;
  /** null nos meses antes do saldo inicial. */
  endBalanceCents: number | null;
}

export interface CashProjection {
  days: number;
  until: DateOnly;
  inCents: number;
  outCents: number;
  balanceCents: number;
}

export interface FinanceSettings {
  openingBalanceCents: number;
  openingDate: DateOnly;
}

export interface CashFlow {
  settings: FinanceSettings;
  today: DateOnly;
  balanceCents: number;
  months: CashMonth[];
  projections: CashProjection[];
}

export interface DreMonth {
  month: string;
  invoicesCents: number;
  revenueCents: number;
  taxesCents: number;
  netRevenueCents: number;
  costsCents: number;
  stockCostCents: number;
  stockLossCents: number;
  grossProfitCents: number;
  operatingCents: number;
  financialCents: number;
  otherIncomeCents: number;
  resultCents: number;
  investmentsCents: number;
  capitalInCents: number;
  capitalOutCents: number;
  categories: { categoryId: string; name: string; group: DreGroup; cents: number }[];
}

export type StockKind = 'TRACKER' | 'SIM' | 'ACCESSORY' | 'OTHER';
export type StockMoveType = 'IN' | 'OUT' | 'LOSS' | 'ADJUST';

export interface StockItem {
  id: string;
  name: string;
  kind: StockKind;
  minQuantity: number;
  quantity: number;
  avgCostCents: number;
  valueCents: number;
  low: boolean;
  active: boolean;
  createdAt: string;
}

export interface StockMovement {
  id: string;
  itemId: string;
  itemName: string;
  type: StockMoveType;
  /** Com sinal: entrada positiva, saída negativa. */
  quantity: number;
  unitCostCents: number;
  totalCents: number;
  occurredOn: DateOnly;
  supplierId: string | null;
  supplierName: string;
  notes: string;
  createdAt: string;
}

export interface FinanceOverview {
  today: DateOnly;
  balanceCents: number;
  overdue: FinanceSum;
  dueToday: FinanceSum;
  dueWeek: FinanceSum;
  receivableOverdue: FinanceSum;
  month: CashMonth;
  monthResultCents: number;
  upcoming: FinanceEntry[];
  lowStock: StockItem[];
  stockValueCents: number;
  projections: CashProjection[];
}

export interface FinanceAlerts {
  overdue: number;
  dueToday: number;
  lowStock: number;
}

// ---------------------------------------------------------------------------
// Programa de afiliados
// ---------------------------------------------------------------------------

/** Um afiliado (influenciador), com os números do programa. */
export interface Affiliate {
  id: string;
  name: string;
  /** O @ do Instagram, sem a arroba. */
  handle: string;
  /** O código do link de cadastro (/indicacao/<code>). */
  code: string;
  /** O segredo da página do afiliado (/parceiro/<token>). */
  reportToken: string;
  /** Por cliente indicado, por mês pago. */
  commissionCents: number;
  email: string;
  phone: string;
  pixKey: string;
  notes: string;
  supplierId: string | null;
  active: boolean;
  createdAt: string;
  /** Pessoas que se cadastraram pelo link (lista ou pré-cadastro). */
  signups: number;
  customers: number;
  /** Clientes indicados com assinatura ativa. */
  activeCustomers: number;
  /** Ainda não fechado. */
  pendingCents: number;
  /** Fechado, a pagar na Empresa. */
  openCents: number;
  paidCents: number;
}

export interface AffiliateInput {
  name: string;
  handle: string;
  code: string;
  /** Nulo: o valor padrão. */
  commissionCents: number | null;
  email: string;
  phone: string;
  pixKey: string;
  notes: string;
  active: boolean;
}

export interface AffiliateSettings {
  defaultCommissionCents: number;
  categoryId: string | null;
}

/** O afiliado como a tela de cadastro mostra ("Indicado por @fulano"). */
export interface PublicAffiliate {
  name: string;
  handle: string;
  code: string;
}

/** Quem indicou o cliente. */
export interface CustomerReferral {
  affiliateId: string;
  name: string;
  handle: string;
  /** lead (pré-cadastro), waitlist (lista de lançamento) ou admin. */
  source: 'lead' | 'waitlist' | 'admin';
  since: string;
}

/** O que um afiliado tem a receber no fechamento do mês. */
export interface AffiliateClosingLine {
  affiliateId: string;
  name: string;
  handle: string;
  pixKey: string;
  commissions: number;
  amountCents: number;
}

/** Um fechamento: as comissões de um afiliado viram uma conta a pagar. */
export interface AffiliatePayout {
  id: string;
  affiliateId: string;
  affiliateName: string;
  /** AAAA-MM: o mês fechado (entram as comissões até ele). */
  month: string;
  amountCents: number;
  commissions: number;
  entryId: string | null;
  /** MISSING: a conta foi excluída ou cancelada na Empresa. */
  status: 'OPEN' | 'PAID' | 'MISSING';
  dueDate: string | null;
  paidOn: string | null;
  createdAt: string;
}

/** Um mês na página do afiliado. */
export interface PartnerMonth {
  month: string;
  customers: number;
  amountCents: number;
  /** pending: o mês não foi fechado; closed: fechado, a pagar; paid: pago. */
  status: 'pending' | 'closed' | 'paid';
}

/** A página do afiliado (link secreto): os números, sem dado de ninguém. */
export interface PartnerReport {
  name: string;
  handle: string;
  code: string;
  active: boolean;
  commissionCents: number;
  signups: number;
  customers: number;
  activeCustomers: number;
  toReceiveCents: number;
  paidCents: number;
  months: PartnerMonth[];
}

// ---------------------------------------------------------------------------
// Configuração do rastreador por SMS (SMSDev) na ativação
// ---------------------------------------------------------------------------

/** Um comando de configuração (texto redigido: as senhas viram ***). */
export interface SmsStep {
  kind: 'UNLOCK' | 'APN' | 'SERVER' | 'GMT' | 'TIMER' | 'PARAM';
  label: string;
  text: string;
}

/** Os comandos que vão sair e o que falta no cadastro. */
export interface SmsPlan {
  /** O número do chip (E.164); vazio se não cadastrado. */
  phone: string;
  steps: SmsStep[];
  problems: string[];
  /** Os opcionais: destravar o canal (CMDLOCK) e pedir a configuração de volta (PARAM#). */
  unlock: SmsStep;
  query: SmsStep;
}

export interface SmsStepView extends SmsStep {
  /** pending (ainda não saiu), queued, sending, sent, delivered, undelivered, failed. */
  status: string;
  error: string;
  sentAt: string | null;
}

/**
 * Uma configuração por SMS. SENDING: mandando os comandos; WAITING: todos
 * enviados, esperando o rastreador conectar; DONE: conectou; FAILED: um SMS
 * não saiu ou não chegou; TIMEOUT: não conectou a tempo; CANCELED.
 */
export interface SmsSession {
  id: string;
  deviceId: string;
  fulfillmentId: string | null;
  phone: string;
  status: 'SENDING' | 'WAITING' | 'DONE' | 'FAILED' | 'TIMEOUT' | 'CANCELED';
  error: string;
  note: string;
  steps: SmsStepView[];
  replies: { body: string; at: string }[];
  waitingSince: string | null;
  connectedAt: string | null;
  finishedAt: string | null;
  createdAt: string;
}

/** O contador da página de rastreadores: o saldo no SMSDev e quantas ativações ele paga. */
export interface SmsUsage {
  /** O SMSDev está configurado no servidor. */
  enabled: boolean;
  /** Saldo em SMS; nulo se não deu para ler (balanceError diz por quê). */
  balance: number | null;
  balanceError: string;
  /** SMS por ativação padrão (os opcionais somam 1 cada). */
  activationSms: number;
  /** Quantas ativações o saldo paga. */
  activations: number;
  /** SMS que saíram nos últimos 30 dias (sem os recusados). */
  sentLast30Days: number;
}

export interface SmsSetupView {
  /** O SMSDev está configurado. */
  enabled: boolean;
  /** Quem envia (o SMSDev). */
  from: string;
  plan: SmsPlan;
  /** A última configuração deste rastreador. */
  session: SmsSession | null;
}

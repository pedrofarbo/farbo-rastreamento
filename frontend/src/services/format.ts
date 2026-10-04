/** Formatação de valores exibidos no painel. */

import type { DeliveryAddress } from '@/types';

const dateTimeFormatter = new Intl.DateTimeFormat('pt-BR', {
  day: '2-digit',
  month: '2-digit',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
});

const timeFormatter = new Intl.DateTimeFormat('pt-BR', {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
});

export function formatDateTime(value: string | Date | null | undefined): string {
  if (!value) return '—';
  const date = typeof value === 'string' ? new Date(value) : value;
  if (Number.isNaN(date.getTime())) return '—';
  return dateTimeFormatter.format(date);
}

export function formatTime(value: string | Date | null | undefined): string {
  if (!value) return '—';
  const date = typeof value === 'string' ? new Date(value) : value;
  if (Number.isNaN(date.getTime())) return '—';
  return timeFormatter.format(date);
}

/** Tempo decorrido em texto curto: "agora", "há 3 min", "há 2 h". */
export function formatRelative(value: string | null | undefined): string {
  if (!value) return 'nunca';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';

  const seconds = Math.floor((Date.now() - date.getTime()) / 1000);
  if (seconds < 0) return 'agora';
  if (seconds < 45) return 'agora';
  if (seconds < 90) return 'há 1 min';

  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `há ${minutes} min`;

  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `há ${hours} h`;

  const days = Math.floor(hours / 24);
  if (days < 30) return `há ${days} d`;

  return formatDateTime(date);
}

export function formatSpeed(kmh: number | null | undefined): string {
  if (kmh === null || kmh === undefined) return '—';
  return `${kmh.toFixed(0)} km/h`;
}

export function formatCoordinates(lat: number, lon: number): string {
  return `${lat.toFixed(6)}, ${lon.toFixed(6)}`;
}

export function formatDistance(meters: number): string {
  if (meters < 1000) return `${meters.toFixed(0)} m`;
  return `${(meters / 1000).toFixed(1)} km`;
}

export function formatDuration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}min`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}min`;
}

/** Converte o rumo em graus para ponto cardeal. */
export function formatHeading(heading: number | null | undefined): string {
  if (heading === null || heading === undefined) return '—';
  const points = ['N', 'NE', 'L', 'SE', 'S', 'SO', 'O', 'NO'];
  const index = Math.round(heading / 45) % 8;
  return `${points[index]} (${heading.toFixed(0)}°)`;
}

const statusLabels: Record<string, string> = {
  ONLINE: 'Online',
  STALE: 'Sinal fraco',
  OFFLINE: 'Offline',
};

export function formatDeviceStatus(status: string | null | undefined): string {
  if (!status) return 'Sem rastreador';
  return statusLabels[status] ?? status;
}

const commandLabels: Record<string, string> = {
  ENGINE_CUT: 'Desligar motor',
  ENGINE_RESUME: 'Liberar motor',
  REQUEST_POSITION: 'Solicitar posição',
  REQUEST_STATUS: 'Solicitar status',
  SET_INTERVAL: 'Definir intervalo',
  SET_HEARTBEAT: 'Definir heartbeat',
  SET_SERVER: 'Definir servidor',
  REBOOT: 'Reiniciar',
  CUSTOM: 'Comando livre',
};

export function formatCommand(command: string): string {
  return commandLabels[command] ?? command;
}

const commandStatusLabels: Record<string, string> = {
  PENDING: 'Na fila',
  SENDING: 'Enviando',
  SENT: 'Enviado, aguardando confirmação',
  ACKNOWLEDGED: 'Confirmado pelo rastreador',
  FAILED: 'Falhou',
  TIMEOUT: 'Sem resposta',
  REJECTED: 'Recusado pela regra de segurança',
};

export function formatCommandStatus(status: string): string {
  return commandStatusLabels[status] ?? status;
}

const eventLabels: Record<string, string> = {
  IGNITION_ON: 'Ignição ligada',
  IGNITION_OFF: 'Ignição desligada',
  OVERSPEED: 'Excesso de velocidade',
  OVERSPEED_END: 'Velocidade normalizada',
  GEOFENCE_ENTER: 'Entrou na cerca',
  GEOFENCE_EXIT: 'Saiu da cerca',
  VIBRATION: 'Vibração',
  POWER_LOSS: 'Queda de energia',
  LOW_BATTERY: 'Bateria fraca',
  SOS: 'Botão de pânico',
  GPS_LOST: 'GPS perdido',
  GPS_RECOVERED: 'GPS recuperado',
  GSM_LOST: 'Sinal de rede perdido',
  GSM_RECOVERED: 'Sinal de rede recuperado',
  ENGINE_CUT_REQUESTED: 'Corte solicitado',
  ENGINE_CUT_SENT: 'Corte enviado',
  ENGINE_CUT_ACK: 'Corte confirmado',
  ENGINE_RESUME_REQUESTED: 'Liberação solicitada',
  ENGINE_RESUME_SENT: 'Liberação enviada',
  ENGINE_RESUME_ACK: 'Liberação confirmada',
  DEVICE_CONNECTED: 'Rastreador conectou',
  DEVICE_DISCONNECTED: 'Rastreador desconectou',
  DEVICE_STALE: 'Rastreador sem comunicação',
  DEVICE_ALARM: 'Alarme do rastreador',
};

export function formatEvent(type: string): string {
  return eventLabels[type] ?? type;
}

/** Severidade do evento, usada para colorir a lista. */
export function eventSeverity(type: string): 'danger' | 'warning' | 'success' | 'neutral' {
  switch (type) {
    case 'SOS':
    case 'POWER_LOSS':
    case 'ENGINE_CUT_ACK':
      return 'danger';
    case 'OVERSPEED':
    case 'LOW_BATTERY':
    case 'GPS_LOST':
    case 'GSM_LOST':
    case 'DEVICE_DISCONNECTED':
    case 'DEVICE_STALE':
    case 'VIBRATION':
    case 'DEVICE_ALARM':
      return 'warning';
    case 'ENGINE_RESUME_ACK':
    case 'GPS_RECOVERED':
    case 'GSM_RECOVERED':
    case 'DEVICE_CONNECTED':
    case 'OVERSPEED_END':
      return 'success';
    default:
      return 'neutral';
  }
}

/** Data ISO de N horas atrás, para os atalhos de período do histórico. */
export function hoursAgo(hours: number): string {
  return new Date(Date.now() - hours * 3600 * 1000).toISOString();
}

export function nowISO(): string {
  return new Date().toISOString();
}

const moneyFormatter = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });

/** Centavos → "R$ 69,90". */
export function formatMoney(cents: number | null | undefined): string {
  if (cents === null || cents === undefined) return '—';
  return moneyFormatter.format(cents / 100);
}

/**
 * "69,90", "69.90", "R$ 1.234,56" → centavos. Devolve null se não for um
 * valor válido.
 */
export function parseMoney(text: string): number | null {
  let clean = text.replace(/[R$\s]/g, '');
  if (!clean) return null;
  // Com vírgula, o ponto é separador de milhar (padrão brasileiro).
  if (clean.includes(',')) clean = clean.replace(/\./g, '').replace(',', '.');
  if (!/^\d+(\.\d{1,2})?$/.test(clean)) return null;
  return Math.round(Number(clean) * 100);
}

/** Centavos → "69,90", para preencher um campo de valor. */
export function centsToInput(cents: number): string {
  return (cents / 100).toFixed(2).replace('.', ',');
}

/**
 * Data de calendário "2026-10-05" → "05/10/2026". Não passa por Date: meia
 * noite UTC viraria o dia anterior no horário de Brasília.
 */
export function formatDateOnly(value: string | null | undefined): string {
  if (!value) return '—';
  const [year, month, day] = value.split('-');
  return day && month && year ? `${day}/${month}/${year}` : value;
}

/** "01310100" → "01310-100" (também serve de máscara enquanto digita). */
export function formatZipCode(value: string): string {
  const digits = value.replace(/\D/g, '').slice(0, 8);
  return digits.length > 5 ? `${digits.slice(0, 5)}-${digits.slice(5)}` : digits;
}

/**
 * Endereço de entrega em duas linhas: "Av. Paulista, 1000 - apto 12" e
 * "Bela Vista, São Paulo/SP · CEP 01310-100".
 */
export function formatAddressLines(a: DeliveryAddress): [string, string] {
  const first = `${a.street}, ${a.number}${a.complement ? ` - ${a.complement}` : ''}`;
  const second = `${a.district}, ${a.city}/${a.state} · CEP ${formatZipCode(a.zipCode)}`;
  return [first, second];
}

export function formatAddress(a: DeliveryAddress): string {
  return formatAddressLines(a).join(' · ');
}

/** Hoje no formato AAAA-MM-DD, no fuso do navegador. */
export function todayISO(): string {
  const now = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

/**
 * Máscara do WhatsApp enquanto a pessoa digita: "11999998888" →
 * "(11) 99999-8888" (fixo: "(11) 3333-4444"). Com `previous`, apagar um
 * caractere da máscara (o hífen, o parêntese) apaga o dígito antes dele, em
 * vez de a máscara pôr o caractere de volta.
 */
export function formatPhoneInput(value: string, previous = ''): string {
  let d = value.replace(/\D/g, '');
  if (value.length < previous.length && d === previous.replace(/\D/g, '')) d = d.slice(0, -1);
  if (d.startsWith('55') && d.length > 11) d = d.slice(2);
  d = d.slice(0, 11);
  if (d.length === 0) return '';
  if (d.length <= 2) return `(${d}`;
  const ddd = d.slice(0, 2);
  const rest = d.slice(2);
  if (rest.length <= 4) return `(${ddd}) ${rest}`;
  const split = rest.length === 9 ? 5 : 4;
  return `(${ddd}) ${rest.slice(0, split)}-${rest.slice(split)}`;
}

/** WhatsApp com DDD completo (10 ou 11 dígitos). */
export function isPhoneComplete(value: string): boolean {
  const d = value.replace(/\D/g, '');
  return d.length >= 10 && d.length <= 13;
}

/** 1536 → "1,5 KB"; 3.2e9 → "3,0 GB". */
export function formatBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || !Number.isFinite(bytes) || bytes < 0) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const digits = unit === 0 || value >= 100 ? 0 : 1;
  return `${value.toLocaleString('pt-BR', { minimumFractionDigits: digits, maximumFractionDigits: digits })} ${units[unit]}`;
}

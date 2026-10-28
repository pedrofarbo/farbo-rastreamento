/**
 * A agenda de instalação e de desinstalação: a situação de cada agendamento,
 * o agrupamento por dia e a mensagem que vai ao instalador (com o link do
 * passo a passo).
 */
import type { BadgeTone } from '@/components/ui/Badge';
import type { InstallationAppointment, InstallationKind, InstallationSlot, InstallationStatus, InstallerLink } from '@/types';

/** "Instalação" ou "Desinstalação". */
export const KIND_LABEL: Record<InstallationKind, string> = { install: 'Instalação', uninstall: 'Desinstalação' };

export const INSTALLATION_STATUS: Record<InstallationStatus, { label: string; tone: BadgeTone }> = {
  scheduled: { label: 'Agendada', tone: 'accent' },
  arrived: { label: 'Instalador no local', tone: 'warning' },
  done: { label: 'Instalado', tone: 'success' },
  canceled: { label: 'Cancelada', tone: 'neutral' },
  no_show: { label: 'Cliente ausente', tone: 'danger' },
};

/** A situação do agendamento (concluída: "Instalado" ou "Retirado"). */
export function statusOf(a: Pick<InstallationAppointment, 'status' | 'kind'>): { label: string; tone: BadgeTone } {
  if (a.status === 'done' && a.kind === 'uninstall') return { label: 'Retirado', tone: 'success' };
  return INSTALLATION_STATUS[a.status];
}

export const INSTALLATION_SLOTS: { slot: InstallationSlot; label: string; hint: string }[] = [
  { slot: 'tracker', label: 'Rastreador instalado', hint: 'O equipamento no lugar, com a etiqueta do IMEI visível.' },
  { slot: 'vehicle', label: 'Veículo', hint: 'O veículo inteiro, com a placa aparecendo.' },
  { slot: 'dashboard', label: 'Painel', hint: 'O painel ligado, sem luz de alerta acesa.' },
];

/** As fotos da desinstalação (a do rastreador retirado é obrigatória). */
export const UNINSTALL_SLOTS: { slot: InstallationSlot; label: string; hint: string }[] = [
  { slot: 'tracker', label: 'Rastreador retirado', hint: 'O equipamento na mão, com a etiqueta do IMEI visível.' },
  { slot: 'vehicle', label: 'Veículo', hint: 'O veículo inteiro, com a placa aparecendo.' },
  { slot: 'dashboard', label: 'Painel', hint: 'O painel ligado depois da retirada, sem luz de alerta.' },
];

export function slotsOf(kind: InstallationKind) {
  return kind === 'uninstall' ? UNINSTALL_SLOTS : INSTALLATION_SLOTS;
}

/** Em aberto: agendada ou com o instalador no local. */
export function isOpen(a: Pick<InstallationAppointment, 'status'>): boolean {
  return a.status === 'scheduled' || a.status === 'arrived';
}

/** O dia (AAAA-MM-DD, no fuso do navegador) de um horário. */
export function dayKey(value: string | Date): string {
  const d = typeof value === 'string' ? new Date(value) : value;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** Os agendamentos por dia, em ordem de horário. */
export function groupByDay(list: InstallationAppointment[]): { day: string; items: InstallationAppointment[] }[] {
  const sorted = [...list].sort((a, b) => Date.parse(a.scheduledFor) - Date.parse(b.scheduledFor));
  const out: { day: string; items: InstallationAppointment[] }[] = [];
  for (const a of sorted) {
    const day = dayKey(a.scheduledFor);
    const last = out[out.length - 1];
    if (last?.day === day) last.items.push(a);
    else out.push({ day, items: [a] });
  }
  return out;
}

/** "sexta, 24/10". */
export function dayLabel(day: string): string {
  const [y, m, d] = day.split('-').map(Number);
  const date = new Date(y, m - 1, d);
  const weekday = date.toLocaleDateString('pt-BR', { weekday: 'long' }).replace('-feira', '');
  return `${weekday}, ${String(d).padStart(2, '0')}/${String(m).padStart(2, '0')}`;
}

/** "14:30". */
export function hourLabel(value: string): string {
  return new Date(value).toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' });
}

/** A mensagem ao instalador, com o link do passo a passo. */
export function installerMessage(a: InstallationAppointment): string {
  const when = `${dayLabel(dayKey(a.scheduledFor))} às ${hourLabel(a.scheduledFor)}`;
  const vehicle = [a.vehicleName, a.plate].filter(Boolean).join(' · ');
  const lines = [
    `Olá${a.installerName ? `, ${a.installerName.split(' ')[0]}` : ''}! ${KIND_LABEL[a.kind]} Farbo agendada:`,
    `📅 ${when}`,
    `🚗 ${vehicle}${a.vehicleDetails ? ` (${a.vehicleDetails})` : ''}`,
  ];
  if (a.kind === 'uninstall' && a.imei) lines.push(`📟 Retirar o rastreador IMEI ${a.imei}`);
  if (a.address) lines.push(`📍 ${a.address}`);
  if (a.notes) lines.push(`📝 ${a.notes}`);
  if (a.url) {
    const steps = a.kind === 'uninstall' ? 'a foto do rastreador retirado' : 'o teste do rastreador e as fotos';
    lines.push('', `No local, siga o passo a passo (${steps}) por este link: ${a.url}`);
  }
  return lines.join('\n');
}

/** A mensagem ao instalador da instalação avulsa, com o link do teste e da conclusão. */
export function installerLinkMessage(l: InstallerLink): string {
  const vehicle = [l.vehicleName, l.plate].filter(Boolean).join(' · ');
  const lines = [
    // O nome inteiro: o prestador costuma ser oficina ("Olá, Auto!" não).
    `Olá${l.installerName ? `, ${l.installerName}` : ''}! Instalação Farbo:`,
    `📟 Rastreador IMEI ${l.imei}${l.deviceModel ? ` (${l.deviceModel})` : ''}`,
  ];
  if (vehicle) lines.push(`🚗 ${vehicle}`);
  if (l.notes) lines.push(`📝 ${l.notes}`);
  if (l.url) lines.push('', `Depois de instalar, teste o sinal e conclua por este link: ${l.url}`);
  return lines.join('\n');
}

/** "2026-10-24T14:30" (datetime-local) de um horário. */
export function localInput(value: string | Date): string {
  const d = typeof value === 'string' ? new Date(value) : value;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** O próximo horário redondo (de hora em hora) a partir de amanhã às 9h. */
export function defaultSlot(now = new Date()): string {
  const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1, 9, 0);
  return localInput(d);
}

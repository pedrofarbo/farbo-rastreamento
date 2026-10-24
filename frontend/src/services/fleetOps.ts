import type { ExpenseKind, FineStatus, JourneyRow, Rental } from '@/types';

// Os textos da gestão da frota: os tipos de custo, a situação das multas, os
// alertas da jornada e a situação das locações.

export const EXPENSE_KINDS: { id: ExpenseKind; label: string }[] = [
  { id: 'fuel', label: 'Combustível' },
  { id: 'maintenance', label: 'Manutenção' },
  { id: 'toll', label: 'Pedágio' },
  { id: 'parking', label: 'Estacionamento' },
  { id: 'insurance', label: 'Seguro' },
  { id: 'tax', label: 'Impostos e taxas' },
  { id: 'wash', label: 'Lavagem' },
  { id: 'fine', label: 'Multa' },
  { id: 'other', label: 'Outro' },
];

export const expenseKindLabel = (kind: ExpenseKind) => EXPENSE_KINDS.find((k) => k.id === kind)?.label ?? kind;

export const FINE_STATUS: Record<FineStatus, { label: string; tone: 'warning' | 'success' | 'accent' | 'neutral' }> = {
  pending: { label: 'A pagar', tone: 'warning' },
  paid: { label: 'Paga', tone: 'success' },
  appealed: { label: 'Recorrida', tone: 'accent' },
  canceled: { label: 'Cancelada', tone: 'neutral' },
};

export const JOURNEY_VIOLATIONS: Record<JourneyRow['violations'][number], string> = {
  continuous: 'Direção contínua acima do limite',
  daily: 'Mais de 10 h ao volante',
  interjourney: 'Descanso menor que 11 h',
};

/** A situação da locação: em andamento, atrasada ou devolvida. */
export function rentalStatus(r: Pick<Rental, 'returnedAt' | 'overdue' | 'startsAt'>, now = new Date()): {
  label: string;
  tone: 'success' | 'danger' | 'accent' | 'neutral';
} {
  if (r.returnedAt) return { label: 'Devolvido', tone: 'neutral' };
  if (r.overdue) return { label: 'Atrasada', tone: 'danger' };
  if (new Date(r.startsAt) > now) return { label: 'Agendada', tone: 'accent' };
  return { label: 'Em andamento', tone: 'success' };
}

/** "2026-10-01" do primeiro dia do mês da data. */
export function monthStart(date = new Date()): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-01`;
}

/** Os km rodados na locação (nulo sem os dois). */
export function rentalKm(r: Pick<Rental, 'kmStart' | 'kmEnd'>): number | null {
  if (r.kmStart === null || r.kmEnd === null) return null;
  return Math.max(0, r.kmEnd - r.kmStart);
}

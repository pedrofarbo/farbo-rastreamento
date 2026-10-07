import { formatDateOnly, formatMoney } from '@/services/format';
import type { Subscription } from '@/types';

/**
 * O rastreador parcelado sem juros, por Pix: a 1ª parcela vai na fatura do
 * pedido (com o frete) e as demais, somadas às mensalidades. A assinatura fica
 * ativa até a última; encerrada antes, o saldo vence de uma vez.
 */

/** Divide como o servidor: a 1ª leva os centavos que sobram (R$ 120,00 em 7x: 17,16 + 6 × 17,14). */
export function splitInstallments(totalCents: number, n: number): number[] {
  const count = Math.max(1, Math.floor(n));
  const base = Math.floor(totalCents / count);
  const out = Array.from({ length: count }, () => base);
  out[0] = totalCents - base * (count - 1);
  return out;
}

/** "10x de R$ 15,00 sem juros"; com a 1ª diferente, "7x sem juros: 1ª de R$ 17,16 e 6 de R$ 17,14". */
export function installmentsLabel(totalCents: number, n: number): string {
  if (n <= 1) return `À vista: ${formatMoney(totalCents)}`;
  const [first, rest] = splitInstallments(totalCents, n);
  if (first === rest) return `${n}x de ${formatMoney(rest)} sem juros`;
  return `${n}x sem juros: 1ª de ${formatMoney(first)} e ${n - 1} de ${formatMoney(rest)}`;
}

/** As formas de pagar o rastreador: à vista e de 2 até max vezes. */
export function installmentChoices(totalCents: number, max: number): { value: number; label: string }[] {
  if (totalCents <= 0) return [];
  return Array.from({ length: Math.max(1, max) }, (_, i) => ({ value: i + 1, label: installmentsLabel(totalCents, i + 1) }));
}

type Financed = Pick<
  Subscription,
  'installments' | 'commitmentUntil' | 'equipmentCents' | 'installmentCents' | 'installmentsPaid' | 'installmentsDueCents'
>;

/**
 * O andamento na assinatura: "Rastreador em 10x de R$ 15,00: 3 de 10 pagas,
 * faltam R$ 105,00 · ativa até 10/06/2027". Nulo quando foi à vista.
 */
export function installmentProgress(sub: Financed): string | null {
  if (!sub.installments) return null;
  const paid = `${sub.installmentsPaid} de ${sub.installments} ${sub.installmentsPaid === 1 ? 'paga' : 'pagas'}`;
  const due = sub.installmentsDueCents > 0 ? `, faltam ${formatMoney(sub.installmentsDueCents)}` : ' (quitado)';
  const until = sub.commitmentUntil && sub.installmentsDueCents > 0 ? ` · ativa até ${formatDateOnly(sub.commitmentUntil)}` : '';
  return `Rastreador em ${sub.installments}x de ${formatMoney(sub.installmentCents)}: ${paid}${due}${until}`;
}

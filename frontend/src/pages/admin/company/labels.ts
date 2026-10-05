import type { BadgeTone } from '@/components/ui/Badge';
import type { DreGroup, FinanceEntry, PaymentMethod, StockKind, StockMoveType } from '@/types';

/** As linhas do resultado do mês (DRE), como a tela mostra. */
export const GROUP_LABELS: Record<DreGroup, string> = {
  REVENUE: 'Receita da operação',
  OTHER_INCOME: 'Outras receitas',
  CAPITAL_IN: 'Aportes e empréstimos',
  TAX: 'Impostos sobre a receita',
  COST: 'Custo do serviço',
  OPERATING: 'Despesas operacionais',
  FINANCIAL: 'Despesas financeiras',
  INVESTMENT: 'Compras para o estoque',
  CAPITAL_OUT: 'Retiradas e empréstimos pagos',
};

/** O que cada linha quer dizer (no cadastro de categoria). */
export const GROUP_HINTS: Record<DreGroup, string> = {
  REVENUE: 'Venda de equipamentos, instalação cobrada à parte.',
  OTHER_INCOME: 'Rendimentos e outras entradas fora da operação.',
  CAPITAL_IN: 'Entra no caixa, mas não é receita: não aparece no resultado.',
  TAX: 'DAS/Simples e outros impostos sobre o que a empresa fatura.',
  COST: 'O que custa entregar o serviço: dados dos chips, servidor, instalação, frete.',
  OPERATING: 'Marketing, contador, software, aluguel, pessoal.',
  FINANCIAL: 'Tarifas bancárias, taxas de pagamento, juros.',
  INVESTMENT: 'Sai do caixa, mas não é custo do mês: o equipamento vira custo quando é instalado.',
  CAPITAL_OUT: 'Sai do caixa, mas não é despesa: não aparece no resultado.',
};

export const EXPENSE_GROUPS: DreGroup[] = ['TAX', 'COST', 'OPERATING', 'FINANCIAL', 'INVESTMENT', 'CAPITAL_OUT'];
export const INCOME_GROUPS: DreGroup[] = ['REVENUE', 'OTHER_INCOME', 'CAPITAL_IN'];

export const METHOD_LABELS: Record<Exclude<PaymentMethod, ''>, string> = {
  PIX: 'Pix',
  BOLETO: 'Boleto',
  CARD: 'Cartão',
  TRANSFER: 'Transferência',
  CASH: 'Dinheiro',
  DEBIT: 'Débito automático',
};

export const STOCK_KIND_LABELS: Record<StockKind, string> = {
  TRACKER: 'Rastreador',
  SIM: 'Chip',
  ACCESSORY: 'Acessório',
  OTHER: 'Outro',
};

export const MOVE_LABELS: Record<StockMoveType, string> = {
  IN: 'Entrada (compra)',
  OUT: 'Saída (instalação ou venda)',
  LOSS: 'Perda ou defeito',
  ADJUST: 'Acerto de contagem',
};

const MONTHS = ['jan', 'fev', 'mar', 'abr', 'mai', 'jun', 'jul', 'ago', 'set', 'out', 'nov', 'dez'];

/** "2026-10" → "out/2026". */
export function monthLabel(month: string): string {
  const [year, m] = month.split('-');
  const index = Number(m) - 1;
  return MONTHS[index] ? `${MONTHS[index]}/${year}` : month;
}

/** Hoje em Brasília, "2026-10-15" (o navegador pode estar em outro fuso). */
export function todayISO(now = new Date()): string {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'America/Sao_Paulo',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(now);
  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? '';
  return `${get('year')}-${get('month')}-${get('day')}`;
}

/** Dias de a até b (datas "AAAA-MM-DD"). */
export function daysBetween(a: string, b: string): number {
  return Math.round((Date.parse(`${b}T00:00:00Z`) - Date.parse(`${a}T00:00:00Z`)) / 86_400_000);
}

/** "2026-10-15" + 3 → "2026-10-18". */
export function addDays(iso: string, days: number): string {
  const d = new Date(`${iso}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}

/** "2/10" na conta parcelada. */
export function installmentLabel(e: Pick<FinanceEntry, 'installment' | 'installments'>): string {
  return e.installment && e.installments ? `${e.installment}/${e.installments}` : '';
}

/** A situação da conta para o selo: paga, vencida há N dias, vence hoje, em N dias. */
export function entrySituation(
  e: Pick<FinanceEntry, 'status' | 'dueDate' | 'kind'>,
  today: string,
): { label: string; tone: BadgeTone } {
  if (e.status === 'PAID') return { label: e.kind === 'PAYABLE' ? 'Paga' : 'Recebida', tone: 'success' };
  if (e.status === 'CANCELED') return { label: 'Cancelada', tone: 'neutral' };
  const days = daysBetween(today, e.dueDate);
  if (days < 0) return { label: `Vencida há ${-days} ${-days === 1 ? 'dia' : 'dias'}`, tone: 'danger' };
  if (days === 0) return { label: 'Vence hoje', tone: 'warning' };
  if (days <= 3) return { label: `Vence em ${days} ${days === 1 ? 'dia' : 'dias'}`, tone: 'warning' };
  return { label: `Em ${days} dias`, tone: 'neutral' };
}

/** Divide o total em parcelas como o servidor (os centavos a mais na primeira). */
export function splitInstallments(total: number, n: number): number[] {
  const count = Math.max(1, Math.floor(n));
  const each = Math.floor(total / count);
  const out = Array.from({ length: count }, () => each);
  out[0] += total - each * count;
  return out;
}

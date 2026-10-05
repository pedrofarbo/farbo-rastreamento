import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';

import { financeApi } from '@/api/resources';
import { Card } from '@/components/ui/Card';
import { Spinner } from '@/components/ui/Spinner';
import { formatMoney } from '@/services/format';
import type { DreGroup, DreMonth } from '@/types';

import { financeKey } from './EntryModals';
import { monthLabel } from './labels';
import styles from './Company.module.css';

/** "12,5%" (ou "—" sem receita). */
export function margin(result: number, revenue: number): string {
  if (revenue <= 0) return '—';
  const value = (result / revenue) * 100;
  return `${value.toLocaleString('pt-BR', { maximumFractionDigits: 1 })}%`;
}

interface Line {
  key: string;
  label: string;
  value: (m: DreMonth) => number;
  kind?: 'total' | 'result';
  /** As categorias que somam a linha (abre ao clicar), e o sinal delas. */
  groups?: DreGroup[];
  sign?: 1 | -1;
  extra?: { label: string; value: (m: DreMonth) => number }[];
}

const LINES: Line[] = [
  {
    key: 'revenue',
    label: 'Receita bruta',
    value: (m) => m.revenueCents,
    groups: ['REVENUE'],
    extra: [{ label: 'Faturas dos clientes', value: (m) => m.invoicesCents }],
  },
  { key: 'taxes', label: '(−) Impostos', value: (m) => -m.taxesCents, groups: ['TAX'], sign: -1 },
  { key: 'net', label: '= Receita líquida', value: (m) => m.netRevenueCents, kind: 'total' },
  {
    key: 'costs',
    label: '(−) Custo do serviço',
    value: (m) => -(m.costsCents + m.stockCostCents + m.stockLossCents),
    groups: ['COST'],
    sign: -1,
    extra: [
      { label: 'Equipamento instalado ou vendido', value: (m) => -m.stockCostCents },
      { label: 'Perdas e acertos do estoque', value: (m) => -m.stockLossCents },
    ],
  },
  { key: 'gross', label: '= Lucro bruto', value: (m) => m.grossProfitCents, kind: 'total' },
  { key: 'operating', label: '(−) Despesas operacionais', value: (m) => -m.operatingCents, groups: ['OPERATING'], sign: -1 },
  { key: 'financial', label: '(−) Despesas financeiras', value: (m) => -m.financialCents, groups: ['FINANCIAL'], sign: -1 },
  { key: 'other', label: '(+) Outras receitas', value: (m) => m.otherIncomeCents, groups: ['OTHER_INCOME'] },
  { key: 'result', label: '= Resultado do mês', value: (m) => m.resultCents, kind: 'result' },
];

const OUTSIDE: Line[] = [
  { key: 'investments', label: 'Compras para o estoque', value: (m) => -m.investmentsCents, groups: ['INVESTMENT'], sign: -1 },
  { key: 'capitalIn', label: 'Aportes e empréstimos recebidos', value: (m) => m.capitalInCents, groups: ['CAPITAL_IN'] },
  { key: 'capitalOut', label: 'Retiradas e empréstimos pagos', value: (m) => -m.capitalOutCents, groups: ['CAPITAL_OUT'], sign: -1 },
];

/** O resultado do mês (DRE): se a operação se paga. */
export function ResultTab() {
  const dre = useQuery({ queryKey: [...financeKey, 'dre', 12], queryFn: () => financeApi.dre(12) });
  const months = dre.data ?? [];
  const [selected, setSelected] = useState<string | null>(null);
  if (dre.isLoading) return <Spinner label="Carregando o resultado" />;
  if (months.length === 0) return null;

  const index = selected ? Math.max(0, months.findIndex((m) => m.month === selected)) : months.length - 1;
  const month = months[index];
  const previous = index > 0 ? months[index - 1] : null;

  return (
    <div className={styles.tab}>
      <Card
        title={`Resultado de ${monthLabel(month.month)}`}
        subtitle={`Margem ${margin(month.resultCents, month.revenueCents)} · pelo que entrou e saiu no mês; o equipamento vira custo quando é instalado.`}
        actions={
          <select className={`${styles.select} ${styles.monthSelect}`} aria-label="Mês" value={month.month} onChange={(e) => setSelected(e.target.value)}>
            {[...months].reverse().map((m) => (
              <option key={m.month} value={m.month}>
                {monthLabel(m.month)}
              </option>
            ))}
          </select>
        }
      >
        <Statement lines={LINES} month={month} previous={previous} />
      </Card>

      <Card title="Fora do resultado" subtitle="Mexem no caixa, mas não são receita nem despesa do mês.">
        <Statement lines={OUTSIDE} month={month} previous={previous} />
      </Card>

      <Card title="Resultado nos últimos 12 meses">
        <ResultBars months={months} />
      </Card>
    </div>
  );
}

/** Valor da linha: o zero de uma despesa (−0) sai sem o sinal. */
export function money(cents: number): string {
  return formatMoney(cents === 0 ? 0 : cents);
}

function Statement({ lines, month, previous }: { lines: Line[]; month: DreMonth; previous: DreMonth | null }) {
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const tone = (v: number) => (v < 0 ? styles.negative : '');
  return (
    <ul className={styles.statement}>
      <li className={`${styles.line} ${styles.lineHead}`}>
        <span />
        <span>{monthLabel(month.month)}</span>
        <span>{previous ? monthLabel(previous.month) : ''}</span>
      </li>
      {lines.map((line) => {
        const categories = month.categories.filter((c) => line.groups?.includes(c.group));
        const expandable = categories.length > 0 || (line.extra?.length ?? 0) > 0;
        const isOpen = open[line.key];
        const value = line.value(month);
        return (
          <li key={line.key}>
            <div className={`${styles.line} ${line.kind === 'total' ? styles.lineTotal : ''} ${line.kind === 'result' ? styles.lineResult : ''}`}>
              <span>
                {expandable ? (
                  <button
                    type="button"
                    className={`${styles.lineToggle} ${isOpen ? styles.lineToggleOpen : ''}`}
                    aria-expanded={isOpen}
                    onClick={() => setOpen((o) => ({ ...o, [line.key]: !o[line.key] }))}
                  >
                    {line.label}
                  </button>
                ) : (
                  line.label
                )}
              </span>
              <span className={tone(value)}>{money(value)}</span>
              <span className={styles.muted}>{previous ? money(line.value(previous)) : ''}</span>
            </div>
            {isOpen && (
              <>
                {line.extra?.map((x) => (
                  <div key={x.label} className={`${styles.line} ${styles.lineSub}`}>
                    <span>{x.label}</span>
                    <span>{money(x.value(month))}</span>
                    <span>{previous ? money(x.value(previous)) : ''}</span>
                  </div>
                ))}
                {categories.map((c) => {
                  const before = previous?.categories.find((p) => p.categoryId === c.categoryId)?.cents ?? 0;
                  const sign = line.sign ?? 1;
                  return (
                    <div key={c.categoryId} className={`${styles.line} ${styles.lineSub}`}>
                      <span>{c.name}</span>
                      <span>{money(sign * c.cents)}</span>
                      <span>{previous ? money(sign * before) : ''}</span>
                    </div>
                  );
                })}
              </>
            )}
          </li>
        );
      })}
    </ul>
  );
}

/** Uma barra por mês: verde com lucro, vermelha com prejuízo. */
function ResultBars({ months }: { months: DreMonth[] }) {
  const max = Math.max(1, ...months.map((m) => Math.abs(m.resultCents)));
  return (
    <div>
      <div className={styles.bars} role="img" aria-label="Resultado por mês">
        {months.map((m) => (
          <div key={m.month} className={styles.barGroup} title={`${monthLabel(m.month)}: ${formatMoney(m.resultCents)}`}>
            <span
              className={`${styles.bar} ${m.resultCents < 0 ? styles.barOut : styles.barIn}`}
              style={{ height: `${(Math.abs(m.resultCents) / max) * 100}%` }}
            />
          </div>
        ))}
      </div>
      <div className={styles.barAxis}>
        {months.map((m) => (
          <span key={m.month}>{monthLabel(m.month).slice(0, 3)}</span>
        ))}
      </div>
    </div>
  );
}

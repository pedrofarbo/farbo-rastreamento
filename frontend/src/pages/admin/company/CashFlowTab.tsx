import { useState } from 'react';
import { keepPreviousData, useMutation, useQuery } from '@tanstack/react-query';

import { financeApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { centsToInput, formatDateOnly, formatMoney, parseMoney } from '@/services/format';
import type { CashMonth, CashProjection } from '@/types';

import pageStyles from '../../Page.module.css';
import { financeKey, useRefreshFinance } from './EntryModals';
import { monthLabel, todayISO } from './labels';
import styles from './Company.module.css';

const PERIODS = [6, 12, 24] as const;

/** O caixa: saldo de hoje, projeções e o que entrou e saiu mês a mês. */
export function CashFlowTab() {
  const [months, setMonths] = useState<(typeof PERIODS)[number]>(12);
  const flow = useQuery({
    queryKey: [...financeKey, 'cashflow', months],
    queryFn: () => financeApi.cashflow(months),
    placeholderData: keepPreviousData,
  });
  const data = flow.data;
  if (!data) return <Spinner label="Carregando o caixa" />;

  return (
    <div className={styles.tab}>
      <div className={billing.tiles}>
        <div className={`${billing.tile} ${data.balanceCents < 0 ? billing.tileDanger : ''}`}>
          <span className={billing.tileLabel}>Saldo hoje</span>
          <span className={billing.tileValue}>{formatMoney(data.balanceCents)}</span>
          <span className={billing.tileHint}>
            Desde {formatMoney(data.settings.openingBalanceCents)} em {formatDateOnly(data.settings.openingDate)}
          </span>
        </div>
      </div>

      <Card title="Projeção" subtitle="O saldo de hoje, mais o que há para receber, menos o que há para pagar até a data.">
        <Projections list={data.projections} />
      </Card>

      <Card
        title="Mês a mês"
        actions={
          <div className={styles.chips} role="group" aria-label="Período">
            {PERIODS.map((p) => (
              <button
                key={p}
                type="button"
                className={`${styles.chip} ${months === p ? styles.chipActive : ''}`}
                aria-pressed={months === p}
                onClick={() => setMonths(p)}
              >
                {p} meses
              </button>
            ))}
          </div>
        }
      >
        <MonthBars months={data.months} />
        <div className={pageStyles.tableWrap}>
          <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
            <thead>
              <tr>
                <th>Mês</th>
                <th className={styles.right}>Faturas dos clientes</th>
                <th className={styles.right}>Outras receitas</th>
                <th className={styles.right}>Saídas</th>
                <th className={styles.right}>Taxas</th>
                <th className={styles.right}>Resultado</th>
                <th className={styles.right}>Saldo no fim</th>
              </tr>
            </thead>
            <tbody>
              {[...data.months].reverse().map((m) => (
                <tr key={m.month}>
                  <td data-label="Mês">{monthLabel(m.month)}</td>
                  <td data-label="Faturas" className={styles.right}>
                    {formatMoney(m.invoicesCents)}
                  </td>
                  <td data-label="Outras receitas" className={styles.right}>
                    {formatMoney(m.otherInCents)}
                  </td>
                  <td data-label="Saídas" className={styles.right}>
                    {formatMoney(m.outCents - m.feesCents)}
                  </td>
                  <td data-label="Taxas" className={styles.right} title="Tarifas da AbacatePay (Pix recebido e enviado) e bancárias">
                    {formatMoney(m.feesCents)}
                  </td>
                  <td data-label="Resultado" className={`${styles.right} ${styles.amount} ${m.netCents < 0 ? styles.negative : styles.positive}`}>
                    {formatMoney(m.netCents)}
                  </td>
                  <td data-label="Saldo no fim" className={`${styles.right} ${styles.amount}`}>
                    {m.endBalanceCents === null ? <span className={styles.muted}>antes do saldo inicial</span> : formatMoney(m.endBalanceCents)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>

      <OpeningBalance cents={data.settings.openingBalanceCents} date={data.settings.openingDate} />
    </div>
  );
}

export function Projections({ list }: { list: CashProjection[] }) {
  return (
    <div className={styles.projections}>
      {list.map((p) => (
        <div key={p.days} className={styles.projection}>
          <span className={styles.muted}>
            Em {p.days} dias ({formatDateOnly(p.until)})
          </span>
          <span className={`${styles.projectionValue} ${p.balanceCents < 0 ? styles.negative : ''}`}>{formatMoney(p.balanceCents)}</span>
          <span className={styles.muted}>
            + {formatMoney(p.inCents)} a receber · − {formatMoney(p.outCents)} a pagar
          </span>
        </div>
      ))}
    </div>
  );
}

/** Barras de entradas e saídas de cada mês. */
function MonthBars({ months }: { months: CashMonth[] }) {
  const max = Math.max(1, ...months.flatMap((m) => [m.inCents, m.outCents]));
  return (
    <div>
      <div className={styles.legend}>
        <span className={styles.legendIn}>Entradas</span>
        <span className={styles.legendOut}>Saídas</span>
      </div>
      <div className={styles.bars} role="img" aria-label="Entradas e saídas por mês">
        {months.map((m) => (
          <div key={m.month} className={styles.barGroup} title={`${monthLabel(m.month)}: entrou ${formatMoney(m.inCents)}, saiu ${formatMoney(m.outCents)}`}>
            <span className={`${styles.bar} ${styles.barIn}`} style={{ height: `${(m.inCents / max) * 100}%` }} />
            <span className={`${styles.bar} ${styles.barOut}`} style={{ height: `${(m.outCents / max) * 100}%` }} />
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

/** O ponto de partida do caixa. */
function OpeningBalance({ cents, date }: { cents: number; date: string }) {
  const { notify } = useToast();
  const refresh = useRefreshFinance();
  const [amount, setAmount] = useState(centsToInput(Math.abs(cents)));
  const [negative, setNegative] = useState(cents < 0);
  const [day, setDay] = useState(date);
  const [error, setError] = useState('');
  const save = useMutation({
    mutationFn: () => {
      const value = parseMoney(amount);
      if (value === null) throw new Error('Valor inválido.');
      return financeApi.saveSettings({ openingBalanceCents: negative ? -value : value, openingDate: day });
    },
    onSuccess: () => {
      setError('');
      refresh();
      notify({ tone: 'success', title: 'Saldo inicial salvo' });
    },
    onError: (err: Error) => setError(err.message),
  });
  return (
    <Card title="Saldo inicial" subtitle="Quanto a empresa tinha (somando as contas) numa data. O caixa conta a partir dela.">
      <div className={pageStyles.form}>
        <div className={pageStyles.formRow}>
          <TextField label="Saldo (R$)" value={amount} onChange={(e) => setAmount(e.target.value)} inputMode="decimal" />
          <TextField label="Em" type="date" value={day} max={todayISO()} onChange={(e) => setDay(e.target.value)} />
        </div>
        <label className={styles.check}>
          <input type="checkbox" checked={negative} onChange={(e) => setNegative(e.target.checked)} />
          Saldo negativo (no cheque especial)
        </label>
        {error && <p className={styles.danger}>{error}</p>}
        <div>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Salvar saldo inicial
          </Button>
        </div>
      </div>
    </Card>
  );
}

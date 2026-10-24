import { useState } from 'react';
import type { ReactNode } from 'react';

import { Badge } from '@/components/ui/Badge';
import { formatDateOnly, formatMoney } from '@/services/format';
import { dayStatus } from '@/services/invoiceGroups';
import type { HistoryDay } from '@/services/invoiceGroups';
import type { Invoice } from '@/types';

import styles from './Billing.module.css';
import { LINES_PAGE } from './InvoiceLines';

/**
 * A fatura do mês numa tabela de faturas: uma linha com o total e a
 * situação (e as ações); as linhas dela (cada veículo, o equipamento, o
 * proporcional...) abrem sob demanda, de 50 em 50, cada uma com a linha de
 * sempre (`renderInvoice`).
 */
export function InvoiceDayRows({
  day,
  renderInvoice,
  actions,
  columns = 5,
}: {
  day: HistoryDay;
  renderInvoice: (invoice: Invoice) => ReactNode;
  actions?: ReactNode;
  columns?: number;
}) {
  const [open, setOpen] = useState(false);
  const [limit, setLimit] = useState(LINES_PAGE);
  const status = dayStatus(day);
  const left = day.invoices.length - limit;

  return (
    <>
      <tr className={styles.dayRow}>
        <td>
          <strong>Fatura de {formatDateOnly(day.dueDate)}</strong>{' '}
          <button
            type="button"
            className={styles.dayToggle}
            aria-expanded={open}
            onClick={() => {
              setOpen(!open);
              setLimit(LINES_PAGE);
            }}
          >
            {open ? 'Recolher' : `Ver as ${day.invoices.length} linhas`}
          </button>
        </td>
        <td data-label="Vencimento">{formatDateOnly(day.dueDate)}</td>
        <td data-label="Valor" className={styles.amount}>
          {formatMoney(day.amountCents)}
        </td>
        <td data-label="Situação">
          <Badge tone={status.tone} dot={status.tone === 'danger' || status.tone === 'warning'}>
            {status.label}
          </Badge>
        </td>
        <td>{actions}</td>
        {columns > 5 && <td colSpan={columns - 5} />}
      </tr>
      {open && day.invoices.slice(0, limit).map(renderInvoice)}
      {open && left > 0 && (
        <tr className={styles.subRow}>
          <td colSpan={columns}>
            <button type="button" className={styles.dayToggle} onClick={() => setLimit((n) => n + LINES_PAGE)}>
              {left > LINES_PAGE ? `Mostrar mais ${LINES_PAGE} (faltam ${left})` : `Mostrar as ${left} restantes`}
            </button>
          </td>
        </tr>
      )}
    </>
  );
}

import { useState } from 'react';
import type { ReactNode } from 'react';

import { InvoiceStatus } from '@/components/billing/InvoiceStatus';
import { Button } from '@/components/ui/Button';
import { formatDateOnly, formatMoney } from '@/services/format';
import type { OrgEstimate, OrgInvoice, OrgInvoiceItem } from '@/types';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

import styles from './Organization.module.css';

/** A linha do extrato, como a organização entende. */
export function itemLabel(item: OrgInvoiceItem): string {
  const vehicle = [item.name, item.plate].filter(Boolean).join(' · ');
  const who = item.ownerName ? ` — ${item.ownerName}` : '';
  switch (item.kind) {
    case 'monthly':
      return `Mensalidade: ${vehicle}${who}`;
    case 'prorata':
      return `Proporcional de ${item.days ?? 0} ${item.days === 1 ? 'dia' : 'dias'}: ${vehicle}${who}`;
    case 'install':
      return `Instalação: ${vehicle}${who}`;
    case 'equipment':
      return `Rastreador: ${vehicle}${who}`;
    case 'uninstall':
      return `Desinstalação: ${vehicle}${who}`;
  }
}

/** O extrato: as linhas e o total. */
export function Statement({ items, totalCents }: { items: OrgInvoiceItem[]; totalCents: number }) {
  return (
    <ul className={styles.items}>
      {items.map((item, i) => (
        <li key={`${item.kind}-${item.vehicleId}-${i}`} className={styles.item}>
          <span>{itemLabel(item)}</span>
          <span>{formatMoney(item.amountCents)}</span>
        </li>
      ))}
      <li className={`${styles.item} ${styles.itemTotal}`}>
        <span>Total</span>
        <span>{formatMoney(totalCents)}</span>
      </li>
    </ul>
  );
}

/** A próxima fatura, com o que conta hoje. */
export function EstimateBox({ estimate }: { estimate: OrgEstimate }) {
  const [open, setOpen] = useState(false);
  return (
    <div className={styles.estimate}>
      <span>
        <strong>Próxima fatura:</strong> {formatMoney(estimate.amountCents)}, vence em {formatDateOnly(estimate.dueDate)} ·{' '}
        {estimate.vehicles} {estimate.vehicles === 1 ? 'veículo instalado' : 'veículos instalados'} ×{' '}
        {formatMoney(estimate.priceCents)}
      </span>
      <span className={styles.muted}>
        Simulação com os veículos de hoje: a fatura sai alguns dias antes do vencimento, com quem estiver instalado
        nesse dia, e traz o proporcional dos dias de quem foi instalado no meio do mês e as taxas de instalação e
        desinstalação.
      </span>
      {estimate.items.length > 0 && (
        <>
          <button type="button" className={styles.toggle} onClick={() => setOpen(!open)}>
            {open ? 'Esconder o extrato' : 'Ver o extrato'}
          </button>
          {open && <Statement items={estimate.items} totalCents={estimate.amountCents} />}
        </>
      )}
    </div>
  );
}

/**
 * As faturas da organização: uma por mês, com o extrato dos veículos. Com
 * onPay, as em aberto ganham o "Pagar"; actions põe os botões da central.
 */
export function OrgInvoices({
  invoices,
  onPay,
  actions,
}: {
  invoices: OrgInvoice[];
  onPay?: (invoice: OrgInvoice) => void;
  actions?: (invoice: OrgInvoice) => ReactNode;
}) {
  const [open, setOpen] = useState<string | null>(null);
  const sorted = [...invoices].sort((a, b) => b.dueDate.localeCompare(a.dueDate));
  const paged = usePagedList(sorted, 'org-invoices');
  if (invoices.length === 0) {
    return <p className={styles.muted}>Nenhuma fatura ainda.</p>;
  }
  return (
    <>
      <ul className={styles.invoices}>
        {paged.items.map((inv) => (
          <li key={inv.id} className={styles.invoice}>
            <div className={styles.invoiceHead}>
              <div className={styles.invoiceMain}>
                <strong>{inv.description}</strong>
                <span className={styles.muted}>Vence em {formatDateOnly(inv.dueDate)}</span>
              </div>
              <span className={styles.amount}>{formatMoney(inv.amountCents)}</span>
              <InvoiceStatus invoice={inv} />
              {onPay && inv.status === 'OPEN' && (
                <Button size="small" variant="primary" onClick={() => onPay(inv)}>
                  Pagar
                </Button>
              )}
              {actions?.(inv)}
            </div>
            {inv.items.length > 0 && (
              <>
                <button type="button" className={styles.toggle} onClick={() => setOpen(open === inv.id ? null : inv.id)}>
                  {open === inv.id ? 'Esconder o extrato' : `Ver o extrato (${inv.items.length} ${inv.items.length === 1 ? 'linha' : 'linhas'})`}
                </button>
                {open === inv.id && <Statement items={inv.items} totalCents={inv.amountCents} />}
              </>
            )}
          </li>
        ))}
      </ul>
      <Pager {...paged.pager} />
    </>
  );
}

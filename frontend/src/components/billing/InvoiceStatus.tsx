import { Badge } from '@/components/ui/Badge';
import { formatDateTime } from '@/services/format';
import type { CustomerSummary, Invoice } from '@/types';

/** Situação da fatura: em aberto, vencida (com os dias), paga ou cancelada. */
export function InvoiceStatus({ invoice }: { invoice: Invoice }) {
  if (invoice.status === 'PAID') {
    return (
      <Badge tone="success" title={invoice.paidAt ? `Paga em ${formatDateTime(invoice.paidAt)}` : undefined}>
        {invoice.paidVia === 'PIX'
          ? 'Paga via Pix'
          : invoice.paidVia === 'BOLETO'
            ? 'Paga via boleto'
            : invoice.paidVia === 'CARD'
              ? 'Paga no cartão'
              : invoice.paidVia === 'CREDIT'
                ? 'Paga com crédito'
                : 'Paga'}
      </Badge>
    );
  }
  if (invoice.status === 'CANCELED') {
    return <Badge tone="neutral">Cancelada</Badge>;
  }
  if (invoice.overdue) {
    return (
      <Badge tone="danger" dot>
        Vencida há {invoice.daysOverdue} {invoice.daysOverdue === 1 ? 'dia' : 'dias'}
      </Badge>
    );
  }
  return (
    <Badge tone="warning" dot>
      Em aberto
    </Badge>
  );
}

/** Situação do cliente na lista da central. */
export function CustomerStatus({ customer }: { customer: CustomerSummary }) {
  if (!customer.active) return <Badge tone="neutral">Desativado</Badge>;
  if (customer.suspended) {
    return (
      <Badge tone="danger" dot>
        Suspenso
      </Badge>
    );
  }
  if (customer.overdueInvoices > 0) {
    return (
      <Badge tone="warning" dot>
        Em atraso
      </Badge>
    );
  }
  return (
    <Badge tone="success" dot>
      Em dia
    </Badge>
  );
}

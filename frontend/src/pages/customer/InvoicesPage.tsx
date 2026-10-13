import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';

import { meApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { meGroupPixApi } from '@/components/billing/groupPix';
import { InvoiceDayRows } from '@/components/billing/InvoiceDayRows';
import { InvoiceLines } from '@/components/billing/InvoiceLines';
import { InvoiceStatus } from '@/components/billing/InvoiceStatus';
import { MonthlyPrice } from '@/components/billing/MonthlyPrice';
import { CardSettings } from '@/components/billing/CardSettings';
import { PixPaymentModal } from '@/components/billing/PixPaymentModal';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { adjustmentNoticeVisible } from '@/services/adjustment';
import { formatDateOnly, formatMoney } from '@/services/format';
import {
  CLOSED_PAGE,
  groupOf,
  historyRows,
  monthlyGroups,
  oldestClosedDue,
  overdueGroup,
} from '@/services/invoiceGroups';
import type { HistoryDay, InvoiceGroup } from '@/services/invoiceGroups';
import type { Invoice } from '@/types';

import styles from '../Page.module.css';

/** Faturas e assinaturas do cliente. Continua acessível mesmo com o acesso suspenso. */
export function InvoicesPage() {
  const queryClient = useQueryClient();
  const [paying, setPaying] = useState<(Pick<Invoice, 'id' | 'description'> & { amountCents?: number }) | null>(null);
  const payGroup = (g: InvoiceGroup, description: string) => setPaying({ id: g.key, description, amountCents: g.amountCents });

  const account = useQuery({ queryKey: ['me', 'account'], queryFn: meApi.account });
  const cardOn = Boolean(useQuery({ queryKey: ['me', 'card'], queryFn: meApi.card }).data?.enabled);
  const invoices = useQuery({ queryKey: ['me', 'invoices'], queryFn: meApi.invoices });
  const subscriptions = useQuery({ queryKey: ['me', 'subscriptions'], queryFn: meApi.subscriptions });

  const data = account.data;
  const next = data?.nextInvoice ?? null;
  const onlinePayment = data?.onlinePayment ?? false;

  // Pelo lembrete no celular (?pagar=<fatura>): já abre o Pix dela.
  const [params, setParams] = useSearchParams();
  const toPay = params.get('pagar');
  useEffect(() => {
    if (!toPay || !onlinePayment || !invoices.data) return;
    const invoice = invoices.data.find((i) => i.id === toPay && i.status === 'OPEN');
    if (invoice) setPaying(invoice);
    setParams({}, { replace: true });
  }, [toPay, onlinePayment, invoices.data, setParams]);

  // Pago: atualiza faturas, conta (suspensão) e veículos, que voltam a abrir.
  const onPaid = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['me'] });
    queryClient.invalidateQueries({ queryKey: ['vehicles'] });
  }, [queryClient]);
  const activeSubscriptions = (subscriptions.data ?? []).filter((s) => s.status === 'ACTIVE');
  // A fatura do mês (frota: uma por veículo, no mesmo vencimento) e as
  // vencidas de mais de um mês, para pagar num Pix só.
  const groups = useMemo(() => (onlinePayment ? monthlyGroups(invoices.data ?? []) : []), [onlinePayment, invoices.data]);
  const late = useMemo(() => (onlinePayment ? overdueGroup(invoices.data ?? []) : null), [onlinePayment, invoices.data]);

  // O histórico vem em páginas (as fechadas; o último vencimento sempre
  // inteiro): a primeira junto com as em aberto, as mais antigas sob demanda.
  const [older, setOlder] = useState<Invoice[]>([]);
  const [olderDone, setOlderDone] = useState(false);
  const [olderLoading, setOlderLoading] = useState(false);
  const [olderError, setOlderError] = useState('');
  const all = useMemo(() => [...(invoices.data ?? []), ...older], [invoices.data, older]);
  const rows = useMemo(() => historyRows(all), [all]);
  const firstClosed = useMemo(() => (invoices.data ?? []).filter((i) => i.status !== 'OPEN').length, [invoices.data]);
  const canLoadOlder = !olderDone && firstClosed >= CLOSED_PAGE;
  const loadOlder = async () => {
    const before = oldestClosedDue(all);
    if (!before) return;
    setOlderLoading(true);
    setOlderError('');
    try {
      const page = await meApi.olderInvoices(before);
      setOlder((current) => [...current, ...page]);
      if (page.length < CLOSED_PAGE) setOlderDone(true);
    } catch (err) {
      setOlderError(err instanceof Error ? err.message : 'Não deu para carregar agora.');
    } finally {
      setOlderLoading(false);
    }
  };
  const payDay = (day: HistoryDay) =>
    payGroup(groupOf(day.open, day.dueDate), `${day.open.length} faturas · vencimento ${formatDateOnly(day.dueDate)}`);

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Faturas</h1>
            <p className={styles.description}>
              {onlinePayment
                ? cardOn
                  ? 'A primeira fatura sai assim que o rastreador é contratado; as outras, alguns dias antes do vencimento. Pague aqui mesmo, com Pix, boleto ou cartão: a baixa é automática (no Pix e no cartão, em segundos; no boleto, quando o banco compensar).'
                  : 'A primeira fatura sai assim que o rastreador é contratado; as outras, alguns dias antes do vencimento. Pague aqui mesmo, com Pix ou boleto: a baixa é automática (no Pix, em segundos; no boleto, quando o banco compensar).'
                : 'A primeira fatura sai assim que o rastreador é contratado; as outras, alguns dias antes do vencimento. Pague pelo link ou pelo Pix copia-e-cola; a baixa é feita pela nossa equipe assim que o pagamento é confirmado.'}{' '}
              {/* Link comum: no app (em /app), a página do contrato fica fora das rotas dele. */}
              <a href="/contrato">Ver o contrato de prestação de serviços</a>.
            </p>
          </div>
        </header>

        {data?.suspended ? (
          <div className={`${billing.banner} ${billing.bannerDanger}`} role="alert">
            <span>
              <strong>Acesso ao mapa suspenso.</strong> Há fatura vencida há mais de{' '}
              {data.suspendAfterDays} dias. O rastreamento continua sendo gravado e o acesso volta
              assim que o pagamento for confirmado
              {onlinePayment ? ' — pagando com Pix, na hora.' : '.'}
            </span>
          </div>
        ) : data && data.overdueInvoices > 0 ? (
          <div className={`${billing.banner} ${billing.bannerWarning}`} role="status">
            <span>
              <strong>
                {data.overdueInvoices === 1
                  ? 'Você tem 1 fatura vencida.'
                  : `Você tem ${data.overdueInvoices} faturas vencidas.`}
              </strong>{' '}
              {data.suspendAfterDays > 0 &&
                `Com mais de ${data.suspendAfterDays} dias de atraso, o acesso ao mapa é suspenso até o pagamento.`}
            </span>
          </div>
        ) : null}

        {(() => {
          // O reajuste anual, só nos 30 dias antes de valer (junto com o e-mail).
          const adjusted = activeSubscriptions.filter((s) => adjustmentNoticeVisible(s));
          if (adjusted.length === 0) return null;
          return (
            <div className={`${billing.banner} ${billing.bannerAccent}`} role="status">
              <span>
                <strong>Reajuste anual pelo IPCA.</strong> A partir das faturas que vencem em{' '}
                {formatDateOnly(adjusted[0].nextPriceFrom)},{' '}
                {adjusted.length === 1
                  ? `a mensalidade passa de ${formatMoney(adjusted[0].priceCents)} para ${formatMoney(adjusted[0].nextPriceCents)}.`
                  : 'as mensalidades mudam: os valores novos estão em Minhas assinaturas.'}{' '}
                Como prevê o <a href="/contrato">contrato</a> (cláusula 5).
              </span>
            </div>
          );
        })()}

        <div className={billing.tiles}>
          <div className={`${billing.tile} ${data && data.overdueInvoices > 0 ? billing.tileDanger : ''}`}>
            <span className={billing.tileLabel}>Em aberto</span>
            <span className={billing.tileValue}>{formatMoney(data?.openAmountCents ?? 0)}</span>
            <span className={billing.tileHint}>
              {data?.openInvoices
                ? `${data.openInvoices} ${data.openInvoices === 1 ? 'fatura' : 'faturas'}`
                : 'Nada a pagar agora'}
            </span>
          </div>
          <div className={billing.tile}>
            <span className={billing.tileLabel}>Próximo vencimento</span>
            <span className={billing.tileValue}>{next ? formatDateOnly(next.dueDate) : '—'}</span>
            <span className={billing.tileHint}>{next ? formatMoney(next.amountCents) : 'Sem fatura em aberto'}</span>
          </div>
          <div className={billing.tile}>
            <span className={billing.tileLabel}>Assinaturas ativas</span>
            <span className={billing.tileValue}>{data?.activeSubscriptions ?? '—'}</span>
            <span className={billing.tileHint}>
              {data ? `${data.vehicles} ${data.vehicles === 1 ? 'veículo' : 'veículos'}` : ''}
            </span>
          </div>
        </div>

        {onlinePayment && <CardSettings />}

        {late && (
          <div className={`${billing.banner} ${billing.bannerDanger}`} role="status">
            <span>
              <strong>
                {late.invoices.length} faturas vencidas · {formatMoney(late.amountCents)}.
              </strong>{' '}
              Pague todas de uma vez, num pagamento só.
            </span>
            <Button size="small" variant="primary" onClick={() => payGroup(late, `${late.invoices.length} faturas vencidas`)}>
              Pagar as vencidas
            </Button>
          </div>
        )}

        {groups.map((g) => (
          <Card
            key={g.key}
            title={`Fatura de ${formatDateOnly(g.dueDate)}`}
            subtitle={`${g.invoices.length} faturas no mesmo vencimento — pague todas num pagamento só.`}
            actions={
              <Button
                size="small"
                variant="primary"
                onClick={() => payGroup(g, `${g.invoices.length} faturas · vencimento ${formatDateOnly(g.dueDate)}`)}
              >
                Pagar {formatMoney(g.amountCents)}
              </Button>
            }
          >
            <InvoiceLines
              lines={g.invoices.map((inv) => ({ key: inv.id, description: inv.description, amountCents: inv.amountCents }))}
              totalCents={g.amountCents}
              className={billing.groupList}
              totalClassName={billing.groupTotal}
            />
          </Card>
        ))}

        <Card title="Histórico de faturas" flush>
          {invoices.isLoading ? (
            <Spinner label="Carregando faturas" />
          ) : (invoices.data ?? []).length === 0 ? (
            <EmptyState
              icon="🧾"
              title="Nenhuma fatura ainda"
              description="A primeira fatura aparece aqui assim que o rastreador é contratado."
            />
          ) : (
            <div className={styles.tableWrap}>
              <table className={`${styles.table} ${styles.stackTable}`}>
                <thead>
                  <tr>
                    <th>Descrição</th>
                    <th>Vencimento</th>
                    <th>Valor</th>
                    <th>Situação</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row) =>
                    row.kind === 'invoice' ? (
                      <InvoiceRow
                        key={row.invoice.id}
                        invoice={row.invoice}
                        onlinePayment={onlinePayment}
                        onPay={setPaying}
                      />
                    ) : (
                      <DayRows
                        key={row.dueDate}
                        day={row}
                        onlinePayment={onlinePayment}
                        onPay={setPaying}
                        onPayDay={payDay}
                      />
                    ),
                  )}
                </tbody>
              </table>
            </div>
          )}
          {(canLoadOlder || olderError) && (
            <div className={billing.olderBar}>
              {olderError && <span className={billing.muted}>{olderError}</span>}
              <Button size="small" variant="ghost" loading={olderLoading} onClick={() => void loadOlder()}>
                Ver faturas mais antigas
              </Button>
            </div>
          )}
        </Card>

        <Card title="Minhas assinaturas" flush>
          {subscriptions.isLoading ? (
            <Spinner inline />
          ) : (subscriptions.data ?? []).length === 0 ? (
            <EmptyState
              icon="📄"
              title="Nenhuma assinatura"
              description="Contrate o rastreamento do seu veículo em Meus veículos, no botão Novo veículo."
            />
          ) : (
            <div className={styles.tableWrap}>
              <table className={`${styles.table} ${styles.stackTable}`}>
                <thead>
                  <tr>
                    <th>Plano</th>
                    <th>Valor mensal</th>
                    <th>Vencimento</th>
                    <th>Situação</th>
                  </tr>
                </thead>
                <tbody>
                  {(subscriptions.data ?? []).map((subscription) => (
                    <tr key={subscription.id}>
                      <td>{subscription.planName}</td>
                      <td data-label="Valor mensal">
                        <MonthlyPrice sub={subscription} />
                      </td>
                      <td data-label="Vencimento">todo dia {subscription.dueDay}</td>
                      <td data-label="Situação">
                        {subscription.status === 'ACTIVE' ? (
                          <Badge tone="success" dot>
                            Ativa
                          </Badge>
                        ) : (
                          <Badge tone="neutral">Cancelada</Badge>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {activeSubscriptions.length > 0 && (
            <p className={billing.muted} style={{ padding: 'var(--space-3) var(--space-4)' }}>
              Cada assinatura ativa dá direito a 1 veículo cadastrado.
            </p>
          )}
        </Card>
      </div>

      <PixPaymentModal
        invoice={paying}
        api={meGroupPixApi}
        audience="customer"
        onClose={() => setPaying(null)}
        onPaid={onPaid}
      />
    </div>
  );
}

function InvoiceRow({
  invoice,
  onlinePayment,
  onPay,
  sub = false,
}: {
  invoice: Invoice;
  onlinePayment: boolean;
  onPay: (invoice: Invoice) => void;
  /** Uma das faturas de um vencimento aberto no histórico. */
  sub?: boolean;
}) {
  return (
    <tr className={sub ? billing.subRow : undefined}>
      <td>{invoice.description}</td>
      <td data-label="Vencimento">{formatDateOnly(invoice.dueDate)}</td>
      <td data-label="Valor" className={billing.amount}>
        {formatMoney(invoice.amountCents)}
      </td>
      <td data-label="Situação">
        <InvoiceStatus invoice={invoice} />
      </td>
      <td>
        <PayActions invoice={invoice} onlinePayment={onlinePayment} onPayPix={() => onPay(invoice)} />
      </td>
    </tr>
  );
}

/** Um vencimento de frota no histórico, com o Pix de todas as em aberto do dia. */
function DayRows({
  day,
  onlinePayment,
  onPay,
  onPayDay,
}: {
  day: HistoryDay;
  onlinePayment: boolean;
  onPay: (invoice: Invoice) => void;
  onPayDay: (day: HistoryDay) => void;
}) {
  const openCents = day.open.reduce((sum, i) => sum + i.amountCents, 0);
  return (
    <InvoiceDayRows
      day={day}
      renderInvoice={(invoice) => (
        <InvoiceRow key={invoice.id} invoice={invoice} onlinePayment={onlinePayment} onPay={onPay} sub />
      )}
      actions={
        onlinePayment &&
        day.open.length > 0 && (
          <div className={styles.actions}>
            <Button
              size="small"
              variant="primary"
              onClick={() => (day.open.length === 1 ? onPay(day.open[0]) : onPayDay(day))}
            >
              {day.open.length === 1 ? 'Pagar' : `Pagar ${formatMoney(openCents)}`}
            </Button>
          </div>
        )
      }
    />
  );
}

function PayActions({
  invoice,
  onlinePayment,
  onPayPix,
}: {
  invoice: Invoice;
  onlinePayment: boolean;
  onPayPix: () => void;
}) {
  const { notify } = useToast();
  if (invoice.status !== 'OPEN') return null;

  // Com o Pix online ligado, é o caminho principal; link e Pix manuais que a
  // central tenha informado continuam disponíveis ao lado.
  if (onlinePayment) {
    return (
      <div className={styles.actions}>
        <Button size="small" variant="primary" onClick={onPayPix}>
          Pagar
        </Button>
        {invoice.paymentUrl && (
          <Button
            size="small"
            variant="ghost"
            onClick={() => window.open(invoice.paymentUrl, '_blank', 'noopener,noreferrer')}
          >
            Outro meio
          </Button>
        )}
      </div>
    );
  }

  if (!invoice.paymentUrl && !invoice.pixCode) {
    return <span className={billing.muted}>Dados de pagamento em breve</span>;
  }

  const copyPix = async () => {
    try {
      await navigator.clipboard.writeText(invoice.pixCode);
      notify({ tone: 'success', title: 'Código Pix copiado', description: 'Cole no app do seu banco.' });
    } catch {
      notify({
        tone: 'error',
        title: 'Não foi possível copiar',
        description: 'Selecione e copie o código manualmente: ' + invoice.pixCode,
      });
    }
  };

  return (
    <div className={styles.actions}>
      {invoice.pixCode && (
        <Button size="small" variant="secondary" onClick={() => void copyPix()}>
          Copiar Pix
        </Button>
      )}
      {invoice.paymentUrl && (
        <Button
          size="small"
          variant="primary"
          onClick={() => window.open(invoice.paymentUrl, '_blank', 'noopener,noreferrer')}
        >
          Pagar
        </Button>
      )}
    </div>
  );
}

import { useQuery } from '@tanstack/react-query';

import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { useToast } from '@/components/ui/Toast';
import { formatDateOnly, formatMoney } from '@/services/format';
import type { FiscalNote } from '@/types';

import pageStyles from '@/pages/Page.module.css';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';
import styles from './Billing.module.css';

/** "2026-10" → "outubro de 2026". */
function monthName(month: string): string {
  const [y, m] = month.split('-').map(Number);
  return new Date(y, m - 1, 1).toLocaleDateString('pt-BR', { month: 'long', year: 'numeric' });
}

/**
 * As notas fiscais do cliente (ou da associação): uma por mês, no fechamento,
 * pelo que foi faturado nele. Sem nenhuma ainda, o cartão não aparece.
 */
export function FiscalNotesCard({
  queryKey,
  load,
  download,
}: {
  queryKey: readonly unknown[];
  load: () => Promise<FiscalNote[]>;
  download: (id: string) => Promise<void>;
}) {
  const { notify } = useToast();
  const notes = useQuery({ queryKey, queryFn: load });
  const list = notes.data ?? [];
  const paged = usePagedList(list, 'fiscal-notes');
  if (list.length === 0) return null;
  return (
    <Card title="Notas fiscais" subtitle="Uma por mês, emitida no fechamento, pelo que foi faturado nele." flush>
      <>
        <div className={pageStyles.tableWrap}>
          <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
            <thead>
              <tr>
                <th>Mês</th>
                <th>Nota</th>
                <th>Valor</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {paged.items.map((n) => (
                <tr key={n.id}>
                  <td style={{ textTransform: 'capitalize' }}>{monthName(n.month)}</td>
                  <td data-label="Nota">
                    Nº {n.number}
                    <div className={styles.muted}>emitida em {formatDateOnly(n.issuedOn)}</div>
                  </td>
                  <td data-label="Valor" className={styles.amount}>
                    {formatMoney(n.amountCents)}
                  </td>
                  <td>
                    {n.hasFile && (
                      <Button
                        size="small"
                        variant="secondary"
                        onClick={() =>
                          void download(n.id).catch((err: Error) =>
                            notify({ tone: 'error', title: 'Não deu para baixar', description: err.message }),
                          )
                        }
                      >
                        Baixar
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <Pager {...paged.pager} />
      </>
    </Card>
  );
}

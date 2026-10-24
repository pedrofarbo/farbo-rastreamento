import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';

import { orgApi } from '@/api/resources';
import { FiscalNotesCard } from '@/components/billing/FiscalNotes';
import { PixPaymentModal } from '@/components/billing/PixPaymentModal';
import { EstimateBox, OrgInvoices } from '@/components/organization/OrgInvoices';
import { Card } from '@/components/ui/Card';
import { Spinner } from '@/components/ui/Spinner';
import type { OrgInvoice } from '@/types';

import styles from '../Page.module.css';

/** As faturas da associação ou seguradora: uma por mês, com o extrato (só o Administrador). */
export function OrgInvoicesPage() {
  const queryClient = useQueryClient();
  const [paying, setPaying] = useState<OrgInvoice | null>(null);
  const invoices = useQuery({ queryKey: ['org', 'invoices'], queryFn: orgApi.invoices });

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Faturas</h1>
            <p className={styles.description}>
              Uma fatura por mês, com a mensalidade de cada veículo com rastreador instalado, o proporcional de quem
              entrou no meio do mês e as taxas de instalação e desinstalação. Pague por Pix ou boleto.
            </p>
          </div>
        </header>
        {invoices.isLoading ? (
          <Spinner label="Carregando as faturas" />
        ) : invoices.data ? (
          <>
            <EstimateBox estimate={invoices.data.estimate} />
            <Card title="Faturas" flush>
              <OrgInvoices invoices={invoices.data.invoices} onPay={invoices.data.onlinePayment ? setPaying : undefined} />
            </Card>
            <FiscalNotesCard queryKey={['org', 'fiscal-notes']} load={orgApi.fiscalNotes} download={orgApi.downloadNote} />
          </>
        ) : null}
      </div>
      <PixPaymentModal
        invoice={paying}
        api={orgApi.pix}
        audience="customer"
        onClose={() => setPaying(null)}
        onPaid={() => void queryClient.invalidateQueries({ queryKey: ['org'] })}
      />
    </div>
  );
}

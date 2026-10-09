import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';

import { catalogApi, customersApi } from '@/api/resources';
import { installmentChoices, splitInstallments } from '@/components/billing/installments';
import { Button } from '@/components/ui/Button';
import { SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { centsToInput, formatDateOnly, formatMoney, parseMoney, todayISO } from '@/services/format';
import type { Invoice, Subscription, VehicleView } from '@/types';

import billing from '@/components/billing/Billing.module.css';
import styles from '../Page.module.css';

/** O frete escrito na fatura do pedido ("… + frete Correios PAC (R$ 22,40)"), em centavos. */
export function freightOf(description: string): number {
  const m = /frete [^(]*\(R\$\s?([\d.]+),(\d{2})\)/.exec(description);
  return m ? Number(m[1].replace(/\./g, '')) * 100 + Number(m[2]) : 0;
}

/** "+ R$ 15,00 em cada uma das 10 próximas mensalidades" (com a 1ª diferente, ela à parte). */
function parcelsText(parcels: number[]): string {
  const [first, rest] = parcels;
  if (first === rest) return `${formatMoney(rest)} em cada uma das ${parcels.length} próximas mensalidades`;
  return `${formatMoney(first)} na próxima mensalidade e ${formatMoney(rest)} em cada uma das ${parcels.length - 1} seguintes`;
}

/**
 * A central altera uma fatura em aberto: o vencimento e, na fatura do
 * rastreador (avulsa, de um pedido feito à vista), o parcelamento — o
 * rastreador sai da fatura, que fica só com o frete (ou é cancelada), e vem
 * em até 10x nas mensalidades de um veículo, a 1ª na próxima.
 */
export function InvoiceAdjustModal({
  invoice,
  subscriptions,
  vehicles,
  onClose,
  onDone,
}: {
  invoice: Invoice;
  subscriptions: Subscription[];
  vehicles: VehicleView[];
  onClose: () => void;
  onDone: (message: string) => void;
}) {
  const catalog = useQuery({ queryKey: ['catalog'], queryFn: catalogApi.get });
  // Os veículos que podem receber o parcelamento: assinatura ativa, ainda à
  // vista. Mensalidade, fatura só do frete ou já parcelada: nada a parcelar.
  const financeable =
    !invoice.subscriptionId &&
    !invoice.description.startsWith('Frete do rastreador') &&
    !invoice.description.includes('sem juros, nas mensalidades');
  const eligible = financeable ? subscriptions.filter((s) => s.status === 'ACTIVE' && s.installments === 0) : [];
  const [dueDate, setDueDate] = useState<string>(invoice.dueDate);
  const [finance, setFinance] = useState(false);
  const [subscriptionId, setSubscriptionId] = useState(eligible[0]?.id ?? '');
  // O rastreador é a fatura menos o frete.
  const [equipment, setEquipment] = useState(centsToInput(Math.max(0, invoice.amountCents - freightOf(invoice.description))));
  const [installments, setInstallments] = useState(10);
  const [error, setError] = useState('');

  const equipmentCents = parseMoney(equipment);
  const max = catalog.data?.equipmentMaxInstallments ?? 10;
  const choices = equipmentCents && equipmentCents > 0 ? installmentChoices(equipmentCents, max).filter((c) => c.value > 1) : [];
  const n = Math.min(installments, max);
  const parcels = equipmentCents && equipmentCents > 0 && equipmentCents <= invoice.amountCents ? splitInstallments(equipmentCents, n) : null;
  // O que fica nesta fatura (o frete); sem nada, ela é cancelada.
  const rest = invoice.amountCents - (equipmentCents ?? 0);
  const vehicleName = (s: Subscription) => {
    const v = vehicles.find((x) => x.id === s.vehicleId);
    return v ? [v.name, v.plate].filter(Boolean).join(' · ') : s.planName;
  };

  const save = useMutation({
    mutationFn: async () => {
      const done: string[] = [];
      if (finance) {
        if (!subscriptionId) throw new Error('Escolha o veículo do parcelamento.');
        if (!parcels || !equipmentCents) throw new Error('O valor do rastreador precisa estar entre R$ 0,01 e o valor da fatura.');
        await customersApi.financeInvoice(invoice.id, { subscriptionId, installments: n, equipmentCents });
        done.push(`rastreador em ${n}x`);
        // Só tinha o rastreador: a fatura foi cancelada, não há o que vencer.
        if (rest === 0) return done;
      }
      if (dueDate && dueDate !== invoice.dueDate) {
        await customersApi.changeInvoiceDueDate(invoice.id, dueDate);
        done.push(`vencimento em ${formatDateOnly(dueDate)}`);
      }
      return done;
    },
    onSuccess: (done) => onDone(done.length ? `Fatura alterada: ${done.join(' e ')}.` : 'Nada mudou.'),
    onError: (err: Error) => setError(err.message),
  });

  return (
    <Modal
      open
      title="Alterar fatura"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Salvar
          </Button>
        </>
      }
    >
      <div className={styles.form}>
        <p className={styles.description} style={{ margin: 0 }}>
          {invoice.description} · {formatMoney(invoice.amountCents)}
        </p>
        <TextField
          label="Vencimento"
          type="date"
          min={todayISO()}
          hint="Hoje ou depois. O lembrete e a suspensão seguem o vencimento novo."
          value={dueDate}
          onChange={(e) => setDueDate(e.target.value)}
        />

        {eligible.length > 0 && (
          <label className={billing.promoOption}>
            <input type="checkbox" checked={finance} onChange={(e) => setFinance(e.target.checked)} />
            <span>
              <strong>Parcelar o rastreador desta fatura</strong>
              <br />
              Para o pedido feito à vista por engano: o rastreador sai desta fatura, que fica só com o frete (ou é
              cancelada), e vem em parcelas nas mensalidades do veículo, a 1ª na próxima. A assinatura fica ativa até a
              última parcela.
            </span>
          </label>
        )}
        {finance && (
          <>
            <SelectField label="Veículo" value={subscriptionId} onChange={(e) => setSubscriptionId(e.target.value)}>
              {eligible.map((s) => (
                <option key={s.id} value={s.id}>
                  {vehicleName(s)}
                </option>
              ))}
            </SelectField>
            <div className={styles.formRow}>
              <TextField
                label="Valor do rastreador (R$)"
                inputMode="decimal"
                hint="A parte da fatura que é o rastreador; o resto (o frete) continua nesta fatura."
                value={equipment}
                onChange={(e) => setEquipment(e.target.value)}
              />
              <SelectField label="Parcelas" value={n} onChange={(e) => setInstallments(Number(e.target.value))}>
                {choices.map((c) => (
                  <option key={c.value} value={c.value}>
                    {c.label}
                  </option>
                ))}
              </SelectField>
            </div>
            {parcels && (
              <p className={styles.description} style={{ margin: 0 }}>
                {rest > 0 ? `Esta fatura fica com ${formatMoney(rest)} (o frete)` : 'Esta fatura é cancelada'}; o rastreador
                vem nas mensalidades: + {parcelsText(parcels)}.
              </p>
            )}
          </>
        )}
        {error && <div className={styles.note}>{error}</div>}
      </div>
    </Modal>
  );
}

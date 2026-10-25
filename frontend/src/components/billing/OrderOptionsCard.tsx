import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { catalogApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { centsToInput, formatMoney, parseMoney } from '@/services/format';
import type { OrderOptions } from '@/types';

import pageStyles from '@/pages/Page.module.css';
import styles from './Billing.module.css';

const key = ['order-options'] as const;

/** "79,90" → centavos; vazio → null; inválido → NaN. */
function money(text: string): number | null {
  if (!text.trim()) return null;
  return parseMoney(text) ?? NaN;
}

/**
 * As opções do pedido do cliente (central): o comodato — ligado, o "Novo
 * veículo" oferece comprar o rastreador ou o comodato, com a mensalidade e as
 * taxas daqui — e a instalação pelos instaladores da Farbo no rastreador
 * comprado, pelo preço daqui. O plano definido para um cliente (na ficha
 * dele) continua valendo para ele.
 */
export function OrderOptionsCard() {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const options = useQuery({ queryKey: key, queryFn: catalogApi.orderOptions });
  const catalog = useQuery({ queryKey: ['catalog'], queryFn: catalogApi.get });
  const [loanOn, setLoanOn] = useState(false);
  const [monthly, setMonthly] = useState('');
  const [install, setInstall] = useState('');
  const [uninstall, setUninstall] = useState('');
  const [farboOn, setFarboOn] = useState(true);
  const [farboPrice, setFarboPrice] = useState('');
  const [error, setError] = useState('');
  useEffect(() => {
    const o = options.data;
    if (!o) return;
    setLoanOn(o.loan.enabled);
    setMonthly(o.loan.monthlyCents !== null ? centsToInput(o.loan.monthlyCents) : '');
    setInstall(o.loan.installFeeCents ? centsToInput(o.loan.installFeeCents) : '');
    setUninstall(o.loan.uninstallFeeCents ? centsToInput(o.loan.uninstallFeeCents) : '');
    setFarboOn(o.install.enabled);
    setFarboPrice(centsToInput(o.install.priceCents));
  }, [options.data]);
  const save = useMutation({
    mutationFn: () => {
      const values = [money(monthly), money(install), money(uninstall), money(farboPrice)];
      if (values.some((v) => Number.isNaN(v))) throw new Error('Valor inválido: use, por exemplo, 79,90.');
      const [monthlyCents, installFeeCents, uninstallFeeCents, priceCents] = values;
      const input: Omit<OrderOptions, 'updatedAt'> = {
        loan: { enabled: loanOn, monthlyCents, installFeeCents: installFeeCents ?? 0, uninstallFeeCents: uninstallFeeCents ?? 0 },
        install: { enabled: farboOn, priceCents: priceCents ?? 0 },
      };
      return catalogApi.setOrderOptions(input);
    },
    onSuccess: (o) => {
      setError('');
      queryClient.setQueryData(key, o);
      void queryClient.invalidateQueries({ queryKey: ['catalog'] });
      notify({ tone: 'success', title: 'Opções do pedido salvas', description: 'Valem para os pedidos novos.' });
    },
    onError: (err: Error) => setError(err.message),
  });
  const plan = catalog.data;
  return (
    <Card
      title="Opções do pedido do cliente"
      subtitle="O que o cliente escolhe no Novo veículo: comprar o rastreador ou o comodato, e quem instala o rastreador comprado. O plano definido na ficha de um cliente continua valendo para ele."
    >
      {options.isLoading ? (
        <Spinner label="Carregando" />
      ) : (
        <div className={pageStyles.form}>
          {error && <div className={pageStyles.note}>{error}</div>}
          <h3 className={pageStyles.sectionTitle}>Instalação do rastreador comprado</h3>
          <label className={styles.promoOption}>
            <input type="checkbox" checked={farboOn} onChange={(e) => setFarboOn(e.target.checked)} />
            <span>
              <strong>Oferecer a instalação pelos instaladores da Farbo</strong>
              <br />
              O cliente escolhe entre instalar por conta própria (o instalador dele, pago direto) ou com os nossos
              instaladores, por este valor. Desligado, só por conta própria.
            </span>
          </label>
          <div className={pageStyles.formRow}>
            <TextField
              label="Preço da instalação (R$)"
              inputMode="decimal"
              hint="Pessoa física paga no pedido; empresa, na fatura. A central agenda na Agenda."
              value={farboPrice}
              onChange={(e) => setFarboPrice(e.target.value)}
            />
          </div>

          <h3 className={pageStyles.sectionTitle}>Comodato</h3>
          <label className={styles.promoOption}>
            <input type="checkbox" checked={loanOn} onChange={(e) => setLoanOn(e.target.checked)} />
            <span>
              <strong>Oferecer o comodato no pedido</strong>
              <br />O rastreador emprestado, que volta na desinstalação; a instalação é sempre nossa. Desligado, o cliente
              só compra o rastreador.
            </span>
          </label>
          <div className={pageStyles.formRow}>
            <TextField
              label="Mensalidade no comodato (R$)"
              inputMode="decimal"
              placeholder={plan ? `${centsToInput(plan.planPriceCents)} (a do plano)` : 'a do plano'}
              hint={plan ? `Vazio: a do plano, ${formatMoney(plan.planPriceCents)}.` : 'Vazio: a do plano.'}
              value={monthly}
              onChange={(e) => setMonthly(e.target.value)}
            />
            <TextField
              label="Taxa de instalação (R$)"
              inputMode="decimal"
              placeholder="0,00 (sem taxa)"
              hint="Pessoa física paga no pedido; empresa, na fatura."
              value={install}
              onChange={(e) => setInstall(e.target.value)}
            />
            <TextField
              label="Taxa de desinstalação (R$)"
              inputMode="decimal"
              placeholder="0,00 (sem taxa)"
              hint="Na fatura seguinte ao encerramento."
              value={uninstall}
              onChange={(e) => setUninstall(e.target.value)}
            />
          </div>
          <div>
            <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
              Salvar
            </Button>
          </div>
        </div>
      )}
    </Card>
  );
}

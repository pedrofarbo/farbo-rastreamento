import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { catalogApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { centsToInput, parseMoney } from '@/services/format';
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
 * As opções do pedido do cliente (central): os preços da contratação pelo
 * site; o comodato — ligado, o "Novo veículo" oferece comprar o rastreador ou
 * o comodato, com as taxas e a garantia de troca daqui — e a instalação pelos
 * instaladores da Farbo no rastreador comprado, pelo preço daqui. O plano
 * definido para um cliente (na ficha dele) continua valendo para ele.
 */
export function OrderOptionsCard() {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const options = useQuery({ queryKey: key, queryFn: catalogApi.orderOptions });
  const [loanOn, setLoanOn] = useState(false);
  const [guarantee, setGuarantee] = useState('');
  const [install, setInstall] = useState('');
  const [uninstall, setUninstall] = useState('');
  const [farboOn, setFarboOn] = useState(true);
  const [farboPrice, setFarboPrice] = useState('');
  const [checkoutOn, setCheckoutOn] = useState(true);
  const [sitePlan, setSitePlan] = useState('');
  const [siteInsanos, setSiteInsanos] = useState('');
  const [siteEquipment, setSiteEquipment] = useState('');
  const [error, setError] = useState('');
  useEffect(() => {
    const o = options.data;
    if (!o) return;
    setLoanOn(o.loan.enabled);
    setGuarantee(o.loan.surchargeCents ? centsToInput(o.loan.surchargeCents) : '');
    setInstall(o.loan.installFeeCents ? centsToInput(o.loan.installFeeCents) : '');
    setUninstall(o.loan.uninstallFeeCents ? centsToInput(o.loan.uninstallFeeCents) : '');
    setFarboOn(o.install.enabled);
    setFarboPrice(centsToInput(o.install.priceCents));
    setCheckoutOn(o.checkout.enabled);
    setSitePlan(centsToInput(o.checkout.planCents));
    setSiteInsanos(centsToInput(o.checkout.insanosCents));
    setSiteEquipment(centsToInput(o.checkout.equipmentCents));
  }, [options.data]);
  const save = useMutation({
    mutationFn: () => {
      const values = [money(guarantee), money(install), money(uninstall), money(farboPrice), money(sitePlan), money(siteInsanos),
        money(siteEquipment)];
      if (values.some((v) => Number.isNaN(v))) throw new Error('Valor inválido: use, por exemplo, 79,90.');
      const [surchargeCents, installFeeCents, uninstallFeeCents, priceCents, planCents, insanosCents, equipmentCents] = values;
      const input: Omit<OrderOptions, 'updatedAt'> = {
        loan: {
          enabled: loanOn,
          surchargeCents: surchargeCents ?? 0,
          installFeeCents: installFeeCents ?? 0,
          uninstallFeeCents: uninstallFeeCents ?? 0,
        },
        install: { enabled: farboOn, priceCents: priceCents ?? 0 },
        checkout: {
          enabled: checkoutOn,
          planCents: planCents ?? 0,
          insanosCents: insanosCents ?? 0,
          equipmentCents: equipmentCents ?? 0,
        },
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
          <h3 className={pageStyles.sectionTitle}>Contratação pelo site</h3>
          <label className={styles.promoOption}>
            <input type="checkbox" checked={checkoutOn} onChange={(e) => setCheckoutOn(e.target.checked)} />
            <span>
              <strong>Aberta na landing (/contratar)</strong>
              <br />
              Pessoa física contrata e paga com Pix na hora. O preço do plano vale enquanto a assinatura durar (só o
              reajuste anual). Mude a landing junto, se mudar os valores.
            </span>
          </label>
          <div className={pageStyles.formRow}>
            <TextField label="Plano normal (R$/mês)" inputMode="decimal" value={sitePlan} onChange={(e) => setSitePlan(e.target.value)} />
            <TextField
              label="Plano Insanos MC (R$/mês)"
              inputMode="decimal"
              value={siteInsanos}
              onChange={(e) => setSiteInsanos(e.target.value)}
            />
            <TextField
              label="Rastreador comprado (R$)"
              inputMode="decimal"
              value={siteEquipment}
              onChange={(e) => setSiteEquipment(e.target.value)}
            />
          </div>

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
              label="Garantia de troca do rastreador (R$/mês)"
              inputMode="decimal"
              placeholder="0,00 (sem acréscimo)"
              hint="Soma na mensalidade do plano, no comodato da pessoa física."
              value={guarantee}
              onChange={(e) => setGuarantee(e.target.value)}
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

import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { contractApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { PRIVACY_PATH, TERMS_PATH } from '@/config/legal';
import { formatTaxId, validTaxId } from '@/services/taxid';
import type { ContractStatus } from '@/types';

import styles from './Contract.module.css';
import { ContractText } from './ContractText';

export const contractKey = ['me', 'contract'] as const;

/**
 * O aceite do contrato: os pontos principais em linguagem simples, o texto
 * completo, o CPF (obrigatório, para as notas fiscais) e o "li e aceito".
 */
export function ContractAcceptForm({
  status,
  onAccepted,
}: {
  status: ContractStatus;
  onAccepted?: (status: ContractStatus) => void;
}) {
  const queryClient = useQueryClient();
  const [taxId, setTaxId] = useState(formatTaxId(status.taxId));
  const [agreed, setAgreed] = useState(false);
  const [touched, setTouched] = useState(false);
  const validDoc = validTaxId(taxId);

  const accept = useMutation({
    mutationFn: () => contractApi.accept(status.contract.version, taxId),
    onSuccess: (next) => {
      queryClient.setQueryData(contractKey, next);
      void queryClient.invalidateQueries({ queryKey: ['me'] });
      onAccepted?.(next);
    },
  });

  const docError = touched && !validDoc ? 'CPF inválido: confira os números (para empresa, informe o CNPJ).' : '';

  return (
    <form
      className={styles.form}
      onSubmit={(event) => {
        event.preventDefault();
        setTouched(true);
        if (validDoc && agreed) accept.mutate();
      }}
    >
      {status.previous && (
        <p className={styles.changed} role="status">
          <strong>O contrato mudou (versão {status.contract.version}).</strong> Você aceitou a versão{' '}
          {status.previous.version} em {new Date(status.previous.acceptedAt).toLocaleDateString('pt-BR')}. O que mudou:{' '}
          {status.contract.changes} Se não concordar, você pode encerrar a assinatura sem multa (cláusula 14).
        </p>
      )}

      <section className={styles.summary} aria-labelledby="contrato-resumo">
        <h2 id="contrato-resumo" className={styles.summaryTitle}>
          Os pontos principais
        </h2>
        <ul>
          <li>
            <strong>É só o sistema de rastreamento</strong>, com o bloqueio e o desbloqueio feitos por você. Não é serviço
            de segurança, central de monitoramento nem seguro, não há equipe de pronta-resposta e não garantimos a
            recuperação do veículo em caso de roubo ou furto.
          </li>
          <li>
            <strong>O rastreador é seu.</strong> Ele continua sendo seu mesmo depois que a assinatura termina.
          </li>
          <li>
            <strong>Permanência mínima de 3 meses</strong> por veículo. Se cancelar antes, a multa é de{' '}
            <strong>1 mensalidade</strong>.
          </li>
          <li>
            <strong>Parcelou o rastreador?</strong> A assinatura fica ativa até a última parcela. Se sair antes, as
            parcelas que faltam vencem de uma vez.
          </li>
          <li>
            <strong>Reajuste anual em agosto</strong>, pelo IPCA dos 12 meses até maio, com aviso por e-mail 30 dias
            antes. Só depois de 12 meses de assinatura; índice negativo mantém o preço.
          </li>
          <li>
            <strong>Atraso de mais de 1 mês</strong> pode levar o seu nome aos órgãos de proteção ao crédito (SPC e
            Serasa), sempre com aviso antes.
          </li>
          <li>
            <strong>Por quê?</strong> Cada rastreador tem um chip com plano de telecomunicações M2M, e a Farbo paga a
            geração e a entrega do chip. Esses custos não são cobrados à parte: são diluídos nas mensalidades.
          </li>
          <li>
            Desistiu? Em até <strong>7 dias</strong> do recebimento do rastreador, sem multa.
          </li>
        </ul>
      </section>

      <div className={styles.scroll} tabIndex={0} aria-label="Contrato completo">
        <ContractText contract={status.contract} />
      </div>

      <label className={styles.field}>
        <span className={styles.label}>CPF</span>
        <input
          className={`${styles.input} ${docError ? styles.inputError : ''}`}
          inputMode="numeric"
          autoComplete="off"
          placeholder="000.000.000-00"
          value={taxId}
          onChange={(event) => setTaxId(formatTaxId(event.target.value))}
          onBlur={() => setTouched(true)}
          aria-invalid={Boolean(docError)}
          aria-describedby="contrato-cpf-ajuda"
          required
        />
        <span id="contrato-cpf-ajuda" className={docError ? styles.error : styles.hint}>
          {docError ||
            'Obrigatório: é com ele que emitimos as notas fiscais (a NF-e do rastreador e a NFS-e das mensalidades). Empresa? Informe o CNPJ.'}
        </span>
      </label>

      <label className={styles.check}>
        <input type="checkbox" checked={agreed} onChange={(event) => setAgreed(event.target.checked)} />
        <span>
          Li e aceito o contrato de prestação de serviços, os{' '}
          <a href={TERMS_PATH} target="_blank" rel="noreferrer">
            Termos de Uso
          </a>{' '}
          e a{' '}
          <a href={PRIVACY_PATH} target="_blank" rel="noreferrer">
            Política de Privacidade
          </a>
          .
        </span>
      </label>

      {accept.error && (
        <p className={styles.error} role="alert">
          {(accept.error as Error).message}
        </p>
      )}

      <Button type="submit" variant="primary" block loading={accept.isPending} disabled={!agreed}>
        Aceitar e continuar
      </Button>
      <p className={styles.footnote}>
        Versão {status.contract.version}, de {status.contract.effectiveDate}. O aceite fica registrado com a data, a hora
        e o endereço de acesso, e uma cópia vai para o seu e-mail.
      </p>
    </form>
  );
}

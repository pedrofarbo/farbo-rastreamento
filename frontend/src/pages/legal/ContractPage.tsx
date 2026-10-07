import { useQuery } from '@tanstack/react-query';

import { contractApi } from '@/api/resources';
import { contractKey } from '@/components/contract/ContractAcceptForm';
import { ContractBlocks } from '@/components/contract/ContractText';
import { formatDateTime } from '@/services/format';
import { formatTaxId } from '@/services/taxid';
import { useAuth } from '@/stores/AuthContext';

import { LegalLayout } from './LegalLayout';
import styles from './Legal.module.css';

/**
 * O contrato de prestação de serviços em vigor, para ler antes de contratar
 * (sem login) — e, para o cliente logado, quando ele aceitou e com que CPF.
 */
export function ContractPage() {
  const { isCustomer } = useAuth();
  const doc = useQuery({ queryKey: ['contract', 'current'], queryFn: contractApi.current });
  const mine = useQuery({ queryKey: contractKey, queryFn: contractApi.mine, enabled: isCustomer });
  const contract = doc.data;
  const accepted = mine.data?.accepted ?? null;

  return (
    <LegalLayout
      title={contract?.title ?? 'Contrato de Prestação de Serviços'}
      updated={contract ? `Termo de adesão, versão ${contract.version}, em vigor desde ${contract.effectiveDate}` : ''}
      intro={
        contract ? (
          <>
            {accepted && (
              <p className={styles.callout}>
                Você aceitou esta versão em {formatDateTime(accepted.acceptedAt)}, com o CPF/CNPJ{' '}
                {formatTaxId(accepted.document)}. Uma cópia foi enviada ao seu e-mail.
              </p>
            )}
          </>
        ) : (
          <p>{doc.isError ? 'Não deu para carregar o contrato agora. Tente de novo em instantes.' : 'Carregando o contrato…'}</p>
        )
      }
      sections={(contract?.sections ?? []).map((section, i) => ({
        id: `clausula-${i + 1}`,
        title: section.title.replace(/^\d+\.\s*/, ''),
        body: <ContractBlocks blocks={section.blocks} />,
      }))}
    />
  );
}

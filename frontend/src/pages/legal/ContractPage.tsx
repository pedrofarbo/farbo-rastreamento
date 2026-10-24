import { useQuery } from '@tanstack/react-query';
import { Navigate } from 'react-router-dom';

import { contractApi } from '@/api/resources';
import { contractKey, isAssociateTerm } from '@/components/contract/ContractAcceptForm';
import { ContractBlocks } from '@/components/contract/ContractText';
import { formatDateTime } from '@/services/format';
import { formatTaxId } from '@/services/taxid';
import { useAuth } from '@/stores/AuthContext';

import { LegalLayout } from './LegalLayout';
import styles from './Legal.module.css';

/**
 * O contrato de prestação de serviços em vigor (ou, em /contrato/associado, o
 * termo do associado), para ler antes de contratar (sem login) — e, para o
 * cliente logado, o texto dele, quando ele aceitou e com que CPF.
 */
export function ContractPage({ associate = false }: { associate?: boolean }) {
  const { isCustomer } = useAuth();
  const doc = useQuery({ queryKey: ['contract', 'current', associate], queryFn: () => contractApi.current(associate) });
  const mine = useQuery({ queryKey: contractKey, queryFn: contractApi.mine, enabled: isCustomer });
  // Logado: o texto que ele aceita, com o aceite dele.
  const own = mine.data && isAssociateTerm(mine.data.contract) === associate ? mine.data : null;
  const contract = own?.contract ?? doc.data;
  const accepted = own?.accepted ?? null;
  // O associado que abre /contrato vai para o termo dele.
  if (!associate && mine.data && isAssociateTerm(mine.data.contract)) return <Navigate to="/contrato/associado" replace />;

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

/** O termo de uso e comodato do associado (/contrato/associado). */
export function AssociateTermPage() {
  return <ContractPage associate />;
}

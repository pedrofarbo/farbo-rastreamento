import { useEffect } from 'react';
import type { ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';

import { CONTRACT_REQUIRED_EVENT } from '@/api/client';
import { contractApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { useAuth } from '@/stores/AuthContext';

import { ContractAcceptForm, contractKey } from './ContractAcceptForm';
import styles from './Contract.module.css';

/**
 * O portão do contrato: o cliente que ainda não aceitou a versão em vigor vê
 * o contrato (e informa o CPF) antes de qualquer outra tela. A API repete a
 * exigência; quando ela responde CONTRACT_REQUIRED, o portão confere de novo.
 */
export function ContractGate({ children }: { children: ReactNode }) {
  const { isCustomer, logout } = useAuth();
  const status = useQuery({ queryKey: contractKey, queryFn: contractApi.mine, enabled: isCustomer, staleTime: Infinity });

  useEffect(() => {
    const recheck = () => void status.refetch();
    window.addEventListener(CONTRACT_REQUIRED_EVENT, recheck);
    return () => window.removeEventListener(CONTRACT_REQUIRED_EVENT, recheck);
  }, [status]);

  if (!isCustomer) return <>{children}</>;
  if (status.isLoading) return <Spinner label="Carregando" />;
  if (status.isError || !status.data) {
    return (
      <div className={styles.page}>
        <div className={styles.card}>
          <p>Não deu para carregar agora. Confira a conexão e tente de novo.</p>
          <Button variant="primary" onClick={() => void status.refetch()}>
            Tentar de novo
          </Button>
        </div>
      </div>
    );
  }
  if (!status.data.required) return <>{children}</>;

  return (
    <div className={styles.page}>
      <div className={styles.card}>
        <header className={styles.head}>
          {status.data.previous ? (
            <>
              <p className={styles.kicker}>Contrato atualizado</p>
              <h1 className={styles.title}>Contrato de prestação de serviços</h1>
              <p className={styles.lead}>
                {status.data.name ? `${status.data.name.split(' ')[0]}, o` : 'O'} contrato mudou. Para continuar, leia e
                aceite a versão nova.
              </p>
            </>
          ) : (
            <>
              <p className={styles.kicker}>Primeiro acesso</p>
              <h1 className={styles.title}>Contrato de prestação de serviços</h1>
              <p className={styles.lead}>
                {status.data.name ? `${status.data.name.split(' ')[0]}, antes` : 'Antes'} de continuar, leia e aceite o
                contrato e informe o seu CPF. É só desta vez.
              </p>
            </>
          )}
        </header>
        <ContractAcceptForm status={status.data} />
        <Button variant="ghost" block onClick={() => void logout()}>
          Sair
        </Button>
      </div>
    </div>
  );
}

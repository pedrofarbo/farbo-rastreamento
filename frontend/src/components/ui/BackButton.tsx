import { useNavigate } from 'react-router-dom';

import { BackIcon } from '@/app/icons';

import styles from './BackButton.module.css';

/**
 * Voltar, nas páginas do painel reaproveitadas no app (Manutenção e
 * Relatórios, abertas pela Conta): volta à tela anterior do app ou, aberta
 * direto (link ou recarregar), vai para `to`.
 */
export function BackButton({ to }: { to: string }) {
  const navigate = useNavigate();
  const back = () => {
    const state = window.history.state as { idx?: number } | null;
    if (state?.idx) navigate(-1);
    else navigate(to, { replace: true });
  };
  return (
    <button type="button" className={styles.back} onClick={back} aria-label="Voltar">
      <BackIcon />
    </button>
  );
}

import { useSearchParams } from 'react-router-dom';

import { CustomerPaymentsPanel } from './CustomerPaymentsPanel';
import { EntriesTab } from './EntriesTab';
import styles from './Company.module.css';

const VIEWS = [
  { id: 'clientes', label: 'Pagamentos dos clientes' },
  { id: 'avulsas', label: 'Outras receitas' },
] as const;

/**
 * Receitas: os pagamentos dos clientes (as faturas pagas) e as outras
 * receitas, lançadas à mão (venda de equipamento, aporte, empréstimo).
 */
export function ReceivablesTab() {
  const [params, setParams] = useSearchParams();
  const view = params.get('ver') === 'avulsas' ? 'avulsas' : 'clientes';
  const choose = (id: (typeof VIEWS)[number]['id']) => {
    const next = new URLSearchParams(params);
    if (id === 'clientes') next.delete('ver');
    else next.set('ver', id);
    setParams(next, { replace: true });
  };

  return (
    <div className={styles.tab}>
      <div>
        <div className={styles.chips} role="group" aria-label="Receitas">
          {VIEWS.map((v) => (
            <button
              key={v.id}
              type="button"
              className={`${styles.chip} ${view === v.id ? styles.chipActive : ''}`}
              aria-pressed={view === v.id}
              onClick={() => choose(v.id)}
            >
              {v.label}
            </button>
          ))}
        </div>
      </div>
      {view === 'clientes' ? <CustomerPaymentsPanel /> : <EntriesTab key="receber" kind="RECEIVABLE" />}
    </div>
  );
}

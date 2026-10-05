import { useSearchParams } from 'react-router-dom';

import pageStyles from '../../Page.module.css';
import { CashFlowTab } from './CashFlowTab';
import { EntriesTab } from './EntriesTab';
import { OverviewTab } from './OverviewTab';
import { RegistryTab } from './RegistryTab';
import { ResultTab } from './ResultTab';
import { StockTab } from './StockTab';
import styles from './Company.module.css';

const TABS = [
  { id: '', label: 'Visão geral' },
  { id: 'pagar', label: 'Contas a pagar' },
  { id: 'receber', label: 'Receitas' },
  { id: 'caixa', label: 'Fluxo de caixa' },
  { id: 'resultado', label: 'Resultado do mês' },
  { id: 'estoque', label: 'Estoque' },
  { id: 'cadastros', label: 'Cadastros' },
];

/**
 * A gestão da empresa (só administradores): contas a pagar e receitas,
 * caixa e projeção, resultado do mês (DRE), estoque e os cadastros.
 */
export function CompanyPage() {
  const [params, setParams] = useSearchParams();
  const tab = TABS.some((t) => t.id === params.get('aba')) ? (params.get('aba') ?? '') : '';
  const open = (id: string) => setParams(id ? { aba: id } : {}, { replace: true });

  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>Empresa</h1>
            <p className={pageStyles.description}>
              A parte administrativa: o que a empresa paga e recebe, o caixa e a projeção, o resultado de cada mês
              e o estoque. As faturas dos clientes entram sozinhas quando são pagas.
            </p>
          </div>
        </header>

        <div className={`${pageStyles.tabs} ${styles.tabs}`} role="tablist">
          {TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={tab === t.id}
              className={`${pageStyles.tab} ${tab === t.id ? pageStyles.tabActive : ''}`}
              onClick={() => open(t.id)}
            >
              {t.label}
            </button>
          ))}
        </div>

        {tab === 'pagar' ? (
          <EntriesTab key="pagar" kind="PAYABLE" />
        ) : tab === 'receber' ? (
          <EntriesTab key="receber" kind="RECEIVABLE" />
        ) : tab === 'caixa' ? (
          <CashFlowTab />
        ) : tab === 'resultado' ? (
          <ResultTab />
        ) : tab === 'estoque' ? (
          <StockTab />
        ) : tab === 'cadastros' ? (
          <RegistryTab />
        ) : (
          <OverviewTab onOpen={open} />
        )}
      </div>
    </div>
  );
}

import { useEffect, useState } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';

import { organizationsApi, vehiclesApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { SelectField, TextField } from '@/components/ui/Field';
import { Pager } from '@/components/ui/Pager';
import { Spinner } from '@/components/ui/Spinner';
import { pageKeepingFirst, usePageSize } from '@/hooks/usePagination';
import { formatDeviceStatus, formatRelative } from '@/services/format';
import { formatTaxId } from '@/services/taxid';
import type { VehicleDirectoryEntry, VehicleDirectoryFilter } from '@/types';

import pageStyles from '../Page.module.css';
import styles from './VehiclesPage.module.css';

/** Os filtros ficam na URL: a busca volta igual depois de abrir um veículo. */
const PARAMS: Record<keyof VehicleDirectoryFilter, string> = {
  plate: 'placa',
  organization: 'associacao',
  customer: 'cliente',
  document: 'documento',
  email: 'email',
};
const KEYS = Object.keys(PARAMS) as (keyof VehicleDirectoryFilter)[];

export function filterOf(params: URLSearchParams): VehicleDirectoryFilter {
  return Object.fromEntries(KEYS.map((k) => [k, params.get(PARAMS[k]) ?? ''])) as unknown as VehicleDirectoryFilter;
}

function sameFilter(a: VehicleDirectoryFilter, b: VehicleDirectoryFilter): boolean {
  return KEYS.every((k) => a[k].trim() === b[k].trim());
}

function paramsOf(filter: VehicleDirectoryFilter, page: number): URLSearchParams {
  const out = new URLSearchParams();
  for (const k of KEYS) if (filter[k].trim()) out.set(PARAMS[k], filter[k].trim());
  if (page > 1) out.set('pagina', String(page));
  return out;
}

/** O valor depois de uma pausa na digitação. */
function useDebounced<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

const EMPTY: VehicleDirectoryFilter = { plate: '', organization: '', customer: '', document: '', email: '' };

/** Os veículos de todas as contas (central): paginados, com filtros. */
export function VehiclesPage() {
  const [params, setParams] = useSearchParams();
  const applied = filterOf(params);
  const page = Math.max(1, Math.floor(Number(params.get('pagina'))) || 1);
  const [pageSize, choosePageSize] = usePageSize('vehicles');
  // O que está sendo digitado vai para a URL (e para a busca) depois de uma
  // pausa, voltando à primeira página.
  const [draft, setDraft] = useState(applied);
  const typed = useDebounced(draft, 350);
  useEffect(() => {
    setParams(
      (current) => (sameFilter(typed, filterOf(current)) ? current : paramsOf(typed, 1)),
      { replace: true },
    );
  }, [typed, setParams]);

  const list = useQuery({
    queryKey: ['vehicle-directory', applied, page, pageSize],
    queryFn: () => vehiclesApi.directory(applied, page, pageSize),
    placeholderData: keepPreviousData,
  });
  const organizations = useQuery({ queryKey: ['organizations'], queryFn: organizationsApi.list });

  const set = (key: keyof VehicleDirectoryFilter) => (value: string) => setDraft((d) => ({ ...d, [key]: value }));
  const filtering = KEYS.some((k) => draft[k].trim());
  const goTo = (next: number) => setParams(paramsOf(applied, next), { replace: true });

  const data = list.data;

  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>Veículos</h1>
            <p className={pageStyles.description}>
              Os veículos de todos os clientes e associações. Busque pela placa, pela associação ou pelos dados do
              cliente.
            </p>
          </div>
        </header>

        <Card>
          <form className={styles.filters} role="search" aria-label="Filtrar veículos" onSubmit={(e) => e.preventDefault()}>
            <TextField
              label="Placa"
              placeholder="ABC1D23"
              value={draft.plate}
              autoCapitalize="characters"
              onChange={(e) => set('plate')(e.target.value)}
            />
            <SelectField
              label="Associação"
              value={draft.organization}
              onChange={(e) => set('organization')(e.target.value)}
            >
              <option value="">Todas</option>
              <option value="none">Sem associação</option>
              {(organizations.data ?? []).map((o) => (
                <option key={o.id} value={o.id}>
                  {o.name}
                </option>
              ))}
            </SelectField>
            <TextField label="Nome do cliente" value={draft.customer} onChange={(e) => set('customer')(e.target.value)} />
            <TextField
              label="CPF ou CNPJ"
              inputMode="numeric"
              value={draft.document}
              onChange={(e) => set('document')(e.target.value)}
            />
            <TextField
              label="E-mail"
              type="search"
              autoCapitalize="none"
              value={draft.email}
              onChange={(e) => set('email')(e.target.value)}
            />
          </form>
          <div className={styles.summary}>
            <span aria-live="polite">
              {data ? (data.total === 1 ? '1 veículo' : `${data.total.toLocaleString('pt-BR')} veículos`) : ' '}
              {list.isFetching && !list.isLoading && <span className={styles.muted}> · atualizando…</span>}
            </span>
            {filtering && (
              <Button size="small" variant="ghost" onClick={() => setDraft(EMPTY)}>
                Limpar filtros
              </Button>
            )}
          </div>
        </Card>

        <Card flush>
          {list.isLoading ? (
            <Spinner label="Carregando os veículos" />
          ) : list.isError ? (
            <EmptyState icon="⚠️" title="Não deu para carregar os veículos" description={(list.error as Error).message} />
          ) : !data || data.items.length === 0 ? (
            <EmptyState
              icon="🚗"
              title={filtering ? 'Nenhum veículo com esses filtros' : 'Nenhum veículo cadastrado'}
              action={
                page > 1 && data && data.total > 0 ? (
                  <Button size="small" onClick={() => goTo(1)}>
                    Voltar à primeira página
                  </Button>
                ) : undefined
              }
            />
          ) : (
            <>
              <div className={pageStyles.tableWrap}>
                <table className={`${pageStyles.table} ${styles.list}`}>
                  <thead>
                    <tr>
                      <th>Veículo</th>
                      <th>Cliente</th>
                      <th>Associação</th>
                      <th>Rastreador</th>
                      <th>Cadastro</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.items.map((v) => (
                      <Row key={v.id} vehicle={v} />
                    ))}
                  </tbody>
                </table>
              </div>
              <Pager
                page={page}
                pageSize={pageSize}
                total={data.total}
                onPage={goTo}
                onPageSize={(size) => {
                  choosePageSize(size);
                  goTo(pageKeepingFirst(page, pageSize, size));
                }}
              />
            </>
          )}
        </Card>
      </div>
    </div>
  );
}

function Row({ vehicle: v }: { vehicle: VehicleDirectoryEntry }) {
  // A marca e o modelo, quando o nome ainda não diz; e o ano.
  const brandModel = [v.brand, v.model].filter(Boolean).join(' ');
  const details = [v.name.toLowerCase().includes(brandModel.toLowerCase()) ? '' : brandModel, v.year].filter(Boolean).join(' ');
  return (
    <tr>
      <td>
        <Link to={`/veiculos/${v.id}`} className={styles.link}>
          {v.name}
        </Link>
        <div className={styles.plate}>{v.plate || 'sem placa'}</div>
        {details && <div className={styles.muted}>{details}</div>}
      </td>
      <td data-label="Cliente">
        {v.customerId ? (
          <>
            <Link to={`/clientes/${v.customerId}`} className={styles.link}>
              {v.customerName || v.customerEmail}
            </Link>
            {v.customerDocument && <div className={styles.muted}>{formatTaxId(v.customerDocument)}</div>}
            <div className={styles.muted}>{v.customerEmail}</div>
          </>
        ) : (
          <span className={styles.muted}>Da central</span>
        )}
      </td>
      <td data-label="Associação">
        {v.organizationId ? (
          <Link to={`/associacoes/${v.organizationId}`} className={styles.link}>
            {v.organizationName}
          </Link>
        ) : (
          <span className={styles.muted}>—</span>
        )}
      </td>
      <td data-label="Rastreador">
        {v.deviceId ? (
          <>
            {v.lastSeenAt ? (
              <Badge tone={v.deviceStatus === 'ONLINE' ? 'success' : v.deviceStatus === 'STALE' ? 'warning' : 'neutral'} dot>
                {formatDeviceStatus(v.deviceStatus)}
              </Badge>
            ) : (
              <Badge tone="neutral">Nunca conectou</Badge>
            )}
            <div className={styles.imei}>{v.deviceImei}</div>
            {v.lastSeenAt && <div className={styles.muted}>{formatRelative(v.lastSeenAt)}</div>}
          </>
        ) : (
          <span className={styles.muted}>Sem rastreador</span>
        )}
      </td>
      <td data-label="Cadastro" className={styles.muted}>
        {new Date(v.createdAt).toLocaleDateString('pt-BR')}
      </td>
    </tr>
  );
}

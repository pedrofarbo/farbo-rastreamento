import { useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { maintenanceApi, vehiclesApi } from '@/api/resources';
import { DoneModal, MaintenanceFormModal } from '@/components/maintenance/MaintenanceModals';
import { STATUS_TONE } from '@/components/maintenance/VehicleMaintenance';
import { Badge } from '@/components/ui/Badge';
import { BackButton } from '@/components/ui/BackButton';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import fieldStyles from '@/components/ui/Field.module.css';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { useFleet } from '@/hooks/useFleet';
import { STATUS_LABEL, intervalLabel, remainingLabel } from '@/services/maintenance';
import { useAuth } from '@/stores/AuthContext';
import type { MaintenanceItem, MaintenanceStatus } from '@/types';

import pageStyles from '../Page.module.css';
import styles from './MaintenancePage.module.css';

export const maintenanceKey = ['me', 'maintenance'] as const;

/** Quantas linhas aparecem de cada vez. */
const PAGE = 50;

const FILTERS: { id: MaintenanceStatus | 'all'; label: string }[] = [
  { id: 'all', label: 'Todas' },
  { id: 'due', label: 'Vencidas' },
  { id: 'soon', label: 'Perto' },
  { id: 'unknown', label: 'Sem hodômetro' },
  { id: 'ok', label: 'Em dia' },
];

/**
 * A manutenção da frota: o que venceu, o que está perto e o resto, das mais
 * urgentes às em dia. Quem mexe na conta cria a manutenção em vários
 * veículos de uma vez e registra várias feitas de uma vez. No app, aberta
 * pela Conta, ganha o voltar (backTo).
 */
export function MaintenancePage({ backTo }: { backTo?: string } = {}) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const { canOperateAccount } = useAuth();
  const { groups } = useFleet();
  const list = useQuery({ queryKey: maintenanceKey, queryFn: maintenanceApi.list });
  const vehicles = useQuery({ queryKey: ['vehicles'], queryFn: vehiclesApi.list, enabled: canOperateAccount });
  const [filter, setFilter] = useState<MaintenanceStatus | 'all'>('all');
  const [group, setGroup] = useState('');
  const [search, setSearch] = useState('');
  const [limit, setLimit] = useState(PAGE);
  const [selected, setSelected] = useState<string[]>([]);
  const [form, setForm] = useState<{ item: MaintenanceItem | null } | null>(null);
  const [doing, setDoing] = useState<MaintenanceItem[]>([]);
  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: maintenanceKey });
    queryClient.invalidateQueries({ queryKey: ['vehicle'] });
  };

  const items = list.data?.items ?? [];
  const counts = list.data?.counts ?? {};
  const shown = useMemo(() => {
    const q = search.trim().toLowerCase();
    return items.filter(
      (it) =>
        (filter === 'all' || it.status === filter) &&
        (!group || it.groupId === group) &&
        (!q || `${it.vehicleName} ${it.plate} ${it.name}`.toLowerCase().includes(q)),
    );
  }, [items, filter, group, search]);
  const pickable = (vehicles.data ?? []).filter((v) => !v.shared).map((v) => ({ id: v.id, name: v.name, plate: v.plate, groupId: v.groupId }));
  const selectedItems = items.filter((it) => selected.includes(it.id));
  const toggle = (id: string, on: boolean) => setSelected(on ? [...selected, id] : selected.filter((s) => s !== id));

  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <div className={pageStyles.titleRow}>
              {backTo && <BackButton to={backTo} />}
              <h1 className={pageStyles.title}>Manutenção</h1>
            </div>
            <p className={pageStyles.description}>
              O que vence em cada veículo, por km ou por data. Os km vêm do hodômetro informado mais o que o GPS mediu
              depois; quem cuida da conta recebe um e-mail quando algo fica perto ou vence.
            </p>
          </div>
          {canOperateAccount && (
            <div className={pageStyles.actions}>
              <Button variant="primary" disabled={pickable.length === 0} onClick={() => setForm({ item: null })}>
                Nova manutenção
              </Button>
            </div>
          )}
        </header>

        <div className={styles.tiles}>
          {(['due', 'soon', 'unknown', 'ok'] as MaintenanceStatus[]).map((status) => (
            <button
              key={status}
              type="button"
              className={`${styles.tile} ${styles[status]} ${filter === status ? styles.tileOn : ''}`}
              onClick={() => setFilter(filter === status ? 'all' : status)}
            >
              <span className={styles.tileValue}>{counts[status] ?? 0}</span>
              <span className={styles.tileLabel}>{FILTERS.find((f) => f.id === status)?.label}</span>
            </button>
          ))}
        </div>

        {list.isLoading ? (
          <Spinner label="Carregando" />
        ) : items.length === 0 ? (
          <Card>
            <EmptyState
              icon="🔧"
              title="Nenhuma manutenção ainda"
              description={
                canOperateAccount
                  ? 'Cadastre a troca de óleo, os pneus ou a revisão: dá para criar em toda a frota de uma vez.'
                  : 'Quem cuida da conta ainda não cadastrou manutenções.'
              }
            />
          </Card>
        ) : (
          <Card flush>
            <div className={styles.toolbar}>
              <input
                className={`${fieldStyles.input} ${styles.search}`}
                type="search"
                placeholder="Buscar veículo, placa ou manutenção"
                value={search}
                onChange={(e) => {
                  setSearch(e.target.value);
                  setLimit(PAGE);
                }}
              />
              <select className={fieldStyles.select} value={filter} onChange={(e) => setFilter(e.target.value as MaintenanceStatus | 'all')}>
                {FILTERS.map((f) => (
                  <option key={f.id} value={f.id}>
                    {f.label}
                  </option>
                ))}
              </select>
              {groups.length > 0 && (
                <select className={fieldStyles.select} value={group} onChange={(e) => setGroup(e.target.value)}>
                  <option value="">Toda a frota</option>
                  {groups.map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.name}
                    </option>
                  ))}
                </select>
              )}
            </div>

            {canOperateAccount && selected.length > 0 && (
              <div className={styles.bulk}>
                <span>
                  {selected.length === 1 ? '1 escolhida' : `${selected.length} escolhidas`}
                </span>
                <Button size="small" variant="primary" onClick={() => setDoing(selectedItems)}>
                  Registrar {selected.length === 1 ? 'feita' : 'feitas'}
                </Button>
                <Button size="small" variant="ghost" onClick={() => setSelected([])}>
                  Limpar
                </Button>
              </div>
            )}

            {shown.length === 0 ? (
              <p className={styles.empty}>Nada com esse filtro.</p>
            ) : (
              <ul className={styles.list}>
                {shown.slice(0, limit).map((it) => (
                  <li key={it.id} className={styles.row}>
                    {canOperateAccount && (
                      <input
                        type="checkbox"
                        className={styles.check}
                        aria-label={`Escolher ${it.name} de ${it.vehicleName}`}
                        checked={selected.includes(it.id)}
                        onChange={(e) => toggle(it.id, e.target.checked)}
                      />
                    )}
                    <div className={styles.what}>
                      <span className={styles.itemName}>
                        {it.name}
                        <Badge tone={STATUS_TONE[it.status]}>{STATUS_LABEL[it.status]}</Badge>
                      </span>
                      <Link to={`/veiculos/${it.vehicleId}`} className={styles.vehicle}>
                        {it.vehicleName}
                        {it.plate && <span className={styles.plate}>{it.plate}</span>}
                      </Link>
                      <span className={styles.muted}>{intervalLabel(it)}</span>
                    </div>
                    <span className={it.status === 'due' ? styles.dueText : it.status === 'soon' ? styles.soonText : styles.muted}>
                      {remainingLabel(it)}
                    </span>
                    {canOperateAccount && (
                      <div className={styles.actions}>
                        <Button size="small" variant="secondary" onClick={() => setDoing([it])}>
                          Registrar feita
                        </Button>
                        <Button size="small" variant="ghost" onClick={() => setForm({ item: it })}>
                          Editar
                        </Button>
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            )}
            {shown.length > limit && (
              <div className={styles.more}>
                <Button variant="ghost" onClick={() => setLimit(limit + PAGE)}>
                  Mostrar mais ({shown.length - limit})
                </Button>
              </div>
            )}
          </Card>
        )}
      </div>

      {form && (
        <MaintenanceFormModal
          open
          item={form.item}
          vehicles={form.item ? [{ id: form.item.vehicleId, name: form.item.vehicleName }] : pickable}
          groups={groups}
          onClose={() => setForm(null)}
          onSaved={(message) => {
            setForm(null);
            refresh();
            notify({ tone: 'success', title: message });
          }}
        />
      )}
      {doing.length > 0 && (
        <DoneModal
          items={doing}
          onClose={() => setDoing([])}
          onDone={(message) => {
            setDoing([]);
            setSelected([]);
            refresh();
            notify({ tone: 'success', title: message });
          }}
        />
      )}
    </div>
  );
}

import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';

import { fleetOpsApi, reportsApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { BackButton } from '@/components/ui/BackButton';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { SelectField, TextField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { useVehicles } from '@/hooks/useVehicles';
import { centsToInput, formatDateOnly, formatDateTime, formatMoney, parseMoney, todayISO } from '@/services/format';
import { EXPENSE_KINDS, FINE_STATUS, expenseKindLabel, monthStart, rentalKm, rentalStatus } from '@/services/fleetOps';
import { useAuth } from '@/stores/AuthContext';
import type {
  ExpenseKind,
  FineStatus,
  FleetDriver,
  FleetDriverInput,
  Rental,
  TrafficFine,
  VehicleView,
} from '@/types';

import pageStyles from '../Page.module.css';
import { BusRoutesTab } from './BusRoutesTab';
import styles from './FleetOpsPage.module.css';

type Tab = 'motoristas' | 'custos' | 'multas' | 'locacoes' | 'linhas';

const TABS: { id: Tab; label: string }[] = [
  { id: 'motoristas', label: 'Motoristas' },
  { id: 'custos', label: 'Custos e abastecimento' },
  { id: 'multas', label: 'Multas' },
  { id: 'locacoes', label: 'Locações' },
  { id: 'linhas', label: 'Linhas (itinerários)' },
];

const fleetKey = ['fleet-ops'] as const;

/** "2026-10-23T14:05" (datetime-local) de uma data. */
function localInput(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** Um número digitado ("10.500,5") → número; vazio → null; inválido → NaN. */
export function parseDecimal(text: string): number | null {
  const clean = text.trim().replace(/\./g, '').replace(',', '.');
  if (!clean) return null;
  const n = Number(clean);
  return Number.isFinite(n) ? n : NaN;
}

/** Quantos dias faltam para a CNH vencer (negativo: vencida). */
export function licenseDays(expires: string | null, today = todayISO()): number | null {
  if (!expires) return null;
  return Math.round((Date.parse(expires) - Date.parse(today)) / 86_400_000);
}

/**
 * A gestão da frota da conta: os motoristas (e quem dirige cada veículo), os
 * custos e o abastecimento, as multas e as locações. O Visualizador da conta
 * só vê.
 */
export function FleetOpsPage({ backTo }: { backTo?: string } = {}) {
  const [params, setParams] = useSearchParams();
  const tab = (TABS.find((t) => t.id === params.get('aba'))?.id ?? 'motoristas') as Tab;
  const { data: all = [] } = useVehicles();
  const { isAccountOwner } = useAuth();
  const vehicles = useMemo(() => all.filter((v) => !v.shared), [all]);
  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <div className={pageStyles.titleRow}>
              {backTo && <BackButton to={backTo} />}
              <h1 className={pageStyles.title}>Frota</h1>
            </div>
            <p className={pageStyles.description}>
              Quem dirige cada veículo, quanto cada um custa (com o consumo de combustível), as multas com o motorista da
              hora, as locações, com o aviso de quem não devolveu no prazo, e as linhas de ônibus, com o atraso ao vivo.
            </p>
          </div>
          {isAccountOwner && !backTo && (
            <div className={pageStyles.actions}>
              <Link to="/integracoes" className={styles.link}>
                Integrações (API e webhooks)
              </Link>
            </div>
          )}
        </header>
        <div className={pageStyles.tabs} role="tablist">
          {TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={tab === t.id}
              className={`${pageStyles.tab} ${tab === t.id ? pageStyles.tabActive : ''}`}
              onClick={() => setParams({ aba: t.id }, { replace: true })}
            >
              {t.label}
            </button>
          ))}
        </div>
        {tab === 'motoristas' ? (
          <DriversTab vehicles={vehicles} />
        ) : tab === 'custos' ? (
          <CostsTab vehicles={vehicles} />
        ) : tab === 'multas' ? (
          <FinesTab vehicles={vehicles} />
        ) : tab === 'linhas' ? (
          <BusRoutesTab vehicles={vehicles} />
        ) : (
          <RentalsTab vehicles={vehicles} />
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Motoristas
// ---------------------------------------------------------------------------

const EMPTY_DRIVER: FleetDriverInput = {
  name: '',
  phone: '',
  document: '',
  licenseNumber: '',
  licenseCategory: '',
  licenseExpires: '',
  notes: '',
};

function DriversTab({ vehicles }: { vehicles: VehicleView[] }) {
  const { canOperateAccount } = useAuth();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [editing, setEditing] = useState<FleetDriver | 'new' | null>(null);
  const drivers = useQuery({ queryKey: [...fleetKey, 'drivers'], queryFn: fleetOpsApi.drivers });
  const assign = useMutation({
    mutationFn: ({ vehicleId, driverId }: { vehicleId: string; driverId: string | null }) =>
      fleetOpsApi.assignDriver(vehicleId, driverId),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: [...fleetKey, 'drivers'] }),
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para trocar', description: err.message }),
  });
  const list = drivers.data ?? [];
  return (
    <>
      <Card
        title="Motoristas"
        subtitle="Quem dirige cada veículo: é por aqui que a jornada e as multas acham o motorista."
        actions={
          canOperateAccount ? (
            <Button size="small" variant="primary" onClick={() => setEditing('new')}>
              Novo motorista
            </Button>
          ) : undefined
        }
        flush
      >
        {drivers.isLoading ? (
          <Spinner label="Carregando" />
        ) : list.length === 0 ? (
          <EmptyState icon="🧑‍✈️" title="Nenhum motorista ainda" description="Cadastre os motoristas e diga quem dirige cada veículo." />
        ) : (
          <div className={pageStyles.tableWrap}>
            <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
              <thead>
                <tr>
                  <th>Motorista</th>
                  <th>CNH</th>
                  <th>Dirige agora</th>
                  <th aria-label="Ações" />
                </tr>
              </thead>
              <tbody>
                {list.map((d) => {
                  const days = licenseDays(d.licenseExpires);
                  return (
                    <tr key={d.id} className={d.active ? undefined : styles.inactive}>
                      <td>
                        <strong>{d.name}</strong>
                        {!d.active && <Badge tone="neutral">Desativado</Badge>}
                        <div className={styles.muted}>{[d.phone, d.document].filter(Boolean).join(' · ')}</div>
                      </td>
                      <td data-label="CNH">
                        {d.licenseCategory || d.licenseNumber ? `${d.licenseCategory || '—'} · ${d.licenseNumber || '—'}` : '—'}
                        {d.licenseExpires && (
                          <div className={days !== null && days <= 30 ? styles.warn : styles.muted}>
                            {days !== null && days < 0 ? 'vencida em ' : 'vale até '}
                            {formatDateOnly(d.licenseExpires)}
                          </div>
                        )}
                      </td>
                      <td data-label="Dirige agora">
                        {canOperateAccount && d.active ? (
                          <select
                            className={fieldStyles.select}
                            aria-label={`Veículo de ${d.name}`}
                            value={d.vehicleId ?? ''}
                            onChange={(e) => {
                              const vehicleId = e.target.value;
                              if (vehicleId) assign.mutate({ vehicleId, driverId: d.id });
                              else if (d.vehicleId) assign.mutate({ vehicleId: d.vehicleId, driverId: null });
                            }}
                          >
                            <option value="">Nenhum veículo</option>
                            {vehicles.map((v) => (
                              <option key={v.id} value={v.id}>
                                {v.name}
                                {v.plate ? ` · ${v.plate}` : ''}
                              </option>
                            ))}
                          </select>
                        ) : (
                          d.vehicleName || '—'
                        )}
                        {d.since && <div className={styles.muted}>desde {formatDateTime(d.since)}</div>}
                      </td>
                      <td>
                        {canOperateAccount && (
                          <Button size="small" variant="ghost" onClick={() => setEditing(d)}>
                            Editar
                          </Button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      {editing && <DriverModal driver={editing === 'new' ? null : editing} onClose={() => setEditing(null)} />}
    </>
  );
}

function DriverModal({ driver, onClose }: { driver: FleetDriver | null; onClose: () => void }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [draft, setDraft] = useState<FleetDriverInput>(() =>
    driver
      ? {
          name: driver.name,
          phone: driver.phone,
          document: driver.document,
          licenseNumber: driver.licenseNumber,
          licenseCategory: driver.licenseCategory,
          licenseExpires: driver.licenseExpires ?? '',
          notes: driver.notes,
          active: driver.active,
        }
      : EMPTY_DRIVER,
  );
  const [error, setError] = useState('');
  const save = useMutation({
    mutationFn: () => (driver ? fleetOpsApi.updateDriver(driver.id, draft) : fleetOpsApi.createDriver(draft)),
    onSuccess: (d) => {
      void queryClient.invalidateQueries({ queryKey: [...fleetKey, 'drivers'] });
      notify({ tone: 'success', title: driver ? `${d.name} atualizado` : `${d.name} cadastrado` });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });
  const set = (k: keyof FleetDriverInput, v: string | boolean) => setDraft({ ...draft, [k]: v });
  return (
    <Modal
      open
      title={driver ? `Editar ${driver.name}` : 'Novo motorista'}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Salvar
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        {error && <div className={pageStyles.note}>{error}</div>}
        <TextField label="Nome" maxLength={120} value={draft.name} onChange={(e) => set('name', e.target.value)} />
        <div className={pageStyles.formRow}>
          <TextField label="Celular" maxLength={30} value={draft.phone} onChange={(e) => set('phone', e.target.value)} />
          <TextField label="CPF" maxLength={30} value={draft.document} onChange={(e) => set('document', e.target.value)} />
        </div>
        <div className={pageStyles.formRow}>
          <TextField label="CNH (número)" maxLength={30} value={draft.licenseNumber} onChange={(e) => set('licenseNumber', e.target.value)} />
          <TextField label="Categoria" maxLength={5} value={draft.licenseCategory} placeholder="D" onChange={(e) => set('licenseCategory', e.target.value)} />
          <TextField label="Validade" type="date" value={draft.licenseExpires} onChange={(e) => set('licenseExpires', e.target.value)} />
        </div>
        <TextField label="Observações" maxLength={500} value={draft.notes} onChange={(e) => set('notes', e.target.value)} />
        {driver && (
          <label className={styles.check}>
            <input type="checkbox" checked={draft.active !== false} onChange={(e) => set('active', e.target.checked)} />
            Ativo (desativado, deixa de dirigir o veículo de agora)
          </label>
        )}
      </div>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Custos e abastecimento
// ---------------------------------------------------------------------------

function CostsTab({ vehicles }: { vehicles: VehicleView[] }) {
  const { canOperateAccount } = useAuth();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [from, setFrom] = useState(monthStart());
  const [to, setTo] = useState(todayISO());
  const [adding, setAdding] = useState(false);
  const expenses = useQuery({ queryKey: [...fleetKey, 'expenses', from, to], queryFn: () => fleetOpsApi.expenses(from, to) });
  const costs = useQuery({ queryKey: [...fleetKey, 'costs', from, to], queryFn: () => reportsApi.costs({ from, to }) });
  const remove = useMutation({
    mutationFn: (id: string) => fleetOpsApi.deleteExpense(id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: fleetKey }),
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para apagar', description: err.message }),
  });
  const names = new Map(vehicles.map((v) => [v.id, v]));
  const rows = (costs.data?.rows ?? []).filter((r) => r.totalCents > 0 || r.distanceKm > 0);
  const total = rows.reduce((t, r) => t + r.totalCents, 0);
  return (
    <>
      <div className={styles.toolbar}>
        <label className={styles.date}>
          De
          <input className={fieldStyles.input} type="date" value={from} max={to} onChange={(e) => setFrom(e.target.value)} />
        </label>
        <label className={styles.date}>
          Até
          <input className={fieldStyles.input} type="date" value={to} min={from} max={todayISO()} onChange={(e) => setTo(e.target.value)} />
        </label>
        <span className={styles.total}>Total no período: {formatMoney(total)}</span>
        {canOperateAccount && (
          <Button variant="primary" onClick={() => setAdding(true)}>
            Lançar custo
          </Button>
        )}
      </div>
      <Card title="Por veículo" subtitle="O custo por km usa os km medidos pelo GPS; o consumo, de um tanque cheio ao outro (com o hodômetro)." flush>
        {costs.isLoading ? (
          <Spinner label="Calculando" />
        ) : rows.length === 0 ? (
          <EmptyState icon="⛽" title="Nada no período" description="Lance o abastecimento, os pedágios e as outras despesas de cada veículo." />
        ) : (
          <div className={pageStyles.tableWrap}>
            <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
              <thead>
                <tr>
                  <th>Veículo</th>
                  <th>Total</th>
                  <th>Combustível</th>
                  <th>Km rodados</th>
                  <th>Custo por km</th>
                  <th>Consumo</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => {
                  const v = names.get(r.vehicleId);
                  return (
                    <tr key={r.vehicleId}>
                      <td>
                        <strong>{v?.name ?? 'Veículo'}</strong>
                        {v?.plate && <span className={styles.plate}>{v.plate}</span>}
                      </td>
                      <td data-label="Total">{formatMoney(r.totalCents)}</td>
                      <td data-label="Combustível">
                        {formatMoney(r.byKind.fuel ?? 0)}
                        {r.liters > 0 && <div className={styles.muted}>{r.liters.toLocaleString('pt-BR')} l</div>}
                      </td>
                      <td data-label="Km rodados">{r.distanceKm.toLocaleString('pt-BR')} km</td>
                      <td data-label="Custo por km">{r.centsPerKm !== null ? formatMoney(Math.round(r.centsPerKm)) : '—'}</td>
                      <td data-label="Consumo">{r.kmPerLiter !== null ? `${r.kmPerLiter.toLocaleString('pt-BR')} km/l` : '—'}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      <Card title="Lançamentos" flush>
        {expenses.isLoading ? (
          <Spinner label="Carregando" />
        ) : (expenses.data ?? []).length === 0 ? (
          <p className={styles.empty}>Nenhum lançamento no período.</p>
        ) : (
          <div className={pageStyles.tableWrap}>
            <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
              <thead>
                <tr>
                  <th>Data</th>
                  <th>Veículo</th>
                  <th>Tipo</th>
                  <th>Valor</th>
                  <th>Detalhe</th>
                  <th aria-label="Ações" />
                </tr>
              </thead>
              <tbody>
                {(expenses.data ?? []).map((e) => (
                  <tr key={e.id}>
                    <td>{formatDateOnly(e.spentOn)}</td>
                    <td data-label="Veículo">{e.vehicleName}</td>
                    <td data-label="Tipo">{expenseKindLabel(e.kind)}</td>
                    <td data-label="Valor">{formatMoney(e.amountCents)}</td>
                    <td data-label="Detalhe" className={styles.muted}>
                      {[
                        e.liters !== null ? `${e.liters.toLocaleString('pt-BR')} l` : '',
                        e.odometerKm !== null ? `${e.odometerKm.toLocaleString('pt-BR')} km` : '',
                        e.fullTank ? 'tanque cheio' : '',
                        e.description,
                      ]
                        .filter(Boolean)
                        .join(' · ') || '—'}
                    </td>
                    <td>
                      {canOperateAccount && (
                        <Button size="small" variant="ghost" loading={remove.isPending && remove.variables === e.id} onClick={() => remove.mutate(e.id)}>
                          Apagar
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      {adding && <ExpenseModal vehicles={vehicles} onClose={() => setAdding(false)} />}
    </>
  );
}

function ExpenseModal({ vehicles, onClose }: { vehicles: VehicleView[]; onClose: () => void }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [vehicleId, setVehicleId] = useState(vehicles[0]?.id ?? '');
  const [kind, setKind] = useState<ExpenseKind>('fuel');
  const [spentOn, setSpentOn] = useState(todayISO());
  const [amount, setAmount] = useState('');
  const [liters, setLiters] = useState('');
  const [odometer, setOdometer] = useState('');
  const [fullTank, setFullTank] = useState(true);
  const [description, setDescription] = useState('');
  const [error, setError] = useState('');
  const save = useMutation({
    mutationFn: () => {
      const cents = parseMoney(amount);
      if (!cents) throw new Error('Informe o valor (por exemplo, 250,00).');
      const l = kind === 'fuel' ? parseDecimal(liters) : null;
      const km = parseDecimal(odometer);
      if (Number.isNaN(l) || Number.isNaN(km)) throw new Error('Use só números nos litros e no hodômetro.');
      return fleetOpsApi.createExpense({
        vehicleId,
        kind,
        spentOn,
        amountCents: cents,
        liters: l,
        odometerKm: km,
        fullTank: kind === 'fuel' && fullTank,
        description,
      });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: fleetKey });
      notify({ tone: 'success', title: 'Custo lançado' });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });
  return (
    <Modal
      open
      title="Lançar custo"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Lançar
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        {error && <div className={pageStyles.note}>{error}</div>}
        <div className={pageStyles.formRow}>
          <VehicleSelect vehicles={vehicles} value={vehicleId} onChange={setVehicleId} />
          <SelectField label="Tipo" value={kind} onChange={(e) => setKind(e.target.value as ExpenseKind)}>
            {EXPENSE_KINDS.map((k) => (
              <option key={k.id} value={k.id}>
                {k.label}
              </option>
            ))}
          </SelectField>
        </div>
        <div className={pageStyles.formRow}>
          <TextField label="Data" type="date" value={spentOn} max={todayISO()} onChange={(e) => setSpentOn(e.target.value)} />
          <TextField label="Valor (R$)" inputMode="decimal" value={amount} placeholder="250,00" onChange={(e) => setAmount(e.target.value)} />
        </div>
        {kind === 'fuel' && (
          <div className={pageStyles.formRow}>
            <TextField label="Litros" inputMode="decimal" value={liters} placeholder="42,5" onChange={(e) => setLiters(e.target.value)} />
            <TextField label="Hodômetro (km)" inputMode="decimal" value={odometer} placeholder="48.312" onChange={(e) => setOdometer(e.target.value)} />
          </div>
        )}
        {kind === 'fuel' && (
          <label className={styles.check}>
            <input type="checkbox" checked={fullTank} onChange={(e) => setFullTank(e.target.checked)} />
            Encheu o tanque (o consumo sai de um tanque cheio ao outro)
          </label>
        )}
        <TextField label="Descrição (opcional)" maxLength={200} value={description} onChange={(e) => setDescription(e.target.value)} />
      </div>
    </Modal>
  );
}

function VehicleSelect({ vehicles, value, onChange }: { vehicles: VehicleView[]; value: string; onChange: (id: string) => void }) {
  return (
    <SelectField label="Veículo" value={value} onChange={(e) => onChange(e.target.value)}>
      {vehicles.map((v) => (
        <option key={v.id} value={v.id}>
          {v.name}
          {v.plate ? ` · ${v.plate}` : ''}
        </option>
      ))}
    </SelectField>
  );
}

// ---------------------------------------------------------------------------
// Multas
// ---------------------------------------------------------------------------

function FinesTab({ vehicles }: { vehicles: VehicleView[] }) {
  const { canOperateAccount } = useAuth();
  const [editing, setEditing] = useState<TrafficFine | 'new' | null>(null);
  const fines = useQuery({ queryKey: [...fleetKey, 'fines'], queryFn: fleetOpsApi.fines });
  const list = fines.data ?? [];
  const pending = list.filter((f) => f.status === 'pending');
  return (
    <>
      <Card
        title="Multas"
        subtitle={
          pending.length > 0
            ? `${pending.length} a pagar: ${formatMoney(pending.reduce((t, f) => t + f.amountCents, 0))} e ${pending.reduce((t, f) => t + f.points, 0)} pontos.`
            : 'O motorista sai de quem dirigia o veículo na hora da infração.'
        }
        actions={
          canOperateAccount ? (
            <Button size="small" variant="primary" onClick={() => setEditing('new')}>
              Lançar multa
            </Button>
          ) : undefined
        }
        flush
      >
        {fines.isLoading ? (
          <Spinner label="Carregando" />
        ) : list.length === 0 ? (
          <EmptyState icon="🧾" title="Nenhuma multa" />
        ) : (
          <div className={pageStyles.tableWrap}>
            <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
              <thead>
                <tr>
                  <th>Infração</th>
                  <th>Veículo</th>
                  <th>Motorista</th>
                  <th>Valor</th>
                  <th>Vencimento</th>
                  <th>Situação</th>
                  <th aria-label="Ações" />
                </tr>
              </thead>
              <tbody>
                {list.map((f) => (
                  <tr key={f.id}>
                    <td>
                      <strong>{f.description}</strong>
                      <div className={styles.muted}>
                        {formatDateTime(f.occurredAt)}
                        {f.place ? ` · ${f.place}` : ''}
                        {f.code ? ` · ${f.code}` : ''}
                      </div>
                    </td>
                    <td data-label="Veículo">{f.vehicleName}</td>
                    <td data-label="Motorista">
                      {f.driverName || '—'}
                      {f.driverIndicated && <div className={styles.muted}>indicado ao órgão</div>}
                    </td>
                    <td data-label="Valor">
                      {formatMoney(f.amountCents)}
                      {f.points > 0 && <div className={styles.muted}>{f.points} pontos</div>}
                    </td>
                    <td data-label="Vencimento">{f.dueOn ? formatDateOnly(f.dueOn) : '—'}</td>
                    <td data-label="Situação">
                      <Badge tone={FINE_STATUS[f.status].tone}>{FINE_STATUS[f.status].label}</Badge>
                    </td>
                    <td>
                      {canOperateAccount && (
                        <Button size="small" variant="ghost" onClick={() => setEditing(f)}>
                          Editar
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      {editing && <FineModal vehicles={vehicles} fine={editing === 'new' ? null : editing} onClose={() => setEditing(null)} />}
    </>
  );
}

function FineModal({ vehicles, fine, onClose }: { vehicles: VehicleView[]; fine: TrafficFine | null; onClose: () => void }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const drivers = useQuery({ queryKey: [...fleetKey, 'drivers'], queryFn: fleetOpsApi.drivers });
  const [vehicleId, setVehicleId] = useState(fine?.vehicleId ?? vehicles[0]?.id ?? '');
  const [when, setWhen] = useState(fine ? localInput(new Date(fine.occurredAt)) : localInput(new Date()));
  const [description, setDescription] = useState(fine?.description ?? '');
  const [place, setPlace] = useState(fine?.place ?? '');
  const [code, setCode] = useState(fine?.code ?? '');
  const [amount, setAmount] = useState(fine ? centsToInput(fine.amountCents) : '');
  const [points, setPoints] = useState(String(fine?.points ?? 0));
  const [dueOn, setDueOn] = useState(fine?.dueOn ?? '');
  const [status, setStatus] = useState<FineStatus>(fine?.status ?? 'pending');
  const [driverId, setDriverId] = useState(fine?.driverId ?? '');
  const [indicated, setIndicated] = useState(fine?.driverIndicated ?? false);
  const [error, setError] = useState('');
  const save = useMutation({
    mutationFn: () => {
      const occurred = new Date(when);
      if (Number.isNaN(occurred.getTime())) throw new Error('Informe quando foi a infração.');
      const input = {
        vehicleId,
        driverId: driverId || null,
        occurredAt: occurred.toISOString(),
        place,
        description,
        code,
        amountCents: parseMoney(amount) ?? 0,
        points: Number(points) || 0,
        dueOn,
        status,
        driverIndicated: indicated,
        notes: fine?.notes ?? '',
      };
      return fine ? fleetOpsApi.updateFine(fine.id, input) : fleetOpsApi.createFine(input);
    },
    onSuccess: (f) => {
      void queryClient.invalidateQueries({ queryKey: [...fleetKey, 'fines'] });
      notify({ tone: 'success', title: fine ? 'Multa atualizada' : 'Multa lançada', description: f.driverName ? `Motorista: ${f.driverName}` : undefined });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });
  const remove = useMutation({
    mutationFn: () => fleetOpsApi.deleteFine(fine!.id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [...fleetKey, 'fines'] });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });
  return (
    <Modal
      open
      title={fine ? 'Editar multa' : 'Lançar multa'}
      onClose={onClose}
      footer={
        <>
          {fine && (
            <Button variant="ghost" loading={remove.isPending} onClick={() => remove.mutate()}>
              Apagar
            </Button>
          )}
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Salvar
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        {error && <div className={pageStyles.note}>{error}</div>}
        <div className={pageStyles.formRow}>
          {fine ? (
            <TextField label="Veículo" value={fine.vehicleName} disabled />
          ) : (
            <VehicleSelect vehicles={vehicles} value={vehicleId} onChange={setVehicleId} />
          )}
          <TextField label="Data e hora" type="datetime-local" value={when} onChange={(e) => setWhen(e.target.value)} />
        </div>
        <TextField label="Infração" maxLength={200} value={description} placeholder="Velocidade até 20% acima do limite" onChange={(e) => setDescription(e.target.value)} />
        <div className={pageStyles.formRow}>
          <TextField label="Local" maxLength={200} value={place} onChange={(e) => setPlace(e.target.value)} />
          <TextField label="Código" maxLength={20} value={code} placeholder="745-50" onChange={(e) => setCode(e.target.value)} />
        </div>
        <div className={pageStyles.formRow}>
          <TextField label="Valor (R$)" inputMode="decimal" value={amount} placeholder="130,16" onChange={(e) => setAmount(e.target.value)} />
          <TextField label="Pontos" inputMode="numeric" value={points} onChange={(e) => setPoints(e.target.value)} />
          <TextField label="Vencimento" type="date" value={dueOn} onChange={(e) => setDueOn(e.target.value)} />
        </div>
        <div className={pageStyles.formRow}>
          <SelectField label="Motorista" value={driverId} onChange={(e) => setDriverId(e.target.value)}>
            <option value="">Quem dirigia na hora</option>
            {(drivers.data ?? []).map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </SelectField>
          <SelectField label="Situação" value={status} onChange={(e) => setStatus(e.target.value as FineStatus)}>
            {(Object.keys(FINE_STATUS) as FineStatus[]).map((s) => (
              <option key={s} value={s}>
                {FINE_STATUS[s].label}
              </option>
            ))}
          </SelectField>
        </div>
        <label className={styles.check}>
          <input type="checkbox" checked={indicated} onChange={(e) => setIndicated(e.target.checked)} />
          O condutor foi indicado ao órgão de trânsito
        </label>
      </div>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Locações
// ---------------------------------------------------------------------------

function RentalsTab({ vehicles }: { vehicles: VehicleView[] }) {
  const { canOperateAccount } = useAuth();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [adding, setAdding] = useState(false);
  const [returning, setReturning] = useState<Rental | null>(null);
  const rentals = useQuery({ queryKey: [...fleetKey, 'rentals'], queryFn: fleetOpsApi.rentals, refetchInterval: 60_000 });
  const list = rentals.data ?? [];
  const busy = new Set(list.filter((r) => !r.returnedAt).map((r) => r.vehicleId));
  const extend = useMutation({
    mutationFn: ({ id, endsAt }: { id: string; endsAt: string }) => fleetOpsApi.extendRental(id, endsAt),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [...fleetKey, 'rentals'] });
      notify({ tone: 'success', title: 'Devolução estendida' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para estender', description: err.message }),
  });
  return (
    <>
      <Card
        title="Locações"
        subtitle="Quem está com cada veículo e até quando. Passou da hora sem devolver, você recebe um e-mail."
        actions={
          canOperateAccount ? (
            <Button size="small" variant="primary" disabled={vehicles.every((v) => busy.has(v.id))} onClick={() => setAdding(true)}>
              Nova locação
            </Button>
          ) : undefined
        }
        flush
      >
        {rentals.isLoading ? (
          <Spinner label="Carregando" />
        ) : list.length === 0 ? (
          <EmptyState icon="🔑" title="Nenhuma locação" />
        ) : (
          <div className={pageStyles.tableWrap}>
            <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
              <thead>
                <tr>
                  <th>Veículo</th>
                  <th>Locatário</th>
                  <th>Retirada</th>
                  <th>Devolução</th>
                  <th>Km</th>
                  <th>Situação</th>
                  <th aria-label="Ações" />
                </tr>
              </thead>
              <tbody>
                {list.map((r) => {
                  const st = rentalStatus(r);
                  const km = rentalKm(r);
                  return (
                    <tr key={r.id}>
                      <td>
                        <strong>{r.vehicleName}</strong>
                        {r.plate && <span className={styles.plate}>{r.plate}</span>}
                      </td>
                      <td data-label="Locatário">
                        {r.renterName}
                        {r.renterPhone && <div className={styles.muted}>{r.renterPhone}</div>}
                      </td>
                      <td data-label="Retirada">{formatDateTime(r.startsAt)}</td>
                      <td data-label="Devolução">
                        {r.returnedAt ? formatDateTime(r.returnedAt) : `até ${formatDateTime(r.endsAt)}`}
                      </td>
                      <td data-label="Km">{km !== null ? `${km.toLocaleString('pt-BR')} km` : '—'}</td>
                      <td data-label="Situação">
                        <Badge tone={st.tone}>{st.label}</Badge>
                      </td>
                      <td>
                        {canOperateAccount && !r.returnedAt && (
                          <div className={styles.actions}>
                            <Button size="small" variant="secondary" onClick={() => setReturning(r)}>
                              Devolvido
                            </Button>
                            <Button
                              size="small"
                              variant="ghost"
                              onClick={() => {
                                const end = new Date(Math.max(Date.parse(r.endsAt), Date.now()) + 24 * 3_600_000);
                                extend.mutate({ id: r.id, endsAt: end.toISOString() });
                              }}
                            >
                              +1 dia
                            </Button>
                          </div>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      {adding && <RentalModal vehicles={vehicles.filter((v) => !busy.has(v.id))} onClose={() => setAdding(false)} />}
      {returning && <ReturnModal rental={returning} onClose={() => setReturning(null)} />}
    </>
  );
}

function RentalModal({ vehicles, onClose }: { vehicles: VehicleView[]; onClose: () => void }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const now = new Date();
  const [vehicleId, setVehicleId] = useState(vehicles[0]?.id ?? '');
  const [renterName, setRenterName] = useState('');
  const [renterPhone, setRenterPhone] = useState('');
  const [renterDocument, setRenterDocument] = useState('');
  const [startsAt, setStartsAt] = useState(localInput(now));
  const [endsAt, setEndsAt] = useState(localInput(new Date(now.getTime() + 24 * 3_600_000)));
  const [kmStart, setKmStart] = useState('');
  const [error, setError] = useState('');
  useEffect(() => setError(''), [vehicleId]);
  const save = useMutation({
    mutationFn: () => {
      const km = parseDecimal(kmStart);
      if (Number.isNaN(km)) throw new Error('O km precisa ser um número.');
      return fleetOpsApi.createRental({
        vehicleId,
        renterName,
        renterPhone,
        renterDocument,
        startsAt: new Date(startsAt).toISOString(),
        endsAt: new Date(endsAt).toISOString(),
        kmStart: km,
        notes: '',
      });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [...fleetKey, 'rentals'] });
      notify({ tone: 'success', title: 'Locação registrada' });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });
  return (
    <Modal
      open
      title="Nova locação"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Registrar
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        {error && <div className={pageStyles.note}>{error}</div>}
        <VehicleSelect vehicles={vehicles} value={vehicleId} onChange={setVehicleId} />
        <TextField label="Locatário" maxLength={120} value={renterName} onChange={(e) => setRenterName(e.target.value)} />
        <div className={pageStyles.formRow}>
          <TextField label="Telefone" maxLength={30} value={renterPhone} onChange={(e) => setRenterPhone(e.target.value)} />
          <TextField label="CPF ou CNPJ" maxLength={30} value={renterDocument} onChange={(e) => setRenterDocument(e.target.value)} />
        </div>
        <div className={pageStyles.formRow}>
          <TextField label="Retirada" type="datetime-local" value={startsAt} onChange={(e) => setStartsAt(e.target.value)} />
          <TextField label="Devolução" type="datetime-local" value={endsAt} onChange={(e) => setEndsAt(e.target.value)} />
        </div>
        <TextField
          label="Km na retirada (opcional)"
          inputMode="decimal"
          value={kmStart}
          placeholder="o hodômetro de agora"
          onChange={(e) => setKmStart(e.target.value)}
        />
      </div>
    </Modal>
  );
}

function ReturnModal({ rental, onClose }: { rental: Rental; onClose: () => void }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [km, setKm] = useState('');
  const [error, setError] = useState('');
  const save = useMutation({
    mutationFn: () => {
      const value = parseDecimal(km);
      if (Number.isNaN(value)) throw new Error('O km precisa ser um número.');
      return fleetOpsApi.returnRental(rental.id, value);
    },
    onSuccess: (r) => {
      void queryClient.invalidateQueries({ queryKey: [...fleetKey, 'rentals'] });
      const driven = rentalKm(r);
      notify({ tone: 'success', title: 'Devolução registrada', description: driven !== null ? `${driven.toLocaleString('pt-BR')} km rodados.` : undefined });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });
  return (
    <Modal
      open
      title={`Devolução: ${rental.vehicleName}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Registrar devolução
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        {error && <div className={pageStyles.note}>{error}</div>}
        <p className={pageStyles.description}>
          {rental.renterName} devolveu agora. Sem o km, vale o hodômetro do sistema.
        </p>
        <TextField label="Km na devolução (opcional)" inputMode="decimal" value={km} onChange={(e) => setKm(e.target.value)} />
      </div>
    </Modal>
  );
}

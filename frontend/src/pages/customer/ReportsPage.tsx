import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';

import { customersApi, orgApi, reportsApi } from '@/api/resources';
import type { ReportKind, ReportQuery } from '@/api/resources';
import { ReportEmailModal } from '@/components/reports/ReportEmailModal';
import { BackButton } from '@/components/ui/BackButton';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import fieldStyles from '@/components/ui/Field.module.css';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { useFleet } from '@/hooks/useFleet';
import { useAuth } from '@/stores/AuthContext';
import { formatDateOnly, formatSpeed, todayISO } from '@/services/format';
import {
  PRESETS,
  dayTotals,
  daysOf,
  formatClock,
  formatKm,
  formatSpan,
  formatWhen,
  mapLink,
  periodProblem,
  presetPeriod,
  timeline,
} from '@/services/reports';
import type { PeriodPreset } from '@/services/reports';
import { JOURNEY_VIOLATIONS } from '@/services/fleetOps';
import type { ReportDrivingRow, ReportSummaryRow, ReportUsageRow } from '@/types';

import pageStyles from '../Page.module.css';
import styles from './ReportsPage.module.css';

type Tab = 'summary' | 'usage' | 'driving' | 'journey' | 'daily' | 'trips' | 'overspeed';

const TABS: { id: Tab; label: string }[] = [
  { id: 'summary', label: 'Resumo' },
  { id: 'usage', label: 'Utilização' },
  { id: 'driving', label: 'Direção' },
  { id: 'journey', label: 'Jornada' },
  { id: 'daily', label: 'Km por dia' },
  { id: 'trips', label: 'Viagens' },
  { id: 'overspeed', label: 'Excessos de velocidade' },
];

/** Linhas por vez nas tabelas (numa frota, centenas de veículos). */
const ROWS_PAGE = 50;
/** Com mais veículos que isso, as tabelas ganham a busca. */
const SEARCH_FROM = 12;

/**
 * Os relatórios da frota: no período escolhido (até 31 dias), o resumo de
 * cada veículo, os km de cada dia, as viagens de um veículo e os excessos de
 * velocidade — na tela e na planilha (CSV, que o Excel abre direto). No app,
 * aberta pela Conta, ganha o voltar (backTo).
 */
export function ReportsPage({
  customerId,
  customerName,
  backTo,
}: { customerId?: string; customerName?: string; backTo?: string } = {}) {
  const today = todayISO();
  const [preset, setPreset] = useState<PeriodPreset | null>('week');
  const [period, setPeriod] = useState(() => presetPeriod('week', today));
  const [draft, setDraft] = useState(period);
  const [tab, setTab] = useState<Tab>('summary');
  const [vehicleId, setVehicleId] = useState('');
  const [downloading, setDownloading] = useState(false);
  // O relatório por e-mail é de quem está logado (não da central vendo um cliente).
  const [emailing, setEmailing] = useState(false);
  const { membership, isOrgMember } = useAuth();
  // O grupo escolhido vale para as quatro abas e para a planilha.
  const { groups } = useFleet(customerId);
  const [group, setGroup] = useState('');
  // A associação: os veículos de todos os associados, ou de um só.
  const associates = useQuery({ queryKey: ['org', 'associates'], queryFn: orgApi.associates, enabled: isOrgMember });
  const [associate, setAssociate] = useState('');
  const scope: ReportQuery = useMemo(
    () => ({ ...period, groupId: group || undefined, customerId, associateId: associate || undefined }),
    [period, group, customerId, associate],
  );
  const { notify } = useToast();
  const problem = periodProblem(draft.from, draft.to);

  const choose = (p: PeriodPreset) => {
    const next = presetPeriod(p, today);
    setPreset(p);
    setPeriod(next);
    setDraft(next);
  };
  const applyDates = () => {
    if (problem) return;
    setPreset(null);
    setPeriod(draft);
  };

  // O resumo traz os veículos da conta (para escolher o das viagens).
  const summary = useQuery({ queryKey: ['reports', 'summary', scope], queryFn: () => reportsApi.summary(scope) });
  const vehicles = summary.data?.rows ?? [];
  const query: ReportQuery = { ...scope, vehicleId: tab === 'trips' ? vehicleId : undefined };

  const download = async () => {
    setDownloading(true);
    try {
      await reportsApi.download(tab as ReportKind, query);
    } catch (err) {
      notify({ tone: 'error', title: 'Não deu para baixar', description: err instanceof Error ? err.message : '' });
    } finally {
      setDownloading(false);
    }
  };

  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            {customerId && (
              <Link to={`/clientes/${customerId}`} className={styles.back}>
                ← {customerName || 'Cliente'}
              </Link>
            )}
            <div className={pageStyles.titleRow}>
              {backTo && <BackButton to={backTo} />}
              <h1 className={pageStyles.title}>{customerId ? `Relatórios de ${customerName || 'cliente'}` : 'Relatórios'}</h1>
            </div>
            <p className={pageStyles.description}>
              {isOrgMember ? 'Os números dos veículos dos associados' : 'Os números da frota'} no período: km, tempo
              andando, parado com a ignição ligada, dias sem rodar, viagens e excessos de velocidade. Atualizados a cada 5
              minutos.
            </p>
          </div>
          <div className={pageStyles.actions}>
            {!customerId && !isOrgMember && (
              <Button variant="secondary" onClick={() => setEmailing(true)}>
                Receber por e-mail
              </Button>
            )}
            <Button
              variant="secondary"
              loading={downloading}
              disabled={(tab === 'trips' && !vehicleId) || tab === 'journey'}
              onClick={() => void download()}
            >
              Baixar planilha
            </Button>
          </div>
        </header>

        <div className={styles.period}>
          <div className={styles.presets} role="group" aria-label="Período">
            {PRESETS.map((p) => (
              <button
                key={p.id}
                type="button"
                className={`${styles.preset} ${preset === p.id ? styles.presetOn : ''}`}
                aria-pressed={preset === p.id}
                onClick={() => choose(p.id)}
              >
                {p.label}
              </button>
            ))}
          </div>
          <div className={styles.dates}>
            <label className={styles.date}>
              De
              <input
                className={fieldStyles.input}
                type="date"
                value={draft.from}
                max={today}
                onChange={(e) => setDraft({ ...draft, from: e.target.value })}
              />
            </label>
            <label className={styles.date}>
              Até
              <input
                className={fieldStyles.input}
                type="date"
                value={draft.to}
                max={today}
                onChange={(e) => setDraft({ ...draft, to: e.target.value })}
              />
            </label>
            <Button
              variant="ghost"
              disabled={Boolean(problem) || (draft.from === period.from && draft.to === period.to)}
              onClick={applyDates}
            >
              Ver
            </Button>
          </div>
          {groups.length > 0 && (
            <label className={styles.date}>
              Grupo
              <select
                className={fieldStyles.select}
                value={group}
                onChange={(e) => {
                  setGroup(e.target.value);
                  setVehicleId('');
                }}
              >
                <option value="">Toda a frota</option>
                {groups.map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          {isOrgMember && (associates.data?.length ?? 0) > 0 && (
            <label className={styles.date}>
              Associado
              <select
                className={fieldStyles.select}
                value={associate}
                onChange={(e) => {
                  setAssociate(e.target.value);
                  setVehicleId('');
                }}
              >
                <option value="">Todos os associados</option>
                {associates.data!.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          {problem && <p className={styles.problem}>{problem}</p>}
        </div>

        <div className={pageStyles.tabs} role="tablist">
          {TABS.filter((t) => !(t.id === 'journey' && isOrgMember)).map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={tab === t.id}
              className={`${pageStyles.tab} ${tab === t.id ? pageStyles.tabActive : ''}`}
              onClick={() => setTab(t.id)}
            >
              {t.label}
            </button>
          ))}
        </div>

        {summary.isLoading ? (
          <Spinner label="Calculando" />
        ) : summary.error ? (
          <p className={styles.problem}>{(summary.error as Error).message}</p>
        ) : vehicles.length === 0 ? (
          <Card>
            <EmptyState icon="📊" title="Nenhum veículo" description="Os relatórios aparecem quando a conta tiver veículos com rastreador." />
          </Card>
        ) : tab === 'summary' ? (
          <SummaryTab rows={vehicles} />
        ) : tab === 'usage' ? (
          <UsageTab period={scope} />
        ) : tab === 'driving' ? (
          <DrivingTab period={scope} />
        ) : tab === 'journey' ? (
          <JourneyTab period={scope} vehicles={vehicles} />
        ) : tab === 'daily' ? (
          <DailyTab period={scope} />
        ) : tab === 'trips' ? (
          <TripsTab period={scope} vehicles={vehicles} vehicleId={vehicleId} onVehicle={setVehicleId} />
        ) : (
          <OverspeedTab period={scope} />
        )}
      </div>
      {!customerId && !isOrgMember && (
        <ReportEmailModal
          open={emailing}
          onClose={() => setEmailing(false)}
          groups={groups}
          restricted={Boolean(membership?.restricted)}
        />
      )}
    </div>
  );
}

/** A busca pelo nome ou placa e quantas linhas aparecem (de 50 em 50). */
function useRows<T>(rows: T[], text: (row: T) => string) {
  const [search, setSearch] = useState('');
  const [limit, setLimit] = useState(ROWS_PAGE);
  const found = useMemo(() => {
    const term = search.trim().toLowerCase();
    return term ? rows.filter((r) => text(r).toLowerCase().includes(term)) : rows;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, search]);
  const searchBox = (
    <input
      className={`${fieldStyles.input} ${styles.search}`}
      type="search"
      placeholder="Buscar por nome ou placa"
      aria-label="Buscar veículo"
      value={search}
      onChange={(e) => {
        setSearch(e.target.value);
        setLimit(ROWS_PAGE);
      }}
    />
  );
  const left = found.length - limit;
  const more =
    left > 0 ? (
      <div className={styles.more}>
        <Button variant="ghost" onClick={() => setLimit((n) => n + ROWS_PAGE)}>
          {left > ROWS_PAGE ? `Mostrar mais ${ROWS_PAGE} (faltam ${left})` : `Mostrar as ${left} restantes`}
        </Button>
      </div>
    ) : null;
  return { visible: found.slice(0, limit), found, searchBox, more };
}

function SummaryTab({ rows }: { rows: ReportSummaryRow[] }) {
  const sorted = useMemo(() => [...rows].sort((a, b) => b.distanceKm - a.distanceKm || a.name.localeCompare(b.name)), [rows]);
  const { visible, found, searchBox, more } = useRows(
    sorted,
    (r) => `${r.name} ${r.plate} ${r.custom1 ?? ''} ${r.custom2 ?? ''} ${r.ownerName ?? ''}`,
  );
  const total = rows.reduce(
    (t, r) => ({
      km: t.km + r.distanceKm,
      moving: t.moving + r.movingSeconds,
      idle: t.idle + r.idleSeconds,
      trips: t.trips + r.trips,
      overspeeds: t.overspeeds + r.overspeeds,
    }),
    { km: 0, moving: 0, idle: 0, trips: 0, overspeeds: 0 },
  );
  return (
    <>
      <div className={styles.tiles}>
        <Tile label="Km rodados" value={formatKm(total.km)} />
        <Tile label="Tempo andando" value={formatSpan(total.moving)} />
        <Tile label="Parado ligado" value={formatSpan(total.idle)} hint="ignição ligada, sem andar" />
        <Tile label="Viagens" value={String(total.trips)} />
        <Tile label="Excessos de velocidade" value={String(total.overspeeds)} danger={total.overspeeds > 0} />
      </div>
      <Card flush>
        {rows.length > SEARCH_FROM && <div className={styles.toolbar}>{searchBox}</div>}
        <div className={pageStyles.tableWrap}>
          <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
            <thead>
              <tr>
                <th>Veículo</th>
                <th>Km</th>
                <th>Andando</th>
                <th>Parado ligado</th>
                <th>Viagens</th>
                <th>Vel. máxima</th>
                <th>Excessos</th>
                <th>Primeira saída</th>
                <th>Última chegada</th>
              </tr>
            </thead>
            <tbody>
              {visible.map((r) => (
                <tr key={r.vehicleId}>
                  <td>
                    <VehicleName name={r.name} plate={r.plate} owner={r.ownerName} />
                  </td>
                  <td data-label="Km" className={styles.number}>
                    {formatKm(r.distanceKm)}
                  </td>
                  <td data-label="Andando">{formatSpan(r.movingSeconds)}</td>
                  <td data-label="Parado ligado">{formatSpan(r.idleSeconds)}</td>
                  <td data-label="Viagens">{r.trips}</td>
                  <td data-label="Vel. máxima">{r.maxSpeedKmh > 0 ? formatSpeed(r.maxSpeedKmh) : '—'}</td>
                  <td data-label="Excessos" className={r.overspeeds > 0 ? styles.danger : undefined}>
                    {r.overspeeds}
                  </td>
                  <td data-label="Primeira saída">{r.firstMoveAt ? formatWhen(r.firstMoveAt) : '—'}</td>
                  <td data-label="Última chegada">{r.lastMoveAt ? formatWhen(r.lastMoveAt) : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {found.length === 0 && <p className={styles.empty}>Nenhum veículo com essa busca.</p>}
        {more}
      </Card>
    </>
  );
}

function DailyTab({ period }: { period: ReportQuery }) {
  const q = useQuery({ queryKey: ['reports', 'daily', period], queryFn: () => reportsApi.daily(period) });
  const data = q.data;
  const days = daysOf(period.from, period.to);
  const totals = useMemo(() => (data ? dayTotals(data.rows, period.from, period.to) : []), [data, period]);
  const byVehicle = useMemo(() => {
    const m = new Map<string, Map<string, number>>();
    for (const r of data?.rows ?? []) {
      const v = m.get(r.vehicleId) ?? new Map<string, number>();
      v.set(r.day, r.distanceKm);
      m.set(r.vehicleId, v);
    }
    return m;
  }, [data]);
  const ranked = useMemo(() => {
    const sum = (id: string) => [...(byVehicle.get(id)?.values() ?? [])].reduce((a, b) => a + b, 0);
    return [...(data?.vehicles ?? [])].map((v) => ({ ...v, km: sum(v.id) })).sort((a, b) => b.km - a.km || a.name.localeCompare(b.name));
  }, [data, byVehicle]);
  const { visible, searchBox, more } = useRows(ranked, (v) => `${v.name} ${v.plate} ${v.ownerName ?? ''}`);
  if (q.isLoading) return <Spinner label="Calculando" />;
  if (!data) return null;
  const max = Math.max(1, ...totals.map((t) => t.km));

  return (
    <>
      <Card title="A frota em cada dia">
        <ul className={styles.bars}>
          {totals.map((t) => (
            <li key={t.day}>
              <span className={styles.barDay}>{formatDateOnly(t.day).slice(0, 5)}</span>
              <span className={styles.barTrack}>
                <span className={styles.bar} style={{ width: `${(t.km / max) * 100}%` }} />
              </span>
              <span className={styles.barValue}>
                {formatKm(t.km)} · {t.trips} {t.trips === 1 ? 'viagem' : 'viagens'}
              </span>
            </li>
          ))}
        </ul>
      </Card>
      <Card title="Km de cada veículo" flush>
        {ranked.length > SEARCH_FROM && <div className={styles.toolbar}>{searchBox}</div>}
        <div className={`${pageStyles.tableWrap} ${styles.wide}`}>
          <table className={pageStyles.table}>
            <thead>
              <tr>
                <th className={styles.sticky}>Veículo</th>
                <th>Total</th>
                {days.map((d) => (
                  <th key={d}>{formatDateOnly(d).slice(0, 5)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {visible.map((v) => (
                <tr key={v.id}>
                  <td className={styles.sticky}>
                    <VehicleName name={v.name} plate={v.plate} owner={v.ownerName} />
                  </td>
                  <td className={styles.number}>
                    <strong>{formatKm(v.km)}</strong>
                  </td>
                  {days.map((d) => {
                    const km = byVehicle.get(v.id)?.get(d);
                    return (
                      <td key={d} className={styles.number}>
                        {km ? km.toLocaleString('pt-BR', { maximumFractionDigits: 1 }) : '·'}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {more}
      </Card>
    </>
  );
}

function TripsTab({
  period,
  vehicles,
  vehicleId,
  onVehicle,
}: {
  period: ReportQuery;
  vehicles: ReportSummaryRow[];
  vehicleId: string;
  onVehicle: (id: string) => void;
}) {
  const [search, setSearch] = useState('');
  const options = useMemo(() => {
    const term = search.trim().toLowerCase();
    const list = [...vehicles].sort((a, b) => a.name.localeCompare(b.name));
    return term ? list.filter((v) => `${v.name} ${v.plate}`.toLowerCase().includes(term)) : list;
  }, [vehicles, search]);
  const q = useQuery({
    queryKey: ['reports', 'trips', period, vehicleId],
    queryFn: () => reportsApi.trips({ ...period, vehicleId }),
    enabled: Boolean(vehicleId),
  });
  const items = useMemo(() => timeline(q.data?.trips ?? []), [q.data]);
  const availableFrom = q.data?.availableFrom ? q.data.availableFrom.slice(0, 10) : null;

  return (
    <Card flush>
      <div className={styles.toolbar}>
        {vehicles.length > SEARCH_FROM && (
          <input
            className={`${fieldStyles.input} ${styles.search}`}
            type="search"
            placeholder="Buscar por nome ou placa"
            aria-label="Buscar veículo"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        )}
        <select
          className={`${fieldStyles.select} ${styles.vehicleSelect}`}
          aria-label="Veículo"
          value={vehicleId}
          onChange={(e) => onVehicle(e.target.value)}
        >
          <option value="">Escolha o veículo</option>
          {options.map((v) => (
            <option key={v.vehicleId} value={v.vehicleId}>
              {v.name}
              {v.plate ? ` · ${v.plate}` : ''}
              {v.ownerName ? ` — ${v.ownerName}` : ''} ({v.trips} {v.trips === 1 ? 'viagem' : 'viagens'})
            </option>
          ))}
        </select>
      </div>
      {!vehicleId ? (
        <p className={styles.empty}>Escolha um veículo para ver as viagens e as paradas no período.</p>
      ) : q.isLoading ? (
        <Spinner label="Calculando as viagens" />
      ) : (
        <>
          {availableFrom && availableFrom > period.from && (
            <p className={styles.note}>
              As viagens vêm do histórico do veículo, que guarda as posições a partir de {formatDateOnly(availableFrom)}.
            </p>
          )}
          {items.length === 0 ? (
            <p className={styles.empty}>Nenhuma viagem no período.</p>
          ) : (
            <div className={pageStyles.tableWrap}>
              <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
                <thead>
                  <tr>
                    <th>Saída</th>
                    <th>Chegada</th>
                    <th>Duração</th>
                    <th>Km</th>
                    <th>Vel. máxima</th>
                    <th>No mapa</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) =>
                    item.kind === 'trip' ? (
                      <tr key={`t${item.trip.start}`}>
                        <td>{formatWhen(item.trip.start)}</td>
                        <td data-label="Chegada">{item.trip.ongoing ? 'andando' : formatClock(item.trip.end)}</td>
                        <td data-label="Duração">
                          {formatSpan((Date.parse(item.trip.end) - Date.parse(item.trip.start)) / 1000)}
                        </td>
                        <td data-label="Km" className={styles.number}>
                          {formatKm(item.trip.distanceMeters / 1000)}
                        </td>
                        <td data-label="Vel. máxima">{formatSpeed(item.trip.maxSpeedKmh)}</td>
                        <td data-label="No mapa">
                          <span>
                            <a href={mapLink(item.trip.startLat, item.trip.startLon)} target="_blank" rel="noreferrer noopener">
                              saída
                            </a>{' '}
                            ·{' '}
                            <a href={mapLink(item.trip.endLat, item.trip.endLon)} target="_blank" rel="noreferrer noopener">
                              chegada
                            </a>
                          </span>
                        </td>
                      </tr>
                    ) : (
                      <tr key={`p${item.start}`} className={styles.stopRow}>
                        <td colSpan={6}>
                          Parado {formatSpan(item.seconds)}, de {formatClock(item.start)} a {formatClock(item.end)} ·{' '}
                          <a href={mapLink(item.lat, item.lon)} target="_blank" rel="noreferrer noopener">
                            onde
                          </a>
                        </td>
                      </tr>
                    ),
                  )}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </Card>
  );
}

function OverspeedTab({ period }: { period: ReportQuery }) {
  const q = useQuery({ queryKey: ['reports', 'overspeed', period], queryFn: () => reportsApi.overspeed(period) });
  const names = useMemo(() => new Map((q.data?.vehicles ?? []).map((v) => [v.id, v])), [q.data]);
  const rows = useMemo(() => [...(q.data?.rows ?? [])].reverse(), [q.data]);
  const { visible, found, searchBox, more } = useRows(rows, (r) => {
    const v = names.get(r.vehicleId);
    return `${v?.name ?? ''} ${v?.plate ?? ''} ${v?.ownerName ?? ''}`;
  });
  if (q.isLoading) return <Spinner label="Calculando" />;
  if (!q.data) return null;
  return (
    <Card flush>
      {rows.length > SEARCH_FROM && <div className={styles.toolbar}>{searchBox}</div>}
      {q.data.truncated && <p className={styles.note}>São muitos excessos: a lista mostra os primeiros 5.000. A planilha também.</p>}
      {rows.length === 0 ? (
        <p className={styles.empty}>Nenhum excesso de velocidade no período.</p>
      ) : (
        <div className={pageStyles.tableWrap}>
          <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
            <thead>
              <tr>
                <th>Veículo</th>
                <th>Quando</th>
                <th>Duração</th>
                <th>Vel. máxima</th>
                <th>Limite</th>
                <th>Onde</th>
              </tr>
            </thead>
            <tbody>
              {visible.map((r) => {
                const v = names.get(r.vehicleId);
                const seconds = r.end ? (Date.parse(r.end) - Date.parse(r.start)) / 1000 : 0;
                return (
                  <tr key={`${r.vehicleId}${r.start}`}>
                    <td>
                      <VehicleName name={v?.name ?? '—'} plate={v?.plate ?? ''} owner={v?.ownerName} />
                    </td>
                    <td data-label="Quando">{formatWhen(r.start)}</td>
                    <td data-label="Duração">{seconds >= 60 ? formatSpan(seconds) : seconds > 0 ? `${Math.round(seconds)} s` : '—'}</td>
                    <td data-label="Vel. máxima" className={styles.danger}>
                      {formatSpeed(r.maxSpeedKmh)}
                    </td>
                    <td data-label="Limite">{r.limitKmh ? formatSpeed(r.limitKmh) : '—'}</td>
                    <td data-label="Onde">
                      {r.latitude !== null && r.longitude !== null ? (
                        <a href={mapLink(r.latitude, r.longitude)} target="_blank" rel="noreferrer noopener">
                          ver no mapa
                        </a>
                      ) : (
                        '—'
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {rows.length > 0 && found.length === 0 && <p className={styles.empty}>Nenhum veículo com essa busca.</p>}
      {more}
    </Card>
  );
}

/** O total da frota na utilização: a média de uso, os parados e a ociosidade. */
export function usageTotals(rows: ReportUsageRow[]) {
  const counted = rows.filter((r) => r.periodDays > 0);
  const moving = rows.reduce((t, r) => t + r.movingSeconds, 0);
  const idle = rows.reduce((t, r) => t + r.idleSeconds, 0);
  const days = counted.reduce((t, r) => t + r.periodDays, 0);
  const active = counted.reduce((t, r) => t + Math.min(r.activeDays, r.periodDays), 0);
  return {
    usePercent: days > 0 ? (100 * active) / days : 0,
    neverMoved: counted.filter((r) => r.activeDays === 0).length,
    idlePercent: moving + idle > 0 ? (100 * idle) / (moving + idle) : 0,
    idleSeconds: idle,
  };
}

/** 21.7 → "21,7%"; 50 → "50%". */
const percent = (v: number) => `${v.toLocaleString('pt-BR', { maximumFractionDigits: 1 })}%`;

/**
 * A utilização: quanto cada veículo rodou dos dias do período e quanto do
 * tempo ligado ficou parado (motor ligado sem sair do lugar). Os menos
 * usados primeiro — são os candidatos a remanejar ou vender.
 */
function UsageTab({ period }: { period: ReportQuery }) {
  const q = useQuery({ queryKey: ['reports', 'usage', period], queryFn: () => reportsApi.usage(period) });
  const sorted = useMemo(
    () =>
      [...(q.data?.rows ?? [])].sort(
        (a, b) => a.usePercent - b.usePercent || b.stoppedDays - a.stoppedDays || a.name.localeCompare(b.name),
      ),
    [q.data],
  );
  const { visible, found, searchBox, more } = useRows(sorted, (r) => `${r.name} ${r.plate} ${r.ownerName ?? ''}`);
  if (q.isLoading) return <Spinner label="Calculando" />;
  if (q.error) return <p className={styles.problem}>{(q.error as Error).message}</p>;
  const total = usageTotals(sorted);
  return (
    <>
      <div className={styles.tiles}>
        <Tile label="Uso da frota" value={percent(Math.round(total.usePercent * 10) / 10)} hint="dias com movimento" />
        <Tile
          label="Sem rodar no período"
          value={String(total.neverMoved)}
          hint={total.neverMoved === 1 ? 'veículo' : 'veículos'}
          danger={total.neverMoved > 0}
        />
        <Tile label="Ociosidade" value={percent(Math.round(total.idlePercent * 10) / 10)} hint="do tempo ligado, parado" />
        <Tile label="Parado ligado" value={formatSpan(total.idleSeconds)} hint="motor ligado sem sair do lugar" />
      </div>
      <Card flush>
        {sorted.length > SEARCH_FROM && <div className={styles.toolbar}>{searchBox}</div>}
        <div className={pageStyles.tableWrap}>
          <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
            <thead>
              <tr>
                <th>Veículo</th>
                <th>Uso</th>
                <th>Dias parado</th>
                <th>Km por dia rodado</th>
                <th>Ociosidade</th>
                <th>Parado ligado</th>
                <th>Última chegada</th>
              </tr>
            </thead>
            <tbody>
              {visible.map((r) => (
                <tr key={r.vehicleId}>
                  <td>
                    <VehicleName name={r.name} plate={r.plate} owner={r.ownerName} />
                  </td>
                  <td data-label="Uso">
                    <span className={styles.usage}>
                      <span className={styles.barTrack} aria-hidden="true">
                        <span className={styles.bar} style={{ width: `${Math.min(100, r.usePercent)}%` }} />
                      </span>
                      <span className={styles.number}>
                        {percent(r.usePercent)} · {r.activeDays} de {r.periodDays} {r.periodDays === 1 ? 'dia' : 'dias'}
                      </span>
                    </span>
                  </td>
                  <td data-label="Dias parado" className={r.periodDays > 0 && r.activeDays === 0 ? styles.danger : undefined}>
                    {r.stoppedDays}
                  </td>
                  <td data-label="Km por dia rodado" className={styles.number}>
                    {r.activeDays > 0 ? formatKm(r.kmPerActiveDay) : '—'}
                  </td>
                  <td data-label="Ociosidade" className={r.idlePercent >= 30 ? styles.danger : styles.number}>
                    {r.movingSeconds + r.idleSeconds > 0 ? percent(r.idlePercent) : '—'}
                  </td>
                  <td data-label="Parado ligado">{formatSpan(r.idleSeconds)}</td>
                  <td data-label="Última chegada">{r.lastMoveAt ? formatWhen(r.lastMoveAt) : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {found.length === 0 && <p className={styles.empty}>Nenhum veículo com essa busca.</p>}
        {more}
        <p className={styles.note}>
          Ociosidade é o tempo com a ignição ligada e o veículo parado, sobre todo o tempo ligado: acima de 30% costuma ser
          motor ligado esperando (combustível gasto sem rodar).
        </p>
      </Card>
    </>
  );
}

/** A nota com o rótulo: 85+ ótima, 70+ boa, 50+ regular, abaixo disso atenção. */
export function scoreLabel(score: number | null): { label: string; tone: 'good' | 'ok' | 'warn' | 'bad' | 'none' } {
  if (score === null) return { label: 'poucos km', tone: 'none' };
  if (score >= 85) return { label: 'ótima', tone: 'good' };
  if (score >= 70) return { label: 'boa', tone: 'ok' };
  if (score >= 50) return { label: 'regular', tone: 'warn' };
  return { label: 'atenção', tone: 'bad' };
}

/** A nota da frota: a média das notas pesada pelos km de cada veículo. */
export function fleetScore(rows: Pick<ReportDrivingRow, 'score' | 'distanceKm'>[]): number | null {
  const scored = rows.filter((r) => r.score !== null);
  const km = scored.reduce((t, r) => t + r.distanceKm, 0);
  if (km <= 0) return null;
  return Math.round(scored.reduce((t, r) => t + (r.score ?? 0) * r.distanceKm, 0) / km);
}

/**
 * A direção: a nota de cada veículo (de 0 a 100) e o que pesou nela —
 * excessos de velocidade, tempo acima de 120 km/h, freadas e arrancadas
 * bruscas e quanto rodou de noite. As piores primeiro.
 */
function DrivingTab({ period }: { period: ReportQuery }) {
  const q = useQuery({ queryKey: ['reports', 'driving', period], queryFn: () => reportsApi.driving(period) });
  const sorted = useMemo(
    () =>
      [...(q.data?.rows ?? [])].sort(
        (a, b) => (a.score ?? 101) - (b.score ?? 101) || b.distanceKm - a.distanceKm || a.name.localeCompare(b.name),
      ),
    [q.data],
  );
  const { visible, found, searchBox, more } = useRows(sorted, (r) => `${r.name} ${r.plate} ${r.ownerName ?? ''}`);
  if (q.isLoading) return <Spinner label="Calculando" />;
  if (q.error) return <p className={styles.problem}>{(q.error as Error).message}</p>;
  const score = fleetScore(sorted);
  const km = sorted.reduce((t, r) => t + r.distanceKm, 0);
  const nightKm = sorted.reduce((t, r) => t + r.nightKm, 0);
  const overspeeds = sorted.reduce((t, r) => t + r.overspeeds, 0);
  const harsh = sorted.reduce((t, r) => t + r.harshBrakes + r.harshAccels, 0);
  return (
    <>
      <div className={styles.tiles}>
        <Tile label="Nota da frota" value={score === null ? '—' : String(score)} hint={scoreLabel(score).label} danger={score !== null && score < 50} />
        <Tile label="Excessos de velocidade" value={String(overspeeds)} danger={overspeeds > 0} />
        <Tile label="Freadas e arrancadas bruscas" value={String(harsh)} />
        <Tile label="À noite" value={km > 0 ? percent(Math.round((1000 * nightKm) / km) / 10) : '—'} hint="dos km, das 22h às 6h" />
      </div>
      <Card flush>
        {sorted.length > SEARCH_FROM && <div className={styles.toolbar}>{searchBox}</div>}
        <div className={pageStyles.tableWrap}>
          <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
            <thead>
              <tr>
                <th>Veículo</th>
                <th>Nota</th>
                <th>Km</th>
                <th>Excessos</th>
                <th>Acima de 120 km/h</th>
                <th>Freadas / arrancadas</th>
                <th>À noite</th>
              </tr>
            </thead>
            <tbody>
              {visible.map((r) => {
                const s = scoreLabel(r.score);
                return (
                  <tr key={r.vehicleId}>
                    <td>
                      <VehicleName name={r.name} plate={r.plate} owner={r.ownerName} />
                    </td>
                    <td data-label="Nota">
                      <span className={`${styles.score} ${styles[`score_${s.tone}`]}`}>
                        {r.score === null ? '—' : r.score}
                        <small>{s.label}</small>
                      </span>
                    </td>
                    <td data-label="Km" className={styles.number}>
                      {formatKm(r.distanceKm)}
                    </td>
                    <td data-label="Excessos" className={r.overspeeds > 0 ? styles.danger : undefined}>
                      {r.overspeeds}
                    </td>
                    <td data-label="Acima de 120 km/h">{r.highSpeedSeconds > 0 ? formatSpan(r.highSpeedSeconds) : '—'}</td>
                    <td data-label="Freadas / arrancadas">
                      {r.harshBrakes} / {r.harshAccels}
                    </td>
                    <td data-label="À noite">{r.distanceKm > 0 ? percent(r.nightPercent) : '—'}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        {found.length === 0 && <p className={styles.empty}>Nenhum veículo com essa busca.</p>}
        {more}
        <p className={styles.note}>
          A nota começa em 100 e perde pontos a cada 100 km: 4 por excesso do limite de velocidade do veículo (até 35) e 3
          por freada ou arrancada brusca (até 25); 1 por cada 1% do tempo andando acima de 120 km/h (até 20) e 1 por cada
          10% dos km rodados de noite (até 10). Com menos de {q.data?.minScoreKm ?? 20} km no período, não há nota. As
          bruscas só aparecem quando o rastreador manda a posição a cada poucos segundos.
        </p>
      </Card>
    </>
  );
}

/**
 * A jornada do motorista (Lei 13.103): cada jornada de cada veículo, com o
 * motorista, o tempo ao volante, o maior trecho sem descanso de 30 min e o
 * descanso antes; o que passou do limite em destaque.
 */
function JourneyTab({ period, vehicles }: { period: ReportQuery; vehicles: ReportSummaryRow[] }) {
  const [kind, setKind] = useState<'carga' | 'passageiros'>('carga');
  const q = useQuery({ queryKey: ['reports', 'journey', period, kind], queryFn: () => reportsApi.journey(period, kind) });
  const names = new Map(vehicles.map((v) => [v.vehicleId, v]));
  const rows = useMemo(
    () => [...(q.data?.rows ?? [])].sort((a, b) => b.violations.length - a.violations.length || b.start.localeCompare(a.start)),
    [q.data],
  );
  const flagged = rows.filter((r) => r.violations.length > 0).length;
  return (
    <>
      <div className={styles.period}>
        <div className={styles.presets} role="group" aria-label="Tipo de transporte">
          {(
            [
              ['carga', 'Cargas (5h30 contínuas)'],
              ['passageiros', 'Passageiros (4 h contínuas)'],
            ] as const
          ).map(([id, label]) => (
            <button
              key={id}
              type="button"
              className={`${styles.preset} ${kind === id ? styles.presetOn : ''}`}
              aria-pressed={kind === id}
              onClick={() => setKind(id)}
            >
              {label}
            </button>
          ))}
        </div>
      </div>
      {q.isLoading ? (
        <Spinner label="Calculando" />
      ) : q.error ? (
        <p className={styles.problem}>{(q.error as Error).message}</p>
      ) : (
        <Card flush>
          {rows.length === 0 ? (
            <p className={styles.empty}>Nenhuma jornada no período (o rastreador não registrou viagens).</p>
          ) : (
            <div className={pageStyles.tableWrap}>
              <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
                <thead>
                  <tr>
                    <th>Jornada</th>
                    <th>Motorista</th>
                    <th>Ao volante</th>
                    <th>Maior trecho contínuo</th>
                    <th>Descanso antes</th>
                    <th>Km</th>
                    <th>Alertas</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((r) => {
                    const v = names.get(r.vehicleId);
                    return (
                      <tr key={`${r.vehicleId}-${r.start}`}>
                        <td>
                          <VehicleName name={v?.name ?? 'Veículo'} plate={v?.plate ?? ''} />
                          <span className={styles.owner}>
                            {formatWhen(r.start)} – {formatClock(r.end)}
                          </span>
                        </td>
                        <td data-label="Motorista">{r.driverName || '—'}</td>
                        <td data-label="Ao volante" className={r.violations.includes('daily') ? styles.danger : undefined}>
                          {formatSpan(r.drivingSeconds)}
                        </td>
                        <td data-label="Maior trecho contínuo" className={r.violations.includes('continuous') ? styles.danger : undefined}>
                          {formatSpan(r.longestSeconds)}
                        </td>
                        <td data-label="Descanso antes" className={r.violations.includes('interjourney') ? styles.danger : undefined}>
                          {r.restBeforeSeconds !== null ? formatSpan(r.restBeforeSeconds) : '—'}
                        </td>
                        <td data-label="Km" className={styles.number}>
                          {formatKm(r.distanceKm)}
                        </td>
                        <td data-label="Alertas">
                          {r.violations.length === 0 ? '—' : r.violations.map((x) => JOURNEY_VIOLATIONS[x]).join('; ')}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
          <p className={styles.note}>
            {flagged > 0 ? `${flagged} ${flagged === 1 ? 'jornada passou' : 'jornadas passaram'} de algum limite. ` : ''}
            As jornadas são separadas por um descanso de 8 h; uma parada de 30 min zera a direção contínua. O rastreador vê
            as viagens: confira com o diário de bordo (espera, carga e descarga não aparecem aqui).
          </p>
        </Card>
      )}
    </>
  );
}

function Tile({ label, value, hint, danger = false }: { label: string; value: string; hint?: string; danger?: boolean }) {
  return (
    <div className={`${styles.tile} ${danger ? styles.tileDanger : ''}`}>
      <span className={styles.tileLabel}>{label}</span>
      <span className={styles.tileValue}>{value}</span>
      {hint && <span className={styles.tileHint}>{hint}</span>}
    </div>
  );
}

/** O veículo na tabela: o nome, a placa e, na associação, o associado. */
function VehicleName({ name, plate, owner }: { name: string; plate: string; owner?: string }) {
  return (
    <span className={styles.vehicle}>
      <strong>{name}</strong>
      {plate && <span className={styles.plate}>{plate}</span>}
      {owner && <span className={styles.owner}>{owner}</span>}
    </span>
  );
}

/** Os relatórios de um cliente, para a central (/clientes/:id/relatorios). */
export function CustomerReportsPage() {
  const { id = '' } = useParams<{ id: string }>();
  const customer = useQuery({ queryKey: ['customer', id], queryFn: () => customersApi.get(id), enabled: Boolean(id) });
  return <ReportsPage customerId={id} customerName={customer.data?.name} />;
}

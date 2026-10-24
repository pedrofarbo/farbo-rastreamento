import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';

import { vehiclesApi } from '@/api/resources';
import { TrackerMap } from '@/components/map/TrackerMap';
import { Address } from '@/components/ui/Address';
import { Button } from '@/components/ui/Button';
import { EmptyState } from '@/components/ui/EmptyState';
import fieldStyles from '@/components/ui/Field.module.css';
import { Spinner } from '@/components/ui/Spinner';
import { useVehicle } from '@/hooks/useVehicles';
import { formatCoordinates, formatDateTime, formatEvent, formatSpeed } from '@/services/format';
import { useAuth } from '@/stores/AuthContext';
import type { Position } from '@/types';

import local from './ClaimReport.module.css';
import styles from './TheftReport.module.css';

const MINUTE = 60 * 1000;

/** As janelas em volta do momento, em minutos para cada lado. */
const WINDOWS = [15, 30, 60, 180];

const mapsLink = (lat: number, lon: number) =>
  `https://www.google.com/maps/search/?api=1&query=${lat.toFixed(6)},${lon.toFixed(6)}`;

/** "2026-10-23T14:05" (o campo datetime-local, no fuso do navegador) ↔ Date. */
export function toLocalInput(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** A posição mais perto do momento (antes ou depois). */
export function closestPosition<T extends Pick<Position, 'gpsTimestamp'>>(positions: T[], moment: Date): T | null {
  let best: T | null = null;
  let bestGap = Infinity;
  for (const p of positions) {
    const gap = Math.abs(new Date(p.gpsTimestamp).getTime() - moment.getTime());
    if (gap < bestGap) {
      best = p;
      bestGap = gap;
    }
  }
  return best;
}

/**
 * O gráfico da velocidade no período: os pontos da linha (SVG, de 0 a
 * width e de 0 a height) e a escala de cima (o múltiplo de 20 km/h acima da
 * máxima e do limite cadastrado, para o tracejado do limite caber). Sem
 * posições, vazio.
 */
export function speedChart(
  positions: Pick<Position, 'gpsTimestamp' | 'speedKmh'>[],
  from: Date,
  to: Date,
  width: number,
  height: number,
  limit: number | null = null,
): { points: string; top: number; max: number } {
  const span = Math.max(1, to.getTime() - from.getTime());
  const max = positions.reduce((m, p) => Math.max(m, p.speedKmh), 0);
  const top = Math.max(20, Math.ceil(Math.max(max, (limit ?? 0) + 1) / 20) * 20);
  const points = positions
    .map((p) => {
      const x = ((new Date(p.gpsTimestamp).getTime() - from.getTime()) / span) * width;
      const y = height - (p.speedKmh / top) * height;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
  return { points, top, max };
}

/**
 * O relatório de sinistro para o seguro (ou a perícia): o trajeto, a
 * velocidade e a ignição em volta do momento informado, a posição naquele
 * instante e os eventos do rastreador (impacto, excesso de velocidade) —
 * para imprimir ou salvar em PDF. Do dono do veículo e da equipe da
 * associação ou seguradora.
 */
export function ClaimReportPage() {
  const { id = '' } = useParams<{ id: string }>();
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const { user } = useAuth();
  const vehicle = useVehicle(id);
  const initial = params.get('quando');
  const [momentText, setMomentText] = useState(() =>
    toLocalInput(initial && !Number.isNaN(Date.parse(initial)) ? new Date(initial) : new Date(Date.now() - 30 * MINUTE)),
  );
  const [minutes, setMinutes] = useState(() => {
    const n = Number(params.get('janela'));
    return WINDOWS.includes(n) ? n : 30;
  });
  const moment = useMemo(() => new Date(momentText), [momentText]);
  const valid = !Number.isNaN(moment.getTime()) && moment.getTime() <= Date.now() + MINUTE;
  const range = useMemo(() => {
    if (!valid) return null;
    const end = Math.min(moment.getTime() + minutes * MINUTE, Date.now());
    return { from: new Date(moment.getTime() - minutes * MINUTE), to: new Date(end) };
  }, [moment, minutes, valid]);

  const allowed = Boolean(vehicle.data && !vehicle.data.shared);
  const history = useQuery({
    queryKey: ['claim-report', id, range?.from.toISOString(), range?.to.toISOString()],
    queryFn: () =>
      vehiclesApi.positions(id, { from: range!.from.toISOString(), to: range!.to.toISOString(), limit: 1000, simplify: false }),
    enabled: allowed && range !== null,
  });
  const events = useQuery({
    queryKey: ['claim-report-events', id, range?.from.toISOString(), range?.to.toISOString()],
    queryFn: () => vehiclesApi.events(id, { from: range!.from.toISOString(), to: range!.to.toISOString(), limit: 200 }),
    enabled: allowed && range !== null,
  });

  if (vehicle.isLoading) return <Spinner label="Carregando o veículo" />;
  const v = vehicle.data;
  if (!v || v.shared) {
    return <EmptyState title="Relatório indisponível" description="O relatório de sinistro é do dono do veículo." />;
  }
  const positions = history.data?.positions ?? [];
  const at = closestPosition(positions, moment);
  const details = [v.brand, v.model, v.year, v.color].filter(Boolean).join(' · ');
  const chartW = 640;
  const chartH = 160;
  const chart = range ? speedChart(positions, range.from, range.to, chartW, chartH, v.speedLimitKmh) : null;
  const markerX = range ? ((moment.getTime() - range.from.getTime()) / Math.max(1, range.to.getTime() - range.from.getTime())) * chartW : 0;
  const limitY = chart && v.speedLimitKmh ? chartH - (v.speedLimitKmh / chart.top) * chartH : null;

  const apply = (text: string, window: number) => {
    setMomentText(text);
    setMinutes(window);
    const when = new Date(text);
    if (!Number.isNaN(when.getTime())) setParams({ quando: when.toISOString(), janela: String(window) }, { replace: true });
  };

  return (
    <div className={styles.screen}>
      <div className={styles.toolbar}>
        <Button variant="ghost" size="small" onClick={() => navigate(-1)}>
          Voltar
        </Button>
        <label className={local.field}>
          Data e hora da ocorrência
          <input
            className={fieldStyles.input}
            type="datetime-local"
            value={momentText}
            max={toLocalInput(new Date())}
            onChange={(e) => apply(e.target.value, minutes)}
          />
        </label>
        <div className={styles.chips} role="group" aria-label="Janela em volta do momento">
          {WINDOWS.map((w) => (
            <button
              key={w}
              type="button"
              className={`${styles.chip} ${minutes === w ? styles.chipActive : ''}`}
              aria-pressed={minutes === w}
              onClick={() => apply(momentText, w)}
            >
              ± {w < 60 ? `${w} min` : `${w / 60} h`}
            </button>
          ))}
        </div>
        <Button variant="primary" size="small" disabled={!valid} onClick={() => window.print()}>
          Imprimir ou salvar PDF
        </Button>
      </div>

      {!valid || !range ? (
        <p className={styles.muted}>Informe a data e a hora da ocorrência (até agora).</p>
      ) : (
        <article className={styles.report}>
          <header className={styles.head}>
            <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={styles.logo} />
            <div>
              <h1 className={styles.title}>Relatório de sinistro</h1>
              <p className={styles.muted}>
                Ocorrência informada: {formatDateTime(moment.toISOString())}. Gerado em {formatDateTime(new Date().toISOString())} pela
                plataforma Farbo Rastreadores.
              </p>
            </div>
          </header>

          <section className={styles.block}>
            <h2 className={styles.heading}>Veículo</h2>
            <dl className={styles.grid}>
              <Item label="Identificação" value={v.name} />
              <Item label="Placa" value={v.plate || '—'} mono />
              <Item label="Marca, modelo, ano e cor" value={details || '—'} />
              <Item label="Proprietário" value={v.ownerName || user?.name || '—'} />
              <Item label="Rastreador (IMEI)" value={v.device?.imei || '—'} mono />
              <Item label="Limite de velocidade cadastrado" value={v.speedLimitKmh ? formatSpeed(v.speedLimitKmh) : '—'} />
            </dl>
          </section>

          <section className={styles.block}>
            <h2 className={styles.heading}>No momento da ocorrência</h2>
            {history.isLoading ? (
              <Spinner label="Buscando as posições" />
            ) : at ? (
              <dl className={styles.grid}>
                <div className={styles.wide}>
                  <dt>Endereço aproximado</dt>
                  <dd>
                    <Address lat={at.latitude} lon={at.longitude} />
                  </dd>
                </div>
                <Item label="Posição mais próxima (GPS)" value={formatDateTime(at.gpsTimestamp)} />
                <Item label="Velocidade" value={formatSpeed(at.speedKmh)} />
                <Item label="Ignição" value={at.acc === null ? '—' : at.acc ? 'ligada' : 'desligada'} />
                <div>
                  <dt>Coordenadas</dt>
                  <dd className={styles.mono}>
                    <a href={mapsLink(at.latitude, at.longitude)} target="_blank" rel="noreferrer noopener">
                      {formatCoordinates(at.latitude, at.longitude)}
                    </a>
                  </dd>
                </div>
              </dl>
            ) : (
              <p className={styles.muted}>Sem posições do rastreador nesse período.</p>
            )}
          </section>

          {positions.length > 0 && chart && (
            <>
              <section className={styles.block}>
                <h2 className={styles.heading}>
                  Velocidade de {formatDateTime(range.from.toISOString())} a {formatDateTime(range.to.toISOString())}
                </h2>
                <figure className={local.chart}>
                  <svg viewBox={`0 0 ${chartW} ${chartH}`} preserveAspectRatio="none" role="img"
                    aria-label={`Velocidade no período: máxima de ${Math.round(chart.max)} km/h`}>
                    {[0.25, 0.5, 0.75].map((f) => (
                      <line key={f} x1={0} x2={chartW} y1={chartH * f} y2={chartH * f} className={local.grid} />
                    ))}
                    {limitY !== null && limitY >= 0 && (
                      <line x1={0} x2={chartW} y1={limitY} y2={limitY} className={local.limit} />
                    )}
                    <polyline points={chart.points} className={local.line} />
                    <line x1={markerX} x2={markerX} y1={0} y2={chartH} className={local.marker} />
                  </svg>
                  <figcaption className={styles.muted}>
                    Escala até {chart.top} km/h · máxima no período {formatSpeed(chart.max)}
                    {v.speedLimitKmh ? ` · limite cadastrado ${formatSpeed(v.speedLimitKmh)} (tracejado)` : ''} · a linha
                    vertical é o momento informado.
                  </figcaption>
                </figure>
              </section>

              <section className={styles.block}>
                <h2 className={styles.heading}>Trajeto</h2>
                <div className={styles.map}>
                  <TrackerMap vehicles={[v]} selectedId={v.id} track={positions} showControls={false} recenterKey={positions.length} />
                </div>
                <p className={styles.muted}>
                  {history.data?.returned} de {history.data?.total} posições
                  {history.data?.sampled ? ` (1 a cada ${history.data.sampleStep})` : ''}.
                </p>
                <table className={styles.table}>
                  <thead>
                    <tr>
                      <th>Data e hora</th>
                      <th>Velocidade</th>
                      <th>Ignição</th>
                      <th>Coordenadas</th>
                    </tr>
                  </thead>
                  <tbody>
                    {positions.map((p) => (
                      <tr key={p.id} className={p === at ? local.moment : undefined}>
                        <td>{formatDateTime(p.gpsTimestamp)}</td>
                        <td>{formatSpeed(p.speedKmh)}</td>
                        <td>{p.acc === null ? '—' : p.acc ? 'ligada' : 'desligada'}</td>
                        <td>
                          <a href={mapsLink(p.latitude, p.longitude)} target="_blank" rel="noreferrer noopener">
                            {formatCoordinates(p.latitude, p.longitude)}
                          </a>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </section>
            </>
          )}

          <section className={styles.block}>
            <h2 className={styles.heading}>Eventos do rastreador no período</h2>
            {events.isLoading ? (
              <Spinner label="Buscando os eventos" />
            ) : (events.data ?? []).length === 0 ? (
              <p className={styles.muted}>Nenhum evento no período.</p>
            ) : (
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>Data e hora</th>
                    <th>Evento</th>
                    <th>Coordenadas</th>
                  </tr>
                </thead>
                <tbody>
                  {(events.data ?? []).map((e) => (
                    <tr key={e.id}>
                      <td>{formatDateTime(e.timestamp)}</td>
                      <td>{formatEvent(e.type)}</td>
                      <td>
                        {e.latitude !== null && e.longitude !== null ? formatCoordinates(e.latitude, e.longitude) : '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>

          <p className={styles.muted}>
            Horários de Brasília. As posições vêm do rastreador instalado no veículo, pela rede celular, no intervalo
            configurado nele (em movimento, costuma ser a cada poucos segundos); entre duas posições, o trajeto é uma
            linha reta. A velocidade é a do GPS.
          </p>
        </article>
      )}
    </div>
  );
}

function Item({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd className={mono ? styles.mono : undefined}>{value}</dd>
    </div>
  );
}

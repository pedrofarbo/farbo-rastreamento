import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';

import { theftApi, vehiclesApi } from '@/api/resources';
import { TrackerMap } from '@/components/map/TrackerMap';
import { Address } from '@/components/ui/Address';
import { Button } from '@/components/ui/Button';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { useVehicle } from '@/hooks/useVehicles';
import { formatCoordinates, formatDateTime, formatEvent, formatSpeed } from '@/services/format';
import { useAuth } from '@/stores/AuthContext';

import styles from './TheftReport.module.css';

const HOUR = 60 * 60 * 1000;

/** Os períodos do relatório; "desde o roubo" começa 2 h antes de ligar o modo. */
type Period = 'theft' | '6h' | '24h' | '72h';

export function reportWindow(period: Period, activatedAt: string | null, now: Date): { from: Date; to: Date } {
  const hours = { theft: 0, '6h': 6, '24h': 24, '72h': 72 }[period];
  if (period === 'theft' && activatedAt) return { from: new Date(new Date(activatedAt).getTime() - 2 * HOUR), to: now };
  return { from: new Date(now.getTime() - (hours || 24) * HOUR), to: now };
}

const mapsLink = (lat: number, lon: number) =>
  `https://www.google.com/maps/search/?api=1&query=${lat.toFixed(6)},${lon.toFixed(6)}`;

/**
 * O relatório para o boletim de ocorrência e o seguro: o veículo, o
 * rastreador, quando o modo roubo foi ligado, a última posição, os eventos e
 * o trajeto do período, com mapa — para imprimir ou salvar em PDF. Só o dono
 * (o histórico é dele).
 */
export function TheftReportPage() {
  const { id = '' } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { user } = useAuth();
  const vehicle = useVehicle(id);
  const theft = useQuery({ queryKey: ['theft', id], queryFn: () => theftApi.get(id), enabled: Boolean(id) });
  const activatedAt = theft.data?.mode?.activatedAt ?? null;
  const [chosen, setChosen] = useState<Period | null>(null);
  const period: Period = chosen ?? (activatedAt ? 'theft' : '24h');
  // O "agora" do relatório fica parado até trocar o período (a consulta não muda a cada render).
  const range = useMemo(() => {
    const w = reportWindow(period, activatedAt, new Date());
    return { from: w.from.toISOString(), to: w.to.toISOString() };
  }, [period, activatedAt]);
  const generatedAt = range.to;

  const owned = Boolean(vehicle.data && !vehicle.data.shared);
  const history = useQuery({
    queryKey: ['theft-report', id, range.from, range.to],
    queryFn: () => vehiclesApi.positions(id, { from: range.from, to: range.to, limit: 500, simplify: false }),
    enabled: owned,
  });
  const events = useQuery({
    queryKey: ['theft-report-events', id, range.from, range.to],
    queryFn: () => vehiclesApi.events(id, { from: range.from, to: range.to, limit: 200 }),
    enabled: owned,
  });

  if (vehicle.isLoading) return <Spinner label="Carregando o veículo" />;
  const v = vehicle.data;
  if (!v || v.shared) {
    return <EmptyState title="Relatório indisponível" description="O relatório do trajeto é só do dono do veículo." />;
  }
  const positions = history.data?.positions ?? [];
  const last = v.lastPosition;
  const details = [v.brand, v.model, v.year, v.color].filter(Boolean).join(' · ');
  const periods: { value: Period; label: string }[] = [
    ...(activatedAt ? [{ value: 'theft' as const, label: 'Desde o roubo' }] : []),
    { value: '6h', label: 'Últimas 6 h' },
    { value: '24h', label: 'Últimas 24 h' },
    { value: '72h', label: 'Últimos 3 dias' },
  ];

  return (
    <div className={styles.screen}>
      <div className={styles.toolbar}>
        <Button variant="ghost" size="small" onClick={() => navigate(-1)}>
          Voltar
        </Button>
        <div className={styles.chips} role="group" aria-label="Período do relatório">
          {periods.map((option) => (
            <button
              key={option.value}
              type="button"
              className={`${styles.chip} ${period === option.value ? styles.chipActive : ''}`}
              aria-pressed={period === option.value}
              onClick={() => setChosen(option.value)}
            >
              {option.label}
            </button>
          ))}
        </div>
        <Button variant="primary" size="small" onClick={() => window.print()}>
          Imprimir ou salvar PDF
        </Button>
      </div>

      <article className={styles.report}>
        <header className={styles.head}>
          <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={styles.logo} />
          <div>
            <h1 className={styles.title}>Relatório de rastreamento</h1>
            <p className={styles.muted}>Gerado em {formatDateTime(generatedAt)} pela plataforma Farbo Rastreadores.</p>
          </div>
        </header>

        <section className={styles.block}>
          <h2 className={styles.heading}>Veículo</h2>
          <dl className={styles.grid}>
            <Item label="Identificação" value={v.name} />
            <Item label="Placa" value={v.plate || '—'} mono />
            <Item label="Marca, modelo, ano e cor" value={details || '—'} />
            <Item label="Proprietário" value={user?.name || '—'} />
            <Item label="Rastreador (IMEI)" value={v.device?.imei || '—'} mono />
            <Item
              label="Modo roubo"
              value={activatedAt ? `ligado em ${formatDateTime(activatedAt)}` : 'desligado no momento'}
            />
          </dl>
        </section>

        <section className={styles.block}>
          <h2 className={styles.heading}>Última posição conhecida</h2>
          {last ? (
            <dl className={styles.grid}>
              <div className={styles.wide}>
                <dt>Endereço aproximado</dt>
                <dd>
                  <Address lat={last.latitude} lon={last.longitude} />
                </dd>
              </div>
              <Item label="Coordenadas" value={formatCoordinates(last.latitude, last.longitude)} mono />
              <Item label="Data e hora (GPS)" value={formatDateTime(last.gpsTimestamp)} />
              <Item label="Velocidade" value={formatSpeed(last.speedKmh)} />
              <div>
                <dt>Mapa</dt>
                <dd>
                  <a href={mapsLink(last.latitude, last.longitude)} target="_blank" rel="noreferrer noopener">
                    Abrir no Google Maps
                  </a>
                </dd>
              </div>
            </dl>
          ) : (
            <p className={styles.muted}>O rastreador ainda não mandou posição.</p>
          )}
        </section>

        <section className={styles.block}>
          <h2 className={styles.heading}>
            Trajeto de {formatDateTime(range.from)} a {formatDateTime(range.to)}
          </h2>
          {history.isLoading ? (
            <Spinner label="Buscando o trajeto" />
          ) : positions.length === 0 ? (
            <p className={styles.muted}>Sem posições registradas no período.</p>
          ) : (
            <>
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
                  {[...positions].reverse().map((p) => (
                    <tr key={p.id}>
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
            </>
          )}
        </section>

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
          Horários de Brasília. As posições vêm do rastreador instalado no veículo, pela rede celular; a precisão do GPS
          costuma ser de alguns metros.
        </p>
      </article>
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

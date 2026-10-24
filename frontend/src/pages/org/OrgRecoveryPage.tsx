import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { orgApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import fieldStyles from '@/components/ui/Field.module.css';
import { Spinner } from '@/components/ui/Spinner';
import { formatDateTime, formatRelative } from '@/services/format';
import type { OrgTheftCase } from '@/types';

import pageStyles from '../Page.module.css';
import styles from './OrgRecovery.module.css';

const PERIODS = [
  { months: 3, label: 'Últimos 3 meses' },
  { months: 6, label: 'Últimos 6 meses' },
  { months: 12, label: 'Últimos 12 meses' },
  { months: 24, label: 'Últimos 24 meses' },
];

const OUTCOME: Record<OrgTheftCase['outcome'], { label: string; tone: 'danger' | 'success' | 'neutral' | 'warning' }> = {
  '': { label: 'Em andamento', tone: 'danger' },
  RECOVERED: { label: 'Recuperado', tone: 'success' },
  CANCELLED: { label: 'Alarme falso', tone: 'neutral' },
  EXPIRED: { label: 'Desligou no prazo', tone: 'warning' },
};

/** 95 → "1 h 35 min"; 40 → "40 min"; 3000 → "2 d 2 h". */
export function minutesLabel(minutes: number): string {
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return minutes % 60 ? `${hours} h ${minutes % 60} min` : `${hours} h`;
  const days = Math.floor(hours / 24);
  return hours % 24 ? `${days} d ${hours % 24} h` : `${days} d`;
}

/** Quanto durou (ou dura) o modo roubo. */
function duration(c: OrgTheftCase): string {
  const end = c.endedAt ? new Date(c.endedAt) : new Date();
  return minutesLabel(Math.max(0, Math.round((end.getTime() - new Date(c.activatedAt).getTime()) / 60_000)));
}

/**
 * O painel de recuperação da associação ou seguradora: os veículos dos
 * associados com o modo roubo ligado agora (para acompanhar no mapa) e o
 * histórico — quantos foram recuperados, em quanto tempo, e os que o prazo
 * venceu sem recuperar.
 */
export function OrgRecoveryPage() {
  const [months, setMonths] = useState(12);
  const q = useQuery({
    queryKey: ['org', 'recoveries', months],
    queryFn: () => orgApi.recoveries(months),
    // Com algum em andamento, acompanha.
    refetchInterval: (query) => ((query.state.data?.active ?? 0) > 0 ? 30_000 : false),
  });
  const data = q.data;
  const active = data?.cases.filter((c) => c.outcome === '') ?? [];
  const history = data?.cases.filter((c) => c.outcome !== '') ?? [];

  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>Recuperação</h1>
            <p className={pageStyles.description}>
              Os veículos dos associados com o modo roubo ligado e o histórico: quantos foram recuperados e em quanto tempo.
              A equipe recebe o aviso por e-mail quando um associado liga o modo roubo.
            </p>
          </div>
          <label className={styles.period}>
            Período
            <select className={fieldStyles.select} value={months} onChange={(e) => setMonths(Number(e.target.value))}>
              {PERIODS.map((p) => (
                <option key={p.months} value={p.months}>
                  {p.label}
                </option>
              ))}
            </select>
          </label>
        </header>

        {q.isLoading ? (
          <Spinner label="Carregando" />
        ) : q.error ? (
          <p className={pageStyles.note}>{(q.error as Error).message}</p>
        ) : !data || data.total === 0 ? (
          <Card>
            <EmptyState
              icon="🛡️"
              title="Nenhum roubo no período"
              description="Quando um associado ligar o modo roubo, o veículo aparece aqui com a posição ao vivo."
            />
          </Card>
        ) : (
          <>
            <div className={styles.tiles}>
              <div className={`${styles.tile} ${data.active > 0 ? styles.tileDanger : ''}`}>
                <span className={styles.tileLabel}>Em andamento</span>
                <span className={styles.tileValue}>{data.active}</span>
              </div>
              <div className={styles.tile}>
                <span className={styles.tileLabel}>Recuperados</span>
                <span className={styles.tileValue}>{data.recovered}</span>
                <span className={styles.tileHint}>de {data.total} no período</span>
              </div>
              <div className={styles.tile}>
                <span className={styles.tileLabel}>Taxa de recuperação</span>
                <span className={styles.tileValue}>
                  {data.recoveryRate !== null ? `${data.recoveryRate.toLocaleString('pt-BR')}%` : '—'}
                </span>
                <span className={styles.tileHint}>sem contar os alarmes falsos</span>
              </div>
              <div className={styles.tile}>
                <span className={styles.tileLabel}>Tempo até recuperar</span>
                <span className={styles.tileValue}>
                  {data.medianRecoveryMinutes !== null ? minutesLabel(data.medianRecoveryMinutes) : '—'}
                </span>
                <span className={styles.tileHint}>a mediana</span>
              </div>
            </div>

            {active.length > 0 && (
              <Card title="Em andamento">
                <ul className={styles.active}>
                  {active.map((c) => (
                    <li key={c.id} className={styles.activeItem}>
                      <span className={styles.pulse} aria-hidden="true" />
                      <div className={styles.who}>
                        <Link to={`/veiculos/${c.vehicleId}`} className={styles.vehicle}>
                          {c.vehicleName}
                          {c.plate && <span className={styles.plate}>{c.plate}</span>}
                        </Link>
                        <span className={styles.muted}>
                          <Link to={`/associados/${c.associateId}`}>{c.associateName}</Link> · ligado{' '}
                          {formatRelative(c.activatedAt)}
                          {c.activatedByName && c.activatedByName !== c.associateName ? ` por ${c.activatedByName}` : ''}
                        </span>
                      </div>
                      <Link to={`/veiculos/${c.vehicleId}`} className={styles.follow}>
                        Acompanhar
                      </Link>
                    </li>
                  ))}
                </ul>
              </Card>
            )}

            {history.length > 0 && (
              <Card title="Histórico" flush>
                <div className={pageStyles.tableWrap}>
                  <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
                    <thead>
                      <tr>
                        <th>Veículo</th>
                        <th>Associado</th>
                        <th>Ligado em</th>
                        <th>Duração</th>
                        <th>Resultado</th>
                      </tr>
                    </thead>
                    <tbody>
                      {history.map((c) => (
                        <tr key={c.id}>
                          <td>
                            <Link to={`/veiculos/${c.vehicleId}`} className={styles.vehicle}>
                              {c.vehicleName}
                              {c.plate && <span className={styles.plate}>{c.plate}</span>}
                            </Link>
                          </td>
                          <td data-label="Associado">
                            <Link to={`/associados/${c.associateId}`}>{c.associateName}</Link>
                          </td>
                          <td data-label="Ligado em">{formatDateTime(c.activatedAt)}</td>
                          <td data-label="Duração">{duration(c)}</td>
                          <td data-label="Resultado">
                            <Badge tone={OUTCOME[c.outcome].tone}>{OUTCOME[c.outcome].label}</Badge>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </Card>
            )}
          </>
        )}
      </div>
    </div>
  );
}

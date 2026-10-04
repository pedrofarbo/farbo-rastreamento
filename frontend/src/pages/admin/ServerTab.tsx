import { useState } from 'react';
import type { ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';

import { infraApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import type { BadgeTone } from '@/components/ui/Badge';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { formatBytes, formatDateTime, formatRelative, formatTime } from '@/services/format';
import type { InfraOffsite, InfraStatus, SystemLogEntry } from '@/types';

import styles from './Server.module.css';

type Tone = 'ok' | 'warn' | 'danger';

/** Faixas de alerta (em %): a partir de warn fica amarelo; de danger, vermelho. */
export const LIMITS = {
  cpu: { warn: 70, danger: 90 },
  mem: { warn: 80, danger: 92 },
  disk: { warn: 80, danger: 90 },
} as const;

export function toneOf(value: number, limit: { warn: number; danger: number }): Tone {
  if (value >= limit.danger) return 'danger';
  if (value >= limit.warn) return 'warn';
  return 'ok';
}

/** O backup é diário: com mais de 26 h já atrasou um; com mais de 50 h, dois. */
export function backupTone(latestAt: string | null | undefined, now = Date.now()): Tone {
  if (!latestAt) return 'danger';
  const hours = (now - new Date(latestAt).getTime()) / 3_600_000;
  if (hours > 50) return 'danger';
  if (hours > 26) return 'warn';
  return 'ok';
}

/**
 * A cópia fora da VPS: sem ela, perder o disco do servidor leva o banco e os
 * backups juntos.
 */
export function offsiteView(
  offsite: InfraOffsite | null | undefined,
  now = Date.now(),
): { tone: Tone; label: string; alert?: string } {
  if (!offsite?.configured) {
    return {
      tone: 'warn',
      label: 'Não configurada',
      alert:
        'Os backups ficam só no disco do servidor: se ele falhar, vão junto com o banco. Configure a cópia externa (BACKUP_S3_* no .env.production, ver deploy/PRODUCTION.md).',
    };
  }
  if (!offsite.ok) {
    return {
      tone: 'danger',
      label: 'Falhou',
      alert: `O último envio para fora da VPS falhou${offsite.error ? `: ${offsite.error}` : '.'}`,
    };
  }
  return { tone: backupTone(offsite.at, now), label: `Enviada ${formatRelative(offsite.at)}` };
}

/** 200000 → "2 dias e 7 h"; 4000 → "1 h 6 min". */
export function formatUptime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '—';
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (days > 0) return `${days} ${days === 1 ? 'dia' : 'dias'}${hours ? ` e ${hours} h` : ''}`;
  if (hours > 0) return `${hours} h ${minutes} min`;
  return `${Math.max(1, minutes)} min`;
}

const pct = (part: number, total: number) => (total > 0 ? (part / total) * 100 : 0);
/** 0.8 → "0,8"; com casas fixas para a carga ("0,40"). */
const num = (value: number, digits = 1) =>
  value.toLocaleString('pt-BR', { minimumFractionDigits: digits, maximumFractionDigits: digits });
const BADGE: Record<Tone, BadgeTone> = { ok: 'success', warn: 'warning', danger: 'danger' };

/**
 * A saúde do sistema: CPU, memória e disco da máquina (com as últimas 24 h),
 * o banco, os backups, o servidor da API, as conexões abertas e os avisos e
 * erros que o servidor registrou. Atualiza sozinho.
 */
export function ServerTab() {
  const status = useQuery({ queryKey: ['infra', 'status'], queryFn: infraApi.status, refetchInterval: 15_000 });
  const data = status.data;

  if (status.isLoading) return <Spinner label="Lendo o servidor" />;
  if (status.isError || !data) {
    return (
      <Card>
        <EmptyState icon="⚠️" title="Não foi possível ler o servidor" description={(status.error as Error)?.message} />
      </Card>
    );
  }

  const cpu = data.host.cpuPercent;
  const mem = pct(data.host.memUsed, data.host.memTotal);
  const disk = pct(data.host.diskUsed, data.host.diskTotal);

  return (
    <div className={styles.tab}>
      <div className={styles.gauges}>
        <Gauge
          label="CPU"
          value={cpu}
          tone={toneOf(cpu, LIMITS.cpu)}
          detail={`${data.host.cores} ${data.host.cores === 1 ? 'núcleo' : 'núcleos'} · carga ${num(data.host.load1, 2)} / ${num(data.host.load5, 2)} / ${num(data.host.load15, 2)}`}
        />
        <Gauge
          label="Memória"
          value={mem}
          tone={toneOf(mem, LIMITS.mem)}
          detail={`${formatBytes(data.host.memUsed)} de ${formatBytes(data.host.memTotal)} em uso`}
        />
        <Gauge
          label="Disco"
          value={disk}
          tone={toneOf(disk, LIMITS.disk)}
          detail={`${formatBytes(data.host.diskTotal - data.host.diskUsed)} livres de ${formatBytes(data.host.diskTotal)}`}
        />
      </div>

      <Card
        title="Últimas 24 horas"
        subtitle={
          data.history.length > 0
            ? `Desde ${formatTime(data.history[0].at)} · o histórico recomeça quando o servidor reinicia`
            : undefined
        }
      >
        <HistoryChart points={data.history} />
      </Card>

      <div className={styles.grid}>
        <Card title="Banco de dados">
          <Rows>
            <Row label="Situação">
              <Badge tone={data.database.ok ? 'success' : 'danger'} dot>
                {data.database.ok ? `Respondendo em ${num(data.database.latencyMs)} ms` : 'Sem resposta'}
              </Badge>
            </Row>
            {data.database.error && <Row label="Erro">{data.database.error}</Row>}
            <Row label="Tamanho">{formatBytes(data.database.sizeBytes)}</Row>
            <Row label="Conexões">
              {data.database.connections} de {data.database.maxConnections || '—'}
            </Row>
          </Rows>
          {(data.database.tables ?? []).length > 0 && (
            <>
              <p className={styles.subheading}>Maiores tabelas</p>
              <Rows>
                {(data.database.tables ?? []).map((t) => (
                  <Row key={t.name} label={t.name} mono>
                    {formatBytes(t.sizeBytes)} · {t.rows.toLocaleString('pt-BR')} linhas
                  </Row>
                ))}
              </Rows>
            </>
          )}
        </Card>

        <BackupsCard backups={data.backups} />

        <Card title="Servidor da API">
          <Rows>
            <Row label="No ar há">{formatUptime((Date.now() - new Date(data.process.startedAt).getTime()) / 1000)}</Row>
            <Row label="Memória do processo">
              {formatBytes(data.process.rssBytes)} · {formatBytes(data.process.heapBytes)} em objetos
            </Row>
            <Row label="Tarefas (goroutines)">{data.process.goroutines.toLocaleString('pt-BR')}</Row>
            <Row label="Go">{data.process.goVersion}</Row>
            <Row label="Máquina ligada há">{formatUptime(data.host.hostUptime)}</Row>
            <Row label="Redis">
              {data.redis.enabled ? (
                <Badge tone={data.redis.ok ? 'success' : 'danger'} dot>
                  {data.redis.ok ? `Respondendo em ${num(data.redis.latencyMs)} ms` : 'Sem resposta'}
                </Badge>
              ) : (
                <span className={styles.muted}>Desligado (uma instância só)</span>
              )}
            </Row>
          </Rows>
        </Card>

        <Card title="Conexões agora">
          <Rows>
            <Row label="Rastreadores conectados">{data.live.trackerConnections}</Row>
            <Row label="Rastreadores online">{data.live.onlineDevices >= 0 ? data.live.onlineDevices : '—'}</Row>
            <Row label="Painéis e apps no tempo real">{data.live.realtimeClients}</Row>
          </Rows>
        </Card>
      </div>

      <LogsCard counts={data.logs} />
    </div>
  );
}

function Gauge({ label, value, tone, detail }: { label: string; value: number; tone: Tone; detail: string }) {
  return (
    <div className={`${styles.gauge} ${styles[tone]}`}>
      <span className={styles.gaugeLabel}>{label}</span>
      <span className={styles.gaugeValue}>
        {value.toLocaleString('pt-BR', { maximumFractionDigits: value < 10 ? 1 : 0 })}%
      </span>
      <span className={styles.gaugeTrack} role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(value)}>
        <span className={styles.gaugeFill} style={{ width: `${Math.min(100, value)}%` }} />
      </span>
      <span className={styles.gaugeDetail}>{detail}</span>
    </div>
  );
}

function Rows({ children }: { children: ReactNode }) {
  return <dl className={styles.rows}>{children}</dl>;
}

function Row({ label, children, mono = false }: { label: string; children: ReactNode; mono?: boolean }) {
  return (
    <div className={styles.row}>
      <dt className={mono ? styles.mono : undefined}>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

function BackupsCard({ backups }: { backups: InfraStatus['backups'] }) {
  if (!backups.available) {
    return (
      <Card title="Backups do banco">
        <p className={styles.muted}>A pasta dos backups não está disponível neste ambiente (BACKUP_DIR).</p>
      </Card>
    );
  }
  const tone = backupTone(backups.latest?.at);
  const offsite = offsiteView(backups.offsite);
  return (
    <Card title="Backups do banco">
      <Rows>
        <Row label="Último">
          <Badge tone={BADGE[tone]} dot>
            {backups.latest ? formatRelative(backups.latest.at) : 'Nenhum backup'}
          </Badge>
        </Row>
        {backups.latest && (
          <Row label="Tamanho do último">
            {formatBytes(backups.latest.sizeBytes)} · {formatDateTime(backups.latest.at)}
          </Row>
        )}
        <Row label="Cópias guardadas">
          {backups.count} · {formatBytes(backups.totalBytes)}
        </Row>
        <Row label="Fora da VPS">
          <Badge tone={BADGE[offsite.tone]} dot>
            {offsite.label}
          </Badge>
        </Row>
      </Rows>
      {tone !== 'ok' && (
        <p className={styles.alert}>
          O backup é feito uma vez por dia. Sem um recente, confira o contêiner postgres-backup no servidor.
        </p>
      )}
      {offsite.alert && <p className={styles.alert}>{offsite.alert}</p>}
    </Card>
  );
}

const PERIODS = [
  { hours: 24, label: '24 h' },
  { hours: 168, label: '7 dias' },
  { hours: 720, label: '30 dias' },
];

function LogsCard({ counts }: { counts: InfraStatus['logs'] }) {
  const [hours, setHours] = useState(24);
  const [level, setLevel] = useState<'' | 'ERROR' | 'WARN'>('ERROR');
  const [search, setSearch] = useState('');
  const logs = useQuery({
    queryKey: ['infra', 'logs', hours, level, search.trim()],
    queryFn: () => infraApi.logs({ hours, level, search: search.trim() }),
    refetchInterval: 30_000,
  });
  const page = logs.data;

  return (
    <Card
      title="Erros e avisos do servidor"
      subtitle={`Nas últimas 24 h: ${counts.errors24h} ${counts.errors24h === 1 ? 'erro' : 'erros'} e ${counts.warnings24h} ${counts.warnings24h === 1 ? 'aviso' : 'avisos'}`}
    >
      <div className={styles.filters}>
        <div className={styles.chips} role="group" aria-label="Período">
          {PERIODS.map((p) => (
            <button
              key={p.hours}
              type="button"
              className={`${styles.chip} ${hours === p.hours ? styles.chipActive : ''}`}
              aria-pressed={hours === p.hours}
              onClick={() => setHours(p.hours)}
            >
              {p.label}
            </button>
          ))}
        </div>
        <div className={styles.chips} role="group" aria-label="Nível">
          {(
            [
              ['ERROR', 'Erros'],
              ['WARN', 'Avisos'],
              ['', 'Todos'],
            ] as const
          ).map(([value, label]) => (
            <button
              key={label}
              type="button"
              className={`${styles.chip} ${level === value ? styles.chipActive : ''}`}
              aria-pressed={level === value}
              onClick={() => setLevel(value)}
            >
              {label}
            </button>
          ))}
        </div>
        <input
          className={styles.search}
          type="search"
          placeholder="Buscar na mensagem, no componente ou nos detalhes"
          aria-label="Buscar nos registros"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>

      {(counts.dropped > 0 || counts.failed > 0) && (
        <p className={styles.alert}>
          {counts.dropped > 0 && `${counts.dropped} registro(s) descartado(s) por excesso. `}
          {counts.failed > 0 && `${counts.failed} registro(s) não gravado(s) no banco.`}
        </p>
      )}

      {logs.isLoading ? (
        <Spinner label="Carregando os registros" />
      ) : !page || page.entries.length === 0 ? (
        <p className={styles.empty}>Nada registrado no período. 🎉</p>
      ) : (
        <>
          <p className={styles.subheading}>Mais frequentes</p>
          <ul className={styles.groups}>
            {page.groups.map((g) => (
              <li key={`${g.level}|${g.component}|${g.message}`} className={styles.group}>
                <LevelBadge level={g.level} />
                <span className={styles.groupText}>
                  {g.component && <span className={styles.component}>{g.component}</span>}
                  {g.message}
                </span>
                <span className={styles.groupMeta}>
                  {g.count}× · {formatRelative(g.lastAt)}
                </span>
              </li>
            ))}
          </ul>

          <p className={styles.subheading}>Mais recentes</p>
          <ul className={styles.entries}>
            {page.entries.map((e) => (
              <LogEntry key={e.id} entry={e} />
            ))}
          </ul>
        </>
      )}
    </Card>
  );
}

function LevelBadge({ level }: { level: 'WARN' | 'ERROR' }) {
  return <Badge tone={level === 'ERROR' ? 'danger' : 'warning'}>{level === 'ERROR' ? 'Erro' : 'Aviso'}</Badge>;
}

function LogEntry({ entry }: { entry: SystemLogEntry }) {
  const attrs = Object.entries(entry.attrs ?? {});
  return (
    <li className={styles.entry}>
      <details>
        <summary>
          <LevelBadge level={entry.level} />
          <span className={styles.entryTime}>{formatDateTime(entry.at)}</span>
          {entry.component && <span className={styles.component}>{entry.component}</span>}
          <span className={styles.entryMessage}>{entry.message}</span>
        </summary>
        {attrs.length === 0 ? (
          <p className={styles.muted}>Sem detalhes.</p>
        ) : (
          <dl className={styles.attrs}>
            {attrs.map(([key, value]) => (
              <div key={key}>
                <dt>{key}</dt>
                <dd>{typeof value === 'string' ? value : JSON.stringify(value)}</dd>
              </div>
            ))}
          </dl>
        )}
      </details>
    </li>
  );
}

/** As três linhas (CPU, memória e disco, em %) dos últimos minutos. */
function HistoryChart({ points }: { points: InfraStatus['history'] }) {
  if (points.length < 2) {
    return <p className={styles.muted}>O gráfico aparece depois dos primeiros minutos de leitura.</p>;
  }
  const w = Math.max(points.length - 1, 1);
  const line = (key: 'cpu' | 'mem' | 'disk') =>
    points.map((p, i) => `${((i / w) * 100).toFixed(2)},${(40 - (p[key] / 100) * 40).toFixed(2)}`).join(' ');
  const last = points[points.length - 1];
  return (
    <div className={styles.chart}>
      <div className={styles.legend}>
        <span className={styles.legendCpu}>CPU {num(last.cpu)}%</span>
        <span className={styles.legendMem}>Memória {num(last.mem)}%</span>
        <span className={styles.legendDisk}>Disco {num(last.disk)}%</span>
      </div>
      <div className={styles.chartArea}>
        <span className={styles.axisTop}>100%</span>
        <span className={styles.axisMid}>50%</span>
        <svg viewBox="0 0 100 40" preserveAspectRatio="none" className={styles.chartSvg} role="img" aria-label="CPU, memória e disco nas últimas horas">
          <line x1="0" y1="20" x2="100" y2="20" className={styles.gridLine} />
          <polyline points={line('disk')} className={styles.lineDisk} />
          <polyline points={line('mem')} className={styles.lineMem} />
          <polyline points={line('cpu')} className={styles.lineCpu} />
        </svg>
      </div>
      <div className={styles.axis}>
        <span>{formatTime(points[0].at)}</span>
        <span>{formatTime(last.at)}</span>
      </div>
    </div>
  );
}

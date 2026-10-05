import { useState } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';

import { analyticsApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import type { AnalyticsCount, AnalyticsOrigin, LandingAnalytics } from '@/types';

import styles from './SiteAnalytics.module.css';

const PERIODS = [7, 30, 90] as const;

/** As seções da landing, na ordem da página (para "até onde leem"). */
export const SECTIONS: { id: string; label: string }[] = [
  { id: 'beneficios', label: 'Topo (primeira tela)' },
  { id: 'planos', label: 'Planos e preços' },
  { id: 'instalacao', label: 'Instalação' },
  { id: 'como-funciona', label: 'Como funciona' },
  { id: 'lancamento', label: 'Pré-lançamento' },
  { id: 'depoimentos', label: 'Depoimentos' },
  { id: 'contato', label: 'Chamada final' },
  { id: 'rodape', label: 'Rodapé' },
];

const CLICKS: Record<string, string> = {
  'menu-pre-lancamento': 'Menu · Pré-lançamento',
  'hero-pre-lancamento': 'Topo · Entrar no pré-lançamento',
  'planos-banner': 'Planos · faixa do pré-lançamento',
  'plano-mensal': 'Planos · Plano Mensal',
  'plano-insanos': 'Planos · Insanos MC',
  'cta-final': 'Chamada final',
  instagram: 'Instagram',
  email: 'E-mail',
  whatsapp: 'WhatsApp · rodapé',
  'whatsapp-flutuante': 'WhatsApp · botão flutuante',
  'whatsapp-pre-cadastro': 'WhatsApp · pré-cadastro',
};

const DEVICES: Record<string, string> = { mobile: 'Celular', tablet: 'Tablet', desktop: 'Computador' };

/** As origens conhecidas; uma campanha ou um site fora da lista aparece como veio. */
const CHANNELS: Record<string, string> = {
  direto: 'Direto',
  instagram: 'Instagram',
  facebook: 'Facebook',
  whatsapp: 'WhatsApp',
  tiktok: 'TikTok',
  youtube: 'YouTube',
  google: 'Google',
  busca: 'Outros buscadores',
  evento: 'Eventos (QR Code)',
};

export function channelLabel(channel: string): string {
  return CHANNELS[channel] ?? channel;
}

/** As origens para escolher: as do período, mais a escolhida (se sumiu dele). */
export function originOptions(origins: AnalyticsOrigin[], selected: string): string[] {
  const list = origins.map((o) => o.channel);
  return selected && !list.includes(selected) ? [...list, selected] : list;
}

function plural(n: number, one: string, many: string): string {
  return `${n.toLocaleString('pt-BR')} ${n === 1 ? one : many}`;
}

/** "12,5%" do total (ou "—" sem base). */
export function percent(part: number, total: number): string {
  if (total <= 0) return '—';
  const value = (part / total) * 100;
  return `${value.toLocaleString('pt-BR', { maximumFractionDigits: value < 10 ? 1 : 0 })}%`;
}

/** As seções na ordem da página, com quantos visitantes chegaram a cada uma. */
export function sectionReach(data: Pick<LandingAnalytics, 'sections'>): { id: string; label: string; visitors: number }[] {
  const byId = new Map(data.sections.map((s) => [s.key, s.visitors]));
  return SECTIONS.filter((s) => byId.has(s.id)).map((s) => ({ ...s, visitors: byId.get(s.id) ?? 0 }));
}

/**
 * As visitas da landing page: quantos vieram, de onde, em que aparelho, até
 * onde leram e quantos viraram pré-cliente ou entraram na lista. Sem cookies
 * e sem dados pessoais (cada visitante conta uma vez por dia).
 */
export function SiteAnalyticsTab() {
  const [days, setDays] = useState<(typeof PERIODS)[number]>(30);
  // Só os visitantes que chegaram por essa origem ("" = todas).
  const [origin, setOrigin] = useState('');
  const query = useQuery({
    queryKey: ['analytics', 'landing', days, origin],
    queryFn: () => analyticsApi.landing(days, origin),
    refetchInterval: 60_000,
    // Trocar o período ou a origem mantém a tela até chegar o novo resumo.
    placeholderData: keepPreviousData,
  });
  const data = query.data;
  const originName = channelLabel(origin);

  return (
    <div className={styles.tab}>
      <div className={styles.toolbar}>
        <div className={styles.filters}>
          <div className={styles.periods} role="group" aria-label="Período">
            {PERIODS.map((p) => (
              <button
                key={p}
                type="button"
                className={`${styles.period} ${days === p ? styles.periodActive : ''}`}
                aria-pressed={days === p}
                onClick={() => setDays(p)}
              >
                {p} dias
              </button>
            ))}
          </div>
          {data && (
            <label className={styles.originPicker}>
              Origem
              <select className={styles.select} value={origin} onChange={(event) => setOrigin(event.target.value)}>
                <option value="">Todas</option>
                {originOptions(data.origins, origin).map((channel) => (
                  <option key={channel} value={channel}>
                    {channelLabel(channel)}
                  </option>
                ))}
              </select>
            </label>
          )}
        </div>
        {data && (
          <span className={styles.live} title="Visitantes nos últimos 10 minutos">
            <span className={`${styles.liveDot} ${data.activeNow > 0 ? styles.liveDotOn : ''}`} aria-hidden="true" />
            {data.activeNow} {data.activeNow === 1 ? 'pessoa' : 'pessoas'} no site agora
          </span>
        )}
      </div>

      {query.isLoading ? (
        <Spinner label="Carregando as visitas" />
      ) : query.isError || !data ? (
        <Card>
          <EmptyState icon="⚠️" title="Não foi possível carregar" description={(query.error as Error)?.message} />
        </Card>
      ) : (
        <>
          {origin && (
            <div className={styles.filterNote} role="status">
              <span>
                Mostrando só quem chegou por <strong>{originName}</strong>.
              </span>
              <button type="button" onClick={() => setOrigin('')}>
                Ver todas as origens
              </button>
            </div>
          )}

          <div className={billing.tiles}>
            <div className={billing.tile}>
              <span className={billing.tileLabel}>Visitantes</span>
              <span className={billing.tileValue}>{data.visitors.toLocaleString('pt-BR')}</span>
              <span className={billing.tileHint}>{data.pageviews.toLocaleString('pt-BR')} visualizações da página</span>
            </div>
            <div className={billing.tile}>
              <span className={billing.tileLabel}>Pré-cadastros</span>
              <span className={billing.tileValue}>{data.leads}</span>
              <span className={billing.tileHint}>{percent(data.leads, data.visitors)} dos visitantes</span>
            </div>
            <div className={billing.tile}>
              <span className={billing.tileLabel}>Lista de lançamento</span>
              <span className={billing.tileValue}>{data.waitlist}</span>
              <span className={billing.tileHint}>{percent(data.waitlist, data.visitors)} dos visitantes</span>
            </div>
          </div>

          <Card title="Visitantes por dia" subtitle={`${formatDay(data.from)} a ${formatDay(data.to)}`}>
            {data.visitors === 0 ? (
              <p className={styles.muted}>Nenhuma visita no período ainda.</p>
            ) : (
              <DailyChart days={data.days} />
            )}
          </Card>

          <div className={styles.grid}>
            <Card title="Funil" subtitle={origin ? `Só quem veio de ${originName}` : 'Todas as origens'}>
              <Ranking
                total={data.visitors}
                rows={[
                  { label: 'Visitaram a página', visitors: data.visitors },
                  { label: 'Abriram o pré-cadastro', visitors: data.leadOpens },
                  { label: 'Enviaram o pré-cadastro', visitors: data.leads },
                  { label: 'Entraram na lista de lançamento', visitors: data.waitlist },
                  { label: 'Viram os prestadores', visitors: data.installerOpens },
                ]}
              />
            </Card>

            <Card title="Funil por origem" subtitle="De onde vêm e quantos viram contato. Escolha uma para filtrar a página.">
              <OriginsFunnel origins={data.origins} selected={origin} onSelect={setOrigin} />
            </Card>

            <Card title="Até onde leem" subtitle="Visitantes que chegaram a cada parte da página">
              <Ranking
                total={data.visitors}
                rows={sectionReach(data).map((s) => ({ label: s.label, visitors: s.visitors }))}
                empty="Ainda sem leitura registrada."
              />
            </Card>

            <Card title="Campanhas" subtitle="Links com utm_source e utm_campaign">
              <Ranking
                total={data.visitors}
                rows={data.campaigns.map((c) => ({ label: c.key, visitors: c.visitors }))}
                empty="Nenhuma visita por link de campanha. Ex.: farborastreadores.com.br/?utm_source=instagram&utm_campaign=lancamento"
              />
            </Card>

            <Card title="Aparelhos">
              <Ranking total={data.visitors} rows={labeled(data.devices, DEVICES)} />
            </Card>

            <Card title="Navegadores e sistemas">
              <Ranking total={data.visitors} rows={labeled(data.browsers)} />
              <div className={styles.split} />
              <Ranking total={data.visitors} rows={labeled(data.systems)} />
            </Card>

            <Card title="Cliques nos botões" subtitle="Visitantes que clicaram (e quantas vezes)">
              <Ranking
                total={data.visitors}
                rows={data.clicks.map((c) => ({
                  label: CLICKS[c.key] ?? c.key,
                  visitors: c.visitors,
                  extra: c.count !== c.visitors ? `${c.count} cliques` : undefined,
                }))}
                empty="Nenhum clique registrado ainda."
              />
            </Card>
          </div>

          <p className={styles.note}>
            Sem cookies e sem dados pessoais: cada visitante vira um código anônimo que muda todo dia, e o
            IP não é guardado. Por isso a mesma pessoa conta uma vez por dia. Visitas de robôs ficam de fora.
            A origem é a primeira de fora com que a pessoa chegou no dia (link de campanha, rede social,
            buscador ou outro site); quem chega sem nenhuma conta como direto, inclusive quem abre um link
            mandado pelo WhatsApp, que não diz de onde veio. Para separar, use links com ?utm_source=whatsapp.
          </p>
        </>
      )}
    </div>
  );
}

/**
 * O funil de cada origem: visitantes (com a barra) e, embaixo, quantos
 * abriram e mandaram o pré-cadastro, entraram na lista e viraram contato.
 * Escolher uma filtra a página; escolher de novo volta a todas.
 */
function OriginsFunnel({
  origins,
  selected,
  onSelect,
}: {
  origins: AnalyticsOrigin[];
  selected: string;
  onSelect: (channel: string) => void;
}) {
  if (origins.length === 0) return <p className={styles.muted}>Nada no período.</p>;
  const total = origins.reduce((sum, o) => sum + o.visitors, 0);
  return (
    <ul className={styles.ranking}>
      {origins.map((o) => {
        const active = o.channel === selected;
        return (
          <li key={o.channel}>
            <button
              type="button"
              className={`${styles.row} ${styles.originRow} ${active ? styles.originActive : ''}`}
              aria-pressed={active}
              onClick={() => onSelect(active ? '' : o.channel)}
            >
              <span className={styles.bar} style={{ width: total > 0 ? `${Math.min(100, (o.visitors / total) * 100)}%` : 0 }} />
              <span className={styles.originHead}>
                <span className={styles.rowLabel}>{channelLabel(o.channel)}</span>
                <span className={styles.rowValue}>
                  {o.visitors.toLocaleString('pt-BR')}
                  <span className={styles.muted}> · {percent(o.visitors, total)}</span>
                </span>
              </span>
              <span className={styles.originSteps}>
                {plural(o.leadOpens, 'abriu o pré-cadastro', 'abriram o pré-cadastro')} ·{' '}
                {plural(o.leads, 'pré-cadastro', 'pré-cadastros')} · {o.waitlist.toLocaleString('pt-BR')} na lista ·{' '}
                <strong>{percent(o.converted, o.visitors)}</strong> viraram contato
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}

interface Row {
  label: string;
  visitors: number;
  extra?: string;
}

function labeled(list: AnalyticsCount[], names: Record<string, string> = {}): Row[] {
  return list.map((c) => ({ label: names[c.key] ?? (c.key || 'Outro'), visitors: c.visitors }));
}

/** Linhas com a barra proporcional ao total de visitantes. */
function Ranking({ rows, total, empty = 'Nada no período.' }: { rows: Row[]; total: number; empty?: string }) {
  if (rows.length === 0) return <p className={styles.muted}>{empty}</p>;
  return (
    <ul className={styles.ranking}>
      {rows.map((row) => (
        <li key={row.label} className={styles.row}>
          <span className={styles.bar} style={{ width: total > 0 ? `${Math.min(100, (row.visitors / total) * 100)}%` : 0 }} />
          <span className={styles.rowLabel}>{row.label}</span>
          <span className={styles.rowValue}>
            {row.visitors.toLocaleString('pt-BR')}
            <span className={styles.muted}> · {percent(row.visitors, total)}</span>
            {row.extra && <span className={styles.muted}> · {row.extra}</span>}
          </span>
        </li>
      ))}
    </ul>
  );
}

/** Barras dos visitantes de cada dia (a dica mostra o dia e as visualizações). */
function DailyChart({ days }: { days: LandingAnalytics['days'] }) {
  const max = Math.max(1, ...days.map((d) => d.visitors));
  const width = 100 / days.length;
  return (
    <div className={styles.chart}>
      <span className={styles.chartMax}>{max}</span>
      <svg className={styles.chartSvg} viewBox="0 0 100 40" preserveAspectRatio="none" role="img" aria-label="Visitantes por dia">
        {days.map((d, i) => {
          const h = (d.visitors / max) * 38;
          return (
            <rect
              key={d.day}
              className={styles.chartBar}
              x={i * width + width * 0.15}
              y={40 - h}
              width={width * 0.7}
              height={Math.max(h, d.visitors > 0 ? 0.6 : 0)}
            >
              <title>
                {formatDay(d.day)}: {d.visitors} {d.visitors === 1 ? 'visitante' : 'visitantes'}, {d.pageviews}{' '}
                {d.pageviews === 1 ? 'visualização' : 'visualizações'}
              </title>
            </rect>
          );
        })}
      </svg>
      <div className={styles.chartAxis}>
        <span>{formatDay(days[0]?.day)}</span>
        <span>{formatDay(days[days.length - 1]?.day)}</span>
      </div>
    </div>
  );
}

/** "2026-10-04" → "04/10". */
function formatDay(day: string | undefined): string {
  if (!day) return '';
  const [, month, d] = day.split('-');
  return `${d}/${month}`;
}

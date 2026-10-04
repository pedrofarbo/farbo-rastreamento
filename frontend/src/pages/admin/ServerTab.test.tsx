// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { formatBytes } from '@/services/format';
import type { InfraStatus, SystemLogPage } from '@/types';

const now = new Date();
const GB = 1024 ** 3;
const STATUS: InfraStatus = {
  host: {
    at: now.toISOString(), cpuPercent: 12.5, cores: 2, load1: 0.4, load5: 0.3, load15: 0.2,
    memUsed: 6 * GB, memTotal: 8 * GB, diskUsed: 46 * GB, diskTotal: 50 * GB, hostUptime: 3 * 86400 + 7200,
  },
  history: [
    { at: new Date(now.getTime() - 120_000).toISOString(), cpu: 10, mem: 70, disk: 92 },
    { at: new Date(now.getTime() - 60_000).toISOString(), cpu: 15, mem: 75, disk: 92 },
  ],
  process: { startedAt: new Date(now.getTime() - 3_700_000).toISOString(), goVersion: 'go1.27', goroutines: 42, heapBytes: 20e6, rssBytes: 60e6 },
  database: {
    enabled: true, ok: true, latencyMs: 0.8, sizeBytes: 300e6, connections: 7, maxConnections: 100,
    tables: [{ name: 'positions', sizeBytes: 200e6, rows: 1_200_000 }],
  },
  redis: { enabled: false, ok: false, latencyMs: 0 },
  backups: {
    available: true, count: 14, totalBytes: 2e9,
    latest: { name: 'tracker-x.dump', at: new Date(now.getTime() - 30 * 3_600_000).toISOString(), sizeBytes: 150e6 },
  },
  logs: { errors24h: 3, warnings24h: 5, dropped: 0, failed: 0 },
  live: { trackerConnections: 9, realtimeClients: 4, onlineDevices: 8 },
};
const LOGS: SystemLogPage = {
  groups: [{ level: 'ERROR', component: 'pagamentos', message: 'falha ao gerar o Pix', count: 3, lastAt: now.toISOString() }],
  entries: [
    { id: 1, at: now.toISOString(), level: 'ERROR', component: 'pagamentos', message: 'falha ao gerar o Pix', attrs: { fatura: 'f-1' } },
  ],
};

const asked: unknown[] = [];
vi.mock('@/api/resources', () => ({
  infraApi: {
    status: async () => STATUS,
    logs: async (q: unknown) => {
      asked.push(q);
      return LOGS;
    },
  },
}));

import { backupTone, formatUptime, ServerTab, toneOf, LIMITS } from './ServerTab';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

afterEach(() => {
  document.body.innerHTML = '';
  asked.length = 0;
});

describe('regras do painel', () => {
  it('faixas de alerta e atraso do backup', () => {
    expect(toneOf(50, LIMITS.disk)).toBe('ok');
    expect(toneOf(85, LIMITS.disk)).toBe('warn');
    expect(toneOf(92, LIMITS.disk)).toBe('danger');
    const t = Date.parse('2026-10-04T12:00:00Z');
    expect(backupTone('2026-10-04T03:00:00Z', t)).toBe('ok');
    expect(backupTone('2026-10-03T06:00:00Z', t)).toBe('warn');
    expect(backupTone('2026-10-02T03:00:00Z', t)).toBe('danger');
    expect(backupTone(null, t)).toBe('danger');
  });

  it('tempo no ar e tamanhos', () => {
    expect(formatUptime(3 * 86400 + 7200)).toBe('3 dias e 2 h');
    expect(formatUptime(3700)).toBe('1 h 1 min');
    expect(formatUptime(30)).toBe('1 min');
    expect(formatBytes(1536)).toBe('1,5 KB');
    expect(formatBytes(50 * GB)).toBe('50,0 GB');
    expect(formatBytes(512)).toBe('512 B');
  });
});

describe('ServerTab', () => {
  it('mostra máquina, banco, backup atrasado, conexões e os erros', async () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <ServerTab />
        </QueryClientProvider>,
      ),
    );
    await flush();
    await flush();
    const text = host.textContent ?? '';
    for (const part of ['CPU13%', 'carga 0,40 / 0,30 / 0,20', 'Memória75%', 'Disco92%', '4,0 GB livres de 50,0 GB', 'Respondendo em 0,8 ms', '7 de 100', 'positions',
      '14 · 1,9 GB', 'Rastreadores conectados9', '3 erros e 5 avisos', 'falha ao gerar o Pix', '3×']) {
      expect(text).toContain(part);
    }
    // O disco a 92% está no vermelho; o backup de 30 h, atrasado.
    expect(host.querySelector('[aria-label="Disco"]')?.getAttribute('aria-valuenow')).toBe('92');
    expect(text).toContain('Sem um recente, confira o contêiner postgres-backup');

    // Por padrão, os erros das últimas 24 h; trocar o nível refaz a busca.
    expect(asked[0]).toEqual({ hours: 24, level: 'ERROR', search: '' });
    const all = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'Todos') as HTMLButtonElement;
    await act(async () => all.click());
    await flush();
    expect(asked.at(-1)).toEqual({ hours: 24, level: '', search: '' });
  });
});

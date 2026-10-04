import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';

import { diagnosticsApi } from '@/api/resources';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { formatDateTime, formatRelative } from '@/services/format';

import { ServerTab } from './admin/ServerTab';
import styles from './Page.module.css';

type Tab = 'server' | 'protocols' | 'raw' | 'connections' | 'audit';

const CONFIDENCE_CLASS: Record<string, string> = {
  DOCUMENTED: styles.documented,
  ASSUMED: styles.assumed,
  UNKNOWN: styles.unknown,
};

export function DiagnosticsPage() {
  const [tab, setTab] = useState<Tab>('server');

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Diagnóstico</h1>
            <p className={styles.description}>
              A saúde do servidor (máquina, banco, backups e erros) e o que ele sabe sobre os
              protocolos, as conexões abertas e o tráfego que não conseguiu interpretar.
            </p>
          </div>
        </header>

        <div className={styles.tabs}>
          <TabButton current={tab} value="server" onSelect={setTab}>
            Servidor
          </TabButton>
          <TabButton current={tab} value="protocols" onSelect={setTab}>
            Protocolos
          </TabButton>
          <TabButton current={tab} value="raw" onSelect={setTab}>
            Pacotes não interpretados
          </TabButton>
          <TabButton current={tab} value="connections" onSelect={setTab}>
            Conexões
          </TabButton>
          <TabButton current={tab} value="audit" onSelect={setTab}>
            Auditoria
          </TabButton>
        </div>

        {tab === 'server' && <ServerTab />}
        {tab === 'protocols' && <ProtocolsTab />}
        {tab === 'raw' && <RawPacketsTab />}
        {tab === 'connections' && <ConnectionsTab />}
        {tab === 'audit' && <AuditTab />}
      </div>
    </div>
  );
}

function TabButton({
  current,
  value,
  onSelect,
  children,
}: {
  current: Tab;
  value: Tab;
  onSelect: (tab: Tab) => void;
  children: string;
}) {
  return (
    <button
      type="button"
      className={`${styles.tab} ${current === value ? styles.tabActive : ''}`}
      onClick={() => onSelect(value)}
    >
      {children}
    </button>
  );
}

function ProtocolsTab() {
  const protocols = useQuery({ queryKey: ['protocols'], queryFn: diagnosticsApi.protocols });

  return (
    <Card flush>
      <div className={styles.note}>
        <strong>DOCUMENTED</strong> significa formato conferido contra a especificação do
        protocolo. <strong>ASSUMED</strong> é gramática inferida, ainda não confirmada contra o
        firmware — confira antes de operar o corte de motor por ela. <strong>UNKNOWN</strong> é
        apenas modo de captura: não produz posição nem aceita comando.
      </div>

      {protocols.isLoading ? (
        <Spinner inline />
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>Protocolo</th>
                <th>Confiança</th>
                <th>Comandos</th>
                <th>Observações</th>
              </tr>
            </thead>
            <tbody>
              {(protocols.data ?? []).map((protocol) => (
                <tr key={protocol.name}>
                  <td>
                    <div>{protocol.label}</div>
                    <div className={styles.mono}>{protocol.name}</div>
                  </td>
                  <td>
                    <span
                      className={`${styles.confidence} ${CONFIDENCE_CLASS[protocol.confidence] ?? ''}`}
                    >
                      {protocol.confidence}
                    </span>
                  </td>
                  <td>{protocol.commands?.length ? protocol.commands.length : 'nenhum'}</td>
                  <td style={{ maxWidth: 360, fontSize: 'var(--text-xs)' }}>{protocol.notes}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

function RawPacketsTab() {
  const packets = useQuery({
    queryKey: ['raw-packets'],
    queryFn: () => diagnosticsApi.rawPackets(100),
    refetchInterval: 15_000,
  });

  return (
    <Card flush>
      <div className={styles.note}>{packets.data?.hint}</div>

      {packets.isLoading ? (
        <Spinner inline />
      ) : (packets.data?.packets ?? []).length === 0 ? (
        <EmptyState
          icon="✓"
          title="Nenhum pacote sem interpretação"
          description="Todo o tráfego recebido até agora foi reconhecido por algum protocolo."
        />
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>Quando</th>
                <th>Origem</th>
                <th>Motivo</th>
                <th>Conteúdo</th>
              </tr>
            </thead>
            <tbody>
              {(packets.data?.packets ?? []).map((packet) => (
                <tr key={packet.id}>
                  <td style={{ whiteSpace: 'nowrap' }}>{formatDateTime(packet.receivedAt)}</td>
                  <td className={styles.mono}>
                    {packet.remoteAddr}
                    {packet.imei && <div>IMEI {packet.imei}</div>}
                    {packet.protocol && <div>{packet.protocol}</div>}
                  </td>
                  <td style={{ fontSize: 'var(--text-xs)', maxWidth: 220 }}>{packet.reason}</td>
                  <td>
                    <div className={styles.ascii}>{packet.payloadAscii}</div>
                    <div className={styles.hexdump}>{packet.payloadHex}</div>
                    <div style={{ fontSize: 'var(--text-xs)', color: 'var(--text-muted)' }}>
                      {packet.byteCount} bytes
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

function ConnectionsTab() {
  const connections = useQuery({
    queryKey: ['connections'],
    queryFn: diagnosticsApi.connections,
    refetchInterval: 10_000,
  });

  return (
    <Card flush title={`${connections.data?.count ?? 0} sessões abertas`}>
      {connections.isLoading ? (
        <Spinner inline />
      ) : (connections.data?.connections ?? []).length === 0 ? (
        <EmptyState icon="🔌" title="Nenhum rastreador conectado agora" />
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>IMEI</th>
                <th>Protocolo</th>
                <th>Origem</th>
                <th>Conectado</th>
                <th>Último pacote</th>
              </tr>
            </thead>
            <tbody>
              {(connections.data?.connections ?? []).map((connection) => (
                <tr key={connection.imei}>
                  <td className={styles.mono}>{connection.imei}</td>
                  <td className={styles.mono}>{connection.protocol}</td>
                  <td className={styles.mono}>{connection.remoteAddr}</td>
                  <td>{formatRelative(connection.connectedAt)}</td>
                  <td>{formatRelative(connection.lastSeenAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

function AuditTab() {
  const logs = useQuery({
    queryKey: ['audit-logs'],
    queryFn: () => diagnosticsApi.auditLogs(200),
  });

  return (
    <Card flush>
      <div className={styles.note}>
        Toda ação que chega ao veículo é registrada aqui, inclusive as recusadas pela regra de
        segurança.
      </div>

      {logs.isLoading ? (
        <Spinner inline />
      ) : (logs.data ?? []).length === 0 ? (
        <EmptyState icon="📋" title="Sem registros de auditoria" />
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>Quando</th>
                <th>Ação</th>
                <th>Resultado</th>
                <th>Origem</th>
                <th>Detalhes</th>
              </tr>
            </thead>
            <tbody>
              {(logs.data ?? []).map((entry) => (
                <tr key={entry.id}>
                  <td style={{ whiteSpace: 'nowrap' }}>{formatDateTime(entry.createdAt)}</td>
                  <td className={styles.mono}>{entry.action}</td>
                  <td>{entry.result}</td>
                  <td className={styles.mono}>{entry.ipAddress || '—'}</td>
                  <td className={styles.hexdump}>{JSON.stringify(entry.metadata)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

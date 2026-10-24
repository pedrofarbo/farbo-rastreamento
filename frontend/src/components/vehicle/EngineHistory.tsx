import { useQuery } from '@tanstack/react-query';

import { vehiclesApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import type { BadgeTone } from '@/components/ui/Badge';
import { Spinner } from '@/components/ui/Spinner';
import { formatDateTime } from '@/services/format';
import { deviceLabel } from '@/services/userAgent';
import type { EngineHistoryItem } from '@/types';

import styles from './EngineHistory.module.css';

/** O resultado do comando, como o cliente entende. */
function outcome(item: EngineHistoryItem): { label: string; tone: BadgeTone } {
  switch (item.status) {
    case 'ACKNOWLEDGED':
      return { label: 'confirmado pelo rastreador', tone: 'success' };
    case 'TIMEOUT':
      return { label: 'sem resposta do rastreador', tone: 'warning' };
    case 'FAILED':
      return { label: 'não enviado', tone: 'danger' };
    case 'REJECTED':
      return { label: 'recusado', tone: 'danger' };
    default:
      return { label: 'aguardando o rastreador', tone: 'neutral' };
  }
}

/** De onde e como: "iPhone · Safari (iOS 26.6) · confirmado com a senha". */
export function originLine(item: EngineHistoryItem): string {
  if (item.whoKind === 'central' || item.whoKind === 'system') return '';
  // O aparelho de quem é da associação não vai para o associado.
  const parts: string[] = [];
  if (item.userAgent) parts.push(deviceLabel(item.userAgent));
  else if (item.whoKind !== 'organization') parts.push('aparelho não registrado');
  if (item.confirmedWith === 'password') parts.push('confirmado com a senha');
  if (item.confirmedWith === 'biometric') parts.push('confirmado com a biometria');
  return parts.join(' · ');
}

/**
 * O rastreio de bloqueio e desbloqueio do veículo: quando, quem pediu, de
 * qual aparelho, como confirmou e o que o rastreador respondeu.
 */
export function EngineHistory({ vehicleId }: { vehicleId: string }) {
  // Na chave dos comandos: o painel de comandos atualiza depois de enviar e
  // quando o rastreador responde.
  const history = useQuery({
    queryKey: ['commands', vehicleId, 'engine-history'],
    queryFn: () => vehiclesApi.engineHistory(vehicleId),
  });

  if (history.isLoading) return <Spinner label="Carregando bloqueios" />;
  if (history.isError) return <p className={styles.muted}>Não foi possível carregar os bloqueios.</p>;
  const list = history.data ?? [];
  if (list.length === 0) return <p className={styles.muted}>Nenhum bloqueio ou desbloqueio ainda.</p>;

  return (
    <ul className={styles.list}>
      {list.map((item) => {
        const result = outcome(item);
        const origin = originLine(item);
        return (
          <li key={item.id} className={styles.item}>
            <div className={styles.head}>
              <span className={styles.action}>{item.command === 'ENGINE_CUT' ? '🔒 Bloqueio' : '🔓 Desbloqueio'}</span>
              <Badge tone={result.tone}>{result.label}</Badge>
            </div>
            <span className={styles.line}>
              {formatDateTime(item.requestedAt)} · {item.who}
              {item.whoKind === 'shared' ? ' (acesso compartilhado)' : ''}
            </span>
            {origin && <span className={styles.muted}>{origin}</span>}
            {item.status === 'REJECTED' && item.error && <span className={styles.muted}>{item.error}</span>}
          </li>
        );
      })}
    </ul>
  );
}

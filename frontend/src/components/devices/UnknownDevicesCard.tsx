import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { deviceInputFrom, devicesApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { useToast } from '@/components/ui/Toast';
import { formatRelative } from '@/services/format';
import type { Device, UnknownAttempt } from '@/types';

import styles from '@/pages/Page.module.css';

export const unknownDevicesKey = ['devices', 'unknown'];

/**
 * Os rastreadores que tentam conectar e são recusados por não estarem
 * cadastrados. Quase sempre é o IMEI digitado errado no cadastro, com o
 * rastreador já configurado: para esses, o cadastro parecido aparece com o
 * botão que corrige o IMEI. Some quando não há nenhum.
 */
export function UnknownDevicesCard({
  devices,
  onRegister,
}: {
  devices: Device[];
  /** Abre o cadastro de um rastreador novo com este IMEI. */
  onRegister: (imei: string) => void;
}) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const unknown = useQuery({ queryKey: unknownDevicesKey, queryFn: devicesApi.unknown, refetchInterval: 60_000 });

  // O IMEI que o rastreador manda é o de verdade: vale mesmo fora do padrão.
  const fix = useMutation({
    mutationFn: ({ device, imei }: { device: Device; imei: string }) =>
      devicesApi.update(device.id, { ...deviceInputFrom(device), imei, imeiConfirmed: true }),
    onSuccess: (_, { device, imei }) => {
      queryClient.invalidateQueries({ queryKey: ['devices'] });
      notify({
        tone: 'success',
        title: 'IMEI corrigido',
        description: `${device.imei} → ${imei}. O rastreador entra na próxima tentativa (em até um minuto).`,
      });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível corrigir', description: err.message }),
  });

  const list = unknown.data ?? [];
  if (list.length === 0) return null;
  const byId = new Map(devices.map((d) => [d.id, d]));

  return (
    <Card
      title="Rastreadores tentando conectar sem cadastro"
      subtitle="O servidor recusa o rastreador cujo IMEI não está cadastrado. Quase sempre é o IMEI digitado errado no cadastro: o rastreador já pegou a configuração e está tentando entrar."
    >
      <div className={styles.tableWrap}>
        <table className={`${styles.table} ${styles.stackTable}`}>
          <thead>
            <tr>
              <th>IMEI que está chegando</th>
              <th>Tentativas</th>
              <th>Cadastro parecido</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {list.map((attempt: UnknownAttempt) => {
              const device = attempt.suggestion ? byId.get(attempt.suggestion.deviceId) : undefined;
              return (
                <tr key={attempt.imei}>
                  <td data-label="IMEI que está chegando">
                    <code>{attempt.imei}</code>
                    <div className={billing.muted}>{attempt.protocol || 'protocolo ?'}</div>
                  </td>
                  <td data-label="Tentativas">
                    {attempt.attempts} · última {formatRelative(attempt.lastSeenAt)}
                  </td>
                  <td data-label="Cadastro parecido">
                    {attempt.suggestion ? (
                      <>
                        <code>{attempt.suggestion.imei}</code>
                        <div className={billing.muted}>provável erro de digitação no cadastro</div>
                      </>
                    ) : (
                      <span className={billing.muted}>nenhum</span>
                    )}
                  </td>
                  <td data-label="">
                    {device ? (
                      <Button
                        size="small"
                        variant="primary"
                        loading={fix.isPending && fix.variables?.imei === attempt.imei}
                        onClick={() => {
                          if (
                            window.confirm(
                              `Corrigir o IMEI do cadastro de ${device.imei} para ${attempt.imei}, o que o rastreador está mandando?`,
                            )
                          ) {
                            fix.mutate({ device, imei: attempt.imei });
                          }
                        }}
                      >
                        Corrigir para este IMEI
                      </Button>
                    ) : (
                      <Button size="small" onClick={() => onRegister(attempt.imei)}>
                        Cadastrar
                      </Button>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

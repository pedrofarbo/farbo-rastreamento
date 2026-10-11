import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { meApi } from '@/api/resources';
import { AlertSettingsPanel } from '@/components/alerts/AlertSettingsPanel';
import { ExtraRecipients } from '@/components/alerts/ExtraRecipients';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import type { AlertSettings, AlertSettingsInput } from '@/types';

import styles from '../Page.module.css';

const alertsKey = ['me', 'alerts'] as const;

/** Alertas por e-mail do cliente: o que receber, horário de vigilância e histórico. */
export function AlertsPage() {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const settings = useQuery({ queryKey: alertsKey, queryFn: meApi.alerts });

  const update = (data: AlertSettings) => queryClient.setQueryData(alertsKey, data);
  const save = useMutation({
    mutationFn: (input: AlertSettingsInput) => meApi.saveAlerts(input),
    onSuccess: (data) => {
      update(data);
      notify({ tone: 'success', title: 'Alertas salvos' });
    },
    onError: (error: Error) => notify({ tone: 'error', title: 'Não foi possível salvar', description: error.message }),
  });
  const test = useMutation({
    mutationFn: () => meApi.testAlerts(),
    onSuccess: (data) => {
      update(data);
      notify({ tone: 'success', title: 'E-mail de teste enviado', description: `Confira a caixa de ${data.email}.` });
    },
    onError: (error: Error) => notify({ tone: 'error', title: 'E-mail de teste não saiu', description: error.message }),
  });

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Alertas</h1>
            <p className={styles.description}>
              Avisamos por e-mail quando algo importante acontece com os seus veículos — botão de pânico, bateria
              desconectada, movimento com a ignição desligada, ignição ligada de madrugada e mais. Escolha o que
              receber.
            </p>
          </div>
        </header>

        {settings.isLoading ? (
          <Spinner label="Carregando alertas" />
        ) : settings.data ? (
          <AlertSettingsPanel
            settings={settings.data}
            audience="customer"
            saving={save.isPending}
            onSave={(input) => save.mutate(input)}
            onTest={() => test.mutate()}
            testing={test.isPending}
          />
        ) : (
          <EmptyState title="Não foi possível carregar os alertas" description={settings.error?.message} />
        )}
        {settings.data && (settings.data.canEditRecipients || settings.data.extraRecipients.length > 0) && (
          <Card title="Também avisar">
            <ExtraRecipients settings={settings.data} queryKey={alertsKey} />
          </Card>
        )}
      </div>
    </div>
  );
}

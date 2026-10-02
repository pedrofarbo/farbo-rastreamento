import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';

import { geofencesApi, meApi } from '@/api/resources';
import { AlertSettingsPanel } from '@/components/alerts/AlertSettingsPanel';
import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import type { AlertSettings, AlertSettingsInput } from '@/types';

import { currentSubscription, pushSupport, subscribePush, unsubscribePush } from '../pwa';
import { fencesKey } from './FencesScreen';
import styles from './Screen.module.css';

const pushKey = ['me', 'push'] as const;
const alertsKey = ['me', 'alerts'] as const;

/** Liga/desliga as notificações neste aparelho. */
function PushCard() {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const status = useQuery({ queryKey: pushKey, queryFn: meApi.push });
  const support = pushSupport();
  const [endpoint, setEndpoint] = useState<string | null>(null);
  const [checked, setChecked] = useState(false);

  useEffect(() => {
    currentSubscription()
      .then((sub) => setEndpoint(sub?.endpoint ?? null))
      .finally(() => setChecked(true));
  }, []);

  const refresh = () => queryClient.invalidateQueries({ queryKey: pushKey });
  const enable = useMutation({
    mutationFn: async () => {
      const subscription = await subscribePush(status.data!.publicKey);
      await meApi.subscribePush(subscription);
      return subscription.endpoint ?? null;
    },
    onSuccess: (value) => {
      setEndpoint(value);
      refresh();
      notify({ tone: 'success', title: 'Notificações ligadas neste celular' });
    },
    onError: (error: Error) => notify({ tone: 'error', title: 'Não foi possível ligar', description: error.message }),
  });
  const disable = useMutation({
    mutationFn: async () => {
      const value = (await unsubscribePush()) ?? endpoint;
      if (value) await meApi.unsubscribePush(value);
    },
    onSuccess: () => {
      setEndpoint(null);
      refresh();
      notify({ tone: 'success', title: 'Notificações desligadas neste celular' });
    },
    onError: (error: Error) => notify({ tone: 'error', title: 'Não foi possível desligar', description: error.message }),
  });
  const test = useMutation({
    mutationFn: meApi.testPush,
    onSuccess: ({ delivered }) =>
      notify({
        tone: 'success',
        title: 'Notificação de teste enviada',
        description: delivered === 1 ? 'Deve chegar em instantes.' : `Enviada para ${delivered} aparelhos.`,
      }),
    onError: (error: Error) => notify({ tone: 'error', title: 'Teste não enviado', description: error.message }),
  });

  const devices = status.data?.devices ?? [];
  const here = Boolean(endpoint && devices.some((d) => d.endpoint === endpoint));
  const denied = support === 'supported' && typeof Notification !== 'undefined' && Notification.permission === 'denied';

  let body;
  if (!status.data || !checked) {
    body = <Spinner label="Verificando notificações" />;
  } else if (!status.data.enabled) {
    body = <p className={styles.muted}>As notificações no celular estão desligadas pela central.</p>;
  } else if (support === 'ios-needs-install') {
    body = (
      <p className={styles.muted}>
        No iPhone, as notificações funcionam com o app instalado: toque em <strong>Compartilhar</strong> e depois em{' '}
        <strong>Adicionar à Tela de Início</strong>. Abra o app pelo ícone e volte aqui (iOS 16.4 ou mais novo).
      </p>
    );
  } else if (support === 'unsupported') {
    body = <p className={styles.muted}>Este navegador não recebe notificações. Os alertas continuam chegando por e-mail.</p>;
  } else if (denied) {
    body = (
      <p className={styles.muted}>
        As notificações deste site estão bloqueadas. Libere nas configurações do navegador (ícone de cadeado ao lado do
        endereço) e toque em ativar de novo.
      </p>
    );
  } else if (here) {
    body = (
      <>
        <p className={styles.good}>Ligadas neste celular.</p>
        <div className={styles.row}>
          <Button variant="secondary" onClick={() => test.mutate()} loading={test.isPending}>
            Enviar teste
          </Button>
          <Button variant="ghost" onClick={() => disable.mutate()} loading={disable.isPending}>
            Desligar
          </Button>
        </div>
      </>
    );
  } else {
    body = (
      <>
        <p className={styles.muted}>
          Receba SOS, bateria desconectada, movimento com a ignição desligada e os outros alertas na hora, mesmo com o app
          fechado.
        </p>
        <Button onClick={() => enable.mutate()} loading={enable.isPending} block>
          Ativar notificações neste celular
        </Button>
      </>
    );
  }

  return (
    <section className={styles.section} aria-label="Notificações no celular">
      <h2 className={styles.sectionTitle}>Notificações no celular</h2>
      {body}
      {devices.length > 0 && (
        <p className={styles.muted}>
          {devices.length === 1 ? '1 aparelho recebe' : `${devices.length} aparelhos recebem`} as notificações da sua conta.
        </p>
      )}
    </section>
  );
}

/** Atalho para as cercas: aviso de entrada e saída de um lugar. */
function FencesCard() {
  const navigate = useNavigate();
  const fences = useQuery({ queryKey: fencesKey, queryFn: geofencesApi.list });
  const count = fences.data?.length ?? 0;
  return (
    <section className={styles.section} aria-label="Cercas">
      <h2 className={styles.sectionTitle}>Cercas</h2>
      <p className={styles.muted}>
        {count === 0
          ? 'Desenhe um círculo em volta de casa, do trabalho ou da escola e saiba quando o veículo chega ou sai.'
          : count === 1
            ? '1 cerca avisa quando o veículo entra ou sai.'
            : `${count} cercas avisam quando o veículo entra ou sai.`}
      </p>
      <Button
        variant={count === 0 ? 'primary' : 'secondary'}
        onClick={() => navigate(count === 0 ? '/cercas/nova' : '/cercas')}
        disabled={fences.isLoading}
        block
      >
        {count === 0 ? 'Criar cerca' : 'Ver cercas'}
      </Button>
    </section>
  );
}

/** Alertas: notificações no celular e as escolhas que valem para e-mail e celular. */
export function AlertsScreen() {
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
  const testEmail = useMutation({
    mutationFn: () => meApi.testAlerts(),
    onSuccess: (data) => {
      update(data);
      notify({ tone: 'success', title: 'E-mail de teste enviado', description: `Confira a caixa de ${data.email}.` });
    },
    onError: (error: Error) => notify({ tone: 'error', title: 'E-mail de teste não saiu', description: error.message }),
  });

  return (
    <div className={styles.screen}>
      <div>
        <h1 className={styles.title}>Alertas</h1>
        <p className={styles.lead}>Os alertas escolhidos abaixo chegam por e-mail e, com as notificações ligadas, neste celular.</p>
      </div>
      <PushCard />
      <FencesCard />
      {settings.isLoading ? (
        <Spinner label="Carregando alertas" />
      ) : settings.data ? (
        <AlertSettingsPanel
          settings={settings.data}
          audience="customer"
          saving={save.isPending}
          onSave={(input) => save.mutate(input)}
          onTest={() => testEmail.mutate()}
          testing={testEmail.isPending}
        />
      ) : null}
    </div>
  );
}

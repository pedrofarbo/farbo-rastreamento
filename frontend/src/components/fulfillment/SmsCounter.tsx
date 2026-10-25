import { useQuery } from '@tanstack/react-query';

import { smsSetupApi } from '@/api/resources';

import styles from './SmsCounter.module.css';

/** Abaixo disso, o contador avisa para recarregar. */
const LOW_ACTIVATIONS = 10;

/** O painel da SMS Market, onde se compra mais SMS. */
const SMS_PANEL = 'https://dashboard.smsmarket.com.br';

const number = (n: number) => n.toLocaleString('pt-BR');

/**
 * O contador de SMS da configuração por SMS: o saldo na SMS Market, quantos
 * rastreadores ele ativa (2 SMS cada: o resto vai pela conexão) e o que saiu
 * nos últimos 30 dias.
 */
export function SmsCounter() {
  const usage = useQuery({
    queryKey: ['sms', 'usage'],
    queryFn: smsSetupApi.usage,
    staleTime: 60_000,
    refetchInterval: 5 * 60_000,
  });
  const u = usage.data;
  if (!u) return null;

  if (!u.enabled) {
    return (
      <section className={styles.counter} aria-label="SMS da ativação">
        <p className={styles.off}>
          SMS da ativação desligado: o servidor está sem o usuário e a senha da SMS Market (SMSMARKET_USER e SMSMARKET_PASSWORD).
        </p>
      </section>
    );
  }

  const low = u.balance !== null && u.activations < LOW_ACTIVATIONS;
  return (
    <section className={styles.counter} aria-label="SMS da ativação">
      <div className={styles.stat}>
        <span className={styles.label}>Saldo na SMS Market</span>
        <span className={styles.value}>{u.balance === null ? '—' : `${number(u.balance)} SMS`}</span>
        {u.balanceError && <span className={styles.hint}>Não deu para ler o saldo: {u.balanceError}</span>}
      </div>
      <div className={styles.stat}>
        <span className={styles.label}>Dá para ativar</span>
        <span className={`${styles.value} ${styles.highlight}`}>
          {u.balance === null ? '—' : `${number(u.activations)} ${u.activations === 1 ? 'rastreador' : 'rastreadores'}`}
        </span>
        <span className={styles.hint}>
          {u.activationSms} SMS por ativação ({u.activationSms + 1} destravando o canal de comandos)
        </span>
      </div>
      <div className={styles.stat}>
        <span className={styles.label}>Enviados em 30 dias</span>
        <span className={styles.value}>{number(u.sentLast30Days)} SMS</span>
      </div>
      <a className={styles.link} href={SMS_PANEL} target="_blank" rel="noopener noreferrer">
        Comprar SMS
      </a>
      {low && (
        <p className={styles.low}>
          Saldo baixo: {u.activations === 0 ? 'não dá para ativar nenhum rastreador' : `dá para só ${u.activations}`}.
          Recarregue no painel da SMS Market.
        </p>
      )}
    </section>
  );
}

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';

import { meApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import type { AppNotification, NotificationInbox } from '@/types';

import styles from './Screen.module.css';
import local from './Notifications.module.css';

export const notificationsKey = ['me', 'notifications'] as const;

/** A notificação leva para uma tela do app: "/app/veiculos/x" → "/veiculos/x". */
export function appPath(url: string): string | null {
  if (url === '/app' || url === '/app/') return '/';
  return url.startsWith('/app/') ? url.slice(4) : null;
}

const dayFormat = new Intl.DateTimeFormat('pt-BR', { weekday: 'short', day: 'numeric', month: 'short' });
const timeFormat = new Intl.DateTimeFormat('pt-BR', { hour: '2-digit', minute: '2-digit' });

/** "Hoje", "Ontem" ou "qua., 14 de out." — o dia em que a notificação chegou. */
export function dayLabel(value: string, now = new Date()): string {
  const date = new Date(value);
  const start = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((start(now) - start(date)) / 86_400_000);
  if (days === 0) return 'Hoje';
  if (days === 1) return 'Ontem';
  return dayFormat.format(date);
}

/** As notificações agrupadas por dia, na ordem em que chegam (da mais nova). */
export function byDay(items: AppNotification[], now = new Date()): { day: string; items: AppNotification[] }[] {
  const groups: { day: string; items: AppNotification[] }[] = [];
  for (const item of items) {
    const day = dayLabel(item.createdAt, now);
    const last = groups[groups.length - 1];
    if (last && last.day === day) last.items.push(item);
    else groups.push({ day, items: [item] });
  }
  return groups;
}

/**
 * A área de notificações: os alertas dos veículos, os lembretes de fatura e
 * os avisos da conta, chegue ou não a notificação no celular. Tocar abre o
 * que ela avisa e a marca como lida.
 */
export function NotificationsScreen() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const inbox = useQuery({ queryKey: notificationsKey, queryFn: meApi.notifications, refetchInterval: 60_000 });

  // Marca na hora (a lista e o número da aba), sem esperar o servidor.
  const markLocally = (id?: number) =>
    queryClient.setQueryData<NotificationInbox>(notificationsKey, (data) => {
      if (!data) return data;
      const now = new Date().toISOString();
      let marked = 0;
      const items = data.items.map((n) => {
        if (n.readAt || (id !== undefined && n.id !== id)) return n;
        marked++;
        return { ...n, readAt: now };
      });
      return { items, unread: id === undefined ? 0 : Math.max(0, data.unread - marked) };
    });
  const read = useMutation({
    mutationFn: (id?: number) => meApi.readNotifications(id),
    onMutate: (id) => markLocally(id),
    onError: (error: Error) => {
      void queryClient.invalidateQueries({ queryKey: notificationsKey });
      notify({ tone: 'error', title: 'Não foi possível marcar como lida', description: error.message });
    },
  });

  const open = (n: AppNotification) => {
    if (!n.readAt) read.mutate(n.id);
    const path = appPath(n.url);
    if (path) navigate(path);
  };

  const data = inbox.data;
  const unread = data?.unread ?? 0;

  return (
    <div className={styles.screen}>
      <div>
        <h1 className={styles.title}>Notificações</h1>
        <p className={styles.lead}>Os alertas dos seus veículos, os lembretes de fatura e os avisos da sua conta.</p>
      </div>
      <div className={styles.row}>
        <Button variant="secondary" onClick={() => navigate('/alertas/configurar')}>
          Configurar alertas
        </Button>
        {unread > 0 && (
          <Button variant="ghost" onClick={() => read.mutate(undefined)}>
            Marcar todas como lidas
          </Button>
        )}
      </div>

      {inbox.isLoading ? (
        <Spinner label="Carregando notificações" />
      ) : !data ? (
        <p className={styles.bad}>Não foi possível carregar as notificações.</p>
      ) : data.items.length === 0 ? (
        <section className={styles.section}>
          <h2 className={styles.sectionTitle}>Nenhuma notificação ainda</h2>
          <p className={styles.muted}>
            Quando um veículo der um alerta (SOS, bateria desconectada, entrada ou saída de cerca…), uma fatura estiver
            para vencer ou algo mudar na sua conta, o aviso aparece aqui.
          </p>
        </section>
      ) : (
        byDay(data.items).map((group) => (
          <section key={group.day} className={local.day} aria-label={group.day}>
            <h2 className={local.dayTitle}>{group.day}</h2>
            <ul className={local.list}>
              {group.items.map((n) => (
                <li key={n.id}>
                  <button
                    type="button"
                    className={`${local.item} ${n.readAt ? '' : local.unread}`}
                    onClick={() => open(n)}
                    aria-label={`${n.readAt ? '' : 'Não lida: '}${n.title}`}
                  >
                    <span className={`${local.mark} ${local[n.severity || 'info'] ?? ''}`} aria-hidden="true" />
                    <span className={local.text}>
                      <span className={local.itemTitle}>{n.title}</span>
                      {n.body && <span className={local.body}>{n.body}</span>}
                    </span>
                    <time className={local.time} dateTime={n.createdAt}>
                      {timeFormat.format(new Date(n.createdAt))}
                    </time>
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ))
      )}
      {data && data.items.length >= 100 && (
        <p className={styles.muted}>Mostrando as 100 mais recentes. As de mais de 90 dias saem sozinhas.</p>
      )}
    </div>
  );
}

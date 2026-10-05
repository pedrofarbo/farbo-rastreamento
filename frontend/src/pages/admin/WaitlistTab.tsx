import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { leadsApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateTime, formatMoney, formatRelative } from '@/services/format';
import type { WaitlistEntry } from '@/types';

import styles from '../Page.module.css';
import { EventQrCard } from './EventQrCard';

/** De onde veio: "Site" ou o evento do QR Code ("encontro-insanos" → "Encontro Insanos"). */
export function eventLabel(event: string): string {
  if (!event) return 'Site';
  return event
    .split('-')
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
}

/** Quantos vieram de cada origem, da que mais trouxe gente. */
export function byOrigin(list: WaitlistEntry[]): { label: string; count: number }[] {
  const counts = new Map<string, number>();
  for (const e of list) counts.set(e.event, (counts.get(e.event) ?? 0) + 1);
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([event, count]) => ({ label: eventLabel(event), count }));
}

/** CSV com ";" e BOM: o Excel em português abre direto, com acentos. */
export function waitlistCsv(list: WaitlistEntry[]): string {
  const cell = (value: string) => `"${value.replace(/"/g, '""')}"`;
  const rows = list.map((e) =>
    [e.name, e.email, e.phone, e.city, eventLabel(e.event), formatDateTime(e.createdAt)].map(cell).join(';'),
  );
  return '\uFEFF' + ['Nome;E-mail;WhatsApp;Cidade da instalação;Origem;Inscrito em', ...rows].join('\r\n');
}

/** Conversa no WhatsApp com o número da inscrição (com o 55 do Brasil). */
function whatsappLink(phone: string): string {
  const d = phone.replace(/\D/g, '');
  return `https://wa.me/${d.length <= 11 ? `55${d}` : d}`;
}

function download(list: WaitlistEntry[]) {
  const blob = new Blob([waitlistCsv(list)], { type: 'text/csv;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = `lista-lancamento-${new Date().toISOString().slice(0, 10)}.csv`;
  link.click();
  URL.revokeObjectURL(url);
}

/**
 * Lista de lançamento: quem pediu, na landing ou no QR Code de um evento,
 * para ser avisado quando a Farbo lançar. Daqui sai a lista do aviso (CSV ou
 * e-mails copiados) e o QR Code dos eventos.
 */
export function WaitlistTab() {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const [removing, setRemoving] = useState<WaitlistEntry | null>(null);

  const waitlist = useQuery({ queryKey: ['leads', 'waitlist'], queryFn: leadsApi.waitlist, refetchInterval: 60_000 });
  const promo = useQuery({ queryKey: ['leads', 'promo'], queryFn: leadsApi.promo, refetchInterval: 60_000 });
  const list = waitlist.data ?? [];
  const usage = promo.data;

  const remove = useMutation({
    mutationFn: (id: string) => leadsApi.removeFromWaitlist(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['leads', 'waitlist'] });
      notify({ tone: 'success', title: 'Removido da lista de lançamento' });
      setRemoving(null);
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível remover', description: err.message }),
  });

  const copyEmails = async () => {
    try {
      await navigator.clipboard.writeText(list.map((e) => e.email).join(', '));
      notify({ tone: 'success', title: `${list.length} e-mail(s) copiados`, description: 'Cole no campo Cco do e-mail.' });
    } catch {
      notify({ tone: 'error', title: 'Não deu para copiar', description: 'Use Baixar CSV.' });
    }
  };

  return (
    <>
      {usage && (
        <div className={styles.note}>
          {usage.enabled ? (
            <>
              <strong>
                Promoção de pré-lançamento: {usage.used} de {usage.slots} vagas usadas.
              </strong>{' '}
              Quem está na lista contrata o primeiro rastreador por {formatMoney(usage.offer.equipmentCents)} e paga{' '}
              {formatMoney(usage.offer.monthlyCents)} de mensalidade ({formatMoney(usage.offer.insanosMonthlyCents)} no plano{' '}
              {usage.offer.insanosPlanName}) nos {usage.offer.months} primeiros meses. A vaga
              é ocupada na contratação (1 veículo por cliente), pelo e-mail da conta.
            </>
          ) : (
            <>
              Promoção de pré-lançamento encerrada ({usage.used} {usage.used === 1 ? 'cliente usou' : 'clientes usaram'}).
            </>
          )}
        </div>
      )}
      <EventQrCard />
      <Card
        flush
        title={`${list.length} ${list.length === 1 ? 'pessoa' : 'pessoas'} esperando o lançamento`}
        subtitle={
          list.length > 0
            ? `Por origem: ${byOrigin(list)
                .map((o) => `${o.label} ${o.count}`)
                .join(' · ')}`
            : 'Inscritas pela landing e pelo QR Code dos eventos.'
        }
        actions={
          list.length > 0 && (
            <div className={styles.actions}>
              <Button size="small" variant="ghost" onClick={() => void copyEmails()}>
                Copiar e-mails
              </Button>
              <Button size="small" variant="secondary" onClick={() => download(list)}>
                Baixar CSV
              </Button>
            </div>
          )
        }
      >
        {waitlist.isLoading ? (
          <Spinner label="Carregando a lista" />
        ) : list.length === 0 ? (
          <EmptyState
            icon="🚀"
            title="Ninguém na lista ainda"
            description="Quem deixa o e-mail em “Seja avisado no lançamento”, na landing, ou se cadastra pelo QR Code de um evento aparece aqui."
          />
        ) : (
          <div className={styles.tableWrap}>
            <table className={`${styles.table} ${styles.stackTable}`}>
              <thead>
                <tr>
                  <th>Nome</th>
                  <th>E-mail</th>
                  <th>WhatsApp</th>
                  <th>Cidade</th>
                  <th>Origem</th>
                  <th>Inscrito</th>
                  <th>Situação</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {list.map((entry) => (
                  <tr key={entry.id}>
                    <td data-label="Nome">{entry.name || <span className={billing.muted}>—</span>}</td>
                    <td data-label="E-mail">
                      <a href={`mailto:${entry.email}`}>{entry.email}</a>
                    </td>
                    <td data-label="WhatsApp">
                      {entry.phone ? (
                        <a href={whatsappLink(entry.phone)} target="_blank" rel="noreferrer">
                          {entry.phone}
                        </a>
                      ) : (
                        <span className={billing.muted}>—</span>
                      )}
                    </td>
                    <td data-label="Cidade">{entry.city || <span className={billing.muted}>—</span>}</td>
                    <td data-label="Origem">
                      {entry.event ? (
                        <Badge tone="accent">{eventLabel(entry.event)}</Badge>
                      ) : (
                        <span className={billing.muted}>Site</span>
                      )}
                    </td>
                    <td data-label="Inscrito" title={formatDateTime(entry.createdAt)}>
                      {formatRelative(entry.createdAt)}
                    </td>
                    <td data-label="Situação">
                      {entry.promoClaimed ? (
                        <Badge tone="success">Usou a promoção</Badge>
                      ) : entry.customerId ? (
                        <Badge tone="accent">Já é cliente</Badge>
                      ) : (
                        <span className={billing.muted}>Na espera</span>
                      )}
                    </td>
                    <td>
                      <Button size="small" variant="ghost" onClick={() => setRemoving(entry)}>
                        Remover
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Modal
        open={removing !== null}
        title="Remover da lista de lançamento"
        onClose={() => setRemoving(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRemoving(null)}>
              Cancelar
            </Button>
            <Button variant="danger" loading={remove.isPending} onClick={() => removing && remove.mutate(removing.id)}>
              Remover
            </Button>
          </>
        }
      >
        {removing && (
          <p>
            <strong>{removing.email}</strong> sai da lista e não recebe o aviso do lançamento. Use quando a pessoa
            pedir para sair.
          </p>
        )}
      </Modal>
    </>
  );
}

import { useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { leadsApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { Badge } from '@/components/ui/Badge';
import type { BadgeTone } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { SelectField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { whatsappUrl } from '@/config/contact';
import { formatDateTime, formatMoney, formatRelative } from '@/services/format';
import { useAuth } from '@/stores/AuthContext';
import type { Lead, LeadStatus, PromoOffer, PromoUsage } from '@/types';

import styles from '../Page.module.css';
import { EventQrCard } from './EventQrCard';
import local from './Leads.module.css';

export const LEAD_STATUS: Record<LeadStatus, { label: string; tone: BadgeTone }> = {
  NEW: { label: 'Novo', tone: 'warning' },
  CONTACTED: { label: 'Em contato', tone: 'accent' },
  CONVERTED: { label: 'Virou cliente', tone: 'success' },
  DISCARDED: { label: 'Descartado', tone: 'neutral' },
};

const VEHICLE_LABELS: Record<string, string> = { moto: 'moto', carro: 'carro', frota: 'frota' };

/** O plano e os veículos do pré-cadastro; vazio para quem só entrou na lista. */
export function interest(lead: Pick<Lead, 'plan' | 'vehicleType' | 'vehicleCount'>): string {
  if (!lead.plan && !lead.vehicleType) return '';
  const type = VEHICLE_LABELS[lead.vehicleType];
  const vehicles = `${lead.vehicleCount} ${lead.vehicleCount === 1 ? 'veículo' : 'veículos'}${type ? ` (${type})` : ''}`;
  return [lead.plan, vehicles].filter(Boolean).join(' · ');
}

/** O nome do evento no link ("encontro-insanos" → "Encontro Insanos"). */
export function eventLabel(event: string): string {
  return event
    .split('-')
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
}

/** De onde veio: a indicação, o evento, o pré-cadastro ou a lista da landing. */
export function originLabel(lead: Pick<Lead, 'referrer' | 'event' | 'source'>): string {
  if (lead.referrer) return `Indicação ${lead.referrer}`;
  if (lead.event) return eventLabel(lead.event);
  return lead.source === 'landing' ? 'Pré-cadastro' : 'Lista (site)';
}

/** Quantos vieram de cada origem, da que mais trouxe gente. */
export function byOrigin(list: Pick<Lead, 'referrer' | 'event' | 'source'>[]): { label: string; count: number }[] {
  const counts = new Map<string, number>();
  for (const l of list) {
    const label = originLabel(l);
    counts.set(label, (counts.get(label) ?? 0) + 1);
  }
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([label, count]) => ({ label, count }));
}

/** CSV com ";" e BOM: o Excel em português abre direto, com acentos. */
export function leadsCsv(list: Lead[]): string {
  const cell = (value: string) => `"${value.replace(/"/g, '""')}"`;
  const rows = list.map((l) =>
    [
      l.name, l.email, l.phone, l.city, originLabel(l), interest(l), l.onLaunchList ? 'Sim' : 'Não',
      LEAD_STATUS[l.status].label, formatDateTime(l.createdAt),
    ]
      .map(cell)
      .join(';'),
  );
  return (
    '﻿' +
    ['Nome;E-mail;WhatsApp;Cidade da instalação;Origem;Interesse;Lista de lançamento;Situação;Recebido em', ...rows].join('\r\n')
  );
}

/** Busca por nome, e-mail, WhatsApp ou cidade, sem acento nem caixa. */
export function matches(lead: Pick<Lead, 'name' | 'email' | 'city' | 'phone'>, search: string): boolean {
  const plain = (s: string) => s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase();
  const q = plain(search.trim());
  if (!q) return true;
  const digits = q.replace(/\D/g, '');
  return (
    [lead.name, lead.email, lead.city].some((v) => plain(v).includes(q)) ||
    (digits.length >= 4 && lead.phone.replace(/\D/g, '').includes(digits))
  );
}

/** Conversa no WhatsApp com o número (com o 55 do Brasil) e, se houver, a mensagem já escrita. */
export function whatsappLink(phone: string, text = ''): string {
  const d = phone.replace(/\D/g, '');
  return whatsappUrl(text, d.length <= 11 ? `55${d}` : d);
}

/** Dá para chamar: o número tem DDD. */
function hasWhatsapp(phone: string): boolean {
  return phone.replace(/\D/g, '').length >= 10;
}

/** "ANA souza" → "Ana". */
function firstName(name: string): string {
  const first = name.trim().split(/\s+/)[0] ?? '';
  return first.charAt(0).toUpperCase() + first.slice(1).toLowerCase();
}

/** Os veículos na frase: "a sua moto", "3 carros", "a sua frota de 8 veículos". */
function vehiclesPhrase(type: string, count: number): string {
  const n = Math.max(count, 1);
  if (type === 'frota') return `a sua frota de ${n} ${n === 1 ? 'veículo' : 'veículos'}`;
  if (type === 'moto') return n === 1 ? 'a sua moto' : `${n} motos`;
  if (type === 'carro') return n === 1 ? 'o seu carro' : `${n} carros`;
  return n === 1 ? 'o seu veículo' : `${n} veículos`;
}

/** O plano do pré-cadastro, sem o preço (na promoção ele é outro). */
function planPhrase(plan: string): string {
  const name = plan.split(' - ')[0].trim();
  const lower = name.toLowerCase();
  if (lower.includes('insanos')) return 'no preço especial do Insanos MC';
  if (lower.startsWith('apenas equipamento')) return 'em comprar só o equipamento';
  return `no ${name}`;
}

/** O que a promoção dá a este pré-cliente (no plano do Insanos MC, a mensalidade deles). */
function promoPhrase(plan: string, offer: PromoOffer): string {
  const insanos = offer.insanosPlanName !== '' && plan.toLowerCase().includes(offer.insanosPlanName.toLowerCase());
  const money = (cents: number) => formatMoney(cents).replace(/ /g, ' ');
  return (
    `o primeiro rastreador sai por ${money(offer.equipmentCents)}, com mensalidade de ` +
    `${money(insanos ? offer.insanosMonthlyCents : offer.monthlyCents)} nos ${offer.months} primeiros meses, enquanto houver vagas`
  );
}

/**
 * A mensagem pronta para chamar o pré-cliente no WhatsApp: o primeiro
 * contato (de onde veio, o que quer e a promoção, se está na lista), o
 * retorno para quem já está em contato e um oi para quem já é cliente.
 */
export function whatsappMessage(
  lead: Pick<Lead, 'name' | 'city' | 'plan' | 'vehicleType' | 'vehicleCount' | 'status' | 'customerId' | 'onLaunchList' | 'promoClaimed' | 'source' | 'referrer' | 'event'>,
  sender: string,
  promo?: PromoUsage,
): string {
  const name = firstName(lead.name);
  const me = firstName(sender);
  const hello = `Olá${name ? `, ${name}` : ''}! Tudo bem? Aqui é ${me ? `${me}, ` : ''}da Farbo Rastreadores.`;

  if (lead.status === 'CONVERTED' || lead.promoClaimed || lead.customerId) {
    return [hello, 'Obrigado por escolher a Farbo! Passando para saber se está tudo certo e se ficou alguma dúvida. É só chamar por aqui.'].join('\n\n');
  }

  const vehicles = vehiclesPhrase(lead.vehicleType, lead.vehicleCount);
  const offer = lead.onLaunchList && promo?.enabled ? promoPhrase(lead.plan, promo.offer) : '';

  if (lead.status === 'CONTACTED') {
    return [
      hello,
      `Passando para saber se ficou alguma dúvida sobre o rastreador${lead.vehicleType ? ` para ${vehicles}` : ''}.`,
      offer && `Lembrando que, pela lista de lançamento, você ainda tem a promoção de pré-lançamento: ${offer}.`,
      'Se quiser, te ajudo a contratar por aqui mesmo.',
    ]
      .filter(Boolean)
      .join('\n\n');
  }

  const origin = lead.referrer
    ? `Você chegou até a gente pela indicação de ${lead.referrer}.`
    : lead.event
      ? `Você deixou seu contato com a gente no evento ${eventLabel(lead.event)}.`
      : lead.source === 'landing'
        ? 'Recebemos o seu pré-cadastro no nosso site.'
        : 'Você deixou seu contato no nosso site.';
  const want = lead.plan
    ? `Vi que você tem interesse ${planPhrase(lead.plan)} para ${vehicles}`
    : lead.vehicleType
      ? `Vi que você quer rastrear ${vehicles}`
      : '';
  const interested = want && `${want}${lead.city ? `, com instalação em ${lead.city}` : ''}.`;
  return [
    hello,
    [origin, interested].filter(Boolean).join(' '),
    offer && `Como você está na nossa lista de lançamento, tem direito à promoção de pré-lançamento: ${offer}.`,
    'Posso te explicar como funciona e tirar suas dúvidas por aqui?',
  ]
    .filter(Boolean)
    .join('\n\n');
}

/** O botão verde que abre a conversa com a mensagem pronta. */
function WhatsAppButton({ lead, sender, promo }: { lead: Lead; sender: string; promo?: PromoUsage }) {
  if (!hasWhatsapp(lead.phone)) return null;
  return (
    <a
      className={local.whatsapp}
      href={whatsappLink(lead.phone, whatsappMessage(lead, sender, promo))}
      target="_blank"
      rel="noopener noreferrer"
      title="Abre a conversa no WhatsApp com uma mensagem pronta para este pré-cliente"
    >
      WhatsApp
    </a>
  );
}

function download(list: Lead[]) {
  const blob = new Blob([leadsCsv(list)], { type: 'text/csv;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = `pre-clientes-${new Date().toISOString().slice(0, 10)}.csv`;
  link.click();
  URL.revokeObjectURL(url);
}

/**
 * Pré-clientes: todo mundo que deixou o contato — o pré-cadastro da landing
 * e a lista de lançamento (landing, QR Code dos eventos, link dos
 * afiliados). A equipe entra em contato, anota e, fechando, cadastra o
 * cliente daqui. Estar na lista de lançamento é o direito à promoção.
 */
export function LeadsTab({ onConvert }: { onConvert: (lead: Lead) => void }) {
  const { notify } = useToast();
  const sender = useAuth().user?.name ?? '';
  const [status, setStatus] = useState<LeadStatus | ''>('');
  const [onList, setOnList] = useState<'' | 'on' | 'off'>('');
  const [origin, setOrigin] = useState('');
  const [search, setSearch] = useState('');
  const [opened, setOpened] = useState<Lead | null>(null);

  const leads = useQuery({ queryKey: ['leads', 'all'], queryFn: () => leadsApi.list(), refetchInterval: 60_000 });
  const promo = useQuery({ queryKey: ['leads', 'promo'], queryFn: leadsApi.promo, refetchInterval: 60_000 });
  const all = useMemo(() => leads.data ?? [], [leads.data]);
  const origins = useMemo(() => byOrigin(all), [all]);
  const list = all.filter(
    (l) =>
      (!status || l.status === status) &&
      (!onList || l.onLaunchList === (onList === 'on')) &&
      (!origin || originLabel(l) === origin) &&
      matches(l, search),
  );
  const usage = promo.data;
  const filtered = list.length !== all.length;
  const onLaunch = all.filter((l) => l.onLaunchList).length;

  const copyEmails = async () => {
    try {
      await navigator.clipboard.writeText(list.map((l) => l.email).join(', '));
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
              Quem está na lista de lançamento contrata o primeiro rastreador por {formatMoney(usage.offer.equipmentCents)} e
              paga {formatMoney(usage.offer.monthlyCents)} de mensalidade ({formatMoney(usage.offer.insanosMonthlyCents)} no plano{' '}
              {usage.offer.insanosPlanName}) nos {usage.offer.months} primeiros meses. A vaga é ocupada na contratação (1
              veículo por cliente), pelo e-mail da conta.
            </>
          ) : (
            <>
              Promoção de pré-lançamento encerrada ({usage.used} {usage.used === 1 ? 'cliente usou' : 'clientes usaram'}).
            </>
          )}
        </div>
      )}

      <Card
        flush
        title={`${filtered ? `${list.length} de ${all.length}` : all.length} ${all.length === 1 ? 'pré-cliente' : 'pré-clientes'}`}
        subtitle={
          all.length > 0
            ? `${onLaunch} na lista de lançamento · por origem: ${origins.map((o) => `${o.label} ${o.count}`).join(' · ')}`
            : 'Pré-cadastro e lista de lançamento (landing, QR Code dos eventos e links dos afiliados).'
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
        <div className={local.filters}>
          <input
            className={fieldStyles.input}
            type="search"
            aria-label="Buscar"
            placeholder="Buscar por nome, e-mail, WhatsApp ou cidade"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <select
            className={fieldStyles.select}
            aria-label="Situação"
            value={status}
            onChange={(e) => setStatus(e.target.value as LeadStatus | '')}
          >
            <option value="">Todas as situações</option>
            {(Object.keys(LEAD_STATUS) as LeadStatus[]).map((s) => (
              <option key={s} value={s}>
                {LEAD_STATUS[s].label}
              </option>
            ))}
          </select>
          <select
            className={fieldStyles.select}
            aria-label="Lista de lançamento"
            value={onList}
            onChange={(e) => setOnList(e.target.value as '' | 'on' | 'off')}
          >
            <option value="">Na lista ou não</option>
            <option value="on">Na lista de lançamento</option>
            <option value="off">Fora da lista</option>
          </select>
          <select className={fieldStyles.select} aria-label="Origem" value={origin} onChange={(e) => setOrigin(e.target.value)}>
            <option value="">Todas as origens</option>
            {origins.map((o) => (
              <option key={o.label} value={o.label}>
                {o.label} ({o.count})
              </option>
            ))}
          </select>
        </div>
        {leads.isLoading ? (
          <Spinner label="Carregando pré-clientes" />
        ) : list.length === 0 ? (
          <EmptyState
            icon="📝"
            title={all.length === 0 ? 'Nenhum pré-cliente ainda' : 'Ninguém com esses filtros'}
            description={
              all.length === 0
                ? 'Quem faz o pré-cadastro ou entra na lista de lançamento — pela landing, pelo QR Code de um evento ou pelo link de um afiliado — aparece aqui.'
                : 'Mude a busca ou os filtros.'
            }
          />
        ) : (
          <div className={styles.tableWrap}>
            <table className={`${styles.table} ${styles.stackTable}`}>
              <thead>
                <tr>
                  <th>Pré-cliente</th>
                  <th>Cidade</th>
                  <th>Origem</th>
                  <th>Interesse</th>
                  <th>Recebido</th>
                  <th>Situação</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {list.map((lead) => (
                  <tr key={lead.id}>
                    <td data-label="Pré-cliente">
                      <span className={local.who}>
                        <strong>{lead.name || <span className={billing.muted}>sem nome</span>}</strong>
                        <span className={billing.muted}>{lead.email}</span>
                        {lead.phone && (
                          <a href={whatsappLink(lead.phone)} target="_blank" rel="noreferrer">
                            {lead.phone}
                          </a>
                        )}
                      </span>
                    </td>
                    <td data-label="Cidade">{lead.city || <span className={billing.muted}>—</span>}</td>
                    <td data-label="Origem">
                      <Badge tone={lead.referrer ? 'success' : lead.event ? 'accent' : 'neutral'}>{originLabel(lead)}</Badge>
                    </td>
                    <td data-label="Interesse">{interest(lead) || <span className={billing.muted}>—</span>}</td>
                    <td data-label="Recebido" title={formatDateTime(lead.createdAt)}>
                      {formatRelative(lead.createdAt)}
                    </td>
                    <td data-label="Situação">
                      <span className={local.badges}>
                        <Badge tone={LEAD_STATUS[lead.status].tone}>{LEAD_STATUS[lead.status].label}</Badge>
                        {lead.promoClaimed ? (
                          <Badge tone="success">Usou a promoção</Badge>
                        ) : (
                          lead.onLaunchList && <Badge tone="accent">Na lista</Badge>
                        )}
                      </span>
                    </td>
                    <td>
                      <div className={styles.actions}>
                        <WhatsAppButton lead={lead} sender={sender} promo={usage} />
                        <Button size="small" variant="secondary" onClick={() => setOpened(lead)}>
                          Abrir
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <EventQrCard />

      <LeadDetailsModal
        lead={opened}
        sender={sender}
        promo={usage}
        onClose={() => setOpened(null)}
        onConvert={(lead) => {
          setOpened(null);
          onConvert(lead);
        }}
      />
    </>
  );
}

function LeadDetailsModal({
  lead,
  sender,
  promo,
  onClose,
  onConvert,
}: {
  lead: Lead | null;
  sender: string;
  promo?: PromoUsage;
  onClose: () => void;
  onConvert: (lead: Lead) => void;
}) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<LeadStatus>('NEW');
  const [notes, setNotes] = useState('');
  const [confirm, setConfirm] = useState<'' | 'list' | 'delete'>('');

  useEffect(() => {
    if (lead) {
      setStatus(lead.status);
      setNotes(lead.notes);
      setConfirm('');
    }
  }, [lead]);

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['leads'] });
  const save = useMutation({
    mutationFn: () => leadsApi.update(lead!.id, { status, notes }),
    onSuccess: () => {
      refresh();
      notify({ tone: 'success', title: 'Pré-cliente atualizado' });
      onClose();
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível salvar', description: err.message }),
  });
  const leaveList = useMutation({
    mutationFn: () => leadsApi.removeFromLaunch(lead!.id),
    onSuccess: () => {
      refresh();
      notify({ tone: 'success', title: 'Fora da lista de lançamento', description: lead?.email });
      onClose();
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível', description: err.message }),
  });
  const remove = useMutation({
    mutationFn: () => leadsApi.remove(lead!.id),
    onSuccess: () => {
      refresh();
      notify({ tone: 'success', title: 'Pré-cliente apagado', description: lead?.email });
      onClose();
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível apagar', description: err.message }),
  });

  const changed = lead !== null && (status !== lead.status || notes.trim() !== lead.notes);

  return (
    <Modal
      open={lead !== null}
      wide
      title={lead ? lead.name || lead.email : 'Pré-cliente'}
      onClose={onClose}
      footer={
        confirm ? (
          <>
            <span className={local.confirm}>
              {confirm === 'list'
                ? 'Tirar da lista de lançamento? Perde o direito à promoção; o pré-cliente fica.'
                : 'Apagar o pré-cliente e a inscrição na lista? Use quando a pessoa pedir (LGPD).'}
            </span>
            <Button variant="ghost" onClick={() => setConfirm('')}>
              Não
            </Button>
            <Button
              variant="danger"
              loading={leaveList.isPending || remove.isPending}
              onClick={() => (confirm === 'list' ? leaveList.mutate() : remove.mutate())}
            >
              {confirm === 'list' ? 'Tirar da lista' : 'Apagar'}
            </Button>
          </>
        ) : (
          <>
            {lead && lead.status !== 'CONVERTED' && !lead.customerId && (
              <Button variant="secondary" onClick={() => onConvert(lead)}>
                Cadastrar como cliente
              </Button>
            )}
            <Button variant="ghost" onClick={onClose}>
              Fechar
            </Button>
            <Button variant="primary" loading={save.isPending} disabled={!changed} onClick={() => save.mutate()}>
              Salvar
            </Button>
          </>
        )
      }
    >
      {lead && (
        <div className={styles.form}>
          <dl className={styles.form} style={{ margin: 0 }}>
            <Info label="E-mail">
              <a href={`mailto:${lead.email}`}>{lead.email}</a>
            </Info>
            {lead.phone && (
              <Info label="WhatsApp">
                <span className={local.phone}>
                  <a href={whatsappLink(lead.phone)} target="_blank" rel="noreferrer">
                    {lead.phone}
                  </a>
                  <WhatsAppButton lead={lead} sender={sender} promo={promo} />
                </span>
              </Info>
            )}
            {lead.city && <Info label="Cidade da instalação">{lead.city}</Info>}
            <Info label="Origem">
              {originLabel(lead)}
              {lead.referrer && ' (link de afiliado)'}
            </Info>
            {interest(lead) && <Info label="Interesse">{interest(lead)}</Info>}
            <Info label="Lista de lançamento">
              {lead.promoClaimed
                ? 'Já contratou com a promoção de pré-lançamento.'
                : lead.onLaunchList
                  ? 'Sim: tem direito à promoção de pré-lançamento enquanto houver vaga (contratando com este e-mail).'
                  : 'Não'}
            </Info>
            {lead.message && (
              <Info label="Mensagem">
                <span style={{ whiteSpace: 'pre-wrap' }}>{lead.message}</span>
              </Info>
            )}
            <Info label="Recebido">
              {formatDateTime(lead.createdAt)}
              {lead.updatedAt !== lead.createdAt && ` · atualizado ${formatRelative(lead.updatedAt)}`}
            </Info>
            {lead.customerId && (
              <Info label="Cliente">
                <Link to={`/clientes/${lead.customerId}`}>Abrir a ficha do cliente</Link>
              </Info>
            )}
          </dl>

          <SelectField label="Situação" value={status} onChange={(e) => setStatus(e.target.value as LeadStatus)}>
            {(Object.keys(LEAD_STATUS) as LeadStatus[]).map((s) => (
              <option key={s} value={s}>
                {LEAD_STATUS[s].label}
              </option>
            ))}
          </SelectField>
          <label className={fieldStyles.field}>
            <span className={fieldStyles.label}>Anotações da equipe</span>
            <textarea
              className={fieldStyles.textarea}
              style={{ fontFamily: 'inherit' }}
              rows={4}
              maxLength={2000}
              placeholder="Ex.: liguei, retorna na sexta; quer instalar em Campinas…"
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
            />
          </label>
          <div className={local.dangerZone}>
            {lead.onLaunchList && !lead.promoClaimed && (
              <Button size="small" variant="ghost" onClick={() => setConfirm('list')}>
                Tirar da lista de lançamento
              </Button>
            )}
            <Button size="small" variant="ghost" onClick={() => setConfirm('delete')}>
              Apagar (pedido da pessoa, LGPD)
            </Button>
          </div>
        </div>
      )}
    </Modal>
  );
}

function Info({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt className={styles.infoLabel}>{label}</dt>
      <dd style={{ margin: 0 }}>{children}</dd>
    </div>
  );
}

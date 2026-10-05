import { useEffect, useState } from 'react';
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
import { formatDateTime, formatRelative } from '@/services/format';
import type { Lead, LeadStatus } from '@/types';

import styles from '../Page.module.css';

export const LEAD_STATUS: Record<LeadStatus, { label: string; tone: BadgeTone }> = {
  NEW: { label: 'Novo', tone: 'warning' },
  CONTACTED: { label: 'Em contato', tone: 'accent' },
  CONVERTED: { label: 'Virou cliente', tone: 'success' },
  DISCARDED: { label: 'Descartado', tone: 'neutral' },
};

const VEHICLE_LABELS: Record<string, string> = { moto: 'moto', carro: 'carro', frota: 'frota' };

function interest(lead: Lead): string {
  const type = VEHICLE_LABELS[lead.vehicleType];
  const vehicles = `${lead.vehicleCount} ${lead.vehicleCount === 1 ? 'veículo' : 'veículos'}${type ? ` (${type})` : ''}`;
  return [lead.plan, vehicles].filter(Boolean).join(' · ');
}

/**
 * Pré-clientes: quem deixou o interesse na landing. A equipe entra em
 * contato, anota e, fechando, cadastra o cliente a partir daqui.
 */
export function LeadsTab({ onConvert }: { onConvert: (lead: Lead) => void }) {
  const [status, setStatus] = useState<LeadStatus | ''>('');
  const [opened, setOpened] = useState<Lead | null>(null);

  const leads = useQuery({
    queryKey: ['leads', status],
    queryFn: () => leadsApi.list(status || undefined),
    refetchInterval: 60_000,
  });
  const list = leads.data ?? [];

  return (
    <>
      <Card flush>
        <div style={{ padding: 'var(--space-3)', maxWidth: 280 }}>
          <select
            className={fieldStyles.select}
            aria-label="Situação"
            value={status}
            onChange={(event) => setStatus(event.target.value as LeadStatus | '')}
          >
            <option value="">Todas as situações</option>
            {(Object.keys(LEAD_STATUS) as LeadStatus[]).map((s) => (
              <option key={s} value={s}>
                {LEAD_STATUS[s].label}
              </option>
            ))}
          </select>
        </div>
        {leads.isLoading ? (
          <Spinner label="Carregando pré-clientes" />
        ) : list.length === 0 ? (
          <EmptyState
            icon="📝"
            title="Nenhum pré-cliente aqui"
            description="Quem preenche o pré-cadastro na landing (Quero meu rastreador) aparece nesta lista."
          />
        ) : (
          <div className={styles.tableWrap}>
            <table className={`${styles.table} ${styles.stackTable}`}>
              <thead>
                <tr>
                  <th>Pré-cliente</th>
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
                      <strong>{lead.name}</strong>
                      <div className={billing.muted}>{lead.email}</div>
                      {lead.onLaunchList && <div className={billing.muted}>na lista de lançamento</div>}
                      {lead.phone && <div className={billing.muted}>{lead.phone}</div>}
                      {lead.referrer && <Badge tone="success">Indicação {lead.referrer}</Badge>}
                    </td>
                    <td data-label="Interesse">
                      {interest(lead)}
                      {lead.city && <div className={billing.muted}>{lead.city}</div>}
                    </td>
                    <td data-label="Recebido" title={formatDateTime(lead.createdAt)}>
                      {formatRelative(lead.createdAt)}
                    </td>
                    <td data-label="Situação">
                      <Badge tone={LEAD_STATUS[lead.status].tone}>{LEAD_STATUS[lead.status].label}</Badge>
                    </td>
                    <td>
                      <Button size="small" variant="secondary" onClick={() => setOpened(lead)}>
                        Abrir
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <LeadDetailsModal
        lead={opened}
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
  onClose,
  onConvert,
}: {
  lead: Lead | null;
  onClose: () => void;
  onConvert: (lead: Lead) => void;
}) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<LeadStatus>('NEW');
  const [notes, setNotes] = useState('');

  useEffect(() => {
    if (lead) {
      setStatus(lead.status);
      setNotes(lead.notes);
    }
  }, [lead]);

  const save = useMutation({
    mutationFn: () => leadsApi.update(lead!.id, { status, notes }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['leads'] });
      notify({ tone: 'success', title: 'Pré-cliente atualizado' });
      onClose();
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível salvar', description: err.message }),
  });

  const changed = lead !== null && (status !== lead.status || notes.trim() !== lead.notes);

  return (
    <Modal
      open={lead !== null}
      wide
      title={lead ? lead.name : 'Pré-cliente'}
      onClose={onClose}
      footer={
        <>
          {lead && lead.status !== 'CONVERTED' && (
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
      }
    >
      {lead && (
        <div className={styles.form}>
          <dl className={styles.form} style={{ margin: 0 }}>
            <Info label="E-mail">
              <a href={`mailto:${lead.email}`}>{lead.email}</a>
            </Info>
            {lead.phone && <Info label="Telefone">{lead.phone}</Info>}
            {lead.city && <Info label="Cidade">{lead.city}</Info>}
            <Info label="Interesse">{interest(lead)}</Info>
            <Info label="Lista de lançamento">
              {lead.onLaunchList
                ? 'Sim: tem direito à promoção de pré-lançamento enquanto houver vaga (contratando com este e-mail).'
                : 'Não'}
            </Info>
            {lead.referrer && <Info label="Indicado por">{lead.referrer} (link de afiliado)</Info>}
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

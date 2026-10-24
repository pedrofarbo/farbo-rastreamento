import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';

import { customersApi, organizationsApi } from '@/api/resources';
import { AssociateImportModal } from '@/components/organization/AssociateImportModal';
import { AssociateModal } from '@/components/organization/OrgForms';
import { EstimateBox, OrgInvoices } from '@/components/organization/OrgInvoices';
import { OrgTeamManager } from '@/components/organization/OrgTeamManager';
import { OrganizationFormModal } from '@/components/organization/OrganizationForm';
import org from '@/components/organization/Organization.module.css';
import { BackButton } from '@/components/ui/BackButton';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatMoney, formatRelative } from '@/services/format';
import { ORG_KIND_LABELS } from '@/services/roles';
import { formatTaxId } from '@/services/taxid';
import type { AssociateInput, OrganizationInput, OrgInvoice } from '@/types';

import styles from '../Page.module.css';
import { organizationsKey, termsLabel } from './OrganizationsPage';

/** A ficha da associação ou seguradora na central. */
export function OrganizationDetailsPage() {
  const { id = '' } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [editing, setEditing] = useState(false);
  const [creatingAssociate, setCreatingAssociate] = useState(false);
  const [importing, setImporting] = useState(false);
  const key = ['organizations', id] as const;
  const detail = useQuery({ queryKey: key, queryFn: () => organizationsApi.get(id), enabled: Boolean(id) });
  const refresh = () => void queryClient.invalidateQueries({ queryKey: organizationsKey });
  const failed = (title: string) => (err: Error) => notify({ tone: 'error', title, description: err.message });

  const update = useMutation({
    mutationFn: (input: OrganizationInput) => organizationsApi.update(id, input),
    onSuccess: (o) => {
      setEditing(false);
      refresh();
      notify({ tone: 'success', title: `${o.name} salva` });
    },
    onError: failed('Não deu para salvar'),
  });
  const generate = useMutation({
    mutationFn: () => organizationsApi.generateInvoice(id),
    onSuccess: (inv) => {
      refresh();
      notify({ tone: 'success', title: 'Fatura gerada', description: `${inv.description}: ${formatMoney(inv.amountCents)}` });
    },
    onError: failed('Não deu para gerar a fatura'),
  });
  const invoiceAction = useMutation({
    mutationFn: ({ inv, action }: { inv: OrgInvoice; action: 'pay' | 'cancel' }) =>
      action === 'pay' ? customersApi.payInvoice(inv.id) : customersApi.cancelInvoice(inv.id),
    onSuccess: (_, { action }) => {
      refresh();
      notify({ tone: 'success', title: action === 'pay' ? 'Baixa registrada' : 'Fatura cancelada' });
    },
    onError: failed('Não deu para alterar a fatura'),
  });
  const createAssociate = useMutation({
    mutationFn: (input: AssociateInput) => organizationsApi.createAssociate(id, input),
    onSuccess: (a) => {
      setCreatingAssociate(false);
      refresh();
      notify({ tone: 'success', title: `${a.name} cadastrado`, description: `Mandamos para ${a.email} o convite do app.` });
    },
    onError: failed('Não deu para cadastrar'),
  });

  if (detail.isLoading) return <Spinner label="Carregando" />;
  const o = detail.data;
  if (!o) return <EmptyState title="Organização não encontrada" description={<Link to="/associacoes">Voltar</Link>} />;

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div className={styles.titleRow}>
            <BackButton to="/associacoes" />
            <div>
              <h1 className={styles.title}>{o.name}</h1>
              <p className={styles.description}>
                {ORG_KIND_LABELS[o.kind]}
                {o.document ? ` · ${formatTaxId(o.document)}` : ''} · cobrança em {o.email}
                {o.phone ? ` · ${o.phone}` : ''} {!o.active && <Badge tone="neutral">Desativada</Badge>}
              </p>
            </div>
          </div>
          <div className={styles.actions}>
            <Button variant="secondary" onClick={() => setEditing(true)}>
              Editar
            </Button>
            <Button variant="primary" loading={generate.isPending} onClick={() => generate.mutate()}>
              Gerar a próxima fatura agora
            </Button>
          </div>
        </header>

        <div className={org.stats}>
          <div className={org.stat}>
            <span className={org.statValue}>{o.associates}</span>
            <span className={org.statLabel}>associados ativos</span>
          </div>
          <div className={org.stat}>
            <span className={org.statValue}>{o.installed}</span>
            <span className={org.statLabel}>de {o.vehicles} veículos instalados</span>
          </div>
          <div className={org.stat}>
            <span className={org.statValue}>{formatMoney(o.priceCents)}</span>
            <span className={org.statLabel}>por veículo · vence dia {o.dueDay}</span>
          </div>
          <div className={org.stat}>
            <span className={org.statValue} style={{ fontSize: 'var(--text-sm)' }}>
              {termsLabel(o)}
            </span>
            <span className={org.statLabel}>rastreador e taxas</span>
          </div>
        </div>
        {o.notes && <p className={styles.note}>{o.notes}</p>}

        <EstimateBox estimate={o.estimate} />

        <Card title="Faturas" subtitle="Uma por mês, com o extrato dos veículos." flush>
          <OrgInvoices
            invoices={o.invoices}
            actions={(inv) =>
              inv.status === 'OPEN' ? (
                <>
                  <Button size="small" variant="secondary" onClick={() => invoiceAction.mutate({ inv, action: 'pay' })}>
                    Dar baixa
                  </Button>
                  <Button size="small" variant="ghost" onClick={() => invoiceAction.mutate({ inv, action: 'cancel' })}>
                    Cancelar
                  </Button>
                </>
              ) : null
            }
          />
        </Card>

        <Card
          title="Associados"
          actions={
            <div className={styles.actions}>
              <Button size="small" variant="secondary" onClick={() => setImporting(true)}>
                Importar planilha
              </Button>
              <Button size="small" variant="secondary" onClick={() => setCreatingAssociate(true)}>
                Novo associado
              </Button>
            </div>
          }
          flush
        >
          {o.associateList.length === 0 ? (
            <EmptyState icon="👥" title="Nenhum associado ainda" description="Um cliente que já existe entra pela ficha dele, em Clientes." />
          ) : (
            <div className={styles.tableWrap}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>Associado</th>
                    <th>Veículos</th>
                    <th>App</th>
                    <th>Situação</th>
                  </tr>
                </thead>
                <tbody>
                  {o.associateList.map((a) => (
                    <tr key={a.id}>
                      <td>
                        <Link to={`/clientes/${a.id}`} className={org.rowLink}>
                          {a.name}
                        </Link>
                        <div className={org.muted}>{a.email}</div>
                      </td>
                      <td>
                        {a.installed} de {a.vehicles}
                      </td>
                      <td className={org.muted}>{a.lastSeenAt ? `entrou ${formatRelative(a.lastSeenAt)}` : 'ainda não entrou'}</td>
                      <td>{a.active ? <Badge tone="success">Ativo</Badge> : <Badge tone="neutral">Desligado</Badge>}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>

        <Card title="Equipe da organização" subtitle="Quem entra no painel dela.">
          <OrgTeamManager api={organizationsApi.team(id)} queryKey={['organizations', id, 'team']} confirmIdentity={false} orgName={o.name} />
        </Card>
      </div>

      <OrganizationFormModal
        open={editing}
        organization={o}
        saving={update.isPending}
        onClose={() => setEditing(false)}
        onSave={(input) => update.mutate(input)}
      />
      <AssociateImportModal
        open={importing}
        importRows={(rows, dryRun) => organizationsApi.importAssociates(id, rows, dryRun)}
        onClose={() => setImporting(false)}
        onDone={(result) => {
          setImporting(false);
          refresh();
          notify({
            tone: 'success',
            title: `${result.associates} ${result.associates === 1 ? 'associado cadastrado' : 'associados cadastrados'} em ${o.name}`,
            description: `${result.vehicles} ${result.vehicles === 1 ? 'veículo entrou' : 'veículos entraram'} na fila de instalação. Cada associado recebe o convite do app por e-mail.`,
          });
        }}
      />
      <AssociateModal
        open={creatingAssociate}
        associate={null}
        saving={createAssociate.isPending}
        onClose={() => setCreatingAssociate(false)}
        onSave={(input) => createAssociate.mutate(input)}
      />
    </div>
  );
}

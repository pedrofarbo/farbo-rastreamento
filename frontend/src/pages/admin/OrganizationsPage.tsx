import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';

import { organizationsApi } from '@/api/resources';
import { OrganizationFormModal } from '@/components/organization/OrganizationForm';
import org from '@/components/organization/Organization.module.css';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatMoney } from '@/services/format';
import { ORG_KIND_LABELS } from '@/services/roles';
import type { Organization } from '@/types';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

import styles from '../Page.module.css';

export const organizationsKey = ['organizations'] as const;

/** O rastreador e as taxas, numa linha. */
export function termsLabel(o: Pick<Organization, 'equipmentMode' | 'equipmentCents' | 'installFeeCents' | 'uninstallFeeCents'>): string {
  const parts = [o.equipmentMode === 'loan' ? 'Comodato' : `Venda ${formatMoney(o.equipmentCents)}`];
  if (o.installFeeCents) parts.push(`instalação ${formatMoney(o.installFeeCents)}`);
  if (o.uninstallFeeCents) parts.push(`desinstalação ${formatMoney(o.uninstallFeeCents)}`);
  return parts.join(' · ');
}

/** As associações e seguradoras (central). */
export function OrganizationsPage() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { notify } = useToast();
  const [creating, setCreating] = useState(false);
  const list = useQuery({ queryKey: organizationsKey, queryFn: organizationsApi.list });
  const paged = usePagedList(list.data ?? [], 'organizations');

  const create = useMutation({
    mutationFn: ({ input, admin }: { input: Parameters<typeof organizationsApi.create>[0]; admin?: { name: string; email: string } }) =>
      organizationsApi.create({ ...input, admin }),
    onSuccess: (res) => {
      setCreating(false);
      void queryClient.invalidateQueries({ queryKey: organizationsKey });
      if (res.adminError) {
        notify({ tone: 'warning', title: `${res.organization.name} cadastrada, sem o Administrador`, description: res.adminError });
      } else {
        notify({ tone: 'success', title: `${res.organization.name} cadastrada` });
      }
      navigate(`/associacoes/${res.organization.id}`);
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para cadastrar', description: err.message }),
  });

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Associações e seguradoras</h1>
            <p className={styles.description}>
              A organização paga o rastreamento dos associados numa fatura só por mês e acompanha os veículos deles num
              painel próprio. O associado usa o app da Farbo, sem faturas.
            </p>
          </div>
          <div className={styles.actions}>
            <Button variant="primary" onClick={() => setCreating(true)}>
              Nova organização
            </Button>
          </div>
        </header>

        <Card flush>
          {list.isLoading ? (
            <Spinner label="Carregando" />
          ) : (list.data ?? []).length === 0 ? (
            <EmptyState icon="🛡️" title="Nenhuma organização ainda" />
          ) : (
            <>
              <div className={styles.tableWrap}>
                <table className={styles.table}>
                  <thead>
                    <tr>
                      <th>Organização</th>
                      <th>Associados</th>
                      <th>Instalados</th>
                      <th>Mensalidade</th>
                      <th>Faturas</th>
                      <th>Situação</th>
                    </tr>
                  </thead>
                  <tbody>
                    {paged.items.map((o) => (
                      <tr key={o.id}>
                        <td>
                          <Link to={`/associacoes/${o.id}`} className={org.rowLink}>
                            {o.name}
                          </Link>
                          <div className={org.muted}>
                            {ORG_KIND_LABELS[o.kind]} · {termsLabel(o)}
                          </div>
                        </td>
                        <td>{o.associates}</td>
                        <td>{o.installed}</td>
                        <td>
                          {formatMoney(o.priceCents)}
                          <div className={org.muted}>dia {o.dueDay}</div>
                        </td>
                        <td>
                          {o.overdueInvoices > 0 ? (
                            <Badge tone="danger">{o.overdueInvoices} vencida(s)</Badge>
                          ) : o.openInvoices > 0 ? (
                            <Badge tone="warning">{o.openInvoices} em aberto</Badge>
                          ) : (
                            <span className={org.muted}>em dia</span>
                          )}
                        </td>
                        <td>{o.active ? <Badge tone="success">Ativa</Badge> : <Badge tone="neutral">Desativada</Badge>}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <Pager {...paged.pager} />
            </>
          )}
        </Card>
      </div>

      <OrganizationFormModal
        open={creating}
        organization={null}
        saving={create.isPending}
        onClose={() => setCreating(false)}
        onSave={(input, admin) => create.mutate({ input, admin })}
      />
    </div>
  );
}

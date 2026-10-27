import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';

import { orgApi } from '@/api/resources';
import { SuspendedNotice, isSuspendedError } from '@/components/billing/SuspendedNotice';
import { AssociateImportModal } from '@/components/organization/AssociateImportModal';
import { AssociateModal } from '@/components/organization/OrgForms';
import org from '@/components/organization/Organization.module.css';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import fieldStyles from '@/components/ui/Field.module.css';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatRelative } from '@/services/format';
import { ORG_KIND_LABELS } from '@/services/roles';
import { useAuth } from '@/stores/AuthContext';
import type { Associate, AssociateInput } from '@/types';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

import styles from '../Page.module.css';

export const orgAssociatesKey = ['org', 'associates'] as const;
export const orgOverviewKey = ['org', 'overview'] as const;

/** Busca pelo nome, e-mail, celular ou documento. */
export function matchesAssociate(a: Associate, term: string): boolean {
  const t = term.trim().toLowerCase();
  if (!t) return true;
  const digits = t.replace(/\D/g, '');
  return (
    a.name.toLowerCase().includes(t) ||
    a.email.toLowerCase().includes(t) ||
    (digits.length > 0 && (a.phone.replace(/\D/g, '').includes(digits) || a.document.includes(digits)))
  );
}

/** O painel da associação: os números e os associados. */
export function OrgAssociatesPage() {
  const { canOperateOrg, canRegisterOrg, isOrgAdmin, isOrgBroker, orgMembership } = useAuth();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { notify } = useToast();
  const [search, setSearch] = useState('');
  const [creating, setCreating] = useState(false);
  const [importing, setImporting] = useState(false);

  const overview = useQuery({ queryKey: orgOverviewKey, queryFn: orgApi.overview });
  const associates = useQuery({ queryKey: orgAssociatesKey, queryFn: orgApi.associates });
  // Os corretores, para o Administrador escolher no cadastro.
  const team = useQuery({ queryKey: ['org', 'team'], queryFn: orgApi.team.list, enabled: isOrgAdmin });
  const brokers = isOrgAdmin
    ? (team.data ?? []).filter((m) => m.role === 'broker' && m.active).map((m) => ({ id: m.userId, name: m.name }))
    : undefined;
  const showBroker = !isOrgBroker && (associates.data ?? []).some((a) => a.brokerId);
  const list = useMemo(() => (associates.data ?? []).filter((a) => matchesAssociate(a, search)), [associates.data, search]);
  const paged = usePagedList(list, 'associates', search);

  const create = useMutation({
    mutationFn: (input: AssociateInput) => orgApi.createAssociate(input),
    onSuccess: (a) => {
      setCreating(false);
      void queryClient.invalidateQueries({ queryKey: ['org'] });
      notify({ tone: 'success', title: `${a.name} cadastrado`, description: `Mandamos para ${a.email} o convite do app.` });
      navigate(`/associados/${a.id}`);
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para cadastrar', description: err.message }),
  });

  if (isSuspendedError(associates.error)) return <SuspendedNotice message={(associates.error as Error).message} />;
  const o = overview.data;
  const kind = orgMembership ? ORG_KIND_LABELS[orgMembership.kind] : 'Organização';

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>{orgMembership?.name ?? 'Associados'}</h1>
            <p className={styles.description}>
              {kind}: os associados, os veículos deles e quem já está com o rastreador instalado.
            </p>
          </div>
          {canRegisterOrg && (
            <div className={styles.actions}>
              {canOperateOrg && (
                <Button variant="secondary" onClick={() => setImporting(true)}>
                  Importar planilha
                </Button>
              )}
              <Button variant="primary" onClick={() => setCreating(true)}>
                Novo associado
              </Button>
            </div>
          )}
        </header>

        {o && (
          <div className={org.stats}>
            <div className={org.stat}>
              <span className={org.statValue}>{o.organization.associates}</span>
              <span className={org.statLabel}>associados ativos</span>
            </div>
            <div className={org.stat}>
              <span className={org.statValue}>{o.organization.installed}</span>
              <span className={org.statLabel}>veículos com rastreador</span>
            </div>
            <div className={org.stat}>
              <span className={org.statValue}>{o.online}</span>
              <span className={org.statLabel}>online agora</span>
            </div>
            <div className={org.stat}>
              <span className={org.statValue}>{o.awaitingInstall}</span>
              <span className={org.statLabel}>aguardando instalação</span>
            </div>
          </div>
        )}

        <Card
          title="Associados"
          actions={
            <input
              className={`${fieldStyles.input} ${org.search}`}
              type="search"
              placeholder="Buscar associado"
              aria-label="Buscar associado"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          }
          flush
        >
          {associates.isLoading ? (
            <Spinner label="Carregando os associados" />
          ) : list.length === 0 ? (
            <EmptyState
              icon="👥"
              title={search ? 'Ninguém com essa busca' : 'Nenhum associado ainda'}
              description={canRegisterOrg && !search ? 'Cadastre o primeiro: ele recebe o convite do app por e-mail.' : undefined}
            />
          ) : (
            <>
              <div className={styles.tableWrap}>
                <table className={styles.table}>
                  <thead>
                    <tr>
                      <th>Associado</th>
                      {showBroker && <th>Corretor</th>}
                      <th>Veículos</th>
                      <th>App</th>
                      <th>Situação</th>
                    </tr>
                  </thead>
                  <tbody>
                    {paged.items.map((a) => (
                      <tr key={a.id}>
                        <td>
                          <Link to={`/associados/${a.id}`} className={org.rowLink}>
                            {a.name}
                          </Link>
                          <div className={org.muted}>{a.email}</div>
                        </td>
                        {showBroker && <td className={org.muted}>{a.brokerName || '—'}</td>}
                        <td>
                          {a.installed} de {a.vehicles} {a.vehicles === 1 ? 'instalado' : 'instalados'}
                        </td>
                        <td className={org.muted}>{a.lastSeenAt ? `entrou ${formatRelative(a.lastSeenAt)}` : 'ainda não entrou'}</td>
                        <td>{a.active ? <Badge tone="success">Ativo</Badge> : <Badge tone="neutral">Desligado</Badge>}</td>
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

      <AssociateModal
        open={creating}
        associate={null}
        saving={create.isPending}
        onClose={() => setCreating(false)}
        onSave={(input) => create.mutate(input)}
        brokers={brokers}
      />
      {canOperateOrg && (
        <AssociateImportModal
          open={importing}
          onClose={() => setImporting(false)}
          onDone={(result) => {
            setImporting(false);
            void queryClient.invalidateQueries({ queryKey: ['org'] });
            notify({
              tone: 'success',
              title: `${result.associates} ${result.associates === 1 ? 'associado cadastrado' : 'associados cadastrados'}`,
              description: `${result.vehicles} ${result.vehicles === 1 ? 'veículo entrou' : 'veículos entraram'} na fila de instalação. Cada associado recebe o convite do app por e-mail.`,
            });
          }}
        />
      )}
    </div>
  );
}

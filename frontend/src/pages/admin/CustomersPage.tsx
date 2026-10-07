import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';

import { customersApi, leadsApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { CustomerStatus } from '@/components/billing/InvoiceStatus';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { TextField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatMoney } from '@/services/format';
import { formatTaxId } from '@/services/taxid';
import type { Lead } from '@/types';

import styles from '../Page.module.css';
import { AffiliatesTab } from './affiliates/AffiliatesTab';
import { LeadsTab } from './LeadsTab';
import { SiteAnalyticsTab } from './SiteAnalyticsTab';

interface CustomerDraft {
  name: string;
  email: string;
  phone: string;
  document: string;
  access: 'invite' | 'password';
  password: string;
  /** Cadastro a partir de um pré-cliente (que passa a convertido). */
  lead?: Lead;
}

const EMPTY: CustomerDraft = {
  name: '',
  email: '',
  phone: '',
  document: '',
  access: 'invite',
  password: '',
};

/** Clientes da central: lista com a situação financeira e cadastro. */
export function CustomersPage() {
  const navigate = useNavigate();
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const tab = params.get('aba');
  // A lista de lançamento entrou nos pré-clientes (o endereço antigo leva para lá).
  const showLeads = tab === 'pre-clientes' || tab === 'lancamento';
  // As visitas da landing: o começo do funil (visita → pré-cliente → cliente).
  const showVisits = tab === 'visitas';
  // Programa de afiliados: os links dos influenciadores e as comissões.
  const showAffiliates = tab === 'afiliados';
  const showCustomers = !showLeads && !showVisits && !showAffiliates;
  const leadStats = useQuery({ queryKey: ['leads', 'stats'], queryFn: leadsApi.stats, refetchInterval: 60_000 });
  const newLeads = leadStats.data?.new ?? 0;

  const [search, setSearch] = useState('');
  const [draft, setDraft] = useState<CustomerDraft | null>(null);
  const [formError, setFormError] = useState('');

  const customers = useQuery({ queryKey: ['customers'], queryFn: customersApi.list });

  const create = useMutation({
    mutationFn: customersApi.create,
    onSuccess: (customer, input) => {
      queryClient.invalidateQueries({ queryKey: ['customers'] });
      if (input.leadId) queryClient.invalidateQueries({ queryKey: ['leads'] });
      notify({
        tone: 'success',
        title: 'Cliente cadastrado',
        description: `${
          input.password ? 'Passe a senha ao cliente por um canal seguro.' : `Convite enviado para ${customer.email}.`
        } Agora inclua o primeiro veículo em Novo veículo.`,
      });
      setDraft(null);
      navigate(`/clientes/${customer.id}`);
    },
    onError: (error: Error) => setFormError(error.message),
  });

  const filtered = useMemo(() => {
    const term = search.trim().toLowerCase();
    const list = customers.data ?? [];
    if (!term) return list;
    return list.filter((c) =>
      [c.name, c.email, c.document, c.phone].some((value) => value.toLowerCase().includes(term)),
    );
  }, [customers.data, search]);

  const submit = () => {
    if (!draft) return;
    setFormError('');
    create.mutate({
      name: draft.name,
      email: draft.email,
      phone: draft.phone,
      document: draft.document,
      password: draft.access === 'password' ? draft.password : '',
      leadId: draft.lead?.id,
    });
  };

  const totals = (customers.data ?? []).reduce(
    (acc, c) => ({
      open: acc.open + c.openAmountCents,
      overdue: acc.overdue + (c.overdueInvoices > 0 ? 1 : 0),
      suspended: acc.suspended + (c.suspended ? 1 : 0),
    }),
    { open: 0, overdue: 0, suspended: 0 },
  );

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Clientes</h1>
            <p className={styles.description}>
              Cada assinatura ativa dá direito a 1 veículo. As faturas são geradas sozinhas antes
              do vencimento; aqui você acompanha, dá baixa e informa o link de pagamento ou o Pix.
            </p>
          </div>
          <Button
            variant="primary"
            onClick={() => {
              setFormError('');
              setDraft(EMPTY);
            }}
          >
            Novo cliente
          </Button>
        </header>

        <div className={styles.tabs} role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={showCustomers}
            className={`${styles.tab} ${showCustomers ? styles.tabActive : ''}`}
            onClick={() => setParams({}, { replace: true })}
          >
            Clientes
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={showLeads}
            className={`${styles.tab} ${showLeads ? styles.tabActive : ''}`}
            onClick={() => setParams({ aba: 'pre-clientes' }, { replace: true })}
          >
            Pré-clientes{newLeads > 0 ? ` (${newLeads} novo${newLeads > 1 ? 's' : ''})` : ''}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={showVisits}
            className={`${styles.tab} ${showVisits ? styles.tabActive : ''}`}
            onClick={() => setParams({ aba: 'visitas' }, { replace: true })}
          >
            Visitas do site
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={showAffiliates}
            className={`${styles.tab} ${showAffiliates ? styles.tabActive : ''}`}
            onClick={() => setParams({ aba: 'afiliados' }, { replace: true })}
          >
            Afiliados
          </button>
        </div>

        {showAffiliates ? (
          <AffiliatesTab />
        ) : showVisits ? (
          <SiteAnalyticsTab />
        ) : showLeads ? (
          <LeadsTab
            onConvert={(lead) => {
              setFormError('');
              setDraft({ ...EMPTY, name: lead.name, email: lead.email, phone: lead.phone, lead });
            }}
          />
        ) : (
          <>
            <div className={billing.tiles}>
              <div className={billing.tile}>
                <span className={billing.tileLabel}>Clientes</span>
                <span className={billing.tileValue}>{customers.data?.length ?? '—'}</span>
              </div>
              <div className={billing.tile}>
                <span className={billing.tileLabel}>A receber</span>
                <span className={billing.tileValue}>{formatMoney(totals.open)}</span>
                <span className={billing.tileHint}>faturas em aberto</span>
              </div>
              <div className={`${billing.tile} ${totals.overdue > 0 ? billing.tileDanger : ''}`}>
                <span className={billing.tileLabel}>Em atraso</span>
                <span className={billing.tileValue}>{totals.overdue}</span>
                <span className={billing.tileHint}>
                  {totals.suspended} com acesso suspenso
                </span>
              </div>
            </div>

            <Card flush>
              <div style={{ padding: 'var(--space-3)' }}>
                <input
                  className={fieldStyles.input}
                  placeholder="Buscar por nome, e-mail, telefone ou documento"
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                />
              </div>
              {customers.isLoading ? (
                <Spinner label="Carregando clientes" />
              ) : filtered.length === 0 ? (
                <EmptyState
                  icon="👤"
                  title={(customers.data ?? []).length === 0 ? 'Nenhum cliente cadastrado' : 'Nada encontrado'}
                  description={
                    (customers.data ?? []).length === 0
                      ? 'Cadastre o primeiro cliente para ele acessar o painel e acompanhar os veículos.'
                      : 'Ajuste a busca para ver outros clientes.'
                  }
                />
              ) : (
                <div className={styles.tableWrap}>
                  <table className={styles.table}>
                    <thead>
                      <tr>
                        <th>Cliente</th>
                        <th>Contato</th>
                        <th>Veículos</th>
                        <th>Em aberto</th>
                        <th>Situação</th>
                      </tr>
                    </thead>
                    <tbody>
                      {filtered.map((customer) => (
                        <tr key={customer.id}>
                          <td>
                            <Link to={`/clientes/${customer.id}`}>
                              <strong>{customer.name}</strong>
                            </Link>
                            {customer.document && <div className={billing.muted}>{formatTaxId(customer.document)}</div>}
                          </td>
                          <td>
                            {customer.email}
                            {customer.phone && <div className={billing.muted}>{customer.phone}</div>}
                          </td>
                          <td>
                            {customer.vehicleCount}
                            {customer.sharedVehicles > 0 && (
                              <div className={billing.muted}>
                                + acompanha {customer.sharedVehicles} de outro cliente
                              </div>
                            )}
                          </td>
                          <td className={billing.amount}>
                            {customer.openInvoices > 0 ? formatMoney(customer.openAmountCents) : '—'}
                          </td>
                          <td>
                            <CustomerStatus customer={customer} />
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Card>
          </>
        )}
      </div>

      <Modal
        open={draft !== null}
        wide
        title="Novo cliente"
        onClose={() => setDraft(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setDraft(null)}>
              Cancelar
            </Button>
            <Button
              variant="primary"
              loading={create.isPending}
              disabled={!draft?.name.trim() || !draft?.email.trim()}
              onClick={submit}
            >
              Cadastrar
            </Button>
          </>
        }
      >
        {draft && (
          <div className={styles.form}>
            {draft.lead && (
              <div className={styles.note}>
                A partir do pré-cliente <strong>{draft.lead.name}</strong>: ao cadastrar, ele fica marcado como
                "Virou cliente".
              </div>
            )}
            {formError && <div className={styles.note}>{formError}</div>}
            <div className={styles.formRow}>
              <TextField
                label="Nome"
                required
                autoFocus
                value={draft.name}
                onChange={(event) => setDraft({ ...draft, name: event.target.value })}
              />
              <TextField
                label="E-mail"
                type="email"
                required
                value={draft.email}
                onChange={(event) => setDraft({ ...draft, email: event.target.value })}
              />
            </div>
            <div className={styles.formRow}>
              <TextField
                label="Telefone"
                placeholder="(11) 99999-9999"
                value={draft.phone}
                onChange={(event) => setDraft({ ...draft, phone: event.target.value })}
              />
              <TextField
                label="CPF ou CNPJ"
                value={draft.document}
                hint="Para as notas fiscais. Se não souber, o cliente informa no primeiro acesso."
                onChange={(event) => setDraft({ ...draft, document: formatTaxId(event.target.value) })}
              />
            </div>

            <fieldset style={{ border: 0, padding: 0, margin: 0 }}>
              <legend className={styles.infoLabel}>Acesso ao painel</legend>
              <label style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
                <input
                  type="radio"
                  checked={draft.access === 'invite'}
                  onChange={() => setDraft({ ...draft, access: 'invite' })}
                />
                Enviar convite por e-mail para o cliente criar a senha (recomendado)
              </label>
              <label style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
                <input
                  type="radio"
                  checked={draft.access === 'password'}
                  onChange={() => setDraft({ ...draft, access: 'password' })}
                />
                Definir uma senha agora
              </label>
            </fieldset>
            {draft.access === 'password' && (
              <TextField
                label="Senha inicial"
                type="text"
                autoComplete="off"
                hint="Mínimo de 10 caracteres."
                value={draft.password}
                onChange={(event) => setDraft({ ...draft, password: event.target.value })}
              />
            )}

            <p className={billing.muted}>
              Veículo, rastreador e assinatura vêm depois, na ficha do cliente, em Novo veículo.
            </p>
          </div>
        )}
      </Modal>
    </div>
  );
}

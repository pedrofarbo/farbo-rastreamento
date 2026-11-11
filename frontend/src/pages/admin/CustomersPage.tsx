import { useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';

import { customersApi, leadsApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { CustomerStatus } from '@/components/billing/InvoiceStatus';
import { OrderOptionsCard } from '@/components/billing/OrderOptionsCard';
import { PriceAdjustmentCard } from '@/components/billing/PriceAdjustmentCard';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { TextField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatMoney, formatPhoneInput } from '@/services/format';
import { formatTaxId } from '@/services/taxid';
import type { CustomerSummary, Lead } from '@/types';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

import styles from '../Page.module.css';
import { AffiliatesTab } from './affiliates/AffiliatesTab';
import { LeadsTab } from './LeadsTab';
import { ForecastTile, MrrCard, mrrParts, signedMoney } from './MrrCard';
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

/** Os filtros da lista pela situação (no endereço: ?situacao=atraso). */
type Situation = 'todos' | 'atraso' | 'suspensos';

const SITUATIONS: { id: Situation; label: string; match: (c: CustomerSummary) => boolean }[] = [
  { id: 'todos', label: 'Todos', match: () => true },
  // Em atraso: com fatura vencida (os suspensos também).
  { id: 'atraso', label: 'Em atraso', match: (c) => c.overdueInvoices > 0 },
  { id: 'suspensos', label: 'Acesso suspenso', match: (c) => c.suspended },
];

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
  // A situação vem do endereço: o cartão "Em atraso" (ou um link) já abre a lista filtrada.
  const situation = SITUATIONS.find((s) => s.id === params.get('situacao')) ?? SITUATIONS[0];
  const listRef = useRef<HTMLDivElement>(null);
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
    const list = (customers.data ?? []).filter(situation.match);
    const found = term
      ? list.filter((c) => [c.name, c.email, c.document, c.phone].some((value) => value.toLowerCase().includes(term)))
      : list;
    // Em atraso: quem tem mais faturas vencidas (e mais em aberto) primeiro.
    if (situation.id === 'todos') return found;
    return [...found].sort((a, b) => b.overdueInvoices - a.overdueInvoices || b.openAmountCents - a.openAmountCents);
  }, [customers.data, search, situation]);
  const paged = usePagedList(filtered, 'customers', [search, situation.id]);
  const countOf = (s: (typeof SITUATIONS)[number]) => (customers.data ?? []).filter(s.match).length;

  /** Filtra a lista pela situação (o filtro vai para o endereço). */
  const filterBy = (id: Situation, scroll = false) => {
    setParams(id === 'todos' ? {} : { situacao: id }, { replace: true });
    // O cartão leva até a lista.
    if (scroll) listRef.current?.scrollIntoView?.({ behavior: 'smooth', block: 'start' });
  };

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

  // A receita recorrente (só na aba de clientes).
  const mrr = useQuery({ queryKey: ['customers', 'mrr'], queryFn: customersApi.mrr, enabled: !showLeads && !showVisits && !showAffiliates });
  const thisMonth = mrr.data?.months[0];

  // As assinaturas contam sem as grátis (mensalidade zero).
  const totals = (customers.data ?? []).reduce(
    (acc, c) => ({
      open: acc.open + c.openAmountCents,
      overdue: acc.overdue + (c.overdueInvoices > 0 ? 1 : 0),
      suspended: acc.suspended + (c.suspended ? 1 : 0),
      subscriptions: acc.subscriptions + c.activeSubscriptions - c.freeSubscriptions,
      free: acc.free + c.freeSubscriptions,
    }),
    { open: 0, overdue: 0, suspended: 0, subscriptions: 0, free: 0 },
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
              setDraft({ ...EMPTY, name: lead.name, email: lead.email, phone: formatPhoneInput(lead.phone), lead });
            }}
          />
        ) : (
          <>
            <div className={`${billing.tiles} ${billing.tilesSix}`}>
              <div className={billing.tile}>
                <span className={billing.tileLabel}>Clientes</span>
                <span className={billing.tileValue}>{customers.data?.length ?? '—'}</span>
              </div>
              <div className={billing.tile}>
                <span className={billing.tileLabel}>Assinaturas ativas</span>
                <span className={billing.tileValue}>{customers.data ? totals.subscriptions : '—'}</span>
                <span className={billing.tileHint}>
                  {totals.free > 0 ? `sem contar ${totals.free} grátis` : 'pagas, sem as grátis'}
                </span>
              </div>
              <div className={billing.tile}>
                <span className={billing.tileLabel}>MRR</span>
                <span className={billing.tileValue}>{mrr.data ? formatMoney(mrr.data.totalCents) : '—'}</span>
                <span className={billing.tileHint}>
                  {!mrr.data
                    ? 'receita recorrente mensal'
                    : mrr.data.installmentsCents > 0 || mrr.data.organizationsCents > 0
                      ? mrrParts({ ...mrr.data, monthlyCents: mrr.data.currentCents })
                      : `mensalidades, ${thisMonth ? signedMoney(thisMonth.changeCents) : '—'} no mês`}
                </span>
              </div>
              <ForecastTile forecast={mrr.data?.forecast} />
              <div className={billing.tile}>
                <span className={billing.tileLabel}>A receber</span>
                <span className={billing.tileValue}>{formatMoney(totals.open)}</span>
                <span className={billing.tileHint}>faturas em aberto</span>
              </div>
              <button
                type="button"
                aria-pressed={situation.id === 'atraso'}
                title={situation.id === 'atraso' ? 'Mostrar todos os clientes' : 'Ver os clientes em atraso'}
                className={`${billing.tile} ${billing.tileButton} ${totals.overdue > 0 ? billing.tileDanger : ''} ${
                  situation.id === 'atraso' ? billing.tileActive : ''
                }`}
                onClick={() => filterBy(situation.id === 'atraso' ? 'todos' : 'atraso', situation.id !== 'atraso')}
              >
                <span className={billing.tileLabel}>Em atraso</span>
                <span className={billing.tileValue}>{totals.overdue}</span>
                <span className={billing.tileHint}>
                  {totals.suspended} com acesso suspenso · {situation.id === 'atraso' ? 'na lista abaixo' : 'ver a lista'}
                </span>
              </button>
            </div>

            <div ref={listRef} className={styles.anchor}>
              <Card flush>
                <div className={styles.listTools}>
                  <input
                    className={fieldStyles.input}
                    placeholder="Buscar por nome, e-mail, telefone ou documento"
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                  />
                  <div className={styles.filterChips} role="group" aria-label="Situação dos clientes">
                    {SITUATIONS.map((s) => (
                      <Button
                        key={s.id}
                        size="small"
                        variant={situation.id === s.id ? 'primary' : 'ghost'}
                        aria-pressed={situation.id === s.id}
                        onClick={() => filterBy(s.id)}
                      >
                        {s.label}
                        {customers.data ? ` (${countOf(s)})` : ''}
                      </Button>
                    ))}
                  </div>
                </div>
                {customers.isLoading ? (
                  <Spinner label="Carregando clientes" />
                ) : filtered.length === 0 ? (
                  (customers.data ?? []).length === 0 ? (
                    <EmptyState
                      icon="👤"
                      title="Nenhum cliente cadastrado"
                      description="Cadastre o primeiro cliente para ele acessar o painel e acompanhar os veículos."
                    />
                  ) : situation.id !== 'todos' && !search.trim() ? (
                    <EmptyState
                      icon="✅"
                      title={situation.id === 'atraso' ? 'Nenhum cliente em atraso' : 'Nenhum cliente com acesso suspenso'}
                      description="Todos os clientes estão com as faturas em dia."
                    />
                  ) : (
                    <EmptyState icon="🔎" title="Nada encontrado" description="Ajuste a busca ou a situação para ver outros clientes." />
                  )
                ) : (
                  <>
                    <div className={styles.tableWrap}>
                      <table className={styles.table}>
                        <thead>
                          <tr>
                            <th>Cliente</th>
                            <th>Contato</th>
                            <th>Veículos</th>
                            <th>Assinaturas ativas</th>
                            <th>Em aberto</th>
                            <th>Situação</th>
                          </tr>
                        </thead>
                        <tbody>
                          {paged.items.map((customer) => (
                            <tr key={customer.id}>
                              <td>
                                <Link to={`/clientes/${customer.id}`}>
                                  <strong>{customer.name}</strong>
                                </Link>
                                {customer.document && <div className={billing.muted}>{formatTaxId(customer.document)}</div>}
                                {customer.organizationId && (
                                  <div className={billing.muted}>Associado da {customer.organizationName}</div>
                                )}
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
                              <td>
                                {customer.activeSubscriptions - customer.freeSubscriptions}
                                {customer.freeSubscriptions > 0 && (
                                  <div className={billing.muted}>+ {customer.freeSubscriptions} grátis</div>
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
                    <Pager {...paged.pager} />
                  </>
                )}
              </Card>
            </div>

            <MrrCard mrr={mrr} />
            <OrderOptionsCard />
            <PriceAdjustmentCard />
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
                value={draft.phone}
                inputMode="tel"
                placeholder="(11) 9-8888-7777"
                onChange={(event) => setDraft({ ...draft, phone: formatPhoneInput(event.target.value, draft.phone) })}
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

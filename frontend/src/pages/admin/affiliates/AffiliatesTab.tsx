import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { affiliatesApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { Badge } from '@/components/ui/Badge';
import type { BadgeTone } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { centsToInput, formatDateOnly, formatMoney, parseMoney } from '@/services/format';
import { monthLabel, referralUrl } from '@/services/referral';
import type { Affiliate, AffiliatePayout } from '@/types';

import styles from '../../Page.module.css';
import company from '../company/Company.module.css';
import { AffiliateModal } from './AffiliateModal';
import local from './Affiliates.module.css';

/** O mês anterior ao de `today` (AAAA-MM): o que se fecha no começo do mês. */
export function previousMonth(today: Date): string {
  const d = new Date(today.getFullYear(), today.getMonth() - 1, 1);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
}

/** AAAA-MM-DD, `days` dias depois de `today`. */
export function inDays(today: Date, days: number): string {
  const d = new Date(today.getFullYear(), today.getMonth(), today.getDate() + days);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

const PAYOUT_STATUS: Record<AffiliatePayout['status'], { label: string; tone: BadgeTone }> = {
  OPEN: { label: 'A pagar', tone: 'warning' },
  PAID: { label: 'Pago', tone: 'success' },
  MISSING: { label: 'Conta excluída', tone: 'danger' },
};

/**
 * Programa de afiliados: os links de cadastro de cada influenciador, o
 * valor por cliente indicado e o fechamento do mês (as comissões viram
 * contas a pagar em Empresa).
 */
export function AffiliatesTab() {
  const { notify } = useToast();
  const affiliates = useQuery({ queryKey: ['affiliates'], queryFn: affiliatesApi.list });
  const settings = useQuery({ queryKey: ['affiliates', 'settings'], queryFn: affiliatesApi.settings });
  const [editing, setEditing] = useState<Affiliate | null>(null);
  const [creating, setCreating] = useState(false);

  const list = affiliates.data ?? [];
  const defaultCents = settings.data?.defaultCommissionCents ?? 600;
  const sum = (pick: (a: Affiliate) => number) => list.reduce((total, a) => total + pick(a), 0);

  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      notify({ tone: 'success', title: 'Link copiado', description: text });
    } catch {
      notify({ tone: 'error', title: 'Não deu para copiar', description: text });
    }
  };

  return (
    <>
      <div className={styles.note}>
        Cada afiliado tem um link de cadastro. Quem se cadastra por ele e vira cliente rende ao afiliado o valor dele
        (padrão {formatMoney(defaultCents)}) <strong>por cliente, em cada mês em que o cliente paga a mensalidade</strong>.
        No fechamento do mês, as comissões viram contas a pagar em{' '}
        <Link to="/empresa?aba=pagar">Empresa → Contas a pagar</Link>; pagar a conta paga as comissões. O afiliado
        acompanha os números pela página dele, sem ver dados de ninguém.
      </div>

      <div className={billing.tiles}>
        <div className={billing.tile}>
          <span className={billing.tileLabel}>Afiliados ativos</span>
          <span className={billing.tileValue}>{affiliates.data ? list.filter((a) => a.active).length : '—'}</span>
        </div>
        <div className={billing.tile}>
          <span className={billing.tileLabel}>Clientes indicados</span>
          <span className={billing.tileValue}>{affiliates.data ? sum((a) => a.activeCustomers) : '—'}</span>
          <span className={billing.tileHint}>ativos, de {sum((a) => a.customers)}</span>
        </div>
        <div className={billing.tile}>
          <span className={billing.tileLabel}>A fechar</span>
          <span className={billing.tileValue}>{formatMoney(sum((a) => a.pendingCents))}</span>
          <span className={billing.tileHint}>comissões do mês ainda abertas</span>
        </div>
        <div className={billing.tile}>
          <span className={billing.tileLabel}>A pagar</span>
          <span className={billing.tileValue}>{formatMoney(sum((a) => a.openCents))}</span>
          <span className={billing.tileHint}>fechadas, em Contas a pagar</span>
        </div>
        <div className={billing.tile}>
          <span className={billing.tileLabel}>Pago</span>
          <span className={billing.tileValue}>{formatMoney(sum((a) => a.paidCents))}</span>
        </div>
      </div>

      <Card
        flush
        title="Afiliados"
        subtitle="Os links são criados aqui. Clique no afiliado para ver os links, o QR Code e a página dele."
        actions={
          <Button size="small" variant="primary" onClick={() => setCreating(true)}>
            Novo afiliado
          </Button>
        }
      >
        {affiliates.isLoading ? (
          <Spinner label="Carregando os afiliados" />
        ) : list.length === 0 ? (
          <EmptyState
            icon="🤝"
            title="Nenhum afiliado ainda"
            description="Crie o primeiro: ele recebe um link de cadastro para divulgar e uma página para acompanhar as indicações."
          />
        ) : (
          <div className={styles.tableWrap}>
            <table className={`${styles.table} ${styles.stackTable}`}>
              <thead>
                <tr>
                  <th>Afiliado</th>
                  <th>Link</th>
                  <th className={company.right}>Por cliente/mês</th>
                  <th className={company.right}>Cadastros</th>
                  <th className={company.right}>Clientes</th>
                  <th className={company.right}>A receber</th>
                  <th className={company.right}>Pago</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {list.map((a) => (
                  <tr key={a.id}>
                    <td data-label="Afiliado">
                      <span className={local.who}>
                        <strong>{a.name}</strong>
                        <span className={company.muted}>
                          {a.handle ? `@${a.handle}` : 'sem Instagram'}
                          {!a.active && ' · '}
                          {!a.active && <Badge tone="neutral">Inativo</Badge>}
                        </span>
                      </span>
                    </td>
                    <td data-label="Link">
                      <span className={local.code}>
                        /indicacao/{a.code}
                        <button type="button" className={local.copy} onClick={() => void copy(referralUrl(a.code))}>
                          Copiar
                        </button>
                      </span>
                    </td>
                    <td data-label="Por cliente/mês" className={company.right}>
                      {formatMoney(a.commissionCents)}
                    </td>
                    <td data-label="Cadastros" className={company.right}>
                      {a.signups}
                    </td>
                    <td data-label="Clientes" className={company.right} title={`${a.activeCustomers} ativos de ${a.customers}`}>
                      {a.activeCustomers}
                      {a.customers !== a.activeCustomers && <span className={company.muted}> / {a.customers}</span>}
                    </td>
                    <td data-label="A receber" className={company.right}>
                      {formatMoney(a.pendingCents + a.openCents)}
                    </td>
                    <td data-label="Pago" className={company.right}>
                      {formatMoney(a.paidCents)}
                    </td>
                    <td className={company.right}>
                      <Button size="small" variant="secondary" onClick={() => setEditing(a)}>
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

      <ClosingCard />
      <SettingsCard defaultCents={defaultCents} loaded={settings.isSuccess} />

      <AffiliateModal
        open={creating || editing !== null}
        affiliate={editing}
        defaultCents={defaultCents}
        onClose={() => {
          setCreating(false);
          setEditing(null);
        }}
        onSaved={(saved) => {
          // Criado: a janela continua aberta, agora com os links para copiar.
          setCreating(false);
          setEditing(saved);
        }}
      />
    </>
  );
}

/** O valor padrão dos afiliados novos (e, se quiser, de todos). */
function SettingsCard({ defaultCents, loaded }: { defaultCents: number; loaded: boolean }) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const [value, setValue] = useState(centsToInput(defaultCents));
  const [applyToAll, setApplyToAll] = useState(false);
  useEffect(() => setValue(centsToInput(defaultCents)), [defaultCents]);

  const save = useMutation({
    mutationFn: (cents: number) => affiliatesApi.saveSettings({ defaultCommissionCents: cents, applyToAll }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['affiliates'] });
      notify({
        tone: 'success',
        title: 'Valor padrão salvo',
        description: applyToAll ? 'Vale também para todos os afiliados, nos meses ainda não pagos.' : 'Vale para os afiliados novos.',
      });
      setApplyToAll(false);
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível salvar', description: err.message }),
  });

  const cents = parseMoney(value);
  return (
    <Card title="Valor por cliente" subtitle="Quanto o afiliado ganha por mês por cliente indicado que pagou a mensalidade.">
      <div className={local.settings}>
        <TextField label="Valor padrão (R$)" value={value} inputMode="decimal" onChange={(e) => setValue(e.target.value)} />
        <label className={company.check}>
          <input type="checkbox" checked={applyToAll} onChange={(e) => setApplyToAll(e.target.checked)} />
          Aplicar também aos afiliados atuais
        </label>
        <Button
          variant="primary"
          loading={save.isPending}
          disabled={!loaded || cents === null || (cents === defaultCents && !applyToAll)}
          onClick={() => cents !== null && save.mutate(cents)}
        >
          Salvar
        </Button>
      </div>
    </Card>
  );
}

/** O fechamento do mês: a prévia, o botão e o que já foi fechado. */
function ClosingCard() {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const today = new Date();
  const [month, setMonth] = useState(previousMonth(today));
  const [due, setDue] = useState(inDays(today, 5));

  const preview = useQuery({
    queryKey: ['affiliates', 'closing', month],
    queryFn: () => affiliatesApi.closing(month),
    enabled: /^\d{4}-\d{2}$/.test(month),
  });
  const payouts = useQuery({ queryKey: ['affiliates', 'payouts'], queryFn: affiliatesApi.payouts });
  const lines = preview.data ?? [];
  const total = lines.reduce((sum, l) => sum + l.amountCents, 0);

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['affiliates'] });
    queryClient.invalidateQueries({ queryKey: ['finance'] });
  };
  const close = useMutation({
    mutationFn: () => affiliatesApi.close({ month, dueDate: due }),
    onSuccess: (created) => {
      refresh();
      notify({
        tone: 'success',
        title: `${created.length} ${created.length === 1 ? 'conta a pagar criada' : 'contas a pagar criadas'}`,
        description: 'Estão em Empresa → Contas a pagar. Pagar a conta paga as comissões.',
      });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível fechar', description: err.message }),
  });
  const undo = useMutation({
    mutationFn: (id: string) => affiliatesApi.undoPayout(id),
    onSuccess: () => {
      refresh();
      notify({ tone: 'success', title: 'Fechamento desfeito', description: 'A conta a pagar saiu e as comissões voltaram a ficar abertas.' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível desfazer', description: err.message }),
  });

  return (
    <Card
      title="Fechamento do mês"
      subtitle="No começo de cada mês, feche o anterior: entram as comissões abertas até ele (as atrasadas também)."
    >
      <div className={local.closing}>
        <div className={local.closingFields}>
          <TextField label="Mês" type="month" value={month} onChange={(e) => setMonth(e.target.value)} />
          <TextField label="Vencimento das contas" type="date" value={due} onChange={(e) => setDue(e.target.value)} />
          <Button
            variant="primary"
            loading={close.isPending}
            disabled={lines.length === 0 || !due}
            onClick={() => close.mutate()}
          >
            Fechar {/^\d{4}-\d{2}$/.test(month) ? monthLabel(month) : 'o mês'}
          </Button>
        </div>

        {preview.isLoading ? (
          <Spinner label="Calculando" />
        ) : preview.isError ? (
          <p className={company.danger}>{(preview.error as Error).message}</p>
        ) : lines.length === 0 ? (
          <p className={company.muted}>Nenhuma comissão aberta até {monthLabel(month)}.</p>
        ) : (
          <ul className={company.list}>
            {lines.map((l) => (
              <li key={l.affiliateId} className={company.listItem}>
                <span className={local.who}>
                  <strong>{l.name}</strong>
                  <span className={company.muted}>
                    {l.commissions} {l.commissions === 1 ? 'comissão' : 'comissões'}
                    {l.pixKey ? ` · Pix ${l.pixKey}` : ' · sem chave Pix'}
                  </span>
                </span>
                <strong>{formatMoney(l.amountCents)}</strong>
              </li>
            ))}
            <li className={company.listItem}>
              <span>Total</span>
              <strong>{formatMoney(total)}</strong>
            </li>
          </ul>
        )}

        {(payouts.data?.length ?? 0) > 0 && (
          <div className={styles.tableWrap}>
            <table className={`${styles.table} ${styles.stackTable}`}>
              <thead>
                <tr>
                  <th>Fechamento</th>
                  <th>Afiliado</th>
                  <th className={company.right}>Valor</th>
                  <th>Vencimento</th>
                  <th>Situação</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {payouts.data!.map((p) => (
                  <tr key={p.id}>
                    <td data-label="Fechamento">até {monthLabel(p.month)}</td>
                    <td data-label="Afiliado">
                      {p.affiliateName}
                      <div className={company.muted}>
                        {p.commissions} {p.commissions === 1 ? 'comissão' : 'comissões'}
                      </div>
                    </td>
                    <td data-label="Valor" className={company.right}>
                      {formatMoney(p.amountCents)}
                    </td>
                    <td data-label="Vencimento">{p.dueDate ? formatDateOnly(p.dueDate) : '—'}</td>
                    <td data-label="Situação">
                      <Badge tone={PAYOUT_STATUS[p.status].tone}>{PAYOUT_STATUS[p.status].label}</Badge>
                      {p.paidOn && <div className={company.muted}>em {formatDateOnly(p.paidOn)}</div>}
                    </td>
                    <td className={company.right}>
                      {p.status !== 'PAID' && (
                        <Button size="small" variant="ghost" loading={undo.isPending && undo.variables === p.id} onClick={() => undo.mutate(p.id)}>
                          Desfazer
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </Card>
  );
}

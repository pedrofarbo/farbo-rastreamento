import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { chipsApi } from '@/api/resources';
import { ChipBulkModal } from '@/components/chips/ChipBulkModal';
import type { ChipInput } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Pager } from '@/components/ui/Pager';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { usePagedList } from '@/hooks/usePagination';
import { formatICCID, formatPhoneInput } from '@/services/format';
import type { Chip } from '@/types';

import pageStyles from '../Page.module.css';
import styles from './ChipsPage.module.css';

export const chipsKey = ['chips'] as const;

const EMPTY: ChipInput = { iccid: '', phoneNumber: '', carrier: '', apn: '', apnUser: '', apnPassword: '', notes: '' };
const CARRIERS = ['Algar', 'Vivo', 'Claro', 'TIM'];

type Filter = 'all' | 'free' | 'used';

/** O estoque de chips M2M (central): cadastro, correção e qual rastreador está com cada um. */
export function ChipsPage() {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const list = useQuery({ queryKey: chipsKey, queryFn: chipsApi.list });
  const [filter, setFilter] = useState<Filter>('all');
  const [search, setSearch] = useState('');
  // O chip em edição (null: chip novo); o formulário aberto é o form.
  const [editing, setEditing] = useState<Chip | null>(null);
  const [form, setForm] = useState<ChipInput | null>(null);
  const [formError, setFormError] = useState('');
  const [removing, setRemoving] = useState<Chip | null>(null);
  const [importing, setImporting] = useState(false);

  const all = list.data ?? [];
  const free = all.filter((c) => !c.device).length;
  const shown = useMemo(() => {
    const term = search.trim().toLowerCase();
    const digits = term.replace(/\D/g, '');
    return all.filter((c) => {
      if (filter === 'free' && c.device) return false;
      if (filter === 'used' && !c.device) return false;
      if (!term) return true;
      return (
        (digits.length >= 3 && (c.iccid.includes(digits) || c.phoneNumber.replace(/\D/g, '').includes(digits) || c.device?.imei.includes(digits))) ||
        [c.carrier, c.apn, c.notes].some((v) => v.toLowerCase().includes(term))
      );
    });
  }, [all, filter, search]);
  const paged = usePagedList(shown, 'chips', [filter, search]);

  const close = () => {
    setForm(null);
    setEditing(null);
    setFormError('');
  };
  const save = useMutation({
    mutationFn: (input: ChipInput) => (editing ? chipsApi.update(editing.id, input) : chipsApi.create(input)),
    onSuccess: (chip) => {
      void queryClient.invalidateQueries({ queryKey: chipsKey });
      if (chip.device) void queryClient.invalidateQueries({ queryKey: ['devices'] });
      notify({
        tone: 'success',
        title: editing ? 'Chip atualizado' : 'Chip cadastrado',
        description: editing && chip.device ? `O rastreador ${chip.device.imei} foi atualizado junto.` : undefined,
      });
      close();
    },
    onError: (err: Error) => setFormError(err.message),
  });
  const remove = useMutation({
    mutationFn: (chip: Chip) => chipsApi.remove(chip.id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: chipsKey });
      notify({ tone: 'success', title: 'Chip retirado do estoque' });
      setRemoving(null);
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para retirar', description: err.message }),
  });

  const open = (chip: Chip | null) => {
    setEditing(chip);
    setFormError('');
    setForm(
      chip
        ? { iccid: chip.iccid, phoneNumber: chip.phoneNumber, carrier: chip.carrier, apn: chip.apn, apnUser: chip.apnUser, apnPassword: '', notes: chip.notes }
        : EMPTY,
    );
  };

  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>Chips</h1>
            <p className={pageStyles.description}>
              O estoque de chips M2M. No cadastro do rastreador, escolha um chip livre: a linha, o ICCID e o APN vão
              para ele. Um chip digitado direto no rastreador também entra aqui.
            </p>
          </div>
          <div className={pageStyles.actions}>
            <Button onClick={() => setImporting(true)}>Importar em massa</Button>
            <Button variant="primary" onClick={() => open(null)}>
              Novo chip
            </Button>
          </div>
        </header>

        <Card>
          <div className={styles.toolbar}>
            <div className={pageStyles.tabs} role="tablist" aria-label="Situação">
              {(
                [
                  ['all', `Todos (${all.length})`],
                  ['free', `Livres (${free})`],
                  ['used', `Em uso (${all.length - free})`],
                ] as [Filter, string][]
              ).map(([key, label]) => (
                <button
                  key={key}
                  type="button"
                  role="tab"
                  aria-selected={filter === key}
                  className={`${pageStyles.tab} ${filter === key ? pageStyles.tabActive : ''}`}
                  onClick={() => setFilter(key)}
                >
                  {label}
                </button>
              ))}
            </div>
            <input
              type="search"
              className={styles.search}
              placeholder="Buscar por ICCID, linha, IMEI ou operadora"
              aria-label="Buscar chip"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
        </Card>

        <Card flush>
          {list.isLoading ? (
            <Spinner label="Carregando os chips" />
          ) : shown.length === 0 ? (
            <EmptyState
              icon="📶"
              title={all.length === 0 ? 'Nenhum chip no estoque' : 'Nenhum chip com esse filtro'}
              description={all.length === 0 ? 'Cadastre os chips quando chegarem, para escolher no cadastro do rastreador.' : undefined}
            />
          ) : (
            <>
              <div className={pageStyles.tableWrap}>
                <table className={`${pageStyles.table} ${styles.list}`}>
                  <thead>
                    <tr>
                      <th>ICCID</th>
                      <th>Linha</th>
                      <th>Operadora e APN</th>
                      <th>Situação</th>
                      <th aria-label="Ações" />
                    </tr>
                  </thead>
                  <tbody>
                    {paged.items.map((c) => (
                      <tr key={c.id}>
                        <td>
                          <span className={styles.iccid}>{formatICCID(c.iccid)}</span>
                          {c.notes && <div className={styles.muted}>{c.notes}</div>}
                        </td>
                        <td data-label="Linha">{c.phoneNumber ? formatPhoneInput(c.phoneNumber) : <span className={styles.muted}>—</span>}</td>
                        <td data-label="Operadora e APN">
                          {c.carrier || <span className={styles.muted}>—</span>}
                          {c.apn && (
                            <div className={styles.muted}>
                              {c.apn}
                              {c.apnUser ? ` · ${c.apnUser}` : ''}
                              {c.apnPasswordSet ? ' · com senha' : ''}
                            </div>
                          )}
                        </td>
                        <td data-label="Situação">
                          {c.device ? (
                            <>
                              <Badge tone="neutral">Em uso</Badge>
                              <div className={styles.muted}>rastreador {c.device.imei}</div>
                            </>
                          ) : (
                            <Badge tone="success">Livre</Badge>
                          )}
                        </td>
                        <td className={styles.rowActions}>
                          <Button size="small" variant="ghost" onClick={() => open(c)}>
                            Editar
                          </Button>
                          {!c.device && (
                            <Button size="small" variant="ghost" onClick={() => setRemoving(c)}>
                              Retirar
                            </Button>
                          )}
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

      <Modal
        open={form !== null}
        title={editing ? 'Editar chip' : 'Novo chip'}
        onClose={close}
        footer={
          <>
            <Button variant="ghost" onClick={close}>
              Cancelar
            </Button>
            <Button variant="primary" loading={save.isPending} onClick={() => form && save.mutate(form)}>
              Salvar
            </Button>
          </>
        }
      >
        {form && (
          <form
            className={pageStyles.form}
            onSubmit={(e) => {
              e.preventDefault();
              save.mutate(form);
            }}
          >
            {editing?.device && (
              <p className={pageStyles.note}>
                Este chip está no rastreador {editing.device.imei}: a linha, o APN e a senha corrigidos aqui vão para ele.
              </p>
            )}
            <TextField
              label="ICCID"
              placeholder="8955 3202 1000 9379 5330"
              inputMode="numeric"
              hint="Os 19 ou 20 números impressos no chip (começam com 89). Se colar com o SP da etiqueta, ele sai."
              value={form.iccid}
              onChange={(e) => setForm({ ...form, iccid: e.target.value })}
            />
            <div className={pageStyles.formRow}>
              <TextField
                label="Linha"
                placeholder="(34) 99999-0000"
                hint="O número do chip, com DDD: é para ele que vai a configuração por SMS."
                value={form.phoneNumber}
                onChange={(e) => setForm({ ...form, phoneNumber: formatPhoneInput(e.target.value, form.phoneNumber) })}
              />
              <TextField
                label="Operadora"
                list="chip-carriers"
                value={form.carrier}
                onChange={(e) => setForm({ ...form, carrier: e.target.value })}
              />
              <datalist id="chip-carriers">
                {CARRIERS.map((c) => (
                  <option key={c} value={c} />
                ))}
              </datalist>
            </div>
            <div className={styles.autofill}>
              <span>Chip Algar (APN ss.algar.br, usuário e senha algar)</span>
              <Button
                type="button"
                size="small"
                onClick={() =>
                  setForm({ ...form, carrier: 'Algar', apn: 'ss.algar.br', apnUser: 'algar', apnPassword: 'algar', clearApnPassword: false })
                }
              >
                Autopreencher
              </Button>
            </div>
            <div className={pageStyles.formRow}>
              <TextField label="APN" placeholder="ss.algar.br" value={form.apn} onChange={(e) => setForm({ ...form, apn: e.target.value })} />
              <TextField label="Usuário APN" value={form.apnUser} onChange={(e) => setForm({ ...form, apnUser: e.target.value })} />
              <TextField
                label="Senha APN"
                type="password"
                autoComplete="new-password"
                placeholder={editing?.apnPasswordSet && !form.clearApnPassword ? 'definida (em branco mantém)' : ''}
                disabled={Boolean(form.clearApnPassword)}
                value={form.apnPassword ?? ''}
                onChange={(e) => setForm({ ...form, apnPassword: e.target.value })}
              />
            </div>
            {editing?.apnPasswordSet && (
              <label className={styles.check}>
                <input
                  type="checkbox"
                  checked={Boolean(form.clearApnPassword)}
                  onChange={(e) => setForm({ ...form, clearApnPassword: e.target.checked, apnPassword: '' })}
                />
                Apagar a senha do APN
              </label>
            )}
            <TextField
              label="Observações"
              placeholder="Lote, nota fiscal, plano..."
              maxLength={500}
              value={form.notes}
              onChange={(e) => setForm({ ...form, notes: e.target.value })}
            />
            {formError && (
              <p className={styles.error} role="alert">
                {formError}
              </p>
            )}
            <button type="submit" hidden />
          </form>
        )}
      </Modal>

      <ChipBulkModal open={importing} onClose={() => setImporting(false)} existingIccids={all.map((c) => c.iccid)} />

      <Modal
        open={removing !== null}
        title="Retirar o chip do estoque?"
        onClose={() => setRemoving(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRemoving(null)}>
              Cancelar
            </Button>
            <Button variant="danger" loading={remove.isPending} onClick={() => removing && remove.mutate(removing)}>
              Retirar
            </Button>
          </>
        }
      >
        {removing && (
          <p>
            O chip <strong>{formatICCID(removing.iccid)}</strong>
            {removing.phoneNumber ? ` (${formatPhoneInput(removing.phoneNumber)})` : ''} sai do estoque. Use para chip
            cancelado, perdido ou devolvido à operadora.
          </p>
        )}
      </Modal>
    </div>
  );
}

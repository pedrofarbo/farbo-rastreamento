import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { usersApi } from '@/api/resources';
import type { TeamUserInput, TeamUserUpdate } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { Badge } from '@/components/ui/Badge';
import type { BadgeTone } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { ROLE_LABELS, TEAM_ROLES } from '@/services/roles';
import type { TeamRole } from '@/services/roles';
import { useAuth } from '@/stores/AuthContext';
import type { User } from '@/types';

import styles from '../Page.module.css';
import own from './Users.module.css';

const ROLE_TONE: Record<TeamRole, BadgeTone> = { admin: 'accent', operator: 'success', viewer: 'neutral' };

/** O mínimo que o servidor aceita (auth.ValidatePassword). */
const MIN_PASSWORD = 10;

export interface UserDraft {
  name: string;
  email: string;
  /** Sem perfil marcado de início: quem cadastra escolhe de propósito. */
  role: TeamRole | '';
  access: 'invite' | 'password';
  password: string;
  active: boolean;
}

export const EMPTY_USER: UserDraft = { name: '', email: '', role: '', access: 'invite', password: '', active: true };

/** Converte o formulário no corpo do cadastro, ou devolve o que falta. */
export function createInputFrom(d: UserDraft): TeamUserInput | string {
  if (!d.name.trim()) return 'Informe o nome.';
  if (!d.email.includes('@')) return 'Informe um e-mail válido.';
  if (!d.role) return 'Escolha o perfil de acesso.';
  if (d.access === 'password' && d.password.length < MIN_PASSWORD) {
    return `A senha precisa de pelo menos ${MIN_PASSWORD} caracteres.`;
  }
  return {
    name: d.name.trim(),
    email: d.email.trim(),
    role: d.role,
    password: d.access === 'password' ? d.password : '',
  };
}

/** Converte o formulário na alteração de alguém já cadastrado. */
export function updateInputFrom(d: UserDraft): TeamUserUpdate | string {
  if (!d.name.trim()) return 'Informe o nome.';
  if (!d.role) return 'Escolha o perfil de acesso.';
  return { name: d.name.trim(), role: d.role, active: d.active };
}

function draftFrom(u: User): UserDraft {
  return { ...EMPTY_USER, name: u.name, email: u.email, role: u.role as TeamRole, active: u.active };
}

/**
 * A equipe da central: quem entra no painel e com que perfil. Os clientes
 * têm acesso próprio e ficam em Clientes. Ninguém muda o próprio perfil nem
 * se desativa (o servidor garante, e também que sobre um administrador).
 */
export function UsersPage() {
  const { notify } = useToast();
  const { user: me } = useAuth();
  const queryClient = useQueryClient();

  const [draft, setDraft] = useState<UserDraft | null>(null);
  const [editing, setEditing] = useState<User | null>(null);
  const [formError, setFormError] = useState('');

  const users = useQuery({ queryKey: ['users'], queryFn: usersApi.list });
  const isSelf = editing !== null && editing.id === me?.id;

  const close = () => {
    setDraft(null);
    setEditing(null);
  };

  const save = useMutation({
    mutationFn: async (d: UserDraft) => {
      const input = editing ? updateInputFrom(d) : createInputFrom(d);
      if (typeof input === 'string') throw new Error(input);
      return editing
        ? usersApi.update(editing.id, input as TeamUserUpdate)
        : usersApi.create(input as TeamUserInput);
    },
    onSuccess: (saved, d) => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      if (!editing) {
        notify({
          tone: 'success',
          title: 'Usuário cadastrado',
          description:
            d.access === 'invite'
              ? `Convite enviado para ${saved.email}.`
              : 'Passe a senha à pessoa por um canal seguro.',
        });
      } else {
        // O perfil vai no token de acesso: as sessões caíram, e a mudança
        // vale quando o token atual vencer.
        const access = saved.role !== editing.role || (editing.active && !saved.active);
        notify({
          tone: 'success',
          title: 'Usuário atualizado',
          description: access
            ? `${saved.name}: as sessões abertas foram encerradas; a mudança vale em até 15 minutos.`
            : saved.name,
        });
      }
      close();
    },
    onError: (err: Error) => setFormError(err.message),
  });

  const invite = useMutation({
    mutationFn: (u: User) => usersApi.invite(u.id),
    onSuccess: (_, u) => notify({ tone: 'success', title: 'Convite reenviado', description: `Para ${u.email}.` }),
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível reenviar', description: err.message }),
  });

  const open = (u: User | null) => {
    setEditing(u);
    setFormError('');
    setDraft(u ? draftFrom(u) : EMPTY_USER);
  };

  const submit = () => {
    if (!draft) return;
    setFormError('');
    save.mutate(draft);
  };

  const list = users.data ?? [];
  const ready =
    draft !== null &&
    typeof (editing ? updateInputFrom(draft) : createInputFrom(draft)) !== 'string';

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Usuários</h1>
            <p className={styles.description}>
              Quem da equipe entra no painel e o que cada um pode fazer. Os clientes têm acesso
              próprio e ficam em Clientes.
            </p>
          </div>
          <div className={styles.actions}>
            <Button variant="primary" onClick={() => open(null)}>
              Novo usuário
            </Button>
          </div>
        </header>

        <Card flush>
          {users.isLoading ? (
            <Spinner label="Carregando usuários" />
          ) : users.isError ? (
            <EmptyState icon="⚠️" title="Não foi possível carregar" description={(users.error as Error).message} />
          ) : list.length === 0 ? (
            <EmptyState icon="👥" title="Nenhum usuário" description="Cadastre a equipe que vai usar o painel." />
          ) : (
            <div className={styles.tableWrap}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>Usuário</th>
                    <th>Perfil</th>
                    <th>Situação</th>
                    <th>Desde</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {list.map((u) => {
                    const role = u.role as TeamRole;
                    return (
                      <tr key={u.id}>
                        <td>
                          <strong>{u.name || u.email}</strong>
                          {u.id === me?.id && <span className={own.you}>você</span>}
                          <div className={billing.muted}>{u.email}</div>
                        </td>
                        <td>
                          <Badge tone={ROLE_TONE[role] ?? 'neutral'} title={TEAM_ROLES.find((r) => r.role === role)?.description}>
                            {ROLE_LABELS[role] ?? u.role}
                          </Badge>
                        </td>
                        <td>
                          {u.active ? (
                            <Badge tone="success" dot>
                              Ativo
                            </Badge>
                          ) : (
                            <Badge tone="neutral">Desativado</Badge>
                          )}
                        </td>
                        <td className={billing.muted}>{new Date(u.createdAt).toLocaleDateString('pt-BR')}</td>
                        <td>
                          <div className={styles.actions}>
                            <Button size="small" variant="ghost" onClick={() => open(u)}>
                              Editar
                            </Button>
                            {u.active && (
                              <Button
                                size="small"
                                variant="ghost"
                                title="Manda de novo o link para criar a senha; o anterior deixa de valer."
                                loading={invite.isPending && invite.variables?.id === u.id}
                                onClick={() => invite.mutate(u)}
                              >
                                Reenviar convite
                              </Button>
                            )}
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </Card>

        <Card title="Perfis de acesso">
          <dl className={own.legend}>
            {TEAM_ROLES.map((r) => (
              <div key={r.role}>
                <dt>
                  <Badge tone={ROLE_TONE[r.role]}>{r.label}</Badge>
                </dt>
                <dd>{r.description}</dd>
              </div>
            ))}
          </dl>
        </Card>
      </div>

      <Modal
        open={draft !== null}
        wide
        title={editing ? 'Editar usuário' : 'Novo usuário'}
        onClose={close}
        footer={
          <>
            <Button variant="ghost" onClick={close}>
              Cancelar
            </Button>
            <Button variant="primary" loading={save.isPending} disabled={!ready} onClick={submit}>
              {editing ? 'Salvar' : 'Cadastrar'}
            </Button>
          </>
        }
      >
        {draft && (
          <div className={styles.form}>
            {formError && (
              <div className={styles.note} role="alert">
                {formError}
              </div>
            )}
            <div className={styles.formRow}>
              <TextField
                name="name"
                label="Nome"
                required
                autoFocus
                value={draft.name}
                onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              />
              {editing ? (
                <TextField name="email" label="E-mail" value={draft.email} disabled hint="O e-mail é o login e não muda." />
              ) : (
                <TextField
                  name="email"
                  label="E-mail"
                  type="email"
                  required
                  autoComplete="off"
                  value={draft.email}
                  onChange={(e) => setDraft({ ...draft, email: e.target.value })}
                />
              )}
            </div>

            <fieldset className={own.roles} disabled={isSelf}>
              <legend className={styles.infoLabel}>Perfil de acesso</legend>
              {TEAM_ROLES.map((r) => (
                <label key={r.role} className={`${own.role} ${draft.role === r.role ? own.roleChecked : ''}`}>
                  <input
                    type="radio"
                    name="user-role"
                    value={r.role}
                    checked={draft.role === r.role}
                    disabled={isSelf}
                    onChange={() => setDraft({ ...draft, role: r.role })}
                  />
                  <span>
                    <strong>{r.label}</strong>
                    <span className={own.roleDescription}>{r.description}</span>
                  </span>
                </label>
              ))}
              {isSelf && <p className={billing.muted}>Você não pode mudar o próprio perfil: peça a outro administrador.</p>}
            </fieldset>

            {editing ? (
              <label className={own.check}>
                <input
                  id="user-active"
                  type="checkbox"
                  checked={draft.active}
                  disabled={isSelf}
                  onChange={(e) => setDraft({ ...draft, active: e.target.checked })}
                />
                <span>
                  Acesso ativo
                  <span className={own.roleDescription}>
                    {isSelf
                      ? 'Você não pode se desativar.'
                      : 'Desativado, não entra mais no painel e as sessões abertas são encerradas.'}
                  </span>
                </span>
              </label>
            ) : (
              <>
                <fieldset className={own.roles}>
                  <legend className={styles.infoLabel}>Senha</legend>
                  <label className={own.check}>
                    <input
                      type="radio"
                      name="user-access"
                      checked={draft.access === 'invite'}
                      onChange={() => setDraft({ ...draft, access: 'invite' })}
                    />
                    Enviar convite por e-mail para a pessoa criar a senha (recomendado)
                  </label>
                  <label className={own.check}>
                    <input
                      type="radio"
                      name="user-access"
                      checked={draft.access === 'password'}
                      onChange={() => setDraft({ ...draft, access: 'password' })}
                    />
                    Definir uma senha agora
                  </label>
                </fieldset>
                {draft.access === 'password' && (
                  <TextField
                    name="password"
                    label="Senha inicial"
                    type="password"
                    autoComplete="new-password"
                    hint={`Mínimo de ${MIN_PASSWORD} caracteres. Passe à pessoa por um canal seguro.`}
                    value={draft.password}
                    onChange={(e) => setDraft({ ...draft, password: e.target.value })}
                  />
                )}
              </>
            )}
          </div>
        )}
      </Modal>
    </div>
  );
}

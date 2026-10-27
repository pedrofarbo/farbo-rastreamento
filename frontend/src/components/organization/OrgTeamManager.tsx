import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import type { OrgMemberInput, OrgTeamApi } from '@/api/resources';
import styles from '@/components/account/TeamManager.module.css';
import { IdentityCheck } from '@/components/auth/IdentityCheck';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { ORG_ROLES } from '@/services/roles';
import { formatRelative } from '@/services/format';
import type { OrgMember, OrgRole } from '@/types';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

const EMPTY: OrgMemberInput = { name: '', email: '', role: 'operator' };

/** O que falta no formulário, ou null se dá para seguir. */
export function orgMemberProblem(d: OrgMemberInput): string | null {
  if (!d.name.trim()) return 'Informe o nome da pessoa.';
  if (!/^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/.test(d.email.trim())) return 'Informe um e-mail válido.';
  return null;
}

const ROLE_TONE: Record<OrgRole, 'accent' | 'success' | 'neutral'> = {
  admin: 'accent',
  operator: 'success',
  viewer: 'neutral',
  broker: 'neutral',
};

/**
 * A equipe de uma associação ou seguradora: no painel dela (o Administrador,
 * com a confirmação extra para pôr alguém) ou pela central. Cada pessoa entra
 * com o próprio e-mail e senha; a organização precisa de pelo menos um
 * Administrador.
 */
export function OrgTeamManager({
  api,
  queryKey,
  confirmIdentity,
  selfId,
  orgName,
}: {
  api: OrgTeamApi;
  queryKey: readonly unknown[];
  /** Pôr alguém pede a biometria ou a senha (no painel da organização). */
  confirmIdentity: boolean;
  /** Quem está vendo (não muda o próprio papel nem sai da equipe por aqui). */
  selfId?: string;
  orgName: string;
}) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [draft, setDraft] = useState<OrgMemberInput>(EMPTY);
  const [verifying, setVerifying] = useState<OrgMemberInput | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);
  const [error, setError] = useState('');

  const team = useQuery({ queryKey, queryFn: api.list });
  const list = team.data ?? [];
  const paged = usePagedList(list, 'org-team');
  const refresh = () => queryClient.invalidateQueries({ queryKey });

  const add = useMutation({
    mutationFn: ({ input, token }: { input: OrgMemberInput; token?: string }) => api.add(input, token),
    onSuccess: (member) => {
      refresh();
      setDraft(EMPTY);
      setVerifying(null);
      notify({ tone: 'success', title: `${member.name} entrou na equipe`, description: `Mandamos para ${member.email} o convite para criar a senha.` });
    },
    onError: (err: Error) => {
      setVerifying(null);
      setError(err.message);
    },
  });
  const setRole = useMutation({
    mutationFn: ({ member, role }: { member: OrgMember; role: OrgRole }) => api.setRole(member.userId, role),
    onSuccess: (member) => {
      refresh();
      notify({ tone: 'success', title: `${member.name} agora é ${ORG_ROLES[member.role].label}` });
    },
    onError: (err: Error) => setError(err.message),
  });
  const remove = useMutation({
    mutationFn: (member: OrgMember) => api.remove(member.userId),
    onSuccess: (_, member) => {
      refresh();
      setRemoving(null);
      notify({ tone: 'success', title: `${member.name} saiu da equipe`, description: 'O acesso acabou na hora.' });
    },
    onError: (err: Error) => setError(err.message),
  });
  const invite = useMutation({
    mutationFn: (member: OrgMember) => api.invite(member.userId),
    onSuccess: (res) => notify({ tone: 'success', title: 'Convite enviado', description: res.message }),
    onError: (err: Error) => setError(err.message),
  });

  const submit = () => {
    const problem = orgMemberProblem(draft);
    if (problem) return setError(problem);
    setError('');
    const input = { ...draft, name: draft.name.trim(), email: draft.email.trim() };
    if (confirmIdentity) setVerifying(input);
    else add.mutate({ input });
  };

  if (verifying) {
    return add.isPending ? (
      <Spinner label="Pondo na equipe" />
    ) : (
      <div className={styles.body}>
        <IdentityCheck
          purpose="team_member"
          action={`Para dar a ${verifying.name} acesso ao painel da ${orgName} como ${ORG_ROLES[verifying.role].label}`}
          onGrant={(token) => add.mutate({ input: verifying, token })}
        />
        <Button variant="ghost" onClick={() => setVerifying(null)}>
          Cancelar
        </Button>
      </div>
    );
  }

  return (
    <div className={styles.body}>
      <div className={styles.roles}>
        {(Object.keys(ORG_ROLES) as OrgRole[]).map((role) => (
          <div key={role} className={styles.roleCard}>
            <strong>{ORG_ROLES[role].label}</strong>
            {ORG_ROLES[role].description}
          </div>
        ))}
      </div>

      {error && (
        <div className={styles.error} role="alert">
          {error}
        </div>
      )}

      <section>
        <h3 className={styles.heading}>Quem é da equipe</h3>
        {team.isLoading ? (
          <Spinner label="Carregando a equipe" />
        ) : list.length === 0 ? (
          <p className={styles.muted}>Ninguém entra no painel ainda.</p>
        ) : (
          <>
            <ul className={styles.people}>
              {paged.items.map((member) => (
                <li key={member.userId} className={styles.person}>
                  <div className={styles.who}>
                    <strong>
                      {member.name}
                      {member.userId === selfId ? ' (você)' : ''}
                    </strong>
                    <span className={styles.muted}>{member.email}</span>
                    <span className={styles.muted}>
                      {member.lastSeenAt ? `Entrou ${formatRelative(member.lastSeenAt)}` : 'Ainda não entrou (convite pendente)'}
                    </span>
                  </div>
                  <Badge tone={ROLE_TONE[member.role]}>{ORG_ROLES[member.role].label}</Badge>
                  {member.userId !== selfId && (
                    <div className={styles.personActions}>
                      {removing === member.userId ? (
                        <span className={styles.confirm}>
                          Tirar {member.name} da equipe? O acesso acaba na hora.
                          <Button size="small" variant="danger" loading={remove.isPending} onClick={() => remove.mutate(member)}>
                            Tirar
                          </Button>
                          <Button size="small" variant="ghost" onClick={() => setRemoving(null)}>
                            Não
                          </Button>
                        </span>
                      ) : (
                        <>
                          <select
                            className={styles.roleSelect}
                            aria-label={`Papel de ${member.name}`}
                            value={member.role}
                            disabled={setRole.isPending}
                            onChange={(e) => setRole.mutate({ member, role: e.target.value as OrgRole })}
                          >
                            {(Object.keys(ORG_ROLES) as OrgRole[]).map((role) => (
                              <option key={role} value={role}>
                                {ORG_ROLES[role].label}
                              </option>
                            ))}
                          </select>
                          {!member.lastSeenAt && (
                            <Button size="small" variant="ghost" loading={invite.isPending} onClick={() => invite.mutate(member)}>
                              Reenviar convite
                            </Button>
                          )}
                          <Button size="small" variant="ghost" onClick={() => setRemoving(member.userId)}>
                            Tirar da equipe
                          </Button>
                        </>
                      )}
                    </div>
                  )}
                </li>
              ))}
            </ul>
            <Pager {...paged.pager} />
          </>
        )}
      </section>

      <section className={styles.add}>
        <h3 className={styles.heading} style={{ margin: 0 }}>
          Pôr alguém na equipe
        </h3>
        <TextField label="Nome" value={draft.name} maxLength={120} onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
        <TextField
          label="E-mail"
          type="email"
          value={draft.email}
          maxLength={254}
          hint="A pessoa recebe neste e-mail o convite para criar a senha."
          onChange={(e) => setDraft({ ...draft, email: e.target.value })}
        />
        <div className={styles.choices} role="radiogroup" aria-label="Papel">
          {(Object.keys(ORG_ROLES) as OrgRole[]).map((role) => (
            <label key={role} className={`${styles.choice} ${draft.role === role ? styles.choiceActive : ''}`}>
              <input type="radio" name="org-role" checked={draft.role === role} onChange={() => setDraft({ ...draft, role })} />
              {ORG_ROLES[role].label}
            </label>
          ))}
        </div>
        <Button variant="primary" onClick={submit} loading={add.isPending && !confirmIdentity}>
          Pôr na equipe
        </Button>
      </section>
    </div>
  );
}

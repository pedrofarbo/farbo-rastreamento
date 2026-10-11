import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { fleetApi, teamApi } from '@/api/resources';
import { IdentityCheck } from '@/components/auth/IdentityCheck';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { fleetKey } from '@/hooks/useFleet';
import type { AccountMember, MemberRole, VehicleGroup } from '@/types';

import styles from './TeamManager.module.css';

export const teamKey = ['me', 'team'] as const;

/** O tamanho mínimo da equipe, enquanto o servidor não diz o da conta (cresce com a frota). */
export const MIN_TEAM = 30;

/** Os papéis, como a tela explica. */
export const ROLES: Record<MemberRole, { label: string; description: string }> = {
  manager: {
    label: 'Gestor',
    description:
      'Acompanha e opera os veículos: bloqueio do motor, modo roubo, cercas, dados dos veículos e faturas. Não mexe na equipe nem no cadastro da conta.',
  },
  viewer: {
    label: 'Visualizador',
    description: 'Só acompanha: mapa ao vivo, histórico, eventos e cercas. Não envia comandos nem vê faturas.',
  },
};

/** Os veículos que a pessoa vê: todos, ou só os de alguns grupos. */
interface Access {
  restricted: boolean;
  groups: string[];
}

interface Draft extends Access {
  name: string;
  email: string;
  role: MemberRole;
}

const EMPTY: Draft = { name: '', email: '', role: 'viewer', restricted: false, groups: [] };

/** O que falta no formulário, ou null se dá para seguir. */
export function memberDraftProblem(d: Draft): string | null {
  if (!d.name.trim()) return 'Informe o nome da pessoa.';
  if (!/^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/.test(d.email.trim())) return 'Informe um e-mail válido.';
  if (d.restricted && d.groups.length === 0) return 'Escolha ao menos um grupo (ou libere todos os veículos).';
  return null;
}

/** O que a pessoa vê, em uma linha: "Todos os veículos" ou "Grupos: Norte, Sul". */
export function accessLabel(member: Pick<AccountMember, 'restricted' | 'groups'>, groups: VehicleGroup[]): string {
  if (!member.restricted) return 'Todos os veículos';
  const names = (member.groups ?? []).map((id) => groups.find((g) => g.id === id)?.name).filter(Boolean);
  if (names.length === 0) return 'Nenhum veículo: os grupos liberados foram apagados';
  return `${names.length === 1 ? 'Grupo' : 'Grupos'}: ${names.join(', ')}`;
}

/** Todos os veículos ou só alguns grupos, com a lista dos grupos da conta. */
function AccessFields({
  value,
  onChange,
  groups,
  name,
}: {
  value: Access;
  onChange: (access: Access) => void;
  groups: VehicleGroup[];
  name: string;
}) {
  return (
    <fieldset className={styles.access}>
      <legend>Veículos que a pessoa vê</legend>
      <div className={styles.choices} role="radiogroup" aria-label="Veículos que a pessoa vê">
        <label className={`${styles.choice} ${!value.restricted ? styles.choiceActive : ''}`}>
          <input type="radio" name={name} checked={!value.restricted} onChange={() => onChange({ restricted: false, groups: [] })} />
          Todos os veículos
        </label>
        <label className={`${styles.choice} ${value.restricted ? styles.choiceActive : ''} ${groups.length === 0 ? styles.choiceDisabled : ''}`}>
          <input
            type="radio"
            name={name}
            checked={value.restricted}
            disabled={groups.length === 0}
            onChange={() => onChange({ restricted: true, groups: value.groups })}
          />
          Só alguns grupos
        </label>
      </div>
      {groups.length === 0 ? (
        <p className={styles.muted}>Para restringir a alguns veículos, crie grupos em Meus veículos e ponha os veículos neles.</p>
      ) : value.restricted ? (
        <>
          <ul className={styles.groupList}>
            {groups.map((g) => (
              <li key={g.id}>
                <label>
                  <input
                    type="checkbox"
                    checked={value.groups.includes(g.id)}
                    onChange={(e) =>
                      onChange({
                        restricted: true,
                        groups: e.target.checked ? [...value.groups, g.id] : value.groups.filter((id) => id !== g.id),
                      })
                    }
                  />
                  {g.name}
                  <span className={styles.muted}>
                    {g.vehicles} {g.vehicles === 1 ? 'veículo' : 'veículos'}
                  </span>
                </label>
              </li>
            ))}
          </ul>
          <p className={styles.muted}>
            A pessoa vê, opera e recebe alertas só dos veículos desses grupos. Faturas e a organização da frota ficam de fora.
          </p>
        </>
      ) : null}
    </fieldset>
  );
}

/**
 * A equipe da conta empresa: o titular põe as pessoas da empresa, cada uma
 * com o próprio login, como Gestor (opera) ou Visualizador (só acompanha).
 * Pôr alguém pede a confirmação do titular (biometria ou senha); trocar o
 * papel e tirar valem na hora.
 */
export function TeamManager() {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [draft, setDraft] = useState<Draft>(EMPTY);
  const [verifying, setVerifying] = useState<Draft | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);
  // A pessoa com os veículos em edição, e o rascunho.
  const [editing, setEditing] = useState<{ userId: string; access: Access } | null>(null);
  const [error, setError] = useState('');

  const team = useQuery({ queryKey: teamKey, queryFn: teamApi.list });
  const limit = useQuery({ queryKey: [...teamKey, 'limit'], queryFn: teamApi.limit });
  const fleet = useQuery({ queryKey: fleetKey, queryFn: fleetApi.get, staleTime: 60_000 });
  const groups = fleet.data?.groups ?? [];
  const list = team.data ?? [];
  const refresh = () => queryClient.invalidateQueries({ queryKey: teamKey });

  const add = useMutation({
    mutationFn: ({ input, token }: { input: Parameters<typeof teamApi.add>[0]; token: string }) => teamApi.add(input, token),
    onSuccess: (member) => {
      refresh();
      setDraft(EMPTY);
      setVerifying(null);
      notify({
        tone: 'success',
        title: `${member.name} entrou na equipe`,
        description: `Mandamos para ${member.email} o convite para entrar.`,
      });
    },
    onError: (err: Error) => {
      setVerifying(null);
      setError(err.message);
    },
  });

  const setRole = useMutation({
    mutationFn: ({ member, role }: { member: AccountMember; role: MemberRole }) => teamApi.setRole(member.userId, role),
    onSuccess: (member) => {
      refresh();
      notify({ tone: 'success', title: `${member.name} agora é ${ROLES[member.role].label}` });
    },
    onError: (err: Error) => setError(err.message),
  });

  const setGroups = useMutation({
    mutationFn: ({ userId, access }: { userId: string; access: Access }) =>
      teamApi.setGroups(userId, access.restricted ? access.groups : []),
    onSuccess: (member) => {
      refresh();
      setEditing(null);
      notify({ tone: 'success', title: `${member.name}: ${accessLabel(member, groups).toLowerCase()}` });
    },
    onError: (err: Error) => setError(err.message),
  });

  const remove = useMutation({
    mutationFn: (member: AccountMember) => teamApi.remove(member.userId),
    onSuccess: (_, member) => {
      refresh();
      setRemoving(null);
      notify({ tone: 'success', title: `${member.name} saiu da equipe`, description: 'O acesso dela acabou na hora.' });
    },
    onError: (err: Error) => setError(err.message),
  });

  const submit = () => {
    const problem = memberDraftProblem(draft);
    if (problem) return setError(problem);
    setError('');
    setVerifying({ ...draft, name: draft.name.trim(), email: draft.email.trim(), groups: draft.restricted ? draft.groups : [] });
  };

  if (verifying) {
    return add.isPending ? (
      <Spinner label="Pondo na equipe" />
    ) : (
      <div className={styles.body}>
        <IdentityCheck
          purpose="team_member"
          action={`Para dar a ${verifying.name} acesso aos veículos ${verifying.restricted ? 'dos grupos escolhidos ' : ''}da sua conta como ${ROLES[verifying.role].label}`}
          onGrant={(token) =>
            add.mutate({
              input: { name: verifying.name, email: verifying.email, role: verifying.role, groups: verifying.groups },
              token,
            })
          }
        />
        <Button variant="ghost" onClick={() => setVerifying(null)}>
          Cancelar
        </Button>
      </div>
    );
  }

  const max = limit.data?.limit ?? MIN_TEAM;
  const full = list.length >= max;

  return (
    <div className={styles.body}>
      <p className={styles.intro}>
        Dê acesso aos veículos da sua conta para as pessoas da sua empresa. Cada uma entra com o próprio e-mail e senha,
        recebe os alertas que escolher e aparece no histórico de quem fez o quê. Você escolhe também quais veículos cada
        uma vê: todos, ou só os de alguns grupos (o supervisor de uma região, por exemplo). Você continua sendo o titular:
        só você mexe na equipe, no cadastro e nos pedidos.
      </p>

      <div className={styles.roles}>
        {(Object.keys(ROLES) as MemberRole[]).map((role) => (
          <div key={role} className={styles.roleCard}>
            <strong>{ROLES[role].label}</strong>
            {ROLES[role].description}
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
          <p className={styles.muted}>Ninguém além de você acessa esta conta.</p>
        ) : (
          <ul className={styles.people}>
            {list.map((member) => (
              <li key={member.userId} className={styles.person}>
                <div className={styles.who}>
                  <strong>{member.name}</strong>
                  <span className={styles.muted}>{member.email}</span>
                  <span className={member.restricted ? styles.scoped : styles.muted}>{accessLabel(member, groups)}</span>
                </div>
                <Badge tone={member.role === 'manager' ? 'success' : 'neutral'}>{ROLES[member.role].label}</Badge>
                <div className={styles.personActions}>
                  {editing?.userId === member.userId ? (
                    <div className={styles.editAccess}>
                      <AccessFields
                        value={editing.access}
                        onChange={(access) => setEditing({ userId: member.userId, access })}
                        groups={groups}
                        name={`access-${member.userId}`}
                      />
                      <div className={styles.editActions}>
                        <Button
                          size="small"
                          variant="primary"
                          loading={setGroups.isPending}
                          disabled={editing.access.restricted && editing.access.groups.length === 0}
                          onClick={() => setGroups.mutate(editing)}
                        >
                          Salvar
                        </Button>
                        <Button size="small" variant="ghost" onClick={() => setEditing(null)}>
                          Cancelar
                        </Button>
                      </div>
                    </div>
                  ) : removing === member.userId ? (
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
                        onChange={(e) => setRole.mutate({ member, role: e.target.value as MemberRole })}
                      >
                        {(Object.keys(ROLES) as MemberRole[]).map((role) => (
                          <option key={role} value={role}>
                            {ROLES[role].label}
                          </option>
                        ))}
                      </select>
                      <Button
                        size="small"
                        variant="ghost"
                        onClick={() =>
                          setEditing({
                            userId: member.userId,
                            access: { restricted: member.restricted, groups: member.groups ?? [] },
                          })
                        }
                      >
                        Veículos
                      </Button>
                      <Button size="small" variant="ghost" onClick={() => setRemoving(member.userId)}>
                        Tirar da equipe
                      </Button>
                    </>
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className={styles.add}>
        <h3 className={styles.heading} style={{ margin: 0 }}>
          Pôr alguém na equipe
        </h3>
        {full ? (
          <p className={styles.muted}>
            A equipe já tem {max} pessoas, o máximo para o tamanho da sua frota: tire alguém para pôr outra.
          </p>
        ) : (
          <>
            <TextField
              label="Nome"
              value={draft.name}
              maxLength={120}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
            />
            <TextField
              label="E-mail"
              type="email"
              value={draft.email}
              maxLength={254}
              hint="A pessoa recebe neste e-mail o convite para criar a senha."
              onChange={(e) => setDraft({ ...draft, email: e.target.value })}
            />
            <div className={styles.choices} role="radiogroup" aria-label="Papel">
              {(Object.keys(ROLES) as MemberRole[]).map((role) => (
                <label key={role} className={`${styles.choice} ${draft.role === role ? styles.choiceActive : ''}`}>
                  <input
                    type="radio"
                    name="team-role"
                    checked={draft.role === role}
                    onChange={() => setDraft({ ...draft, role })}
                  />
                  {ROLES[role].label}
                </label>
              ))}
            </div>
            <AccessFields
              value={draft}
              onChange={(access) => setDraft({ ...draft, ...access })}
              groups={groups}
              name="team-access"
            />
            <Button variant="primary" onClick={submit}>
              Pôr na equipe
            </Button>
          </>
        )}
      </section>
    </div>
  );
}

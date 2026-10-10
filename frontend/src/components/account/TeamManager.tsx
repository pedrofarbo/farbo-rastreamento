import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { teamApi } from '@/api/resources';
import { IdentityCheck } from '@/components/auth/IdentityCheck';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import type { AccountMember, MemberRole } from '@/types';

import styles from './TeamManager.module.css';

export const teamKey = ['me', 'team'] as const;

/** O tamanho máximo da equipe (o servidor confere). */
export const MAX_TEAM = 30;

/** Os papéis, como a tela explica. */
export const ROLES: Record<MemberRole, { label: string; description: string }> = {
  manager: {
    label: 'Gestor',
    description:
      'Acompanha e opera todos os veículos: bloqueio do motor, modo roubo, cercas, dados dos veículos e faturas. Não mexe na equipe nem no cadastro da conta.',
  },
  viewer: {
    label: 'Visualizador',
    description: 'Só acompanha: mapa ao vivo, histórico, eventos e cercas. Não envia comandos nem vê faturas.',
  },
};

interface Draft {
  name: string;
  email: string;
  role: MemberRole;
}

const EMPTY: Draft = { name: '', email: '', role: 'viewer' };

/** O que falta no formulário, ou null se dá para seguir. */
export function memberDraftProblem(d: Draft): string | null {
  if (!d.name.trim()) return 'Informe o nome da pessoa.';
  if (!/^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/.test(d.email.trim())) return 'Informe um e-mail válido.';
  return null;
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
  const [error, setError] = useState('');

  const team = useQuery({ queryKey: teamKey, queryFn: teamApi.list });
  const list = team.data ?? [];
  const refresh = () => queryClient.invalidateQueries({ queryKey: teamKey });

  const add = useMutation({
    mutationFn: ({ input, token }: { input: Draft; token: string }) => teamApi.add(input, token),
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
    setVerifying({ name: draft.name.trim(), email: draft.email.trim(), role: draft.role });
  };

  if (verifying) {
    return add.isPending ? (
      <Spinner label="Pondo na equipe" />
    ) : (
      <div className={styles.body}>
        <IdentityCheck
          purpose="team_member"
          action={`Para dar a ${verifying.name} acesso aos veículos da sua conta como ${ROLES[verifying.role].label}`}
          onGrant={(token) => add.mutate({ input: verifying, token })}
        />
        <Button variant="ghost" onClick={() => setVerifying(null)}>
          Cancelar
        </Button>
      </div>
    );
  }

  const full = list.length >= MAX_TEAM;

  return (
    <div className={styles.body}>
      <p className={styles.intro}>
        Dê acesso aos veículos da sua conta para as pessoas da sua empresa. Cada uma entra com o próprio e-mail e senha,
        recebe os alertas que escolher e aparece no histórico de quem fez o quê. Você continua sendo o titular: só você
        mexe na equipe, no cadastro e nos pedidos.
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
                </div>
                <Badge tone={member.role === 'manager' ? 'success' : 'neutral'}>{ROLES[member.role].label}</Badge>
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
                        onChange={(e) => setRole.mutate({ member, role: e.target.value as MemberRole })}
                      >
                        {(Object.keys(ROLES) as MemberRole[]).map((role) => (
                          <option key={role} value={role}>
                            {ROLES[role].label}
                          </option>
                        ))}
                      </select>
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
          <p className={styles.muted}>A equipe já tem {MAX_TEAM} pessoas: tire alguém para pôr outra.</p>
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
            <Button variant="primary" onClick={submit}>
              Pôr na equipe
            </Button>
          </>
        )}
      </section>
    </div>
  );
}

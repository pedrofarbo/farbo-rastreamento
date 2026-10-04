import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { sharesApi } from '@/api/resources';
import type { VehicleShareInput } from '@/api/resources';
import { IdentityCheck } from '@/components/auth/IdentityCheck';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import type { VehicleShare, VehicleView } from '@/types';

import styles from './SharesModal.module.css';

/** Quantas pessoas podem acompanhar um mesmo veículo (o servidor confere). */
export const MAX_SHARES = 5;

export const sharesKey = ['me', 'shares'] as const;

type Step =
  | { kind: 'list' }
  | { kind: 'verify-create'; input: VehicleShareInput }
  | { kind: 'verify-block'; share: VehicleShare };

interface Draft {
  name: string;
  email: string;
  canBlock: boolean;
}

const EMPTY: Draft = { name: '', email: '', canBlock: false };

/** O que falta no formulário, ou null se dá para seguir. */
export function shareDraftProblem(d: Draft): string | null {
  if (!d.name.trim()) return 'Informe o nome da pessoa.';
  if (!/^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/.test(d.email.trim())) return 'Informe um e-mail válido.';
  return null;
}

/**
 * Quem acompanha um veículo do cliente. Cada pessoa entra com a própria
 * conta e vê a posição ao vivo; com o bloqueio liberado, pode bloquear o
 * motor numa emergência (nunca desbloquear). Dar acesso e liberar o
 * bloqueio pedem a confirmação do dono (biometria ou senha).
 */
export function SharesModal({ vehicle, onClose }: { vehicle: VehicleView | null; onClose: () => void }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [step, setStep] = useState<Step>({ kind: 'list' });
  const [draft, setDraft] = useState<Draft>(EMPTY);
  const [error, setError] = useState('');
  const [removing, setRemoving] = useState<string | null>(null);

  const shares = useQuery({ queryKey: sharesKey, queryFn: sharesApi.list, enabled: vehicle !== null });
  const list = (shares.data ?? []).filter((s) => s.vehicleId === vehicle?.id);

  // Cada abertura começa do zero.
  useEffect(() => {
    setStep({ kind: 'list' });
    setDraft(EMPTY);
    setError('');
    setRemoving(null);
  }, [vehicle?.id]);

  const refresh = () => queryClient.invalidateQueries({ queryKey: sharesKey });

  const create = useMutation({
    mutationFn: ({ input, token }: { input: VehicleShareInput; token: string }) => sharesApi.create(input, token),
    onSuccess: (share) => {
      refresh();
      notify({
        tone: 'success',
        title: `Acesso dado a ${share.guestName}`,
        description: `Avisamos ${share.guestEmail} por e-mail.`,
      });
      setDraft(EMPTY);
      setStep({ kind: 'list' });
    },
    onError: (err: Error) => {
      setError(err.message);
      setStep({ kind: 'list' });
    },
  });

  const setBlock = useMutation({
    mutationFn: ({ share, canBlock, token }: { share: VehicleShare; canBlock: boolean; token?: string }) =>
      sharesApi.setCanBlock(share.id, canBlock, token),
    onSuccess: (share) => {
      refresh();
      notify({
        tone: 'success',
        title: share.canBlock ? `${share.guestName} pode bloquear` : `${share.guestName} só acompanha`,
      });
      setStep({ kind: 'list' });
    },
    onError: (err: Error) => {
      setError(err.message);
      setStep({ kind: 'list' });
    },
  });

  const remove = useMutation({
    mutationFn: (share: VehicleShare) => sharesApi.remove(share.id),
    onSuccess: (_, share) => {
      refresh();
      setRemoving(null);
      notify({ tone: 'success', title: `${share.guestName} não acompanha mais ${share.vehicleName}` });
    },
    onError: (err: Error) => setError(err.message),
  });

  const submit = () => {
    if (!vehicle) return;
    const problem = shareDraftProblem(draft);
    if (problem) return setError(problem);
    setError('');
    setStep({
      kind: 'verify-create',
      input: { vehicleId: vehicle.id, name: draft.name.trim(), email: draft.email.trim(), canBlock: draft.canBlock },
    });
  };

  const verifying = step.kind !== 'list';

  return (
    <Modal
      open={vehicle !== null}
      wide
      title={vehicle ? `Acessos · ${vehicle.name}` : ''}
      onClose={onClose}
      footer={
        verifying ? (
          <Button variant="ghost" onClick={() => setStep({ kind: 'list' })}>
            Cancelar
          </Button>
        ) : (
          <Button variant="ghost" onClick={onClose}>
            Fechar
          </Button>
        )
      }
    >
      {vehicle && step.kind === 'verify-create' && (
        create.isPending ? (
          <Spinner label="Dando o acesso" />
        ) : (
          <IdentityCheck
            purpose="vehicle_share"
            action={`Para dar a ${step.input.name} acesso a ${vehicle.name}`}
            onGrant={(token) => create.mutate({ input: step.input, token })}
          />
        )
      )}

      {vehicle && step.kind === 'verify-block' && (
        setBlock.isPending ? (
          <Spinner label="Liberando o bloqueio" />
        ) : (
          <IdentityCheck
            purpose="vehicle_share"
            action={`Para liberar o bloqueio a ${step.share.guestName}`}
            onGrant={(token) => setBlock.mutate({ share: step.share, canBlock: true, token })}
          />
        )
      )}

      {vehicle && step.kind === 'list' && (
        <div className={styles.body}>
          <p className={styles.intro}>
            Quem você adicionar entra com a própria conta e acompanha a posição de{' '}
            <strong>{vehicle.name}</strong> ao vivo. Com o bloqueio liberado, a pessoa também pode bloquear
            o motor numa emergência — por exemplo, se o seu celular for roubado junto com o veículo. Ela
            nunca desbloqueia: isso continua só com você e com a central.
          </p>

          {error && (
            <div className={styles.error} role="alert">
              {error}
            </div>
          )}

          <section>
            <h3 className={styles.heading}>Quem acompanha</h3>
            {shares.isLoading ? (
              <Spinner label="Carregando acessos" />
            ) : list.length === 0 ? (
              <p className={styles.muted}>Ninguém além de você acompanha este veículo.</p>
            ) : (
              <ul className={styles.people}>
                {list.map((share) => (
                  <li key={share.id} className={styles.person}>
                    <div className={styles.who}>
                      <strong>{share.guestName}</strong>
                      <span className={styles.muted}>{share.guestEmail}</span>
                    </div>
                    <Badge tone={share.canBlock ? 'warning' : 'neutral'}>
                      {share.canBlock ? 'Pode bloquear' : 'Só acompanha'}
                    </Badge>
                    <div className={styles.personActions}>
                      {share.canBlock ? (
                        <Button
                          size="small"
                          variant="ghost"
                          loading={setBlock.isPending && setBlock.variables?.share.id === share.id}
                          onClick={() => setBlock.mutate({ share, canBlock: false })}
                        >
                          Tirar o bloqueio
                        </Button>
                      ) : (
                        <Button size="small" variant="ghost" onClick={() => setStep({ kind: 'verify-block', share })}>
                          Liberar o bloqueio
                        </Button>
                      )}
                      {removing === share.id ? (
                        <Button
                          size="small"
                          variant="danger"
                          loading={remove.isPending}
                          onClick={() => remove.mutate(share)}
                        >
                          Confirmar remoção
                        </Button>
                      ) : (
                        <Button size="small" variant="ghost" onClick={() => setRemoving(share.id)}>
                          Remover
                        </Button>
                      )}
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </section>

          {list.length < MAX_SHARES ? (
            <section className={styles.add}>
              <h3 className={styles.heading}>Dar acesso a alguém</h3>
              <div className={styles.row}>
                <TextField
                  label="Nome"
                  name="share-name"
                  autoComplete="off"
                  value={draft.name}
                  onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                />
                <TextField
                  label="E-mail"
                  name="share-email"
                  type="email"
                  autoComplete="off"
                  hint="A pessoa recebe o convite neste e-mail."
                  value={draft.email}
                  onChange={(e) => setDraft({ ...draft, email: e.target.value })}
                />
              </div>
              <label className={styles.check}>
                <input
                  type="checkbox"
                  name="share-can-block"
                  checked={draft.canBlock}
                  onChange={(e) => setDraft({ ...draft, canBlock: e.target.checked })}
                />
                <span>
                  Pode bloquear o motor numa emergência
                  <span className={styles.muted}>
                    Com a confirmação dela (biometria ou senha). Você recebe um aviso na hora.
                  </span>
                </span>
              </label>
              <div>
                <Button variant="primary" disabled={shareDraftProblem(draft) !== null} onClick={submit}>
                  Dar acesso
                </Button>
              </div>
            </section>
          ) : (
            <p className={styles.muted}>
              Este veículo já tem {MAX_SHARES} pessoas com acesso. Remova alguém para adicionar outra.
            </p>
          )}
        </div>
      )}
    </Modal>
  );
}

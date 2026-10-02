import { useCallback, useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { commandsApi } from '@/api/resources';
import { ApiError } from '@/api/client';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { useToast } from '@/components/ui/Toast';
import { useRealtimeEvent } from '@/hooks/useRealtime';
import { useAuth } from '@/stores/AuthContext';
import { formatCommand } from '@/services/format';
import {
  biometricAvailable,
  biometricGrant,
  biometricLabels,
  deviceCredential,
  enrollBiometric,
  errorCode,
  passwordGrant,
} from '@/services/stepUp';
import type { CommandType, DeviceCommand, EngineCutCheck, VehicleView } from '@/types';

import styles from './CommandPanel.module.css';

type Phase = 'idle' | 'verifying' | 'locating' | 'sending' | 'sent' | 'acknowledged' | 'failed' | 'rejected';
/** O passo "atualizando a posição" antes do corte (none = não precisou). */
type Locate = 'none' | 'active' | 'done' | 'failed';

interface CommandPanelProps {
  vehicle: VehicleView;
}

/** Comandos que mexem no relé exigem confirmação explícita (§24). */
const DESTRUCTIVE: CommandType[] = ['ENGINE_CUT'];

/** Recusas do corte que se resolvem com uma posição nova do rastreador. */
const NEEDS_POSITION: EngineCutCheck['code'][] = ['NO_POSITION', 'STALE_POSITION'];
/** Quanto esperar a posição nova: parado, o GPS do aparelho pode estar
 * dormindo e leva alguns segundos para achar os satélites. */
const POSITION_WAIT_MS = 60_000;
const POSITION_POLL_MS = 1_500;

const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

export function CommandPanel({ vehicle }: CommandPanelProps) {
  const { canSendCommands, isCustomer, user } = useAuth();
  const { notify } = useToast();
  const queryClient = useQueryClient();

  const [pending, setPending] = useState<CommandType | null>(null);
  const [phase, setPhase] = useState<Phase>('idle');
  const [command, setCommand] = useState<DeviceCommand | null>(null);
  const [reason, setReason] = useState<string>('');
  const [locate, setLocate] = useState<Locate>('none');
  const [preflight, setPreflight] = useState<EngineCutCheck | null>(null);
  // Cada envio ganha um número; cancelar troca o número e o envio em
  // andamento para onde estiver.
  const run = useRef(0);
  // Confirmação do cliente antes do corte: biometria ou senha.
  const [verify, setVerify] = useState<'biometric' | 'password'>('biometric');
  const [biometricFailed, setBiometricFailed] = useState(false);
  const [password, setPassword] = useState('');
  const [passwordError, setPasswordError] = useState('');
  const [checkingPassword, setCheckingPassword] = useState(false);
  // Confirmou com a senha num aparelho com biometria ainda não cadastrada:
  // depois do corte, oferece cadastrar (com a senha que acabou de digitar).
  const [canEnroll, setCanEnroll] = useState(false);
  const [enrolling, setEnrolling] = useState(false);
  const typedPassword = useRef<string | null>(null);
  const biometric = biometricLabels();

  const blocked = vehicle.state?.relayOn === true;
  const online = vehicle.device?.status === 'ONLINE';
  const hasDevice = Boolean(vehicle.deviceId);

  // O desfecho do comando chega pelo WebSocket, não pela resposta HTTP: o
  // rastreador confirma depois (§18).
  useRealtimeEvent(['command.acknowledged', 'command.failed'], (message) => {
    const updated = message.data as DeviceCommand | undefined;
    if (!updated || !command || updated.id !== command.id) return;

    setCommand(updated);
    setPhase(message.type === 'command.acknowledged' ? 'acknowledged' : 'failed');
    queryClient.invalidateQueries({ queryKey: ['vehicle', vehicle.id] });
    queryClient.invalidateQueries({ queryKey: ['commands', vehicle.id] });
  });

  const reset = useCallback(() => {
    run.current += 1;
    setPending(null);
    setPhase('idle');
    setCommand(null);
    setReason('');
    setLocate('none');
    setPreflight(null);
    setVerify('biometric');
    setBiometricFailed(false);
    setPassword('');
    setPasswordError('');
    setCanEnroll(false);
    typedPassword.current = null;
  }, []);

  // Ao abrir a confirmação do corte, já confere a regra: com a posição
  // antiga, o diálogo avisa que antes vem uma posição nova.
  useEffect(() => {
    if (pending !== 'ENGINE_CUT' || phase !== 'idle') return;
    let alive = true;
    commandsApi
      .engineCutCheck(vehicle.id)
      .then((check) => alive && setPreflight(check))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [pending, phase, vehicle.id]);

  /**
   * Corte com a última posição antiga demais (ou nenhuma): pede uma nova ao
   * rastreador e espera ela chegar. Quem diz se já está recente é o backend,
   * com o relógio dele. Devolve o motivo se não der, ou null para seguir.
   */
  const refreshPosition = useCallback(
    async (token: number): Promise<string | null> => {
      const first = await commandsApi.engineCutCheck(vehicle.id).catch(() => null);
      // Sem resposta da checagem, segue: o backend decide no envio do corte.
      if (!first || !NEEDS_POSITION.includes(first.code)) return null;

      setLocate('active');
      setPhase('locating');
      const request = await commandsApi.requestPosition(vehicle.id);
      if (request.status === 'FAILED') {
        return `O rastreador não recebeu o pedido de posição${request.error ? `: ${request.error}` : '.'}`;
      }
      const deadline = Date.now() + POSITION_WAIT_MS;
      while (Date.now() < deadline) {
        await wait(POSITION_POLL_MS);
        if (run.current !== token) return null;
        const check = await commandsApi.engineCutCheck(vehicle.id).catch(() => null);
        if (check && !NEEDS_POSITION.includes(check.code)) return null;
      }
      return 'O rastreador não enviou uma posição nova em 1 minuto. Confira se ele está com sinal de GPS e tente de novo.';
    },
    [vehicle.id],
  );

  const execute = useCallback(
    async (type: CommandType, stepUpToken?: string) => {
      const token = ++run.current;
      setReason('');

      if (type === 'ENGINE_CUT') {
        let problem: string | null;
        try {
          problem = await refreshPosition(token);
        } catch (error) {
          problem = error instanceof Error ? error.message : 'falha desconhecida';
        }
        if (run.current !== token) return; // cancelado
        if (problem) {
          setLocate('failed');
          setPhase('failed');
          setReason(problem);
          return;
        }
        setLocate((current) => (current === 'active' ? 'done' : current));
      }

      setPhase('sending');
      try {
        const result = await runCommand(vehicle.id, type, stepUpToken);
        setCommand(result);
        setPhase(result.status === 'FAILED' ? 'failed' : 'sent');

        if (result.status === 'FAILED') {
          setReason(result.error);
        }
        queryClient.invalidateQueries({ queryKey: ['commands', vehicle.id] });
      } catch (error) {
        if (error instanceof ApiError && error.status === 409) {
          // 409 é a recusa pela regra de segurança: não é erro de sistema.
          const body = error.body as { reason?: string; command?: DeviceCommand } | undefined;
          setPhase('rejected');
          setReason(body?.reason ?? error.message);
          setCommand(body?.command ?? null);
          return;
        }
        if (errorCode(error) === 'STEP_UP_REQUIRED') {
          setPhase('failed');
          setReason('A confirmação expirou ou já foi usada. Feche e tente de novo.');
          return;
        }
        setPhase('failed');
        setReason(error instanceof Error ? error.message : 'falha desconhecida');
      }
    },
    [vehicle.id, queryClient, refreshPosition],
  );

  /**
   * O cliente confirma que é ele antes do corte: com a biometria deste
   * aparelho (Face ID, digital) e, se ela falhar ou não estiver cadastrada,
   * com a senha. O servidor exige o comprovante; a equipe da central segue
   * direto.
   */
  const confirmIdentity = useCallback(async () => {
    if (!isCustomer || !user) {
      void execute('ENGINE_CUT');
      return;
    }
    const token = ++run.current;
    setPhase('verifying');
    setPasswordError('');
    if (deviceCredential(user.id) && (await biometricAvailable())) {
      setVerify('biometric');
      try {
        const grant = await biometricGrant(user.id, 'engine_cut');
        if (run.current !== token) return;
        void execute('ENGINE_CUT', grant.token);
        return;
      } catch {
        if (run.current !== token) return;
        setBiometricFailed(true);
      }
    }
    setVerify('password');
  }, [isCustomer, user, execute]);

  const submitPassword = async (event: FormEvent) => {
    event.preventDefault();
    if (!user) return;
    if (!password) {
      setPasswordError('Digite a sua senha.');
      return;
    }
    setCheckingPassword(true);
    setPasswordError('');
    try {
      const grant = await passwordGrant('engine_cut', password);
      typedPassword.current = password;
      setPassword('');
      setCanEnroll(!deviceCredential(user.id) && (await biometricAvailable()));
      void execute('ENGINE_CUT', grant.token);
    } catch (error) {
      setPasswordError(
        errorCode(error) === 'WRONG_PASSWORD'
          ? 'Senha incorreta.'
          : error instanceof Error
            ? error.message
            : 'Não foi possível conferir a senha.',
      );
    } finally {
      setCheckingPassword(false);
    }
  };

  const enroll = async () => {
    if (!user || !typedPassword.current) return;
    setEnrolling(true);
    try {
      await enrollBiometric(user, typedPassword.current);
      notify({ tone: 'success', title: biometric.enabled, description: `Da próxima vez, o bloqueio pede só ${biometric.withArticle}.` });
      setCanEnroll(false);
      typedPassword.current = null;
    } catch (error) {
      notify({
        tone: 'error',
        title: `Não foi possível ativar ${biometric.withArticle}`,
        description: error instanceof Error && !error.name.includes('Biometric') ? error.message : 'Tente de novo em Conta.',
      });
    } finally {
      setEnrolling(false);
    }
  };

  const trigger = useCallback(
    (type: CommandType) => {
      if (DESTRUCTIVE.includes(type)) {
        setPending(type);
        setPhase('idle');
        setCommand(null);
        setReason('');
        return;
      }
      setPending(type);
      void execute(type);
    },
    [execute],
  );

  // Avisos discretos para os comandos que não abrem diálogo.
  useEffect(() => {
    if (phase === 'acknowledged' && pending && !DESTRUCTIVE.includes(pending)) {
      notify({ tone: 'success', title: `${formatCommand(pending)}: confirmado pelo rastreador` });
    }
  }, [phase, pending, notify]);

  if (!hasDevice) {
    return (
      <div className={styles.notice}>
        Este veículo não tem rastreador vinculado. Vincule um dispositivo para enviar comandos.
      </div>
    );
  }

  if (!canSendCommands) {
    return (
      <div className={styles.notice}>
        Seu perfil permite apenas visualizar. Comandos são enviados por operadores e
        administradores.
      </div>
    );
  }

  const busy = phase === 'sending' || phase === 'sent';

  return (
    <div className={styles.panel}>
      <div className={styles.grid}>
        <Button
          variant="secondary"
          onClick={() => trigger('REQUEST_POSITION')}
          disabled={busy || !online}
          loading={busy && pending === 'REQUEST_POSITION'}
        >
          Solicitar posição
        </Button>

        <Button
          variant="secondary"
          onClick={() => trigger('REQUEST_STATUS')}
          disabled={busy || !online}
          loading={busy && pending === 'REQUEST_STATUS'}
        >
          Solicitar status
        </Button>

        {blocked ? (
          <Button
            variant="primary"
            onClick={() => trigger('ENGINE_RESUME')}
            disabled={busy || !online}
            loading={busy && pending === 'ENGINE_RESUME'}
          >
            Liberar motor
          </Button>
        ) : (
          <Button
            variant="danger"
            onClick={() => trigger('ENGINE_CUT')}
            disabled={busy || !online}
          >
            Desligar motor
          </Button>
        )}
      </div>

      {!online && (
        <div className={styles.notice}>
          O rastreador está {vehicle.device?.status === 'STALE' ? 'sem comunicação recente' : 'offline'}.
          Comandos só saem com a sessão aberta — o pedido falharia na hora.
        </div>
      )}

      <Modal
        open={pending !== null && (DESTRUCTIVE.includes(pending) || phase !== 'idle')}
        title={dialogTitle(pending, phase)}
        icon={phase === 'idle' ? '⚠' : undefined}
        onClose={() => {
          if (phase !== 'sending') reset();
        }}
        footer={
          phase === 'idle' ? (
            <>
              <Button variant="ghost" onClick={reset}>
                Cancelar
              </Button>
              <Button
                variant="danger"
                onClick={() => {
                  if (pending === 'ENGINE_CUT') void confirmIdentity();
                  else if (pending) void execute(pending);
                }}
              >
                Confirmar
              </Button>
            </>
          ) : phase === 'verifying' ? (
            <>
              <Button variant="ghost" onClick={reset}>
                Cancelar
              </Button>
              {verify === 'password' && (
                <Button variant="danger" type="submit" form="confirmar-com-senha" loading={checkingPassword}>
                  Confirmar
                </Button>
              )}
            </>
          ) : phase === 'locating' ? (
            // Ainda não saiu nenhum corte: dá para desistir.
            <Button variant="secondary" onClick={reset}>
              Cancelar
            </Button>
          ) : (
            <Button variant="secondary" onClick={reset} disabled={phase === 'sending'}>
              Fechar
            </Button>
          )
        }
      >
        {phase === 'idle' && pending === 'ENGINE_CUT' && (
          <>
            <div className={styles.dialogWarning}>
              O comando será enviado ao rastreador de <strong>{vehicle.name}</strong>.
            </div>
            <p>
              O sistema só permite o corte se o veículo estiver dentro da condição de segurança
              configurada — parado ou em velocidade muito baixa, com posição recente. Se a
              condição não for atendida, o pedido é recusado antes de qualquer byte sair daqui.
            </p>
            {preflight && NEEDS_POSITION.includes(preflight.code) && (
              <p className={styles.notice}>
                {preflight.positionAgeSeconds === null
                  ? 'O rastreador ainda não enviou nenhuma posição'
                  : `A última posição é de ${formatAge(preflight.positionAgeSeconds)} atrás`}
                : antes do corte, o sistema pede uma posição nova ao rastreador e espera ela
                chegar (até 1 minuto).
              </p>
            )}
          </>
        )}

        {phase === 'verifying' &&
          (verify === 'biometric' ? (
            <div className={styles.verify} role="status">
              <span className={styles.stepSpinner} aria-hidden="true" />
              Confirme com {biometric.withArticle}…
            </div>
          ) : (
            <form id="confirmar-com-senha" className={styles.verifyForm} onSubmit={submitPassword}>
              <p>
                {biometricFailed
                  ? `Não deu para confirmar com ${biometric.withArticle}. Use a senha da sua conta.`
                  : 'Para desligar o motor, confirme que é você com a senha da sua conta.'}
              </p>
              <TextField
                label="Sua senha"
                type="password"
                autoComplete="current-password"
                autoFocus
                value={password}
                error={passwordError || undefined}
                onChange={(event) => setPassword(event.target.value)}
              />
              {biometricFailed && (
                <Button type="button" variant="ghost" size="small" onClick={() => void confirmIdentity()}>
                  Tentar {biometric.withArticle} de novo
                </Button>
              )}
            </form>
          ))}

        {phase !== 'idle' && phase !== 'verifying' && (
          <CommandProgress phase={phase} command={command} reason={reason} pending={pending} locate={locate} />
        )}

        {canEnroll && (phase === 'sent' || phase === 'acknowledged') && (
          <div className={styles.enroll}>
            <span>
              Da próxima vez, confirme com {biometric.withArticle}, sem digitar a senha.
            </span>
            <Button size="small" variant="secondary" onClick={() => void enroll()} loading={enrolling}>
              Usar {biometric.withArticle}
            </Button>
          </div>
        )}
      </Modal>
    </div>
  );
}

function CommandProgress({
  phase,
  command,
  reason,
  pending,
  locate,
}: {
  phase: Phase;
  command: DeviceCommand | null;
  reason: string;
  pending: CommandType | null;
  locate: Locate;
}) {
  const isCut = pending === 'ENGINE_CUT';
  // O corte ainda não saiu (buscando posição) ou nem vai sair (posição não
  // chegou): os passos do envio ficam parados.
  const notSent = phase === 'locating' || locate === 'failed';

  if (phase === 'rejected') {
    return (
      <div className={`${styles.result} ${styles.resultError}`}>
        <strong>Comando recusado pela regra de segurança.</strong>
        <div className={styles.detail}>{reason}</div>
      </div>
    );
  }

  return (
    <div className={styles.progress}>
      {locate !== 'none' && (
        <Step
          label="Atualizando a posição do veículo"
          state={locate === 'active' ? 'active' : locate === 'failed' ? 'failed' : 'done'}
        />
      )}
      <Step
        label="Enviando comando ao rastreador"
        state={notSent ? 'idle' : phase === 'sending' ? 'active' : 'done'}
      />
      <Step
        label="Comando enviado, aguardando confirmação"
        state={
          notSent || phase === 'sending'
            ? 'idle'
            : phase === 'sent'
              ? 'active'
              : phase === 'failed'
                ? 'failed'
                : 'done'
        }
      />
      <Step
        label={isCut ? 'Motor bloqueado' : 'Confirmado pelo rastreador'}
        state={phase === 'acknowledged' ? 'done' : phase === 'failed' && !notSent ? 'failed' : 'idle'}
      />

      {phase === 'acknowledged' && command && (
        <div className={`${styles.result} ${styles.resultSuccess}`}>
          <strong>{isCut ? 'Motor bloqueado.' : 'Comando confirmado.'}</strong>
          {command.response && <div className={styles.detail}>{command.response}</div>}
        </div>
      )}

      {phase === 'failed' && locate === 'failed' && (
        <div className={`${styles.result} ${styles.resultError}`}>
          <strong>Não foi possível atualizar a posição. O corte não foi enviado.</strong>
          <div className={styles.detail}>{reason}</div>
        </div>
      )}

      {phase === 'failed' && locate !== 'failed' && (
        <div className={`${styles.result} ${styles.resultError}`}>
          <strong>Falha ao executar o comando.</strong>
          <div className={styles.detail}>{reason || command?.error || command?.response}</div>
        </div>
      )}
    </div>
  );
}

function Step({ label, state }: { label: string; state: 'idle' | 'active' | 'done' | 'failed' }) {
  const className = [
    styles.step,
    state === 'active' ? styles.stepActive : '',
    state === 'done' ? styles.stepDone : '',
    state === 'failed' ? styles.stepFailed : '',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <div className={className}>
      {state === 'active' ? (
        <span className={styles.stepSpinner} aria-hidden="true" />
      ) : (
        <span className={styles.stepMarker} aria-hidden="true">
          {state === 'done' ? '✓' : state === 'failed' ? '×' : ''}
        </span>
      )}
      <span>{label}</span>
    </div>
  );
}

function dialogTitle(pending: CommandType | null, phase: Phase): string {
  if (phase === 'idle' && pending === 'ENGINE_CUT') return 'Desligar motor?';
  if (phase === 'verifying') return 'Confirme que é você';
  if (phase === 'rejected') return 'Comando recusado';
  if (phase === 'failed') return 'Falha no comando';
  if (phase === 'acknowledged') return 'Comando concluído';
  return pending ? formatCommand(pending) : 'Comando';
}

/** "42 min", "3 h", "2 dias". */
function formatAge(seconds: number): string {
  const minutes = Math.max(1, Math.round(seconds / 60));
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.round(minutes / 60);
  if (hours < 48) return `${hours} h`;
  return `${Math.round(hours / 24)} dias`;
}

function runCommand(vehicleId: string, type: CommandType, stepUpToken?: string): Promise<DeviceCommand> {
  switch (type) {
    case 'ENGINE_CUT':
      return commandsApi.engineCut(vehicleId, stepUpToken);
    case 'ENGINE_RESUME':
      return commandsApi.engineResume(vehicleId);
    case 'REQUEST_POSITION':
      return commandsApi.requestPosition(vehicleId);
    case 'REQUEST_STATUS':
      return commandsApi.requestStatus(vehicleId);
    default:
      return commandsApi.generic(vehicleId, { command: type });
  }
}

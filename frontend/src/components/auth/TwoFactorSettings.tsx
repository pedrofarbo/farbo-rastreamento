import { useState } from 'react';
import type { FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { twoFactorApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateTime } from '@/services/format';
import type { TrustedDevice, TwoFactorEnrollment, TwoFactorMethod, TwoFactorStatus } from '@/types';

import styles from './TwoFactor.module.css';
import { EnrollApp, RecoveryCodes, digitsOnly } from './TwoFactorParts';

export const twoFactorKey = ['security', 'two-factor'] as const;

export const METHOD_LABELS: Record<TwoFactorMethod, string> = {
  totp: 'app autenticador',
  email: 'código por e-mail',
};

/** "Chrome no Windows", "Safari no iPhone": o aparelho confiável à vista. */
export function deviceLabel(userAgent: string): string {
  const ua = userAgent || '';
  const os = /iPhone/.test(ua)
    ? 'iPhone'
    : /iPad/.test(ua)
      ? 'iPad'
      : /Android/.test(ua)
        ? 'Android'
        : /Windows/.test(ua)
          ? 'Windows'
          : /Mac OS X|Macintosh/.test(ua)
            ? 'Mac'
            : /Linux/.test(ua)
              ? 'Linux'
              : '';
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /OPR\/|Opera/.test(ua)
      ? 'Opera'
      : /Firefox\//.test(ua)
        ? 'Firefox'
        : /Chrome\/|CriOS\//.test(ua)
          ? 'Chrome'
          : /Safari\//.test(ua)
            ? 'Safari'
            : '';
  if (browser && os) return `${browser} no ${os}`;
  return browser || os || 'Aparelho desconhecido';
}

const messageOf = (err: unknown) => (err instanceof Error ? err.message : 'Não foi possível concluir.');

/**
 * A verificação em duas etapas de quem está conectado: ativar (o app
 * autenticador ou, para o cliente, o e-mail), trocar de método, gerar
 * códigos de recuperação novos, desativar (só o cliente) e os aparelhos
 * confiáveis.
 */
export function TwoFactorSettings() {
  const queryClient = useQueryClient();
  const status = useQuery({ queryKey: twoFactorKey, queryFn: twoFactorApi.status });
  const [flow, setFlow] = useState<
    { kind: 'enable'; method: TwoFactorMethod } | { kind: 'codes' } | { kind: 'disable' } | null
  >(null);
  const [method, setMethod] = useState<TwoFactorMethod>('totp');
  const refresh = () => void queryClient.invalidateQueries({ queryKey: twoFactorKey });
  // Volta só com a situação nova (sem mostrar a antiga por um instante).
  const close = async () => {
    await queryClient.invalidateQueries({ queryKey: twoFactorKey });
    setFlow(null);
  };

  if (status.isLoading) return <Spinner label="Carregando a segurança da conta" />;
  if (!status.data) return <p className={styles.error}>Não deu para carregar a segurança da conta.</p>;
  const s = status.data;

  if (flow?.kind === 'enable') {
    return <EnableFlow method={flow.method} status={s} onDone={() => void close()} onCancel={() => setFlow(null)} />;
  }
  if (flow) {
    return <ConfirmChange kind={flow.kind} status={s} onDone={() => void close()} onCancel={() => setFlow(null)} />;
  }

  const other = s.methods.find((m) => m !== s.method);

  return (
    <div className={styles.root}>
      <div className={styles.status}>
        <span className={`${styles.state} ${s.enabled ? styles.stateOn : ''}`}>{s.enabled ? 'Ativa' : 'Desativada'}</span>
        {s.enabled && s.method && <span>Com o {METHOD_LABELS[s.method]}.</span>}
        {s.required && <span>Obrigatória para a equipe.</span>}
      </div>

      {!s.enabled ? (
        <>
          <p className={styles.lead}>
            Além da senha, a entrada pede um código. Quem descobrir a sua senha não entra sem ele.
          </p>
          {s.methods.length > 1 && (
            <div className={styles.methods} role="radiogroup" aria-label="Como receber o código">
              <label className={styles.method}>
                <input type="radio" name="metodo" checked={method === 'totp'} onChange={() => setMethod('totp')} />
                <span>
                  <strong>App autenticador (recomendado)</strong>
                  <br />O Google Authenticator ou o Microsoft Authenticator mostram o código, mesmo sem internet.
                </span>
              </label>
              <label className={styles.method}>
                <input type="radio" name="metodo" checked={method === 'email'} onChange={() => setMethod('email')} />
                <span>
                  <strong>Código por e-mail</strong>
                  <br />
                  Mandamos o código para {s.emailHint} a cada entrada.
                </span>
              </label>
            </div>
          )}
          <Button variant="primary" onClick={() => setFlow({ kind: 'enable', method: s.methods.length > 1 ? method : 'totp' })}>
            Ativar a verificação em duas etapas
          </Button>
        </>
      ) : (
        <>
          {s.recoveryCodesLeft <= 3 ? (
            <p className={styles.warn}>
              {s.recoveryCodesLeft === 0
                ? 'Você não tem mais códigos de recuperação.'
                : `Restam só ${s.recoveryCodesLeft} códigos de recuperação.`}{' '}
              Gere códigos novos para não ficar sem acesso se perder o celular.
            </p>
          ) : (
            <p className={styles.lead}>{s.recoveryCodesLeft} códigos de recuperação disponíveis.</p>
          )}
          <div className={styles.actions}>
            <Button variant="secondary" onClick={() => setFlow({ kind: 'codes' })}>
              Gerar novos códigos
            </Button>
            {other && (
              <Button variant="secondary" onClick={() => setFlow({ kind: 'enable', method: other })}>
                Trocar para o {METHOD_LABELS[other]}
              </Button>
            )}
            {!s.required && (
              <Button variant="ghost" onClick={() => setFlow({ kind: 'disable' })}>
                Desativar
              </Button>
            )}
          </div>
          <Devices devices={s.devices} onChange={refresh} />
        </>
      )}
    </div>
  );
}

/** Os aparelhos que dispensam o código por 30 dias. */
function Devices({ devices, onChange }: { devices: TrustedDevice[]; onChange: () => void }) {
  const { notify } = useToast();
  const revoke = useMutation({
    mutationFn: (id?: string) => twoFactorApi.revokeDevice(id),
    onSuccess: (_, id) => {
      onChange();
      notify({ tone: 'success', title: id ? 'Aparelho removido' : 'Aparelhos removidos' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível remover', description: err.message }),
  });
  return (
    <div className={styles.head}>
      <h3 className={styles.subtitle}>Aparelhos confiáveis</h3>
      {devices.length === 0 ? (
        <p className={styles.lead}>Nenhum. Ao entrar, marque “Confiar neste aparelho” para não digitar o código por 30 dias.</p>
      ) : (
        <>
          <ul className={styles.devices}>
            {devices.map((d) => (
              <li key={d.id} className={styles.device}>
                <span>
                  {deviceLabel(d.userAgent)}
                  <small>
                    Usado em {formatDateTime(d.lastUsedAt)} · vale até {formatDateTime(d.expiresAt)}
                  </small>
                </span>
                <Button size="small" variant="ghost" loading={revoke.isPending && revoke.variables === d.id} onClick={() => revoke.mutate(d.id)}>
                  Remover
                </Button>
              </li>
            ))}
          </ul>
          {devices.length > 1 && (
            <Button size="small" variant="ghost" loading={revoke.isPending && !revoke.variables} onClick={() => revoke.mutate(undefined)}>
              Remover todos
            </Button>
          )}
        </>
      )}
    </div>
  );
}

/**
 * Ativar (ou trocar de método): a senha; o QR Code do app ou o código do
 * e-mail; o primeiro código; os códigos de recuperação.
 */
function EnableFlow({
  method,
  status,
  onDone,
  onCancel,
}: {
  method: TwoFactorMethod;
  status: TwoFactorStatus;
  onDone: () => void;
  onCancel: () => void;
}) {
  const { notify } = useToast();
  const [step, setStep] = useState<'password' | 'code' | 'codes'>('password');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [enrollment, setEnrollment] = useState<TwoFactorEnrollment | null>(null);
  const [codes, setCodes] = useState<string[]>([]);

  const start = useMutation({
    mutationFn: () => twoFactorApi.start(method, password),
    onSuccess: (result) => {
      setPassword('');
      setEnrollment(result.secret ? result : null);
      setStep('code');
    },
  });
  const confirm = useMutation({
    mutationFn: (value: string) => twoFactorApi.confirm(value),
    onSuccess: (result) => {
      setCodes(result.recoveryCodes);
      setStep('codes');
      notify({ tone: 'success', title: 'Verificação em duas etapas ativada', description: `Com o ${METHOD_LABELS[result.method]}.` });
    },
    onError: () => setCode(''),
  });

  const typeCode = (value: string) => {
    const next = digitsOnly(value);
    setCode(next);
    if (next.length === 6 && !confirm.isPending) confirm.mutate(next);
  };

  if (step === 'codes') {
    return (
      <div className={styles.root}>
        <h3 className={styles.subtitle}>Códigos de recuperação</h3>
        <RecoveryCodes codes={codes} onDone={onDone} doneLabel="Concluir" />
      </div>
    );
  }

  if (step === 'password') {
    const submit = (event: FormEvent) => {
      event.preventDefault();
      if (password) start.mutate();
    };
    return (
      <form className={styles.root} onSubmit={submit}>
        <p className={styles.lead}>
          {method === 'totp'
            ? 'Para cadastrar o app autenticador, confirme a sua senha.'
            : `Para receber o código em ${status.emailHint}, confirme a sua senha.`}
        </p>
        <TextField
          label="Sua senha"
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        {start.error && (
          <p className={styles.error} role="alert">
            {messageOf(start.error)}
          </p>
        )}
        <div className={styles.actions}>
          <Button type="submit" variant="primary" loading={start.isPending} disabled={!password}>
            Continuar
          </Button>
          <Button type="button" variant="ghost" onClick={onCancel}>
            Cancelar
          </Button>
        </div>
      </form>
    );
  }

  return (
    <form
      className={styles.root}
      onSubmit={(event) => {
        event.preventDefault();
        if (code) confirm.mutate(code);
      }}
    >
      {enrollment ? (
        <EnrollApp enrollment={enrollment} />
      ) : (
        <p className={styles.lead}>
          Mandamos um código de 6 dígitos para <strong>{status.emailHint}</strong>. Digite-o abaixo.
        </p>
      )}
      <input
        className={styles.code}
        aria-label="Código de 6 dígitos"
        inputMode="numeric"
        autoComplete="one-time-code"
        placeholder="000000"
        value={code}
        onChange={(e) => typeCode(e.target.value)}
      />
      {confirm.error && (
        <p className={styles.error} role="alert">
          {messageOf(confirm.error)}
        </p>
      )}
      <div className={styles.actions}>
        <Button type="submit" variant="primary" loading={confirm.isPending} disabled={code.length !== 6}>
          Ativar
        </Button>
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancelar
        </Button>
      </div>
    </form>
  );
}

/**
 * Gerar códigos novos ou desativar: a senha e o código (do app, do e-mail ou
 * um de recuperação).
 */
function ConfirmChange({
  kind,
  status,
  onDone,
  onCancel,
}: {
  kind: 'codes' | 'disable';
  status: TwoFactorStatus;
  onDone: () => void;
  onCancel: () => void;
}) {
  const { notify } = useToast();
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [codes, setCodes] = useState<string[] | null>(null);
  const email = status.method === 'email';

  const send = useMutation({
    mutationFn: twoFactorApi.sendCode,
    onSuccess: () => notify({ tone: 'success', title: 'Código enviado', description: `Confira o e-mail ${status.emailHint}.` }),
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível mandar o código', description: err.message }),
  });
  const submit = useMutation({
    mutationFn: async () => {
      if (kind === 'codes') return (await twoFactorApi.recoveryCodes(password, code)).recoveryCodes;
      await twoFactorApi.disable(password, code);
      return null;
    },
    onSuccess: (result) => {
      if (result) {
        setCodes(result);
        return;
      }
      notify({ tone: 'success', title: 'Verificação em duas etapas desativada' });
      onDone();
    },
  });

  if (codes) {
    return (
      <div className={styles.root}>
        <h3 className={styles.subtitle}>Códigos de recuperação novos</h3>
        <p className={styles.lead}>Os códigos antigos deixaram de valer.</p>
        <RecoveryCodes codes={codes} onDone={onDone} doneLabel="Concluir" />
      </div>
    );
  }

  return (
    <form
      className={styles.root}
      onSubmit={(event) => {
        event.preventDefault();
        if (password && code) submit.mutate();
      }}
    >
      <p className={styles.lead}>
        {kind === 'codes'
          ? 'Para gerar códigos de recuperação novos (os antigos deixam de valer), confirme a senha e o código.'
          : 'Sem a verificação, basta a senha para entrar na sua conta. Para desativar, confirme a senha e o código.'}
      </p>
      <TextField
        label="Sua senha"
        type="password"
        autoComplete="current-password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
      />
      <TextField
        label={email ? 'Código do e-mail' : 'Código do app autenticador'}
        inputMode={email ? 'numeric' : 'text'}
        autoComplete="one-time-code"
        value={code}
        hint="Ou um dos códigos de recuperação."
        onChange={(e) => setCode(e.target.value.slice(0, 12))}
      />
      {email && (
        <Button type="button" variant="secondary" size="small" loading={send.isPending} onClick={() => send.mutate()}>
          Mandar o código por e-mail
        </Button>
      )}
      {submit.error && (
        <p className={styles.error} role="alert">
          {messageOf(submit.error)}
        </p>
      )}
      <div className={styles.actions}>
        <Button type="submit" variant={kind === 'disable' ? 'danger' : 'primary'} loading={submit.isPending} disabled={!password || !code}>
          {kind === 'codes' ? 'Gerar códigos novos' : 'Desativar'}
        </Button>
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancelar
        </Button>
      </div>
    </form>
  );
}

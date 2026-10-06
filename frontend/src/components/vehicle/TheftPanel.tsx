import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { theftApi } from '@/api/resources';
import { IdentityCheck } from '@/components/auth/IdentityCheck';
import { Address } from '@/components/ui/Address';
import { Button } from '@/components/ui/Button';
import { useToast } from '@/components/ui/Toast';
import { vehicleKey, vehiclesKey } from '@/hooks/useVehicles';
import { formatDateTime, formatTime } from '@/services/format';
import type { TheftMode, TheftView, VehicleView } from '@/types';

import styles from './TheftPanel.module.css';

export const theftKey = (vehicleId: string) => ['theft', vehicleId] as const;

type Outcome = 'RECOVERED' | 'CANCELLED';

/** A situação do intervalo curto no rastreador, em uma frase. */
export function trackerLine(mode: TheftMode, parkedSeconds: number): string {
  const every = `posição a cada ${parkedSeconds} s, mesmo parado`;
  switch (mode.boostStatus) {
    case 'SENT':
      return mode.boostCommandStatus === 'ACKNOWLEDGED'
        ? `O rastreador confirmou: ${every}.`
        : `Comando enviado ao rastreador: ${every}.`;
    case 'SKIPPED':
      return 'Este modelo de rastreador não muda o intervalo: a posição segue no intervalo normal.';
    default:
      return `O rastreador está fora do ar: o intervalo curto (${every}) vai assim que ele se conectar${
        mode.boostSmsAt ? '; também mandamos por SMS' : ''
      }.`;
  }
}

/** A mensagem que vai junto com o link (WhatsApp, compartilhar). */
export function shareText(vehicle: Pick<VehicleView, 'name' | 'plate' | 'brand' | 'model' | 'color'>, url: string): string {
  const details = [vehicle.brand, vehicle.model, vehicle.color].filter(Boolean).join(' ');
  return `Veículo roubado: ${vehicle.name}${details ? ` (${details})` : ''}${
    vehicle.plate ? `, placa ${vehicle.plate}` : ''
  }. Localização ao vivo: ${url}`;
}

/**
 * O modo roubo do veículo. Sem central: o cliente liga, e a tela vira o
 * passo a passo do que fazer (190, o link para a polícia, o bloqueio e o
 * boletim de ocorrência). Desligar é só do dono, com a biometria ou a senha.
 */
export function TheftPanel({
  vehicle,
  prompt = false,
  reportUrl,
}: {
  vehicle: VehicleView;
  /** Aberto pelo alerta (?roubo=1): já pergunta se é para ligar. */
  prompt?: boolean;
  /** O relatório para o boletim de ocorrência (só o dono: o histórico é dele). */
  reportUrl?: string;
}) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const ref = useRef<HTMLElement>(null);
  const [confirming, setConfirming] = useState(false);
  const [ending, setEnding] = useState<Outcome | null>(null);
  const [error, setError] = useState('');

  const status = useQuery({
    queryKey: theftKey(vehicle.id),
    queryFn: () => theftApi.get(vehicle.id),
    enabled: Boolean(vehicle.device),
    // Ligado: acompanha o rastreador (o comando pode chegar a qualquer hora).
    refetchInterval: (query) => (query.state.data?.mode ? 15_000 : false),
  });
  const view = status.data;
  const mode = view?.mode ?? null;

  // Pelo alerta: abre a pergunta (e mostra o painel) uma vez.
  const prompted = useRef(false);
  useEffect(() => {
    if (!prompt || prompted.current || !view) return;
    prompted.current = true;
    if (!view.mode && view.canActivate) setConfirming(true);
    ref.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, [prompt, view]);

  const refresh = (next: TheftView) => {
    queryClient.setQueryData(theftKey(vehicle.id), next);
    void queryClient.invalidateQueries({ queryKey: vehiclesKey });
    void queryClient.invalidateQueries({ queryKey: vehicleKey(vehicle.id) });
  };

  const activate = useMutation({
    mutationFn: () => theftApi.activate(vehicle.id),
    onSuccess: (next) => {
      setConfirming(false);
      setError('');
      refresh(next);
      notify({ tone: 'success', title: 'Modo roubo ativado', description: 'Siga os passos abaixo.' });
    },
    onError: (err: Error) => setError(err.message),
  });

  const end = useMutation({
    mutationFn: ({ outcome, token }: { outcome: Outcome; token: string }) => theftApi.end(vehicle.id, outcome, token),
    onSuccess: (next, { outcome }) => {
      setEnding(null);
      setError('');
      refresh(next);
      notify(
        outcome === 'RECOVERED'
          ? { tone: 'success', title: 'Que bom! Veículo recuperado', description: 'O rastreador volta ao intervalo normal.' }
          : { tone: 'success', title: 'Modo roubo desativado' },
      );
    },
    onError: (err: Error) => {
      setEnding(null);
      setError(err.message);
    },
  });

  if (!vehicle.device || !view) return null;
  // Quem só acompanha (sem o bloqueio) não liga; com o modo ligado, vê tudo.
  if (!mode && !view.canActivate) return null;

  if (!mode) {
    return (
      <section ref={ref} className={styles.panel} aria-labelledby={`roubo-${vehicle.id}`}>
        <h2 id={`roubo-${vehicle.id}`} className={styles.title}>
          Modo roubo
        </h2>
        {!confirming ? (
          <>
            <p className={styles.text}>
              Se o veículo foi roubado, ative: o rastreador passa a mandar a posição a cada {view.parkedSeconds} s, mesmo
              parado, quem tem acesso a ele é avisado e você recebe um link da localização ao vivo para mandar à polícia.
            </p>
            <Button variant="danger" block onClick={() => setConfirming(true)}>
              Meu veículo foi roubado
            </Button>
          </>
        ) : (
          <div className={styles.confirm}>
            <strong>Ativar o modo roubo de {vehicle.name}?</strong>
            <ul className={styles.bullets}>
              <li>O rastreador manda a posição a cada {view.parkedSeconds} s, inclusive parado (gasta mais bateria).</li>
              <li>Quem tem acesso ao veículo recebe o aviso no celular e por e-mail.</li>
              <li>Um link mostra a posição ao vivo, sem login, para você mandar à polícia.</li>
              <li>Desliga sozinho em {view.durationHours} h, ou quando o dono marcar que recuperou.</li>
            </ul>
            <div className={styles.row}>
              <Button variant="danger" loading={activate.isPending} onClick={() => activate.mutate()}>
                Ativar o modo roubo
              </Button>
              <Button variant="ghost" disabled={activate.isPending} onClick={() => setConfirming(false)}>
                Cancelar
              </Button>
            </div>
          </div>
        )}
        {error && (
          <p className={styles.error} role="alert">
            {error}
          </p>
        )}
      </section>
    );
  }

  const position = vehicle.lastPosition;
  const link = view.publicUrl;
  const message = shareText(vehicle, link);
  const shareLink = async () => {
    try {
      if (navigator.share) {
        await navigator.share({ title: `Veículo roubado: ${vehicle.name}`, text: message, url: link });
        return;
      }
      await navigator.clipboard.writeText(message);
      notify({ tone: 'success', title: 'Link copiado', description: 'Cole na conversa com a polícia.' });
    } catch {
      /* o cliente cancelou */
    }
  };
  const copyLink = async () => {
    try {
      await navigator.clipboard.writeText(link);
      notify({ tone: 'success', title: 'Link copiado' });
    } catch {
      notify({ tone: 'error', title: 'Não deu para copiar', description: link });
    }
  };

  return (
    <section ref={ref} className={`${styles.panel} ${styles.active}`} aria-labelledby={`roubo-${vehicle.id}`}>
      <div className={styles.banner}>
        <span className={styles.pulse} aria-hidden="true" />
        <h2 id={`roubo-${vehicle.id}`} className={styles.title}>
          Modo roubo ativo
        </h2>
        <span className={styles.since}>
          desde {formatTime(mode.activatedAt)}
          {mode.activatedByName ? ` · por ${mode.activatedByName}` : ''}
        </span>
      </div>
      <p className={styles.tracker}>{trackerLine(mode, view.parkedSeconds)}</p>

      <ol className={styles.steps}>
        <li className={styles.step}>
          <strong>Ligue 190</strong>
          <span className={styles.text}>
            Diga que o veículo{vehicle.plate ? ` (placa ${vehicle.plate})` : ''} tem rastreador e onde ele está agora
            {position ? ':' : '.'}
          </span>
          {position && <Address lat={position.latitude} lon={position.longitude} className={styles.address} />}
          <a className={styles.call} href="tel:190">
            Ligar 190
          </a>
        </li>
        <li className={styles.step}>
          <strong>Mande o link da localização ao vivo</strong>
          <span className={styles.text}>
            Abre sem login e mostra a posição atualizada. Envie para a polícia e para quem estiver ajudando.
          </span>
          <div className={styles.row}>
            <Button variant="primary" size="small" onClick={() => void shareLink()}>
              Compartilhar
            </Button>
            <a
              className={styles.whatsapp}
              href={`https://wa.me/?text=${encodeURIComponent(message)}`}
              target="_blank"
              rel="noreferrer noopener"
            >
              WhatsApp
            </a>
            <Button variant="ghost" size="small" onClick={() => void copyLink()}>
              Copiar link
            </Button>
          </div>
        </li>
        <li className={styles.step}>
          <strong>Bloqueie o motor</strong>
          <span className={styles.text}>
            {/* O nome do botão em Comandos: o dono desliga; quem recebeu acesso bloqueia. */}
            Use o {view.canEnd ? 'Desligar motor' : 'Bloquear motor'} em Comandos. Ele só vale com o veículo parado: em
            movimento, é recusado para não causar um acidente.
          </span>
        </li>
        {reportUrl && (
          <li className={styles.step}>
            <strong>Registre o boletim de ocorrência</strong>
            <span className={styles.text}>Leve o relatório com o trajeto e os eventos (dá para imprimir ou salvar em PDF).</span>
            <Link className={styles.report} to={reportUrl}>
              Gerar o relatório
            </Link>
          </li>
        )}
      </ol>

      <p className={styles.warning}>Não tente recuperar o veículo sozinho: deixe a abordagem com a polícia.</p>

      {view.canEnd &&
        (ending ? (
          end.isPending ? (
            <p className={styles.text}>Encerrando…</p>
          ) : (
            <div className={styles.confirm}>
              <IdentityCheck
                purpose="theft_end"
                action={ending === 'RECOVERED' ? 'Para marcar o veículo como recuperado' : 'Para desativar o modo roubo'}
                onGrant={(token) => end.mutate({ outcome: ending, token })}
              />
              <Button variant="ghost" size="small" onClick={() => setEnding(null)}>
                Voltar
              </Button>
            </div>
          )
        ) : (
          <div className={styles.row}>
            <Button variant="primary" onClick={() => setEnding('RECOVERED')}>
              Recuperei o veículo
            </Button>
            <Button variant="ghost" onClick={() => setEnding('CANCELLED')}>
              Desativar
            </Button>
          </div>
        ))}
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
      <p className={styles.muted}>
        Desliga sozinho em {formatDateTime(mode.expiresAt)}
        {view.canEnd ? '' : '; só o dono desliga antes'}.
      </p>
    </section>
  );
}

import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { installerLinkPublicApi } from '@/api/resources';
import { formatDateTime } from '@/services/format';
import { clearInstallPin, pinProblem, saveInstallPin } from '@/services/installPin';
import type { PinProblem } from '@/services/installPin';
import type { InstallCheck, PublicInstallerLinkView } from '@/types';

import { InstallActivation } from './InstallActivation';
import { Shell } from './InstallerPage';
import { InstallPinGate } from './InstallPinGate';
import base from './PreLaunch.module.css';
import { useNoIndex } from './PublicLocation';
import styles from './PublicTheft.module.css';
import local from './InstallerPage.module.css';

/** "a Farbo encerrou…" → "A Farbo encerrou…". */
const sentence = (text: string) => text.charAt(0).toUpperCase() + text.slice(1);

/**
 * O link da instalação avulsa (/instalador/avulsa/<link secreto>): sem
 * agendamento nem veículo, a central escolheu o rastreador. O instalador
 * confere o IMEI — se o rastreador nunca conectou, escolhe o chip e ativa
 * por SMS antes —, testa o sinal ao vivo e conclui; o link expira na
 * conclusão (ou quando a central encerra).
 */
export function StandaloneInstallPage() {
  const { token = '' } = useParams();
  useNoIndex('Instalação · Farbo Rastreadores');
  const [view, setView] = useState<PublicInstallerLinkView | null>(null);
  // Por que o link não vale mais (concluído, encerrado pela central…).
  const [ended, setEnded] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState('');
  const [check, setCheck] = useState<InstallCheck | null>(null);
  const [note, setNote] = useState('');
  // Com a ativação: o rastreador ficou pronto para o teste (conectou).
  const [activated, setActivated] = useState(false);
  // O link da central pede a senha que ela passou junto.
  const [pinGate, setPinGate] = useState<PinProblem | null>(null);

  /** A senha errada (ou o link travado) não fica guardada: mandar de novo contaria outra tentativa. */
  const askPin = (problem: PinProblem) => {
    if (problem !== 'required') clearInstallPin(token);
    setPinGate(problem);
  };
  /** As chamadas da ativação (que mostram o erro na própria tela) também voltam à senha. */
  const guard = <T,>(call: Promise<T>) =>
    call.catch((err: unknown) => {
      const problem = pinProblem(err);
      if (problem) askPin(problem);
      throw err;
    });

  const load = () =>
    installerLinkPublicApi
      .view(token)
      .then((v) => {
        setPinGate(null);
        setView(v);
      })
      .catch((err) => {
        const problem = pinProblem(err);
        if (problem) askPin(problem);
        else if (err instanceof ApiError && (err.status === 410 || err.status === 404)) setEnded(err.message);
        else setError('Sem conexão agora. Recarregue a página.');
      });

  useEffect(() => {
    void load();
    // Uma vez por link.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  const run = async (label: string, action: () => Promise<void>) => {
    setBusy(label);
    setError('');
    try {
      await action();
    } catch (err) {
      const problem = pinProblem(err);
      if (problem) askPin(problem);
      else if (err instanceof ApiError && err.status === 410) setEnded(err.message);
      else setError(err instanceof Error ? err.message : 'Não deu certo. Tente de novo.');
    } finally {
      setBusy('');
    }
  };

  if (ended) {
    return (
      <Shell>
        <section className={styles.card}>
          <h1 className={styles.title}>Este link não está mais ativo</h1>
          <p className={styles.text}>{sentence(ended)}. Se precisar, fale com a Farbo.</p>
        </section>
      </Shell>
    );
  }
  if (pinGate) {
    return (
      <Shell>
        <InstallPinGate
          problem={pinGate}
          busy={busy === 'pin'}
          onSubmit={(pin) =>
            void run('pin', async () => {
              saveInstallPin(token, pin);
              await load();
            })
          }
        />
      </Shell>
    );
  }
  if (!view) {
    return (
      <Shell>
        <p className={base.hint} role="status">
          {error || 'Carregando…'}
        </p>
      </Shell>
    );
  }

  const done = view.status === 'done';
  const vehicle = [view.vehicleName, view.plate].filter(Boolean).join(' · ');
  // Fora de veículo e nunca conectou: o chip e a ativação antes do teste.
  const activation = view.needsActivation;
  const canTest = !activation || activated;
  const n = (step: number) => (activation ? step + 1 : step);

  return (
    <Shell label="Instalação">
      <section className={styles.card}>
        <span className={styles.label}>Instalação avulsa</span>
        <h1 className={styles.title}>{view.deviceModel ? `Rastreador ${view.deviceModel}` : 'Rastreador'}</h1>
        <span className={styles.plate}>IMEI {view.imei}</span>
        {vehicle && <p className={styles.text}>🚗 {vehicle}</p>}
        {view.notes && <p className={styles.muted}>📝 {view.notes}</p>}
      </section>

      {error && <p className={base.error}>{error}</p>}

      {done ? (
        <section className={styles.card}>
          <h2 className={local.step}>✅ Instalação concluída</h2>
          <p className={styles.text}>
            Rastreador {view.imei} testado e instalado{view.completedAt ? ` em ${formatDateTime(view.completedAt)}` : ''}.
            Obrigado! Este link não vale mais.
          </p>
        </section>
      ) : (
        <>
          {activation && (
            <section className={`${styles.card} ${activated ? local.done : ''}`}>
              <h2 className={local.step}>
                <span className={local.number}>1</span> Ativar
              </h2>
              <p className={styles.text}>
                Este rastreador ainda não foi ativado. Confira se o IMEI da etiqueta é este: <strong>{view.imei}</strong>.
              </p>
              <InstallActivation
                fixedImei={view.imei}
                api={{
                  stock: () => guard(installerLinkPublicApi.stock(token)),
                  activation: () => guard(installerLinkPublicApi.activation(token)),
                  activate: (_device, chipId) => guard(installerLinkPublicApi.activate(token, chipId)),
                }}
                disabled={busy !== ''}
                onReady={(ready) => setActivated(Boolean(ready))}
              />
            </section>
          )}
          <section className={`${styles.card} ${check?.ready ? local.done : ''}`}>
            <h2 className={local.step}>
              <span className={local.number}>{n(1)}</span> Testar
            </h2>
            <p className={styles.text}>
              Instale e ligue o rastreador. Confira se o IMEI da etiqueta é este: <strong>{view.imei}</strong>.
            </p>
            <p className={styles.muted}>
              {canTest ? 'Se o IMEI for outro, fale com a Farbo antes de seguir.' : 'Ative o rastreador antes de testar.'}
            </p>
            <button
              type="button"
              className={`${base.secondary} ${local.action}`}
              disabled={busy !== '' || !canTest}
              onClick={() => void run('check', async () => setCheck(await installerLinkPublicApi.check(token)))}
            >
              {busy === 'check' ? 'Testando…' : check ? 'Testar de novo' : 'Testar o sinal'}
            </button>
            {check && (
              <p className={`${local.check} ${check.ready ? local.ok : local.wait}`} role="status">
                {check.message}
              </p>
            )}
          </section>

          <section className={styles.card}>
            <h2 className={local.step}>
              <span className={local.number}>{n(2)}</span> Concluir
            </h2>
            <textarea
              className={local.textarea}
              maxLength={500}
              placeholder="Observação (opcional): onde ficou o rastreador, algum detalhe do veículo…"
              value={note}
              onChange={(e) => setNote(e.target.value)}
            />
            {!check?.ready && <p className={styles.muted}>Para concluir: teste o sinal.</p>}
            <button
              type="button"
              className={`${base.primary} ${local.action}`}
              disabled={!check?.ready || busy !== ''}
              onClick={() => void run('complete', async () => setView(await installerLinkPublicApi.complete(token, note)))}
            >
              {busy === 'complete' ? 'Concluindo…' : 'Concluir a instalação'}
            </button>
          </section>
        </>
      )}
    </Shell>
  );
}

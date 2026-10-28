import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { installerLinkPublicApi } from '@/api/resources';
import { formatDateTime } from '@/services/format';
import type { InstallCheck, PublicInstallerLinkView } from '@/types';

import { Shell } from './InstallerPage';
import base from './PreLaunch.module.css';
import { useNoIndex } from './PublicLocation';
import styles from './PublicTheft.module.css';
import local from './InstallerPage.module.css';

/** "a Farbo encerrou…" → "A Farbo encerrou…". */
const sentence = (text: string) => text.charAt(0).toUpperCase() + text.slice(1);

/**
 * O link da instalação avulsa (/instalador/avulsa/<link secreto>): sem
 * agendamento nem veículo, a central escolheu o rastreador. O instalador
 * confere o IMEI, testa o sinal ao vivo e conclui; o link expira na
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

  useEffect(() => {
    installerLinkPublicApi
      .view(token)
      .then(setView)
      .catch((err) => {
        if (err instanceof ApiError && (err.status === 410 || err.status === 404)) setEnded(err.message);
        else setError('Sem conexão agora. Recarregue a página.');
      });
  }, [token]);

  const run = async (label: string, action: () => Promise<void>) => {
    setBusy(label);
    setError('');
    try {
      await action();
    } catch (err) {
      if (err instanceof ApiError && err.status === 410) setEnded(err.message);
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
          <section className={`${styles.card} ${check?.ready ? local.done : ''}`}>
            <h2 className={local.step}>
              <span className={local.number}>1</span> Testar
            </h2>
            <p className={styles.text}>
              Instale e ligue o rastreador. Confira se o IMEI da etiqueta é este: <strong>{view.imei}</strong>.
            </p>
            <p className={styles.muted}>Se o IMEI for outro, fale com a Farbo antes de seguir.</p>
            <button
              type="button"
              className={`${base.secondary} ${local.action}`}
              disabled={busy !== ''}
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
              <span className={local.number}>2</span> Concluir
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

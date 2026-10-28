import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { useParams } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { installLinkApi } from '@/api/resources';
import { whatsappLink } from '@/config/contact';
import { formatDateTime } from '@/services/format';
import { KIND_LABEL, dayKey, dayLabel, hourLabel, slotsOf } from '@/services/installations';
import { compressImage } from '@/services/photos';
import type { InstallCheck, InstallationSlot, PublicInstallView } from '@/types';

import base from './PreLaunch.module.css';
import { useNoIndex } from './PublicLocation';
import styles from './PublicTheft.module.css';
import local from './InstallerPage.module.css';

/**
 * O link do instalador (/instalador/<link secreto>): o passo a passo no
 * celular, sem login. Na instalação: a chegada, o IMEI com o teste do sinal
 * ao vivo, as fotos e a conclusão, que liga o rastreador ao veículo. Na
 * desinstalação: a chegada, o IMEI a retirar, a foto do rastreador retirado
 * e a conclusão, que tira o rastreador do veículo. Só o primeiro nome e o
 * telefone do cliente, para combinar.
 */
export function InstallerPage() {
  const { token = '' } = useParams();
  useNoIndex('Instalação · Farbo Rastreadores');
  const [view, setView] = useState<PublicInstallView | null>(null);
  const [ended, setEnded] = useState(false);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState('');
  const [imei, setImei] = useState('');
  const [check, setCheck] = useState<InstallCheck | null>(null);
  const [note, setNote] = useState('');
  // Muda a cada foto nova (o navegador não reaproveita a antiga).
  const [version, setVersion] = useState(() => String(Date.now()));

  useEffect(() => {
    installLinkApi
      .view(token)
      .then((v) => {
        setView(v);
        if (v.imei) setImei(v.imei);
      })
      .catch((err) => {
        if (err instanceof ApiError && (err.status === 410 || err.status === 404)) setEnded(true);
        else setError('Sem conexão agora. Recarregue a página.');
      });
  }, [token]);

  const run = async (label: string, action: () => Promise<void>) => {
    setBusy(label);
    setError('');
    try {
      await action();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Não deu certo. Tente de novo.');
    } finally {
      setBusy('');
    }
  };

  if (ended) {
    return (
      <Shell>
        <section className={styles.card}>
          <h1 className={styles.title}>Este link não está mais ativo</h1>
          <p className={styles.text}>O agendamento foi cancelado, remarcado com outro link ou concluído há mais de um dia. Fale com a Farbo.</p>
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

  const uninstall = view.kind === 'uninstall';
  const done = view.status === 'done';
  const arrived = view.status === 'arrived' || done;
  const hasTracker = view.photos.includes('tracker');
  const tested = uninstall || Boolean(check?.ready);
  const canComplete = !done && arrived && tested && hasTracker;
  const missing = [
    !arrived && 'registre a chegada',
    !tested && 'teste o sinal',
    !hasTracker && (uninstall ? 'tire a foto do rastreador retirado' : 'tire a foto do rastreador'),
  ].filter(Boolean);
  const maps = view.address ? `https://www.google.com/maps/search/?api=1&query=${encodeURIComponent(view.address)}` : '';

  return (
    <Shell label={KIND_LABEL[view.kind]}>
      <section className={styles.card}>
        <span className={styles.label}>
          {dayLabel(dayKey(view.scheduledFor))} às {hourLabel(view.scheduledFor)}
        </span>
        <h1 className={styles.title}>{view.vehicleName}</h1>
        {view.plate && <span className={styles.plate}>{view.plate}</span>}
        {view.vehicleDetails && <p className={styles.text}>{view.vehicleDetails}</p>}
        {view.address && <p className={styles.text}>📍 {view.address}</p>}
        {view.notes && <p className={styles.muted}>📝 {view.notes}</p>}
        <div className={local.contact}>
          {view.customerPhone && (
            <>
              <a className={local.chip} href={`tel:${view.customerPhone.replace(/[^\d+]/g, '')}`}>
                Ligar para {view.customerName || 'o cliente'}
              </a>
              <a className={local.chip} href={whatsappLink(view.customerPhone)} target="_blank" rel="noreferrer noopener">
                WhatsApp
              </a>
            </>
          )}
          {maps && (
            <a className={local.chip} href={maps} target="_blank" rel="noreferrer noopener">
              Abrir no mapa
            </a>
          )}
        </div>
      </section>

      {error && <p className={base.error}>{error}</p>}

      {done ? (
        <section className={styles.card}>
          <h2 className={local.step}>✅ {KIND_LABEL[view.kind]} concluída</h2>
          <p className={styles.text}>
            Rastreador {view.imei} {uninstall ? 'retirado do' : 'ligado ao'} veículo
            {view.completedAt ? ` em ${formatDateTime(view.completedAt)}` : ''}. Obrigado!
          </p>
        </section>
      ) : (
        <>
          <section className={`${styles.card} ${arrived ? local.done : ''}`}>
            <h2 className={local.step}>
              <span className={local.number}>1</span> Chegada
            </h2>
            {arrived ? (
              <p className={styles.muted}>Chegada registrada. O cliente foi avisado.</p>
            ) : (
              <button
                type="button"
                className={`${base.primary} ${local.action}`}
                disabled={busy !== ''}
                onClick={() => void run('arrive', async () => setView(await installLinkApi.arrive(token)))}
              >
                {busy === 'arrive' ? 'Registrando…' : 'Cheguei no local'}
              </button>
            )}
          </section>

          {uninstall ? (
            <section className={styles.card}>
              <h2 className={local.step}>
                <span className={local.number}>2</span> Rastreador
              </h2>
              <p className={styles.text}>
                Retire o rastreador e desfaça as ligações com cuidado. Confira o IMEI na etiqueta: <strong>{view.imei || '—'}</strong>.
              </p>
              <p className={styles.muted}>Se o IMEI for outro, fale com a Farbo antes de concluir. O rastreador volta para a Farbo.</p>
            </section>
          ) : (
            <section className={`${styles.card} ${check?.ready ? local.done : ''}`}>
              <h2 className={local.step}>
                <span className={local.number}>2</span> Rastreador
              </h2>
              <p className={styles.muted}>Instale e ligue o rastreador. Depois digite o IMEI da etiqueta (15 números) e teste o sinal.</p>
              <label className={base.field}>
                IMEI
                <input
                  inputMode="numeric"
                  autoComplete="off"
                  maxLength={20}
                  value={imei}
                  onChange={(e) => {
                    setImei(e.target.value.replace(/\D/g, ''));
                    setCheck(null);
                  }}
                />
              </label>
              <button
                type="button"
                className={`${base.secondary} ${local.action}`}
                disabled={busy !== '' || imei.length < 14}
                onClick={() => void run('check', async () => setCheck(await installLinkApi.check(token, imei)))}
              >
                {busy === 'check' ? 'Testando…' : check ? 'Testar de novo' : 'Testar o sinal'}
              </button>
              {check && <p className={`${local.check} ${check.ready ? local.ok : local.wait}`}>{check.message}</p>}
            </section>
          )}

          <section className={`${styles.card} ${hasTracker ? local.done : ''}`}>
            <h2 className={local.step}>
              <span className={local.number}>3</span> Fotos
            </h2>
            <p className={styles.muted}>A do rastreador é obrigatória; as outras ajudam se o cliente reclamar depois.</p>
            <div className={local.slots}>
              {slotsOf(view.kind).map((s) => (
                <PhotoSlot
                  key={s.slot}
                  token={token}
                  slot={s.slot}
                  label={s.label}
                  hint={s.hint}
                  taken={view.photos.includes(s.slot)}
                  version={version}
                  disabled={busy !== ''}
                  onPick={(file) =>
                    void run(`photo-${s.slot}`, async () => {
                      await installLinkApi.savePhoto(token, s.slot, await compressImage(file));
                      setView((v) => (v && !v.photos.includes(s.slot) ? { ...v, photos: [...v.photos, s.slot] } : v));
                      setVersion(String(Date.now()));
                    })
                  }
                  sending={busy === `photo-${s.slot}`}
                />
              ))}
            </div>
          </section>

          <section className={styles.card}>
            <h2 className={local.step}>
              <span className={local.number}>4</span> Concluir
            </h2>
            <textarea
              className={local.textarea}
              maxLength={500}
              placeholder={
                uninstall
                  ? 'Observação (opcional): como ficou o veículo, algum detalhe…'
                  : 'Observação (opcional): onde ficou o rastreador, algum detalhe do veículo…'
              }
              value={note}
              onChange={(e) => setNote(e.target.value)}
            />
            {!canComplete && <p className={styles.muted}>Para concluir: {missing.join(', ')}.</p>}
            <button
              type="button"
              className={`${base.primary} ${local.action}`}
              disabled={!canComplete || busy !== ''}
              onClick={() => void run('complete', async () => setView(await installLinkApi.complete(token, imei, note)))}
            >
              {busy === 'complete' ? 'Concluindo…' : uninstall ? 'Concluir a retirada' : 'Concluir a instalação'}
            </button>
          </section>
        </>
      )}
    </Shell>
  );
}

export function Shell({ children, label = 'Instalador' }: { children: ReactNode; label?: string }) {
  return (
    <main className={`${base.page} ${styles.page} ${styles.live}`}>
      <header className={base.header}>
        <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={base.logo} />
        <span className={`${styles.badge} ${styles.badgeLive}`}>{label}</span>
      </header>
      {children}
    </main>
  );
}

function PhotoSlot({
  token,
  slot,
  label,
  hint,
  taken,
  version,
  disabled,
  sending,
  onPick,
}: {
  token: string;
  slot: InstallationSlot;
  label: string;
  hint: string;
  taken: boolean;
  version: string;
  disabled: boolean;
  sending: boolean;
  onPick: (file: File) => void;
}) {
  return (
    <label className={`${local.slot} ${taken ? local.slotTaken : ''}`}>
      {taken ? (
        <img className={local.photo} src={installLinkApi.photoUrl(token, slot, version)} alt={label} />
      ) : (
        <span className={local.placeholder} aria-hidden="true">
          📷
        </span>
      )}
      <span className={local.slotLabel}>{sending ? 'Enviando…' : taken ? `${label} ✓` : label}</span>
      <span className={local.slotHint}>{hint}</span>
      <input
        type="file"
        accept="image/*"
        capture="environment"
        disabled={disabled}
        onChange={(e) => {
          const file = e.target.files?.[0];
          e.target.value = '';
          if (file) onPick(file);
        }}
      />
    </label>
  );
}

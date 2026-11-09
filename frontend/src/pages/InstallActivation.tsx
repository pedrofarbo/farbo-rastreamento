import { useEffect, useMemo, useState } from 'react';

import type { InstallActivation as Activation, InstallActivationView, InstallStock, InstallStockDevice } from '@/types';

import base from './PreLaunch.module.css';
import styles from './PublicTheft.module.css';
import local from './InstallerPage.module.css';

/** Quantas opções a lista mostra por vez (o resto, digitando mais números). */
const SHOWN = 6;
/** De quanto em quanto tempo a tela acompanha a ativação em andamento. */
const POLL_MS = 5000;

const onlyDigits = (text: string) => text.replace(/\D/g, '');

/** A situação da ativação, como o instalador entende. */
export function activationMessage(a: Activation): string {
  switch (a.status) {
    case 'SENDING':
      return 'Mandando a configuração por SMS para o chip…';
    case 'WAITING':
      return 'Configuração enviada. Esperando o rastreador conectar: pode levar alguns minutos.';
    case 'DONE':
      return 'Rastreador ativado e conectado. Agora instale e teste o sinal.';
    case 'FAILED':
      return `A configuração não chegou ao chip${a.error ? `: ${a.error}` : ''}. Confira o chip e ative de novo.`;
    case 'TIMEOUT':
      return 'O rastreador não conectou em 20 minutos. Confira se o chip está bem encaixado, com sinal e crédito, se o rastreador está ligado, e ative de novo.';
    default:
      return 'Esta ativação foi interrompida.';
  }
}

/** O ícone de cada passo: entregue ou confirmado, a caminho, falhou ou ainda não saiu. */
function stepIcon(status: string): string {
  if (status === 'delivered' || status === 'confirmed') return '✓';
  if (status === 'failed' || status === 'undelivered' || status === 'rejected' || status === 'canceled') return '✗';
  if (status === 'pending') return '•';
  return '⏳';
}

const live = (a: Activation | null | undefined) => a?.status === 'SENDING' || a?.status === 'WAITING';

/**
 * A ativação do rastreador pelo link do instalador, antes de instalar: no
 * agendamento, ele escolhe o rastreador entre os livres (pelo IMEI da
 * etiqueta); o que nunca conectou pede o chip (pelo ICCID impresso nele) e a
 * ativação, que vai por SMS e é acompanhada até o rastreador conectar. Na
 * instalação avulsa, o rastreador é o do link (fixedImei). Quando o rastreador
 * fica pronto para o teste do sinal, avisa com o IMEI (onReady).
 */
export function InstallActivation({
  fixedImei,
  api,
  disabled = false,
  onReady,
}: {
  fixedImei?: string;
  api: {
    stock: () => Promise<InstallStock>;
    activation: () => Promise<InstallActivationView>;
    /** deviceId nulo: o rastreador do link. */
    activate: (deviceId: string | null, chipId: string) => Promise<InstallActivationView>;
  };
  disabled?: boolean;
  onReady: (imei: string | null) => void;
}) {
  const [stock, setStock] = useState<InstallStock | null>(null);
  const [view, setView] = useState<InstallActivationView | null>(null);
  const [loadError, setLoadError] = useState('');
  const [deviceSearch, setDeviceSearch] = useState('');
  const [deviceId, setDeviceId] = useState('');
  const [chipSearch, setChipSearch] = useState('');
  const [chipId, setChipId] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    let alive = true;
    Promise.all([api.stock(), api.activation()])
      .then(([s, v]) => {
        if (!alive) return;
        setStock(s);
        setView(v);
        // A ativação já feita por este link (a página recarregou): o rastreador e o chip dela.
        const a = v.activation;
        if (a) {
          const dev = s.devices?.find((d) => d.imei === a.imei);
          if (dev) {
            setDeviceId(dev.id);
            setDeviceSearch(dev.imei);
          }
          const chip = dev?.chipId && dev.iccid === a.iccid ? dev.chipId : s.chips.find((c) => c.iccid === a.iccid)?.id;
          if (chip) setChipId(chip);
          setChipSearch(a.iccid);
        }
      })
      .catch((err) => alive && setLoadError(err instanceof Error ? err.message : 'Sem conexão agora. Recarregue a página.'));
    return () => {
      alive = false;
    };
    // Uma vez, na abertura.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Enquanto a ativação anda, a tela acompanha.
  const activation = view?.activation ?? null;
  useEffect(() => {
    if (!live(activation)) return;
    const timer = window.setInterval(() => {
      api
        .activation()
        .then(setView)
        .catch(() => undefined);
    }, POLL_MS);
    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activation?.status]);

  const devices = useMemo(() => stock?.devices ?? [], [stock]);
  const device: InstallStockDevice | undefined = fixedImei ? undefined : devices.find((d) => d.id === deviceId);
  const imei = fixedImei ?? device?.imei ?? '';
  // A ativação deste rastreador (a do link pode ser de outro, escolhido antes).
  const mine = activation && activation.imei === imei ? activation : null;
  const ready = Boolean(imei) && (Boolean(device?.activated) || mine?.status === 'DONE');

  useEffect(() => {
    onReady(ready ? imei : null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ready, imei]);

  const deviceDigits = onlyDigits(deviceSearch);
  const deviceMatches = deviceDigits ? devices.filter((d) => d.imei.includes(deviceDigits)) : devices;
  const chipDigits = onlyDigits(chipSearch);
  // O chip que já está no rastreador escolhido vem primeiro (ativar com ele mesmo).
  const chips = useMemo(() => {
    const free = stock?.chips ?? [];
    const own = device?.chipId ? [{ id: device.chipId, iccid: device.iccid, carrier: 'já está neste rastreador' }] : [];
    return [...own, ...free.filter((c) => c.id !== device?.chipId)];
  }, [stock, device]);
  const chipMatches = chipDigits ? chips.filter((c) => c.iccid.includes(chipDigits)) : chips;

  const activate = async () => {
    if (!chipId || busy) return;
    setBusy(true);
    setError('');
    try {
      setView(await api.activate(fixedImei ? null : deviceId, chipId));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Não deu certo. Tente de novo.');
    } finally {
      setBusy(false);
    }
  };

  if (loadError) return <p className={base.error}>{loadError}</p>;
  if (!stock || !view) {
    return (
      <p className={styles.muted} role="status">
        Carregando o estoque…
      </p>
    );
  }

  const needsActivation = Boolean(imei) && !device?.activated && mine?.status !== 'DONE';

  return (
    <div className={local.activation}>
      {!fixedImei && (
        <>
          <p className={styles.muted}>
            Escolha o rastreador que você vai instalar: digite o IMEI da etiqueta (os últimos números bastam).
          </p>
          {!device && (
            <label className={base.field}>
              IMEI do rastreador
              <input
                inputMode="numeric"
                autoComplete="off"
                maxLength={20}
                value={deviceSearch}
                disabled={disabled}
                onChange={(e) => {
                  const text = onlyDigits(e.target.value);
                  setDeviceSearch(text);
                  // O IMEI inteiro escolhe sozinho.
                  const exact = devices.find((d) => d.imei === text);
                  setDeviceId(exact ? exact.id : '');
                }}
              />
            </label>
          )}
          {!device && (
            <div className={local.options} role="radiogroup" aria-label="Rastreadores livres">
              {deviceMatches.slice(0, SHOWN).map((d) => (
                <button
                  key={d.id}
                  type="button"
                  role="radio"
                  aria-checked={false}
                  className={local.option}
                  disabled={disabled}
                  onClick={() => {
                    setDeviceId(d.id);
                    setDeviceSearch(d.imei);
                    setChipId('');
                    setChipSearch('');
                    setError('');
                  }}
                >
                  <span className={local.optionMain}>
                    <span className={local.mono}>{d.imei}</span>
                    {d.model && <span className={styles.muted}>{d.model}</span>}
                  </span>
                  <span className={`${local.tag} ${d.activated ? local.tagOk : local.tagWait}`}>
                    {d.activated ? 'Ativado' : 'Precisa ativar'}
                  </span>
                </button>
              ))}
              {deviceMatches.length === 0 && (
                <p className={styles.muted}>
                  Nenhum rastreador livre com esse IMEI. Confira os números na etiqueta ou fale com a Farbo.
                </p>
              )}
              {deviceMatches.length > SHOWN && (
                <p className={styles.muted}>Mais {deviceMatches.length - SHOWN}: digite mais números do IMEI.</p>
              )}
            </div>
          )}
          {device && (
            <div className={`${local.option} ${local.optionOn}`}>
              <span className={local.optionMain}>
                <span className={local.mono}>{device.imei}</span>
                {device.model && <span className={styles.muted}>{device.model}</span>}
              </span>
              <button
                type="button"
                className={local.link}
                disabled={disabled || busy}
                onClick={() => {
                  setDeviceId('');
                  setDeviceSearch('');
                  setChipId('');
                  setChipSearch('');
                }}
              >
                Trocar
              </button>
            </div>
          )}
          {device?.activated && (
            <p className={`${local.check} ${local.ok}`}>Rastreador já ativado: instale, ligue e teste o sinal.</p>
          )}
        </>
      )}

      {needsActivation && (
        <div className={local.substep}>
          <h3 className={local.substepTitle}>Chip e ativação</h3>
          <p className={styles.muted}>
            Coloque o chip no rastreador e ligue o rastreador (no veículo ou numa bateria). Escolha o chip pelo ICCID
            impresso nele (os últimos números bastam) e ative: a configuração vai por SMS para o chip.
          </p>
          {!view.enabled ? (
            <p className={`${local.check} ${local.wait}`}>A ativação por SMS não está disponível agora: fale com a Farbo.</p>
          ) : (
            <>
              <label className={base.field}>
                ICCID do chip
                <input
                  inputMode="numeric"
                  autoComplete="off"
                  maxLength={22}
                  value={chipSearch}
                  disabled={disabled || busy || live(mine)}
                  onChange={(e) => {
                    const text = onlyDigits(e.target.value);
                    setChipSearch(text);
                    const exact = chips.find((c) => c.iccid === text);
                    setChipId(exact ? exact.id : '');
                  }}
                />
              </label>
              {!chipId && (
                <div className={local.options} role="radiogroup" aria-label="Chips livres">
                  {chipMatches.slice(0, SHOWN).map((c) => (
                    <button
                      key={c.id}
                      type="button"
                      role="radio"
                      aria-checked={false}
                      className={local.option}
                      disabled={disabled}
                      onClick={() => {
                        setChipId(c.id);
                        setChipSearch(c.iccid);
                        setError('');
                      }}
                    >
                      <span className={local.optionMain}>
                        <span className={local.mono}>{c.iccid}</span>
                        {c.carrier && <span className={styles.muted}>{c.carrier}</span>}
                      </span>
                    </button>
                  ))}
                  {chipMatches.length === 0 && (
                    <p className={styles.muted}>Nenhum chip livre com esse ICCID. Confira os números ou fale com a Farbo.</p>
                  )}
                  {chipMatches.length > SHOWN && (
                    <p className={styles.muted}>Mais {chipMatches.length - SHOWN}: digite mais números do ICCID.</p>
                  )}
                </div>
              )}
              <button
                type="button"
                className={`${base.primary} ${local.action}`}
                disabled={disabled || busy || !chipId || live(mine) || view.left === 0}
                onClick={() => void activate()}
              >
                {busy ? 'Ativando…' : live(mine) ? 'Ativação em andamento…' : mine ? 'Ativar de novo' : 'Ativar o rastreador'}
              </button>
              {view.left === 0 && !live(mine) && mine?.status !== 'DONE' && (
                <p className={styles.muted}>Este link já fez todas as ativações que podia: fale com a Farbo.</p>
              )}
              {view.left > 0 && mine && !live(mine) && (
                <p className={styles.muted}>
                  {view.left === 1 ? 'Resta 1 ativação' : `Restam ${view.left} ativações`} neste link.
                </p>
              )}
            </>
          )}
        </div>
      )}

      {error && <p className={base.error}>{error}</p>}

      {mine && (
        <div className={local.progress} aria-live="polite">
          <p className={`${local.check} ${mine.status === 'DONE' ? local.ok : local.wait}`} role="status">
            {activationMessage(mine)}
          </p>
          {mine.status !== 'DONE' && mine.steps.length > 0 && (
            <ul className={local.steps}>
              {mine.steps.map((s, i) => (
                <li key={i}>
                  <span aria-hidden="true">{stepIcon(s.status)}</span> {s.label}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}

import { useState } from 'react';
import type { FormEvent } from 'react';

import { publicApi } from '@/api/resources';
import { centsToInput, formatMoney, parseMoney } from '@/services/format';
import type { InstallPrices } from '@/types';

import styles from '../Partner.module.css';
import base from '../PreLaunch.module.css';

/**
 * Os valores da instalação (sem e, se ele oferece a escolha, com o relé do
 * bloqueio) e da desinstalação do representante que recebe direto do
 * cliente: ele mesmo define, pela página dele. Não passam pela Farbo; o
 * cliente vê esses valores ao contratar pelo link dele.
 */
export function InstallPricesSection({
  token,
  prices,
  onChange,
}: {
  token: string;
  prices: InstallPrices;
  onChange: (prices: InstallPrices) => void;
}) {
  const [install, setInstall] = useState(centsToInput(prices.installCents));
  const [uninstall, setUninstall] = useState(centsToInput(prices.uninstallCents));
  const [blocking, setBlocking] = useState(prices.blockingCents != null ? centsToInput(prices.blockingCents) : '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError('');
    setSaved(false);
    const installCents = parseMoney(install);
    const uninstallCents = parseMoney(uninstall);
    // Em branco: sem a instalação com bloqueio (o cliente não escolhe).
    const blockingCents = blocking.trim() === '' ? null : parseMoney(blocking);
    if (installCents === null || uninstallCents === null || (blocking.trim() !== '' && blockingCents === null)) {
      setError('Confira os valores (por exemplo, 120,00; 0 para sem custo).');
      return;
    }
    setBusy(true);
    try {
      onChange(await publicApi.partnerInstallPrices(token, { installCents, uninstallCents, blockingCents }));
      setSaved(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Não deu para salvar agora. Tente de novo.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className={styles.qrCard} aria-label="Seus valores de instalação">
      <h2 className={styles.heading}>Seus valores de instalação</h2>
      <p className={styles.qrHint}>
        Você instala, desinstala e recebe direto do cliente: esses valores não passam pela Farbo. Quem contrata pelo seu
        link vê estes valores no site e no app (hoje: {formatMoney(prices.installCents)} a instalação
        {prices.blockingCents != null && <>, {formatMoney(prices.blockingCents)} com bloqueio</>} e{' '}
        {formatMoney(prices.uninstallCents)} a desinstalação do comodato).
      </p>
      <form className={styles.installForm} onSubmit={submit} noValidate>
        <div className={styles.row2}>
          <label className={base.field}>
            <span>{blocking.trim() ? 'Instalação sem bloqueio (R$)' : 'Instalação (R$)'}</span>
            <input inputMode="decimal" value={install} onChange={(e) => setInstall(e.target.value)} />
          </label>
          <label className={base.field}>
            <span>Instalação com bloqueio (R$)</span>
            <input
              inputMode="decimal"
              value={blocking}
              placeholder="em branco: sem a escolha"
              onChange={(e) => setBlocking(e.target.value)}
            />
          </label>
        </div>
        <p className={styles.qrHint}>
          Com bloqueio é a instalação com o relé que bloqueia o motor pelo app: preenchida, o cliente escolhe no pedido
          entre as duas.
        </p>
        <label className={base.field}>
          <span>Desinstalação do comodato (R$)</span>
          <input inputMode="decimal" value={uninstall} onChange={(e) => setUninstall(e.target.value)} />
        </label>
        {error && (
          <p className={base.error} role="alert">
            {error}
          </p>
        )}
        {saved && (
          <p className={styles.qrHint} role="status">
            Valores salvos: já valem para quem contratar pelo seu link.
          </p>
        )}
        <div className={styles.installActions}>
          <button type="submit" className={`${styles.installButton} ${styles.installPrimary}`} disabled={busy}>
            {busy ? 'Salvando…' : 'Salvar os valores'}
          </button>
        </div>
      </form>
    </section>
  );
}

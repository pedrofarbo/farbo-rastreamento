import { useCallback, useEffect, useState } from 'react';
import type { FormEvent } from 'react';

import { publicApi } from '@/api/resources';
import { trackingUrl } from '@/services/fulfillment';
import type { KitDevice, KitShipment, KitStatus, PartnerKits } from '@/types';

import { whenLabel } from './InstallationsSection';
import styles from '../Partner.module.css';
import base from '../PreLaunch.module.css';

const STATUS: Record<KitStatus, { label: string; className: string }> = {
  requested: { label: 'Esperando a Farbo', className: styles.pending },
  preparing: { label: 'Separada', className: styles.pending },
  shipped: { label: 'A caminho', className: styles.closed },
  delivered: { label: 'Recebida', className: styles.paid },
  canceled: { label: 'Cancelada', className: styles.none },
};

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/**
 * Os kits de instalação do representante, na página dele: o estoque (os
 * rastreadores que chegaram e ainda não foram instalados — instalar baixa
 * sozinho), as remessas com o código de rastreio, o "recebi" quando chegam e
 * o pedido de mais kits à Farbo. Com a senha na página, os instalados mostram
 * o cliente.
 */
export function KitsSection({ token }: { token: string }) {
  const [kits, setKits] = useState<PartnerKits | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const load = useCallback(
    () =>
      publicApi
        .partnerKits(token)
        .then((k) => {
          setKits(k);
          setError('');
        })
        .catch(() => setError('Não deu para carregar os kits agora. Tente de novo em instantes.')),
    [token],
  );
  useEffect(() => {
    void load();
  }, [load]);

  const act = async (run: () => Promise<unknown>) => {
    setBusy(true);
    setError('');
    try {
      await run();
      await load();
      return true;
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Não deu agora. Tente de novo.');
      return false;
    } finally {
      setBusy(false);
    }
  };

  const open = kits?.shipments.filter((k) => k.status === 'requested' || k.status === 'preparing' || k.status === 'shipped') ?? [];
  const past = kits?.shipments.filter((k) => k.status === 'delivered' || k.status === 'canceled') ?? [];
  const inTransit = open.filter((k) => k.status === 'shipped').reduce((n, k) => n + k.devices.filter((d) => !d.installed).length, 0);
  const waiting = open.some((k) => k.status === 'requested');

  return (
    <section className={styles.qrCard} aria-label="Seus kits">
      <h2 className={styles.heading}>Seus kits</h2>
      <p className={styles.qrHint}>
        Os rastreadores que a Farbo manda para você entregar e instalar. Quando a remessa chegar, confirme aqui: ela entra
        no seu estoque, que baixa sozinho a cada instalação.
      </p>
      {error && (
        <p className={base.error} role="alert">
          {error}
        </p>
      )}
      {!kits ? (
        !error && (
          <p className={styles.qrHint} role="status">
            Carregando…
          </p>
        )
      ) : (
        <>
          <div className={styles.stats}>
            <div className={styles.stat}>
              <strong>{kits.stock.length}</strong>
              <span>Em estoque</span>
            </div>
            <div className={styles.stat}>
              <strong>{inTransit}</strong>
              <span>A caminho</span>
            </div>
            <div className={styles.stat}>
              <strong>{kits.installed}</strong>
              <span>Instalados</span>
            </div>
          </div>

          {kits.stock.length > 0 && (
            <details className={styles.kitDetails}>
              <summary>Rastreadores no estoque ({kits.stock.length})</summary>
              <DeviceList devices={kits.stock} />
            </details>
          )}

          {open.length > 0 && (
            <div className={styles.installGroup}>
              <h3 className={styles.subheading}>Em andamento</h3>
              <ul className={styles.installList}>
                {open.map((k) => (
                  <li key={k.id} className={styles.installItem}>
                    <ShipmentHead shipment={k} />
                    {k.status === 'requested' ? (
                      <>
                        <span>
                          Você pediu {plural(k.requestedQuantity ?? 0, 'kit', 'kits')} em {whenLabel(k.createdAt)}
                        </span>
                        {k.requestNote && <span className={styles.muted}>Obs.: {k.requestNote}</span>}
                        <div className={styles.installActions}>
                          <button
                            type="button"
                            className={styles.installButton}
                            disabled={busy}
                            onClick={() => void act(() => publicApi.partnerKitCancel(token, k.id))}
                          >
                            Cancelar o pedido
                          </button>
                        </div>
                      </>
                    ) : (
                      <>
                        <span>
                          {plural(k.devices.length, 'rastreador', 'rastreadores')}
                          {k.status === 'preparing' ? ' separados: a Farbo vai despachar' : ` · enviada em ${whenLabel(k.shippedAt ?? k.createdAt)}`}
                        </span>
                        {k.status === 'shipped' && <Tracking shipment={k} />}
                        {k.notes && <span className={styles.muted}>Obs.: {k.notes}</span>}
                        {k.devices.length > 0 && (
                          <details className={styles.kitDetails}>
                            <summary>Ver os rastreadores</summary>
                            <DeviceList devices={k.devices} />
                          </details>
                        )}
                        {k.status === 'shipped' && (
                          <div className={styles.installActions}>
                            <button
                              type="button"
                              className={`${styles.installButton} ${styles.installPrimary}`}
                              disabled={busy}
                              onClick={() => void act(() => publicApi.partnerKitReceived(token, k.id))}
                            >
                              Recebi os kits
                            </button>
                          </div>
                        )}
                      </>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {waiting ? (
            <p className={styles.qrHint}>
              Seu pedido está com a Farbo: ela separa e o código de rastreio aparece aqui quando despachar. Para mudar a
              quantidade, cancele e peça de novo.
            </p>
          ) : (
            <RequestForm busy={busy} onSubmit={(quantity, note) => act(() => publicApi.partnerKitRequest(token, { quantity, note }))} />
          )}

          {past.length > 0 && (
            <div className={styles.installGroup}>
              <h3 className={styles.subheading}>Remessas anteriores</h3>
              <ul className={styles.installList}>
                {past.map((k) => (
                  <li key={k.id} className={styles.installItem}>
                    <ShipmentHead shipment={k} />
                    <span className={styles.muted}>
                      {k.status === 'delivered'
                        ? `${plural(k.devices.length, 'rastreador', 'rastreadores')} · recebida em ${whenLabel(k.deliveredAt ?? k.createdAt)}`
                        : `${k.requestedQuantity != null ? `pedido de ${plural(k.requestedQuantity, 'kit', 'kits')} · ` : ''}cancelada em ${whenLabel(k.canceledAt ?? k.createdAt)}`}
                    </span>
                    {k.devices.length > 0 && (
                      <details className={styles.kitDetails}>
                        <summary>
                          {plural(k.devices.filter((d) => d.installed).length, 'instalado', 'instalados')} de {k.devices.length}
                        </summary>
                        <DeviceList devices={k.devices} />
                      </details>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </section>
  );
}

/** A situação da remessa e o código de rastreio (se tiver). */
function ShipmentHead({ shipment: k }: { shipment: KitShipment }) {
  return (
    <span className={styles.kitHead}>
      <strong>{k.requestedQuantity != null && k.devices.length === 0 ? 'Pedido de kits' : 'Remessa da Farbo'}</strong>
      <span className={`${styles.status} ${STATUS[k.status].className}`}>{STATUS[k.status].label}</span>
    </span>
  );
}

function Tracking({ shipment: k }: { shipment: KitShipment }) {
  return (
    <span>
      {k.carrier}
      {k.trackingCode && (
        <>
          {' · '}
          <a href={trackingUrl(k.trackingCode)} target="_blank" rel="noopener noreferrer">
            {k.trackingCode}
          </a>
        </>
      )}
    </span>
  );
}

/** Os rastreadores: o IMEI, o chip e, instalado, o veículo (e o cliente, com a senha). */
function DeviceList({ devices }: { devices: KitDevice[] }) {
  return (
    <ul className={styles.kitDevices}>
      {devices.map((d) => (
        <li key={d.id}>
          <span className={styles.kitImei}>{d.imei}</span>
          {d.iccid && <span className={styles.muted}>chip {d.iccid}</span>}
          {d.installed && (
            <span className={styles.muted}>
              instalado no {d.vehicleName}
              {d.plate && ` (${d.plate})`}
              {d.customerName && ` · ${d.customerName}`}
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}

/** O pedido de mais kits: a quantidade e uma observação. */
function RequestForm({ busy, onSubmit }: { busy: boolean; onSubmit: (quantity: number, note: string) => Promise<boolean> }) {
  const [quantity, setQuantity] = useState('5');
  const [note, setNote] = useState('');
  const n = Number(quantity);
  const valid = Number.isInteger(n) && n >= 1 && n <= 100;
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!valid) return;
    if (await onSubmit(n, note.trim())) setNote('');
  };
  return (
    <form className={styles.installForm} onSubmit={(e) => void submit(e)} aria-label="Pedir kits">
      <h3 className={styles.subheading}>Pedir kits</h3>
      <label className={base.field}>
        <span>Quantos kits</span>
        <input type="number" inputMode="numeric" min={1} max={100} value={quantity} onChange={(e) => setQuantity(e.target.value)} required />
      </label>
      <label className={base.field}>
        <span>Observação (opcional)</span>
        <input
          type="text"
          maxLength={500}
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="Ex.: preciso até sexta"
        />
      </label>
      <div className={styles.installActions}>
        <button type="submit" className={`${styles.installButton} ${styles.installPrimary}`} disabled={!valid || busy}>
          {busy ? 'Enviando…' : 'Pedir à Farbo'}
        </button>
      </div>
    </form>
  );
}

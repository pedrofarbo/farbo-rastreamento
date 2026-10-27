import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Link } from 'react-router-dom';

import { CloseIcon, LocateIcon } from '@/app/icons';
import { Address } from '@/components/ui/Address';
import { Badge } from '@/components/ui/Badge';
import type { BadgeTone } from '@/components/ui/Badge';
import { VehicleSwitcher } from '@/components/vehicle/VehicleSwitcher';
import { formatDeviceStatus, formatRelative, formatSpeed } from '@/services/format';
import type { Geofence, VehicleView } from '@/types';

import { TrackerMap } from './TrackerMap';
import styles from './FleetMapFullscreen.module.css';

function statusTone(vehicle: VehicleView): BadgeTone {
  if (vehicle.device?.status === 'ONLINE') return 'success';
  if (vehicle.device?.status === 'STALE') return 'warning';
  return 'neutral';
}

/**
 * Entra na tela cheia do navegador (desktop e Android; no iPhone não existe):
 * chame no clique, antes de abrir o mapa. Sem ela, o mapa cobre a página.
 */
export function enterBrowserFullscreen() {
  const root = document.documentElement;
  if (root.requestFullscreen && !document.fullscreenElement) {
    void root.requestFullscreen({ navigationUI: 'hide' }).catch(() => undefined);
  }
}

// Quantos mapas em tela cheia estão montados. Desmontar não sai da tela cheia
// do navegador na hora: se o mapa montar de novo logo em seguida (o React, em
// desenvolvimento, monta, desmonta e monta), ela continua.
let mounted = 0;

// A saída da tela cheia do navegador logo depois de entrar não fecha o mapa.
// No Linux (Chrome com a área de trabalho COSMIC), o sistema a desfaz sozinho
// cerca de 1 s depois de entrar: o mapa continua aberto, na janela. O Esc
// depois disso fecha (o navegador já não o segura).
const SETTLE_MS = 3000;

interface FleetMapFullscreenProps {
  /** Os veículos do mapa, na ordem da lista (a do anterior e do próximo). */
  vehicles: VehicleView[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  geofences: Geofence[];
  /** "Frota", ou o grupo escolhido. */
  title: string;
  /** O nome do dono abre a ficha dele (só o admin). */
  customerLinks?: boolean;
  onClose: () => void;
}

/**
 * O mapa do painel da central na tela inteira: a frota, o veículo escolhido
 * (com a troca e a busca) e os dados dele num cartão por cima do mapa. Fecha
 * no X, no Esc e ao sair da tela cheia do navegador.
 */
export function FleetMapFullscreen({
  vehicles,
  selectedId,
  onSelect,
  geofences,
  title,
  customerLinks = false,
  onClose,
}: FleetMapFullscreenProps) {
  const [following, setFollowing] = useState(true);
  const [recenter, setRecenter] = useState(0);
  const [fences, setFences] = useState(true);
  const closeRef = useRef<HTMLButtonElement>(null);
  const closed = useRef(false);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  const selected = vehicles.find((v) => v.id === selectedId) ?? null;
  const online = vehicles.filter((v) => v.device?.status === 'ONLINE').length;
  const position = selected?.lastPosition ?? null;
  const acc = selected ? (selected.state?.acc ?? position?.acc ?? null) : null;

  // Fecha uma vez só: o Esc, no navegador em tela cheia, também a encerra.
  const close = () => {
    if (closed.current) return;
    closed.current = true;
    onCloseRef.current();
  };
  const closeOnce = useRef(close);
  closeOnce.current = close;

  useEffect(() => {
    mounted += 1;
    const opener = document.activeElement as HTMLElement | null;
    closeRef.current?.focus({ preventScroll: true });
    // Saiu da tela cheia do navegador (Esc, voltar do Android): fecha o mapa
    // (menos logo depois de entrar, ver SETTLE_MS).
    let enteredAt = document.fullscreenElement ? Date.now() : null;
    const onChange = () => {
      if (document.fullscreenElement) {
        enteredAt = Date.now();
      } else if (enteredAt !== null) {
        const settled = Date.now() - enteredAt >= SETTLE_MS;
        enteredAt = null;
        if (settled) closeOnce.current();
      }
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.defaultPrevented) closeOnce.current();
    };
    document.addEventListener('fullscreenchange', onChange);
    window.addEventListener('keydown', onKey);
    return () => {
      mounted -= 1;
      document.removeEventListener('fullscreenchange', onChange);
      window.removeEventListener('keydown', onKey);
      setTimeout(() => {
        if (mounted === 0 && document.fullscreenElement) void document.exitFullscreen().catch(() => undefined);
      }, 0);
      opener?.focus?.({ preventScroll: true });
    };
  }, []);

  const select = (id: string) => {
    onSelect(id);
    setFollowing(true);
  };
  const center = () => {
    setFollowing(true);
    setRecenter((key) => key + 1);
  };

  return createPortal(
    <div className={styles.overlay} role="dialog" aria-modal="true" aria-label="Mapa da frota em tela cheia">
      <div className={styles.map}>
        <TrackerMap
          vehicles={vehicles}
          selectedId={selectedId}
          onSelect={select}
          geofences={fences ? geofences : []}
          showControls={false}
          following={following}
          onFollowingChange={setFollowing}
          recenterKey={recenter}
        />
      </div>

      <div className={styles.top}>
        <button ref={closeRef} type="button" className={styles.round} onClick={close} aria-label="Sair da tela cheia" title="Sair da tela cheia (Esc)">
          <CloseIcon />
        </button>
        <div className={styles.title}>
          <span className={styles.name}>{title}</span>
          <span className={styles.counts}>
            {online} de {vehicles.length} online
          </span>
        </div>
        {selected && (
          <div className={styles.switcher}>
            <VehicleSwitcher vehicles={vehicles} currentId={selected.id} onSelect={select} />
          </div>
        )}
      </div>

      <div className={styles.dock}>
        <div className={styles.side}>
          {geofences.length > 0 && (
            <button
              type="button"
              className={`${styles.round} ${styles.text} ${fences ? styles.on : ''}`}
              onClick={() => setFences(!fences)}
              aria-pressed={fences}
              title={fences ? 'Esconder as cercas' : 'Mostrar as cercas'}
            >
              Cercas
            </button>
          )}
          <button
            type="button"
            className={`${styles.round} ${following ? styles.on : ''}`}
            onClick={center}
            aria-label={selected ? 'Seguir o veículo' : 'Mostrar a frota inteira'}
            aria-pressed={selected ? following : undefined}
            title={selected ? 'Seguir o veículo' : 'Mostrar a frota inteira'}
          >
            <LocateIcon active={following && Boolean(selected)} />
          </button>
        </div>

        <div className={styles.card} aria-live="polite">
          {selected ? (
            <>
              <div className={styles.cardHead}>
                <span className={styles.vehicle}>{selected.name}</span>
                {selected.plate && <span className={styles.plate}>{selected.plate}</span>}
                <Badge tone={statusTone(selected)} dot>
                  {selected.device ? formatDeviceStatus(selected.device.status) : 'Sem rastreador'}
                </Badge>
              </div>
              {selected.ownerName && (
                <span className={styles.owner}>
                  {customerLinks && selected.ownerId ? (
                    <Link to={`/clientes/${selected.ownerId}`}>{selected.ownerName}</Link>
                  ) : (
                    selected.ownerName
                  )}
                </span>
              )}
              {position ? (
                <>
                  <span className={styles.stats}>
                    <span>{formatSpeed(position.speedKmh)}</span>
                    <span className={acc ? styles.good : undefined}>
                      Ignição {acc === null ? '—' : acc ? 'ligada' : 'desligada'}
                    </span>
                    <span>{formatRelative(position.gpsTimestamp)}</span>
                  </span>
                  <Address lat={position.latitude} lon={position.longitude} className={styles.address} />
                </>
              ) : (
                <span className={styles.muted}>Sem posição conhecida.</span>
              )}
              <Link to={`/veiculos/${selected.id}`} className={styles.open}>
                Abrir veículo
              </Link>
            </>
          ) : (
            <span className={styles.muted}>
              {vehicles.length === 0 ? 'Nenhum veículo neste filtro.' : 'Toque num veículo no mapa para ver os dados dele.'}
            </span>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}

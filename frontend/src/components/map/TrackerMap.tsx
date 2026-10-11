import { memo, useEffect, useMemo, useReducer, useRef, useState } from 'react';
import { Circle, MapContainer, Marker, Polyline, Popup, useMap } from 'react-leaflet';
import type { LatLngExpression, LatLngTuple } from 'leaflet';
import L from 'leaflet';

import { Address } from '@/components/ui/Address';
import { formatCoordinates, formatDateTime, formatDistance, formatHeading, formatSpeed } from '@/services/format';
import type { Geofence, Position, VehicleView } from '@/types';

import { CLUSTER_MAX_ZOOM, FLEET_SIZE, fleetLayout } from './fleet';
import type { MapView } from './fleet';
import { GIVE_UP_MS, NO_TILES, SHOW_AFTER_MS, mapPhase, trackTiles, vehiclesLabel } from './loading';
import type { TileEvent } from './loading';
import { clusterIcon, endpointIcon, vehicleIcon } from './markers';
import { OsmTiles } from './OsmTiles';
import styles from './TrackerMap.module.css';
import { useVehicleMotion } from './useVehicleMotion';

/** Centro padrão (São Paulo) quando ainda não há nenhuma posição. */
const DEFAULT_CENTER: LatLngTuple = [-23.5505, -46.6333];
const DEFAULT_ZOOM = 13;
const MOVING_SPEED_KMH = 3;
/** Numa frota, escolher um veículo aproxima pelo menos até aqui (bairro). */
const FOCUS_ZOOM = 15;
/** De quanto em quanto tempo, no máximo, a frota se redesenha ao arrastar. */
const VIEW_THROTTLE_MS = 200;
/** O sumir da tela de carregamento (o mesmo do CSS). */
const FADE_MS = 300;
/** Verde da marca, o mesmo da landing. */
const BRAND_GREEN = '#3be558';

interface TrackerMapProps {
  vehicles: VehicleView[];
  selectedId?: string | null;
  onSelect?: (vehicleId: string) => void;
  /** Trajeto desenhado por cima (histórico ou playback). */
  track?: Position[];
  /** Posição destacada durante o playback. */
  highlight?: Position | null;
  geofences?: Geofence[];
  showControls?: boolean;
  /** Seguir o veículo, controlado de fora (tela cheia do app). Sem isso, o
   * mapa usa o próprio botão "Seguindo". */
  following?: boolean;
  /** Chamado com false quando o usuário arrasta o mapa (deixa de seguir). */
  onFollowingChange?: (following: boolean) => void;
  /** Trocar o valor centraliza de novo: no veículo ou no trajeto inteiro. */
  recenterKey?: number;
}

export function TrackerMap({
  vehicles,
  selectedId,
  onSelect,
  track,
  highlight,
  geofences = [],
  showControls = true,
  following: followingProp,
  onFollowingChange,
  recenterKey,
}: TrackerMapProps) {
  const [ownFollowing, setOwnFollowing] = useState(true);
  const following = followingProp ?? ownFollowing;
  const toggleFollowing = () => {
    if (onFollowingChange) onFollowingChange(!following);
    else setOwnFollowing(!following);
  };
  const [showFences, setShowFences] = useState(true);

  const located = useMemo(
    () => vehicles.filter((vehicle) => vehicle.lastPosition !== null),
    [vehicles],
  );

  const selected = located.find((vehicle) => vehicle.id === selectedId) ?? null;

  // Numa frota sem veículo escolhido, o mapa abre mostrando todos (em vez de
  // seguir o primeiro da lista).
  const fleet = located.length > FLEET_SIZE;
  const showAll = fleet && !selected && !track;
  const overview = useMemo<LatLngTuple[] | null>(
    () =>
      showAll
        ? located.map((vehicle) => {
            const { latitude, longitude } = vehicle.lastPosition as Position;
            return [latitude, longitude];
          })
        : null,
    [showAll, located],
  );

  const center: LatLngTuple = selected?.lastPosition
    ? [selected.lastPosition.latitude, selected.lastPosition.longitude]
    : located[0]?.lastPosition
      ? [located[0].lastPosition.latitude, located[0].lastPosition.longitude]
      : DEFAULT_CENTER;

  // O carregamento: até as imagens do mapa da área na tela chegarem. Pronto,
  // o arrastar não mexe mais nisso (nem redesenha o mapa a cada imagem).
  const [tiles, onTile] = useReducer(trackTiles, NO_TILES);
  const phase = mapPhase(tiles);
  const phaseRef = useRef(phase);
  phaseRef.current = phase;
  const tileEvents = useMemo<L.LeafletEventHandlerFnMap>(() => {
    const on = (event: TileEvent) => () => {
      if (phaseRef.current !== 'ready') onTile(event);
    };
    return {
      tileloadstart: on('start'),
      tileload: on('loaded'),
      tileerror: on('failed'),
      tileabort: on('aborted'),
      load: on('settled'),
    };
  }, []);
  useEffect(() => {
    if (phase !== 'loading') return;
    const timer = window.setTimeout(() => onTile('timeout'), GIVE_UP_MS);
    return () => window.clearTimeout(timer);
  }, [phase]);

  const trackLine = useMemo<LatLngExpression[]>(
    () => (track ?? []).map((point) => [point.latitude, point.longitude] as LatLngTuple),
    [track],
  );

  return (
    <div className={styles.wrapper} aria-busy={phase === 'loading'}>
      <MapContainer
        center={center}
        zoom={DEFAULT_ZOOM}
        className={styles.map}
        zoomControl={false}
        preferCanvas
      >
        <OsmTiles eventHandlers={tileEvents} />

        {showFences &&
          geofences
            .filter((fence) => fence.active)
            .map((fence) => (
              <Circle
                key={fence.id}
                center={[fence.latitude, fence.longitude]}
                radius={fence.radiusMeters}
                pathOptions={{ color: BRAND_GREEN, fillOpacity: 0.08, weight: 1.5, dashArray: '4 4' }}
              >
                <Popup>
                  <div className={styles.popup}>
                    <span className={styles.popupTitle}>{fence.name}</span>
                    <div className={styles.popupRow}>
                      <span>Raio</span>
                      <span className={styles.popupValue}>{formatDistance(fence.radiusMeters)}</span>
                    </div>
                  </div>
                </Popup>
              </Circle>
            ))}

        {/* Contorno escuro por baixo do trajeto, para o verde continuar
            legível também sobre o mapa claro. */}
        {trackLine.length > 1 && (
          <>
            <Polyline positions={trackLine} pathOptions={{ color: '#060907', weight: 6, opacity: 0.45 }} />
            <Polyline positions={trackLine} pathOptions={{ color: BRAND_GREEN, weight: 3.5, opacity: 0.95 }} />
          </>
        )}

        {track && track.length > 1 && (
          <>
            <Marker
              position={[track[0].latitude, track[0].longitude]}
              icon={endpointIcon('start')}
            >
              <Popup>Início — {formatDateTime(track[0].gpsTimestamp)}</Popup>
            </Marker>
            <Marker
              position={[track[track.length - 1].latitude, track[track.length - 1].longitude]}
              icon={endpointIcon('end')}
            >
              <Popup>Fim — {formatDateTime(track[track.length - 1].gpsTimestamp)}</Popup>
            </Marker>
          </>
        )}

        {highlight && (
          <Marker
            position={[highlight.latitude, highlight.longitude]}
            icon={vehicleIcon({
              ignition: highlight.acc,
              heading: highlight.heading,
              moving: highlight.speedKmh >= MOVING_SPEED_KMH,
              kind: selected?.kind ?? located[0]?.kind,
              selected: true,
              blocked: highlight.relayOn === true,
              online: true, // reprodução do histórico: sempre trata como "atual"
            })}
          >
            <Popup>
              <PositionPopup title="Reprodução" position={highlight} showAddress={false} />
            </Popup>
          </Marker>
        )}

        {!highlight && (
          <FleetMarkers
            vehicles={located}
            selectedId={selectedId}
            followId={following && !track ? (selected?.id ?? null) : null}
            onSelect={onSelect}
          />
        )}

        <MapFocus
          center={center}
          enabled={following && !track}
          fitTo={track && track.length > 1 ? trackLine : undefined}
          recenterKey={recenterKey}
          focusId={!highlight && selected ? selected.id : null}
          overview={overview}
          focusZoom={fleet ? FOCUS_ZOOM : undefined}
        />
        {onFollowingChange && <MapDragWatch onDrag={() => onFollowingChange(false)} />}
        <MapResize />
      </MapContainer>

      {located.length === 0 && !track && phase !== 'loading' && (
        <div className={styles.empty}>Nenhum veículo com posição conhecida ainda</div>
      )}

      <MapLoading
        loading={phase === 'loading'}
        percent={tiles.peak}
        title={track ? 'Carregando o trajeto' : 'Carregando o mapa'}
        detail={located.length > 1 && !track ? `posicionando ${vehiclesLabel(located.length)}` : ''}
      />
      {phase === 'offline' && (
        <div className={styles.offline} role="status">
          Sem o mapa de fundo agora · os veículos seguem ao vivo
        </div>
      )}

      {showControls && (
        <div className={styles.overlay}>
          <div className={styles.controlGroup}>
            <button
              type="button"
              className={`${styles.controlButton} ${following ? styles.controlActive : ''}`}
              onClick={toggleFollowing}
              title="Centralizar o mapa no veículo selecionado a cada nova posição"
            >
              {following ? 'Seguindo' : 'Seguir'}
            </button>
            <button
              type="button"
              className={`${styles.controlButton} ${showFences ? styles.controlActive : ''}`}
              onClick={() => setShowFences((value) => !value)}
            >
              Cercas
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

/**
 * A tela de carregamento por cima do mapa, até as imagens da área na tela
 * chegarem: a porcentagem e quantos veículos. Pronto rápido (mapa em cache),
 * nem aparece; pronto, some devagar.
 */
function MapLoading({
  loading,
  percent,
  title,
  detail,
}: {
  loading: boolean;
  percent: number;
  title: string;
  detail: string;
}) {
  const loadingRef = useRef(loading);
  loadingRef.current = loading;
  const [shown, setShown] = useState(false);
  const [gone, setGone] = useState(false);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      if (loadingRef.current) setShown(true);
    }, SHOW_AFTER_MS);
    return () => window.clearTimeout(timer);
  }, []);
  useEffect(() => {
    if (loading) return;
    const timer = window.setTimeout(() => setGone(true), FADE_MS);
    return () => window.clearTimeout(timer);
  }, [loading]);
  if (!shown || gone) return null;

  return (
    // map-loading: o app sobe o cartão para a parte que a lista não cobre.
    <div className={`map-loading ${styles.loading} ${loading ? '' : styles.loadingDone}`} role="status">
      <div className={styles.loadingCard}>
        <span className={styles.loadingRing} aria-hidden="true" />
        <span className={styles.loadingTitle}>{title}</span>
        <span
          className={styles.loadingBar}
          role="progressbar"
          aria-label={title}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={percent}
        >
          <span style={{ width: `${percent}%` }} />
        </span>
        <span className={styles.loadingDetail} aria-hidden="true">
          {percent}%{detail ? ` · ${detail}` : ''}
        </span>
      </div>
    </div>
  );
}

/**
 * Os veículos no mapa. Numa conta comum, todos; numa frota, só os da tela (e
 * o escolhido), com os próximos num grupo até o zoom de rua — ver
 * fleetLayout. O desenho acompanha o arrastar e o zoom.
 */
function FleetMarkers({
  vehicles,
  selectedId,
  followId,
  onSelect,
}: {
  vehicles: VehicleView[];
  selectedId?: string | null;
  followId: string | null;
  onSelect?: (vehicleId: string) => void;
}) {
  const map = useMap();
  const [view, setMapView] = useState(() => readView(map));
  useEffect(() => {
    let timer = 0;
    const update = () => {
      timer = 0;
      setMapView(readView(map));
    };
    const onMove = () => {
      if (!timer) timer = window.setTimeout(update, VIEW_THROTTLE_MS);
    };
    map.on('moveend zoomend', onMove);
    return () => {
      map.off('moveend zoomend', onMove);
      window.clearTimeout(timer);
    };
  }, [map]);

  const layout = useMemo(
    () => fleetLayout(vehicles, selectedId, view, (lat, lon) => map.project([lat, lon], view.zoom)),
    [vehicles, selectedId, view, map],
  );

  return (
    <>
      {layout.clusters.map((cluster) => (
        <ClusterMarker
          key={cluster.key}
          latitude={cluster.latitude}
          longitude={cluster.longitude}
          count={cluster.count}
          alert={cluster.alert}
          south={cluster.bounds.south}
          west={cluster.bounds.west}
          north={cluster.bounds.north}
          east={cluster.bounds.east}
        />
      ))}
      {layout.singles.map((vehicle) => (
        <VehicleMarker
          key={vehicle.id}
          vehicle={vehicle}
          selected={vehicle.id === selectedId}
          follow={vehicle.id === followId}
          animate={layout.animate}
          onSelect={onSelect}
        />
      ))}
    </>
  );
}

function readView(map: L.Map): MapView {
  const bounds = map.getBounds();
  return {
    zoom: map.getZoom(),
    bounds: { south: bounds.getSouth(), west: bounds.getWest(), north: bounds.getNorth(), east: bounds.getEast() },
  };
}

/** Um grupo de veículos próximos, com quantos são; tocar aproxima até eles. */
const ClusterMarker = memo(function ClusterMarker({
  latitude,
  longitude,
  count,
  alert,
  south,
  west,
  north,
  east,
}: {
  latitude: number;
  longitude: number;
  count: number;
  alert: boolean;
  south: number;
  west: number;
  north: number;
  east: number;
}) {
  const map = useMap();
  const icon = useMemo(() => clusterIcon(count, alert), [count, alert]);
  const position = useMemo<LatLngTuple>(() => [latitude, longitude], [latitude, longitude]);
  const handlers = useMemo(
    () => ({
      click: () =>
        map.fitBounds(
          [
            [south, west],
            [north, east],
          ],
          { padding: [48, 48], maxZoom: CLUSTER_MAX_ZOOM },
        ),
    }),
    [map, south, west, north, east],
  );
  return (
    <Marker position={position} icon={icon} eventHandlers={handlers} title="Veículos próximos: toque para aproximar" />
  );
});

/**
 * Um veículo no mapa. Com a frota inteira na tela, só se redesenha quando o
 * próprio veículo muda: ícone e posição novos a cada atualização fariam o
 * Leaflet refazer todos os marcadores. (O ícone não pode ser dividido entre
 * marcadores: o SVG dele é um elemento só, e o Leaflet o move de lugar.)
 */
const VehicleMarker = memo(function VehicleMarker({
  vehicle,
  selected,
  follow,
  animate,
  onSelect,
}: {
  vehicle: VehicleView;
  selected: boolean;
  /** O mapa segue este veículo: vai junto no deslize. */
  follow: boolean;
  /** Falso com muitos veículos soltos na tela (frota): pula para a posição
   * nova, sem deslizar, rastro nem ondas. O escolhido anima sempre. */
  animate: boolean;
  onSelect?: (vehicleId: string) => void;
}) {
  const position = vehicle.lastPosition as Position;
  const ignition = vehicle.state?.acc ?? position.acc ?? null;
  const heading = position.heading;
  const moving = position.speedKmh >= MOVING_SPEED_KMH;
  const blocked = vehicle.state?.relayOn === true;
  const online = vehicle.device?.status === 'ONLINE';
  const speedKmh = selected && moving ? position.speedKmh : null;
  const kind = vehicle.kind;
  const lively = animate || selected || follow;
  const calm = !lively;
  const icon = useMemo(
    () => vehicleIcon({ ignition, heading, moving, selected, blocked, online, speedKmh, kind, calm }),
    [ignition, heading, moving, selected, blocked, online, speedKmh, kind, calm],
  );
  // Só a posição inicial: daqui em diante quem move o marcador é o
  // useVehicleMotion (deslizando quando o carro anda).
  const [initial] = useState<LatLngTuple>(() => [position.latitude, position.longitude]);
  const markerRef = useRef<L.Marker | null>(null);
  useVehicleMotion(markerRef, position, moving && online && lively, follow);
  const handlers = useMemo(() => ({ click: () => onSelect?.(vehicle.id) }), [onSelect, vehicle.id]);

  return (
    <Marker ref={markerRef} position={initial} icon={icon} eventHandlers={handlers}>
      <Popup>
        <PositionPopup title={vehicle.name} position={position} />
      </Popup>
    </Marker>
  );
});

function PositionPopup({
  title,
  position,
  showAddress = true,
}: {
  title: string;
  position: Position;
  /** Falso no marcador de reprodução: evita uma consulta de endereço a
   * cada quadro enquanto o histórico é reproduzido/arrastado. */
  showAddress?: boolean;
}) {
  return (
    <div className={styles.popup}>
      <span className={styles.popupTitle}>{title}</span>
      {position.relayOn && <span className={styles.popupBlocked}>🔒 Motor bloqueado</span>}
      <div className={styles.popupRow}>
        <span>Velocidade</span>
        <span className={styles.popupValue}>{formatSpeed(position.speedKmh)}</span>
      </div>
      <div className={styles.popupRow}>
        <span>Direção</span>
        <span className={styles.popupValue}>{formatHeading(position.heading)}</span>
      </div>
      <div className={styles.popupRow}>
        <span>Ignição</span>
        <span className={styles.popupValue}>
          {position.acc === null ? '—' : position.acc ? 'ligada' : 'desligada'}
        </span>
      </div>
      <div className={styles.popupRow}>
        <span>Local</span>
        <span className={styles.popupValue}>
          {showAddress ? (
            <Address lat={position.latitude} lon={position.longitude} />
          ) : (
            formatCoordinates(position.latitude, position.longitude)
          )}
        </span>
      </div>
      <div className={styles.popupRow}>
        <span>Data do GPS</span>
        <span className={styles.popupValue}>{formatDateTime(position.gpsTimestamp)}</span>
      </div>
    </div>
  );
}

/**
 * O Leaflet só percebe mudança de tamanho da janela. Quando só o container
 * muda (painel lateral, lista arrastável do app), ele precisa ser avisado; o
 * invalidateSize mantém o centro, então o veículo continua no meio.
 */
function MapResize() {
  const map = useMap();
  useEffect(() => {
    if (typeof ResizeObserver === 'undefined') return;
    let frame = 0;
    const observer = new ResizeObserver(() => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => map.invalidateSize());
    });
    observer.observe(map.getContainer());
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
    };
  }, [map]);
  return null;
}

/**
 * Centraliza o mapa sem recriar o container a cada atualização. Seguindo um
 * veículo (focusId), centraliza ao escolher, ao ligar o seguir e no
 * recentralizar; as posições novas dele quem acompanha é o próprio marcador,
 * junto com o deslize (useVehicleMotion).
 */
function MapFocus({
  center,
  enabled,
  fitTo,
  recenterKey,
  focusId,
  overview,
  focusZoom,
}: {
  center: LatLngTuple;
  enabled: boolean;
  fitTo?: LatLngExpression[];
  recenterKey?: number;
  focusId: string | null;
  /** A frota inteira, quando nenhum veículo está escolhido: o mapa mostra
   * todos ao abrir (e no recentralizar), sem seguir nenhum. */
  overview?: LatLngTuple[] | null;
  /** O zoom mínimo ao escolher um veículo (numa frota, aproxima nele). */
  focusZoom?: number;
}) {
  const map = useMap();
  const focused = useRef('');
  const overviewed = useRef<string | null>(null);
  const showingAll = Boolean(overview);

  useEffect(() => {
    if (!fitTo || fitTo.length < 2) return;
    map.fitBounds(L.latLngBounds(fitTo), { padding: [40, 40] });
  }, [map, fitTo, recenterKey]);

  useEffect(() => {
    if (!overview || overview.length === 0) {
      overviewed.current = null;
      return;
    }
    const key = String(recenterKey ?? '');
    if (overviewed.current === key) return;
    overviewed.current = key;
    map.invalidateSize({ pan: false });
    map.fitBounds(L.latLngBounds(overview), { padding: [40, 40], maxZoom: FOCUS_ZOOM });
  }, [map, overview, recenterKey]);

  useEffect(() => {
    if (!enabled || fitTo || showingAll) {
      focused.current = '';
      return;
    }
    const key = `${focusId}|${recenterKey}`;
    if (focusId && focused.current === key) return;
    focused.current = key;
    // O mapa pode ter mudado de tamanho junto com a escolha (no painel, a
    // barra do veículo aparece embaixo): mede antes, senão centraliza no
    // tamanho antigo e o veículo fica fora do meio.
    map.invalidateSize({ pan: false });
    map.setView(center, Math.max(map.getZoom(), focusZoom ?? 0), { animate: true });
  }, [map, center[0], center[1], enabled, fitTo, recenterKey, focusId, showingAll, focusZoom]);

  return null;
}

/** Arrastar o mapa com o dedo (não o setView do código) para de seguir. */
function MapDragWatch({ onDrag }: { onDrag: () => void }) {
  const map = useMap();
  const callback = useRef(onDrag);
  callback.current = onDrag;
  useEffect(() => {
    const handler = () => callback.current();
    map.on('dragstart', handler);
    return () => {
      map.off('dragstart', handler);
    };
  }, [map]);
  return null;
}

import type { VehicleView } from '@/types';

/**
 * Acima disto o mapa trata a conta como frota: abre mostrando todos os
 * veículos, junta os próximos num grupo e desenha só o que está na tela. Até
 * aqui, nada muda: cada veículo no seu lugar, deslizando.
 */
export const FLEET_SIZE = 30;

/** O lado da célula, em pixels da tela, dentro da qual os veículos viram um grupo. */
export const CLUSTER_CELL_PX = 60;

/** Deste zoom em diante (nível de rua), cada veículo aparece no seu lugar. */
export const CLUSTER_MAX_ZOOM = 16;

/**
 * Com mais veículos soltos na tela que isso, eles pulam para a posição nova,
 * sem deslizar, sem rastro e sem as ondas: animar centenas de uma vez trava o
 * navegador (medido: 1000 veículos, metade andando, o mapa a 2–11 quadros por
 * segundo). O veículo seguido sempre desliza.
 */
export const ANIMATE_MAX = 40;

/** Quanto o mapa desenha além da borda da tela, em fração do tamanho dela. */
const VIEW_PADDING = 0.25;

export interface Box {
  south: number;
  west: number;
  north: number;
  east: number;
}

export interface MapView {
  zoom: number;
  bounds: Box;
}

export interface VehicleCluster {
  key: string;
  latitude: number;
  longitude: number;
  count: number;
  /** Onde estão os veículos do grupo (clicar aproxima até aqui). */
  bounds: Box;
  /** Algum veículo do grupo em modo roubo. */
  alert: boolean;
}

export interface FleetLayout {
  /** Os veículos desenhados um a um. */
  singles: VehicleView[];
  clusters: VehicleCluster[];
  /** Se os soltos deslizam (e deixam rastro) ou pulam para a posição nova. */
  animate: boolean;
}

type Located = VehicleView & { lastPosition: NonNullable<VehicleView['lastPosition']> };

/**
 * O que o mapa desenha: numa conta comum, todos os veículos; numa frota, os
 * que estão na tela (e o escolhido, sempre), com os próximos agrupados até o
 * zoom de rua. `project` leva latitude e longitude aos pixels do zoom atual.
 */
export function fleetLayout(
  vehicles: VehicleView[],
  selectedId: string | null | undefined,
  view: MapView,
  project: (latitude: number, longitude: number) => { x: number; y: number },
): FleetLayout {
  const located = vehicles.filter((v): v is Located => v.lastPosition !== null);
  if (located.length <= FLEET_SIZE) return { singles: located, clusters: [], animate: true };

  const area = padded(view.bounds);
  const singles: VehicleView[] = [];
  const cells = new Map<string, Located[]>();
  for (const vehicle of located) {
    const { latitude, longitude } = vehicle.lastPosition;
    if (vehicle.id === selectedId) {
      singles.push(vehicle);
      continue;
    }
    if (latitude < area.south || latitude > area.north || longitude < area.west || longitude > area.east) continue;
    if (view.zoom >= CLUSTER_MAX_ZOOM) {
      singles.push(vehicle);
      continue;
    }
    const { x, y } = project(latitude, longitude);
    const key = `${Math.floor(x / CLUSTER_CELL_PX)}:${Math.floor(y / CLUSTER_CELL_PX)}`;
    const cell = cells.get(key);
    if (cell) cell.push(vehicle);
    else cells.set(key, [vehicle]);
  }

  const clusters: VehicleCluster[] = [];
  for (const [key, members] of cells) {
    if (members.length === 1) {
      singles.push(members[0]);
      continue;
    }
    clusters.push(clusterOf(key, members));
  }
  return { singles, clusters, animate: singles.length <= ANIMATE_MAX };
}

function clusterOf(key: string, members: Located[]): VehicleCluster {
  let lat = 0;
  let lon = 0;
  const bounds: Box = { south: 90, west: 180, north: -90, east: -180 };
  let alert = false;
  for (const vehicle of members) {
    const { latitude, longitude } = vehicle.lastPosition;
    lat += latitude;
    lon += longitude;
    bounds.south = Math.min(bounds.south, latitude);
    bounds.north = Math.max(bounds.north, latitude);
    bounds.west = Math.min(bounds.west, longitude);
    bounds.east = Math.max(bounds.east, longitude);
    alert ||= Boolean(vehicle.theft);
  }
  return { key, latitude: lat / members.length, longitude: lon / members.length, count: members.length, bounds, alert };
}

function padded({ south, west, north, east }: Box): Box {
  const lat = (north - south) * VIEW_PADDING;
  const lon = (east - west) * VIEW_PADDING;
  return { south: south - lat, west: west - lon, north: north + lat, east: east + lon };
}

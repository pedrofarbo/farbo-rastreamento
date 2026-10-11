/**
 * O carregamento do mapa: acompanha as imagens do mapa de fundo (tiles) da
 * área na tela até a última chegar — o evento "load" do Leaflet —, para a
 * tela de carregamento sair só com o mapa pronto, e não antes nem presa.
 */

/** Pronto antes disso (mapa já em cache): a tela de carregamento nem aparece. */
export const SHOW_AFTER_MS = 200;

/** Sem resposta do mapa de fundo até aqui, desiste: os veículos aparecem mesmo assim. */
export const GIVE_UP_MS = 12_000;

export interface TileProgress {
  started: number;
  loaded: number;
  failed: number;
  /** Pedidas e descartadas antes de chegar (o mapa mudou de lugar). */
  aborted: number;
  /** Todas as da tela chegaram (ou o tempo acabou). */
  settled: boolean;
  /** A maior porcentagem já mostrada: a barra nunca volta para trás. */
  peak: number;
}

export type TileEvent = 'start' | 'loaded' | 'failed' | 'aborted' | 'settled' | 'timeout';

export const NO_TILES: TileProgress = { started: 0, loaded: 0, failed: 0, aborted: 0, settled: false, peak: 0 };

export function trackTiles(progress: TileProgress, event: TileEvent): TileProgress {
  const next = { ...progress };
  switch (event) {
    case 'start':
      next.started += 1;
      break;
    case 'loaded':
      next.loaded += 1;
      break;
    case 'failed':
      next.failed += 1;
      break;
    case 'aborted':
      next.aborted += 1;
      break;
    case 'settled':
    case 'timeout':
      next.settled = true;
      break;
  }
  next.peak = Math.max(progress.peak, rawPercent(next));
  return next;
}

function rawPercent({ started, loaded, failed, aborted, settled }: TileProgress): number {
  if (settled) return 100;
  if (started === 0) return 0;
  // Até chegar o "load", no máximo 95%: falta a última peça.
  return Math.min(95, Math.round((100 * (loaded + failed + aborted)) / started));
}

export type MapPhase = 'loading' | 'ready' | 'offline';

/**
 * Carregando; pronto; ou sem o mapa de fundo — nenhuma imagem chegou, só
 * erros (sem internet, bloqueio): os veículos continuam, e um aviso explica.
 */
export function mapPhase(progress: TileProgress): MapPhase {
  if (!progress.settled) return 'loading';
  return progress.loaded === 0 && progress.failed > 0 ? 'offline' : 'ready';
}

/** "1.000 veículos", "1 veículo"; nada sem veículos. */
export function vehiclesLabel(count: number): string {
  if (count <= 0) return '';
  return count === 1 ? '1 veículo' : `${count.toLocaleString('pt-BR')} veículos`;
}

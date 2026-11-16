import { useCallback, useEffect, useRef, useState } from 'react';

import { freshFix } from '@/services/inspectionCamera';
import type { PositionFix } from '@/services/inspectionCamera';

/** waiting: pedindo; on: chegando; denied: a pessoa bloqueou; unavailable: o aparelho não deu. */
export type PositionState = 'waiting' | 'on' | 'denied' | 'unavailable';

/**
 * A posição do celular enquanto a vistoria está aberta (cada foto leva a
 * mais recente): o navegador pede a permissão uma vez e vai atualizando.
 */
export function usePositionWatch(enabled: boolean): { state: PositionState; accuracy: number | null; current: () => PositionFix | null } {
  const [state, setState] = useState<PositionState>('waiting');
  const [accuracy, setAccuracy] = useState<number | null>(null);
  const fix = useRef<PositionFix | null>(null);

  useEffect(() => {
    if (!enabled) return;
    if (typeof navigator === 'undefined' || !navigator.geolocation) {
      setState('unavailable');
      return;
    }
    const id = navigator.geolocation.watchPosition(
      (p) => {
        const acc = Number.isFinite(p.coords.accuracy) ? p.coords.accuracy : null;
        fix.current = { latitude: p.coords.latitude, longitude: p.coords.longitude, accuracy: acc, at: Date.now() };
        setAccuracy(acc);
        setState('on');
      },
      (err) => {
        if (err.code === err.PERMISSION_DENIED) setState('denied');
        else if (!fix.current) setState('unavailable');
      },
      { enableHighAccuracy: true, maximumAge: 30_000, timeout: 30_000 },
    );
    return () => navigator.geolocation.clearWatch(id);
  }, [enabled]);

  const current = useCallback(() => freshFix(fix.current), []);
  return { state, accuracy, current };
}

/** A linha do estado da localização, para a tela. */
export function positionText(state: PositionState, accuracy: number | null): string {
  switch (state) {
    case 'on':
      return `📍 Localização ligada${accuracy !== null ? ` (precisão de ${Math.round(accuracy)} m)` : ''}.`;
    case 'denied':
      return '📍 A localização está bloqueada: as fotos vão sem o lugar, e quem pediu a vistoria vai ver isso. Para ligar, permita a localização para este site nas configurações do navegador e abra o link de novo.';
    case 'unavailable':
      return '📍 O celular ainda não achou a localização. Ligue o GPS (localização) do aparelho.';
    default:
      return '📍 Permita a localização quando o navegador perguntar: ela vai junto com cada foto.';
  }
}

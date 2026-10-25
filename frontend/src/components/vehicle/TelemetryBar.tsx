import { formatRelative, formatSpeed, formatVoltage } from '@/services/format';
import type { DeviceState, Position } from '@/types';

import styles from './TelemetryBar.module.css';

interface TelemetryBarProps {
  position: Position | null;
  state: DeviceState | null;
  lastSeenAt?: string | null;
}

/**
 * Faixa de telemetria do rodapé.
 *
 * Mostra apenas o que o aparelho realmente enviou: campo ausente aparece como
 * "—" em vez de zero, porque zero é um valor e ausência não é (§34).
 */
export function TelemetryBar({ position, state, lastSeenAt }: TelemetryBarProps) {
  const acc = state?.acc ?? position?.acc ?? null;
  const battery = state?.batteryPercent ?? position?.batteryPercent ?? null;
  const voltage = state?.batteryVoltage ?? position?.batteryVoltage ?? null;
  const gsm = state?.gsmLevel ?? position?.gsmLevel ?? null;
  const satellites = position?.satellites ?? null;
  const gpsValid = position?.gpsValid ?? state?.gpsValid ?? null;
  const relayOn = state?.relayOn ?? position?.relayOn ?? null;

  return (
    <div className={styles.bar}>
      <Cell label="Velocidade" value={position ? formatSpeed(position.speedKmh) : '—'} />

      <Cell
        label="GPS"
        small
        tone={gpsValid === null ? 'muted' : gpsValid ? 'success' : 'warning'}
        value={
          gpsValid === null
            ? '—'
            : gpsValid
              ? `Fixo${satellites !== null ? ` · ${satellites} sat` : ''}`
              : 'Sem fixo'
        }
      />

      <Cell
        label="Ignição"
        small
        tone={acc === null ? 'muted' : acc ? 'success' : undefined}
        value={acc === null ? '—' : acc ? 'Ligada' : 'Desligada'}
      />

      <Cell label="Rede" small value={gsm === null ? '—' : `${gsm}/4`} />

      {/* A voltagem é a da bateria do veículo; sem ela, o nível da bateria
          interna do rastreador. */}
      <Cell
        label={voltage !== null ? 'Voltagem' : 'Bateria interna'}
        small
        value={
          voltage !== null
            ? formatVoltage(voltage)
            : battery !== null
              ? `${battery}%`
              : '—'
        }
      />

      <Cell
        label="Motor"
        small
        tone={relayOn === null ? 'muted' : relayOn ? 'danger' : 'success'}
        value={relayOn === null ? '—' : relayOn ? 'Bloqueado' : 'Liberado'}
      />

      <Cell label="Última comunicação" small value={formatRelative(lastSeenAt)} />
    </div>
  );
}

function Cell({
  label,
  value,
  small = false,
  tone,
}: {
  label: string;
  value: string;
  small?: boolean;
  tone?: 'danger' | 'success' | 'warning' | 'muted';
}) {
  const classes = [styles.value, small ? styles.small : '', tone ? styles[tone] : '']
    .filter(Boolean)
    .join(' ');

  return (
    <div className={styles.cell}>
      <span className={styles.label}>{label}</span>
      <span className={classes}>{value}</span>
    </div>
  );
}

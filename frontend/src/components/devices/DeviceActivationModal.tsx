import { useState } from 'react';

import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { SmsSetupPanel } from '@/components/fulfillment/SmsSetupPanel';
import { isDeviceActivated } from '@/services/deviceBulk';
import { formatDeviceStatus, formatPhoneInput, formatRelative } from '@/services/format';
import type { Device, Vehicle } from '@/types';

interface DeviceActivationModalProps {
  device: Device;
  vehicle?: Vehicle | null;
  onClose: () => void;
  onEditDevice?: () => void;
}

export function DeviceActivationModal({
  device,
  vehicle,
  onClose,
  onEditDevice,
}: DeviceActivationModalProps) {
  const activated = isDeviceActivated(device);
  const [confirmReactivate, setConfirmReactivate] = useState(false);

  return (
    <Modal
      open
      wide
      title={activated ? `Reativar rastreador ${device.imei}` : `Ativar rastreador ${device.imei}`}
      onClose={onClose}
      footer={
        <Button variant="ghost" onClick={onClose}>
          Fechar
        </Button>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <div
          style={{
            display: 'flex',
            flexWrap: 'wrap',
            gap: 'var(--space-3)',
            padding: 'var(--space-3)',
            borderRadius: 'var(--radius-md)',
            background: 'var(--surface-sunken)',
            fontSize: 'var(--text-sm)',
          }}
        >
          <div>
            <span style={{ color: 'var(--text-muted)' }}>Modelo: </span>
            <strong>{device.model || 'J16'}</strong>
          </div>
          <div>
            <span style={{ color: 'var(--text-muted)' }}>Número do chip: </span>
            <strong style={{ fontFamily: 'var(--font-mono)' }}>
              {device.phoneNumber ? formatPhoneInput(device.phoneNumber) : 'Não informado'}
            </strong>
          </div>
          {vehicle && (
            <div>
              <span style={{ color: 'var(--text-muted)' }}>Veículo: </span>
              <strong>
                {vehicle.name} {vehicle.plate ? `(${vehicle.plate})` : ''}
              </strong>
            </div>
          )}
          <div>
            <span style={{ color: 'var(--text-muted)' }}>Situação: </span>
            <strong>{formatDeviceStatus(device.status)}</strong>
          </div>
          {device.lastSeenAt && (
            <div>
              <span style={{ color: 'var(--text-muted)' }}>Último contato: </span>
              <strong>{formatRelative(device.lastSeenAt)}</strong>
            </div>
          )}
        </div>

        {activated && (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: 'var(--space-2)',
              padding: 'var(--space-3)',
              borderRadius: 'var(--radius-md)',
              background: 'var(--warning-soft)',
              border: '1px solid var(--warning)',
              fontSize: 'var(--text-xs)',
              lineHeight: 1.5,
            }}
          >
            <div style={{ fontWeight: 600, color: 'var(--warning)', fontSize: 'var(--text-sm)' }}>
              ⚠️ Rastreador já ativado anteriormente
            </div>
            <div>
              Este aparelho já possui comunicação ativa com os servidores da Farbo (último sinal{' '}
              <strong>{formatRelative(device.lastSeenAt)}</strong>). Para evitar consumo desnecessário
              de SMS e redefinição de parâmetros, confirme abaixo caso queira realmente reenviar a
              configuração (por exemplo, após troca de chip).
            </div>
            <label
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 'var(--space-2)',
                marginTop: 'var(--space-1)',
                cursor: 'pointer',
                fontWeight: 600,
                color: 'var(--text-primary)',
              }}
            >
              <input
                type="checkbox"
                checked={confirmReactivate}
                onChange={(e) => setConfirmReactivate(e.target.checked)}
              />
              Confirmar reativação por SMS deste aparelho já ativo
            </label>
          </div>
        )}

        <SmsSetupPanel
          deviceId={device.id}
          buttonLabel={activated ? 'Reativar por SMS' : 'Ativar por SMS'}
          force={activated ? confirmReactivate : false}
          disabled={activated && !confirmReactivate}
          onEditDevice={() => {
            onClose();
            onEditDevice?.();
          }}
        />
      </div>
    </Modal>
  );
}

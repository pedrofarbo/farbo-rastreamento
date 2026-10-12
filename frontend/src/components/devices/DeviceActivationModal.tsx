import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { SmsSetupPanel } from '@/components/fulfillment/SmsSetupPanel';
import { formatDeviceStatus, formatPhoneInput } from '@/services/format';
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
  return (
    <Modal
      open
      wide
      title={`Ativar rastreador ${device.imei}`}
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
        </div>

        <SmsSetupPanel
          deviceId={device.id}
          buttonLabel="Ativar por SMS"
          onEditDevice={() => {
            onClose();
            onEditDevice?.();
          }}
        />
      </div>
    </Modal>
  );
}

import type { ReactNode } from 'react';

import { ScanButton } from '@/components/scan/ScanButton';
import { SearchSelectField } from '@/components/ui/SearchSelectField';
import { deviceSearchFields } from '@/services/search';
import type { Device } from '@/types';

/** O rastreador na lista: o IMEI e o modelo. */
export function deviceOptionLabel(d: Device): string {
  return d.model ? `${d.imei} · ${d.model}` : d.imei;
}

/**
 * A escolha do rastreador com a busca por IMEI, chip, linha ou modelo; o
 * botão na busca lê o IMEI da etiqueta e o IMEI inteiro já escolhe.
 * unavailable: o motivo de um rastreador não poder ser escolhido (fica na
 * lista, desabilitado).
 */
export function DeviceSelectField({
  devices,
  label = 'Rastreador',
  optionLabel = deviceOptionLabel,
  unavailable,
  ...rest
}: {
  devices: Device[];
  label?: string;
  hideLabel?: boolean;
  hint?: ReactNode;
  error?: string;
  value: string;
  onChange: (deviceId: string) => void;
  emptyOption: string;
  optionLabel?: (d: Device) => string;
  unavailable?: (d: Device) => string;
  autoPick?: boolean;
  disabled?: boolean;
}) {
  return (
    <SearchSelectField
      label={label}
      searchLabel={rest.hideLabel ? `Buscar: ${label}` : 'Buscar rastreador'}
      searchPlaceholder="Buscar IMEI, chip ou modelo"
      searchAction={(type) => <ScanButton kind="imei" onRead={type} />}
      options={devices.map((d) => {
        const reason = unavailable?.(d) ?? '';
        return {
          value: d.id,
          label: reason ? `${optionLabel(d)} (${reason})` : optionLabel(d),
          search: deviceSearchFields(d),
          codes: [d.imei],
          disabled: Boolean(reason),
        };
      })}
      {...rest}
    />
  );
}

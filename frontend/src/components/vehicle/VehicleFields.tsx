import type { VehicleInput } from '@/api/resources';
import { SelectField, TextField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import type { Device, VehicleKind } from '@/types';

import styles from '@/pages/Page.module.css';
import local from './VehicleFields.module.css';

export const EMPTY_VEHICLE: VehicleInput = { name: '', kind: 'CAR', plate: '', brand: '', model: '', year: null, color: '' };

const KINDS: { id: VehicleKind; label: string }[] = [
  { id: 'CAR', label: '🚗 Carro' },
  { id: 'MOTORCYCLE', label: '🏍️ Moto' },
];

/**
 * Campos do cadastro de veículo, os mesmos para o cliente e para a central.
 * Com `devices`, mostra também a escolha do rastreador (só a central vincula).
 */
export function VehicleFields({
  value,
  onChange,
  devices,
  autoFocus = false,
}: {
  value: VehicleInput;
  onChange: (value: VehicleInput) => void;
  devices?: Device[];
  autoFocus?: boolean;
}) {
  const kind = value.kind ?? 'CAR';
  return (
    <>
      <div className={fieldStyles.field}>
        <span className={fieldStyles.label} id="vehicle-kind">
          Tipo
        </span>
        <div className={local.kind} role="radiogroup" aria-labelledby="vehicle-kind">
          {KINDS.map((k) => (
            <button
              key={k.id}
              type="button"
              role="radio"
              aria-checked={kind === k.id}
              className={`${local.kindOption} ${kind === k.id ? local.kindActive : ''}`}
              onClick={() => onChange({ ...value, kind: k.id })}
            >
              {k.label}
            </button>
          ))}
        </div>
      </div>
      <TextField
        label="Apelido do veículo"
        placeholder="Ex.: Moto do trabalho"
        required
        autoFocus={autoFocus}
        value={value.name}
        onChange={(event) => onChange({ ...value, name: event.target.value })}
      />
      <div className={styles.formRow}>
        <TextField
          label="Placa"
          placeholder="ABC1D23"
          value={value.plate ?? ''}
          onChange={(event) => onChange({ ...value, plate: event.target.value.toUpperCase() })}
        />
        <TextField
          label="Cor"
          value={value.color ?? ''}
          onChange={(event) => onChange({ ...value, color: event.target.value })}
        />
      </div>
      <div className={styles.formRow}>
        <TextField
          label="Marca"
          placeholder="Honda"
          value={value.brand ?? ''}
          onChange={(event) => onChange({ ...value, brand: event.target.value })}
        />
        <TextField
          label="Modelo"
          placeholder="CB 500"
          value={value.model ?? ''}
          onChange={(event) => onChange({ ...value, model: event.target.value })}
        />
        <TextField
          label="Ano"
          type="number"
          value={value.year ?? ''}
          onChange={(event) => onChange({ ...value, year: event.target.value ? Number(event.target.value) : null })}
        />
      </div>
      {devices && (
        <SelectField
          label="Rastreador"
          hint="Opcional: vincule agora se o aparelho já foi instalado."
          value={value.deviceId ?? ''}
          onChange={(event) => onChange({ ...value, deviceId: event.target.value || null })}
        >
          <option value="">aguardando instalação</option>
          {devices.map((device) => (
            <option key={device.id} value={device.id}>
              {device.imei} {device.model ? `· ${device.model}` : ''}
            </option>
          ))}
        </SelectField>
      )}
    </>
  );
}

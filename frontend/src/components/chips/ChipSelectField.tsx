import type { ReactNode } from 'react';

import { ScanButton } from '@/components/scan/ScanButton';
import { SearchSelectField } from '@/components/ui/SearchSelectField';
import { formatICCID, formatPhoneInput } from '@/services/format';
import { chipSearchFields } from '@/services/search';
import type { Chip } from '@/types';

/**
 * A escolha do chip do estoque com a busca por ICCID, linha ou operadora; o
 * botão na busca lê o ICCID da etiqueta e o ICCID inteiro já escolhe.
 */
export function ChipSelectField({
  chips,
  label = 'Chip',
  ...rest
}: {
  chips: Chip[];
  label?: string;
  hint?: ReactNode;
  value: string;
  onChange: (chipId: string) => void;
  emptyOption: string;
  disabled?: boolean;
}) {
  return (
    <SearchSelectField
      label={label}
      searchLabel="Buscar chip"
      searchPlaceholder="Buscar ICCID, linha ou operadora"
      searchAction={(type) => <ScanButton kind="iccid" onRead={type} />}
      options={chips.map((c) => ({
        value: c.id,
        label: [formatICCID(c.iccid), c.phoneNumber && formatPhoneInput(c.phoneNumber), c.carrier].filter(Boolean).join(' · '),
        search: chipSearchFields(c),
        codes: [c.iccid],
      }))}
      {...rest}
    />
  );
}

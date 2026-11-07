import type { ReactNode } from 'react';
import { useId, useState } from 'react';

import { isExactCode, matchesSearch } from '@/services/search';

import styles from './Field.module.css';

export interface SearchOption {
  value: string;
  label: string;
  /** Onde a busca procura, além do texto da opção. */
  search?: (string | null | undefined)[];
  /** Os códigos que, digitados ou lidos inteiros, já escolhem a opção (o IMEI, o ICCID). */
  codes?: (string | null | undefined)[];
  disabled?: boolean;
}

/**
 * Uma lista de escolha com a busca em cima: o que é digitado filtra as
 * opções (a escolhida fica sempre); o código inteiro (digitado ou lido pela
 * câmera, no searchAction) já escolhe. A opção em branco (emptyOption)
 * aparece sempre.
 */
export function SearchSelectField({
  label,
  hideLabel,
  hint,
  error,
  value,
  onChange,
  options,
  emptyOption,
  searchLabel,
  searchPlaceholder,
  searchAction,
  autoPick = true,
  disabled,
}: {
  label: string;
  /** Só para o leitor de tela (a lista numa tabela, por exemplo). */
  hideLabel?: boolean;
  hint?: ReactNode;
  error?: string;
  value: string;
  onChange: (value: string) => void;
  options: SearchOption[];
  emptyOption: string;
  searchLabel: string;
  searchPlaceholder: string;
  /** O botão dentro da busca (ler pela câmera): recebe o que põe o texto na busca. */
  searchAction?: (type: (text: string) => void) => ReactNode;
  /** false: o código inteiro só filtra (quando escolher já salva). */
  autoPick?: boolean;
  disabled?: boolean;
}) {
  const id = useId();
  const [search, setSearch] = useState('');
  const term = search.trim();
  const matches = (o: SearchOption) => matchesSearch(term, [o.label, ...(o.search ?? [])]);
  const shown = term ? options.filter((o) => o.value === value || matches(o)) : options;
  const found = term ? options.filter(matches).length : 0;

  const type = (text: string) => {
    setSearch(text);
    if (!autoPick) return;
    const exact = options.filter((o) => !o.disabled && isExactCode(text, o.codes ?? []));
    if (exact.length === 1 && exact[0].value !== value) onChange(exact[0].value);
  };

  const input = (
    <input
      type="search"
      className={styles.input}
      aria-label={searchLabel}
      placeholder={searchPlaceholder}
      autoComplete="off"
      value={search}
      disabled={disabled}
      onChange={(e) => type(e.target.value)}
    />
  );

  return (
    <div className={styles.field}>
      <label className={hideLabel ? 'visually-hidden' : styles.label} htmlFor={id}>
        {label}
      </label>
      {options.length > 0 &&
        (searchAction ? (
          <div className={styles.adorned}>
            {input}
            {searchAction(type)}
          </div>
        ) : (
          input
        ))}
      <select id={id} className={styles.select} value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
        <option value="">{emptyOption}</option>
        {shown.map((o) => (
          <option key={o.value} value={o.value} disabled={o.disabled}>
            {o.label}
          </option>
        ))}
      </select>
      {term && (
        <span className={styles.hint} aria-live="polite">
          {found === 0 ? `Nenhum encontrado com “${term}”` : found === 1 ? '1 encontrado' : `${found} encontrados`}
        </span>
      )}
      {hint && <span className={styles.hint}>{hint}</span>}
      {error && <span className={styles.error}>{error}</span>}
    </div>
  );
}

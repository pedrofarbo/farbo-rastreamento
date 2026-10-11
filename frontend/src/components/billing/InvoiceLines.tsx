import { useMemo, useState } from 'react';

import { formatMoney } from '@/services/format';

import styles from './InvoiceLines.module.css';

export interface InvoiceLine {
  key: string;
  description: string;
  amountCents: number;
}

/** Quantas aparecem antes de abrir a lista. */
export const LINES_PREVIEW = 5;
/** Aberta, quantas por vez (numa frota, são centenas). */
export const LINES_PAGE = 50;
/** Com mais que isso, a lista aberta ganha a busca. */
const SEARCH_FROM = 20;

/**
 * As faturas de um grupo (a fatura do mês de uma frota, uma por veículo): as
 * primeiras e "ver todas", que abre a lista com busca e de 50 em 50, e o total.
 */
export function InvoiceLines({
  lines,
  totalCents,
  className,
  totalClassName,
  money = formatMoney,
}: {
  lines: InvoiceLine[];
  totalCents?: number;
  /** A lista (ul) e a linha do total, no estilo de quem usa. */
  className?: string;
  totalClassName?: string;
  money?: (cents: number) => string;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [limit, setLimit] = useState(LINES_PAGE);

  const found = useMemo(() => {
    const term = search.trim().toLowerCase();
    return term ? lines.filter((l) => l.description.toLowerCase().includes(term)) : lines;
  }, [lines, search]);
  const visible = open ? found.slice(0, limit) : lines.slice(0, LINES_PREVIEW);
  const hidden = lines.length - LINES_PREVIEW;

  return (
    <>
      {open && lines.length > SEARCH_FROM && (
        <input
          className={styles.search}
          type="search"
          placeholder="Buscar pelo veículo ou placa"
          aria-label="Buscar fatura"
          value={search}
          onChange={(event) => {
            setSearch(event.target.value);
            setLimit(LINES_PAGE);
          }}
        />
      )}
      <ul className={className}>
        {visible.map((line) => (
          <li key={line.key}>
            <span>{line.description}</span>
            <strong>{money(line.amountCents)}</strong>
          </li>
        ))}
        {open && search && found.length === 0 && <li className={styles.note}>Nenhuma fatura com “{search.trim()}”.</li>}
        {!open && hidden > 0 && (
          <li className={styles.note}>
            <button type="button" className={styles.more} onClick={() => setOpen(true)}>
              e mais {hidden} {hidden === 1 ? 'fatura' : 'faturas'} · ver todas as {lines.length}
            </button>
          </li>
        )}
        {open && found.length > limit && (
          <li className={styles.note}>
            <button type="button" className={styles.more} onClick={() => setLimit((n) => n + LINES_PAGE)}>
              {found.length - limit > LINES_PAGE
                ? `Mostrar mais ${LINES_PAGE} (faltam ${found.length - limit})`
                : `Mostrar as ${found.length - limit} restantes`}
            </button>
          </li>
        )}
        {open && hidden > 0 && (
          <li className={styles.note}>
            <button
              type="button"
              className={styles.more}
              onClick={() => {
                setOpen(false);
                setSearch('');
                setLimit(LINES_PAGE);
              }}
            >
              Recolher a lista
            </button>
          </li>
        )}
        {totalCents !== undefined && (
          <li className={totalClassName}>
            <span>Total</span>
            <strong>{money(totalCents)}</strong>
          </li>
        )}
      </ul>
    </>
  );
}

import { useId, useRef } from 'react';

import { PAGE_SIZES } from '@/hooks/usePagination';

import { Button } from './Button';
import styles from './Pager.module.css';

interface PagerProps {
  page: number;
  pageSize: number;
  total: number;
  onPage: (page: number) => void;
  onPageSize: (size: number) => void;
}

/**
 * O rodapé de uma listagem: o trecho mostrado, quantos itens por página e a
 * troca de página. Trocando de página, a tela rola até o começo do cartão da
 * lista (fora de um cartão, do bloco em que o rodapé está).
 */
export function Pager({ page, pageSize, total, onPage, onPageSize }: PagerProps) {
  const sizeId = useId();
  const nav = useRef<HTMLElement>(null);
  const go = (next: number) => {
    onPage(next);
    (nav.current?.closest('section') ?? nav.current?.parentElement)?.scrollIntoView?.({ block: 'start' });
  };
  const pages = Math.max(1, Math.ceil(total / pageSize));
  const current = Math.min(page, pages);
  const from = total === 0 ? 0 : (current - 1) * pageSize + 1;
  const to = Math.min(total, current * pageSize);
  return (
    <nav ref={nav} className={styles.pager} aria-label="Páginas">
      <div className={styles.info}>
        <span>
          {from}–{to} de {total.toLocaleString('pt-BR')}
        </span>
        <label className={styles.size} htmlFor={sizeId}>
          Itens por página
          <select id={sizeId} value={pageSize} onChange={(e) => onPageSize(Number(e.target.value))}>
            {PAGE_SIZES.map((size) => (
              <option key={size} value={size}>
                {size}
              </option>
            ))}
          </select>
        </label>
      </div>
      <div className={styles.buttons}>
        <Button size="small" variant="ghost" disabled={current <= 1} onClick={() => go(current - 1)}>
          Anterior
        </Button>
        <span>
          Página {current} de {pages}
        </span>
        <Button size="small" variant="ghost" disabled={current >= pages} onClick={() => go(current + 1)}>
          Próxima
        </Button>
      </div>
    </nav>
  );
}

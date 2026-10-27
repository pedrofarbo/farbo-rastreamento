import { useState } from 'react';

/** As quantidades por página que dá para escolher nas listagens. */
export const PAGE_SIZES = [10, 25, 50, 100];
const DEFAULT_PAGE_SIZE = 25;

const storageKey = (list: string) => `tracker.pageSize.${list}`;

function readPageSize(list: string): number {
  try {
    const value = Number(window.localStorage.getItem(storageKey(list)));
    return PAGE_SIZES.includes(value) ? value : DEFAULT_PAGE_SIZE;
  } catch {
    return DEFAULT_PAGE_SIZE;
  }
}

/**
 * Quantos itens por página nesta lista. A escolha fica no navegador de quem
 * usa, uma para cada lista.
 */
export function usePageSize(list: string): [number, (size: number) => void] {
  const [size, setSize] = useState(() => readPageSize(list));
  const choose = (next: number) => {
    setSize(next);
    try {
      window.localStorage.setItem(storageKey(list), String(next));
    } catch {
      /* armazenamento bloqueado: vale só nesta visita */
    }
  };
  return [size, choose];
}

/** A página em que fica o primeiro item da página atual com o novo tamanho. */
export function pageKeepingFirst(page: number, oldSize: number, newSize: number): number {
  return Math.floor(((page - 1) * oldSize) / newSize) + 1;
}

/**
 * A paginação de uma lista que já veio inteira do servidor. A página volta
 * para dentro do limite quando a lista encolhe (um filtro, uma exclusão).
 */
export function usePagedList<T>(all: T[], list: string) {
  const [pageSize, choosePageSize] = usePageSize(list);
  const [wanted, setPage] = useState(1);
  const pages = Math.max(1, Math.ceil(all.length / pageSize));
  const page = Math.min(wanted, pages);
  return {
    items: all.slice((page - 1) * pageSize, page * pageSize),
    page,
    pageSize,
    total: all.length,
    setPage,
    setPageSize: (size: number) => {
      setPage(pageKeepingFirst(page, pageSize, size));
      choosePageSize(size);
    },
  };
}

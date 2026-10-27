import { useEffect, useState } from 'react';

/**
 * Se o conteúdo passa da largura do elemento (e ele rola para o lado). O
 * primeiro elemento de dentro (a tabela) também é observado: mudar de página
 * muda a largura dela sem mudar a da moldura.
 */
export function useOverflowX<T extends HTMLElement>() {
  const [el, setEl] = useState<T | null>(null);
  const [overflowing, setOverflowing] = useState(false);
  useEffect(() => {
    if (!el || typeof ResizeObserver === 'undefined') return;
    const check = () => setOverflowing(el.scrollWidth > el.clientWidth + 1);
    const observer = new ResizeObserver(check);
    observer.observe(el);
    if (el.firstElementChild) observer.observe(el.firstElementChild);
    return () => observer.disconnect();
  }, [el]);
  return [setEl, overflowing] as const;
}

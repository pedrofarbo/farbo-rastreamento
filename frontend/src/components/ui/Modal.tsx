import { useEffect, useId, useRef } from 'react';
import type { ReactNode } from 'react';
import { createPortal } from 'react-dom';

import styles from './Modal.module.css';

interface ModalProps {
  open: boolean;
  title: ReactNode;
  icon?: ReactNode;
  wide?: boolean;
  onClose: () => void;
  footer?: ReactNode;
  children: ReactNode;
}

// Os diálogos abertos, do mais antigo ao mais novo: o Esc fecha só o de cima
// (um diálogo aberto de dentro de outro).
const openDialogs: symbol[] = [];

export function Modal({ open, title, icon, wide = false, onClose, footer, children }: ModalProps) {
  const dialogRef = useRef<HTMLDivElement>(null);
  // O título dá o nome do diálogo (leitores de tela).
  const titleId = useId();

  // Quem usa o Modal passa onClose como função nova a cada render. Se ela
  // entrasse nas dependências do efeito, cada tecla digitada num campo do
  // diálogo reexecutaria o efeito e devolveria o foco ao diálogo, engolindo o
  // resto do texto. Por isso ela fica numa ref e o efeito só roda ao abrir.
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  // Esc fecha; o foco vai para o diálogo para quem navega pelo teclado.
  useEffect(() => {
    if (!open) return;

    const me = Symbol('dialog');
    openDialogs.push(me);
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && openDialogs[openDialogs.length - 1] === me) onCloseRef.current();
    };
    document.addEventListener('keydown', onKeyDown);
    // Um campo com autoFocus dentro do diálogo já tem o foco: não tira dele.
    if (!dialogRef.current?.contains(document.activeElement)) {
      dialogRef.current?.focus();
    }

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    return () => {
      document.removeEventListener('keydown', onKeyDown);
      openDialogs.splice(openDialogs.indexOf(me), 1);
      document.body.style.overflow = previousOverflow;
    };
  }, [open]);

  if (!open) return null;

  return createPortal(
    <div
      className={styles.backdrop}
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        ref={dialogRef}
        className={`${styles.dialog} ${wide ? styles.wide : ''}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
      >
        <header className={styles.header}>
          {icon && (
            <span className={styles.icon} aria-hidden="true">
              {icon}
            </span>
          )}
          <h2 id={titleId} className={styles.title}>
            {title}
          </h2>
        </header>
        <div className={styles.body}>{children}</div>
        {footer && <footer className={styles.footer}>{footer}</footer>}
      </div>
    </div>,
    document.body,
  );
}

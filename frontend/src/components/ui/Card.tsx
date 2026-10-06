import type { ReactNode } from 'react';
import styles from './Card.module.css';

interface CardProps {
  title?: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
  flush?: boolean;
  children: ReactNode;
  className?: string;
}

export function Card({ title, subtitle, actions, flush = false, children, className }: CardProps) {
  return (
    <section className={`${styles.card} ${className ?? ''}`}>
      {(title || actions) && (
        <header className={styles.header}>
          <div className={styles.heading}>
            <div className={styles.title}>{title}</div>
            {subtitle && <div className={styles.subtitle}>{subtitle}</div>}
          </div>
          {actions}
        </header>
      )}
      <div className={`${styles.body} ${flush ? styles.flush : ''}`}>{children}</div>
    </section>
  );
}

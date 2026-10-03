import type { InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from 'react';
import { useId, useState } from 'react';

import styles from './Field.module.css';

interface FieldProps {
  label: string;
  hint?: ReactNode;
  error?: string;
  children: (id: string) => ReactNode;
}

export function Field({ label, hint, error, children }: FieldProps) {
  const id = useId();
  return (
    <div className={styles.field}>
      <label className={styles.label} htmlFor={id}>
        {label}
      </label>
      {children(id)}
      {hint && <span className={styles.hint}>{hint}</span>}
      {error && <span className={styles.error}>{error}</span>}
    </div>
  );
}

export function TextField({
  label,
  hint,
  error,
  ...rest
}: { label: string; hint?: ReactNode; error?: string } & InputHTMLAttributes<HTMLInputElement>) {
  return (
    <Field label={label} hint={hint} error={error}>
      {(id) =>
        rest.type === 'password' ? (
          <PasswordInput id={id} {...rest} />
        ) : (
          <input id={id} className={styles.input} {...rest} />
        )
      }
    </Field>
  );
}

const iconBase = {
  width: 18, height: 18, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 2,
  strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true,
};

/** Senha com o "olhinho" para conferir o que foi digitado. */
function PasswordInput(props: InputHTMLAttributes<HTMLInputElement>) {
  const [visible, setVisible] = useState(false);
  return (
    <div className={styles.password}>
      <input
        className={styles.input}
        {...props}
        type={visible ? 'text' : 'password'}
        // À mostra, o teclado do celular não pode corrigir nem pôr maiúscula.
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
      />
      <button
        type="button"
        className={styles.reveal}
        aria-label={visible ? 'Ocultar senha' : 'Mostrar senha'}
        aria-pressed={visible}
        title={visible ? 'Ocultar senha' : 'Mostrar senha'}
        disabled={props.disabled}
        onClick={() => setVisible((v) => !v)}
      >
        {visible ? (
          <svg {...iconBase}>
            <path d="M3 3l18 18" />
            <path d="M10.6 10.6a2 2 0 0 0 2.8 2.8" />
            <path d="M9.4 5.2A9.8 9.8 0 0 1 12 5c5 0 9 4.5 10 7a13 13 0 0 1-2.6 3.8M6.2 6.6C3.9 8 2.5 10.2 2 12c1 2.5 5 7 10 7a9.6 9.6 0 0 0 4.4-1.1" />
          </svg>
        ) : (
          <svg {...iconBase}>
            <path d="M2 12c1-2.5 5-7 10-7s9 4.5 10 7c-1 2.5-5 7-10 7S3 14.5 2 12z" />
            <circle cx="12" cy="12" r="3" />
          </svg>
        )}
      </button>
    </div>
  );
}

export function SelectField({
  label,
  hint,
  error,
  children,
  ...rest
}: {
  label: string;
  hint?: ReactNode;
  error?: string;
  children: ReactNode;
} & SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <Field label={label} hint={hint} error={error}>
      {(id) => (
        <select id={id} className={styles.select} {...rest}>
          {children}
        </select>
      )}
    </Field>
  );
}

export const fieldStyles = styles;

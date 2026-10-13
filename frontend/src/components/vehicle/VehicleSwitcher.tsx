import { useEffect, useMemo, useRef, useState } from 'react';

import { formatDeviceStatus } from '@/services/format';
import { matchesVehicle } from '@/services/vehicleFilters';
import type { VehicleView } from '@/types';

import styles from './VehicleSwitcher.module.css';

/** Quantos a busca mostra por vez (com a frota inteira, milhares). */
const LIMIT = 50;

/**
 * Trocar de veículo sem voltar à lista: o anterior e o próximo (na ordem em
 * que os veículos vêm, a da lista) e, no meio, "3 de 12", que abre a busca
 * por nome, placa ou IMEI. Some com menos de dois veículos.
 */
export function VehicleSwitcher({
  vehicles,
  currentId,
  onSelect,
  search = true,
}: {
  /** Os veículos, já na ordem da lista. */
  vehicles: VehicleView[];
  currentId: string;
  onSelect: (id: string) => void;
  /** Mostra a busca (onde não há a lista ao lado). */
  search?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [term, setTerm] = useState('');
  const root = useRef<HTMLDivElement>(null);
  const index = vehicles.findIndex((v) => v.id === currentId);
  const matching = useMemo(() => vehicles.filter((v) => matchesVehicle(v, term)), [vehicles, term]);

  // Fecha ao clicar fora ou no Esc.
  useEffect(() => {
    if (!open) return undefined;
    const outside = (event: MouseEvent | TouchEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    // O Esc fecha a busca (e não o que está por baixo, como a tela cheia).
    const escape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      setOpen(false);
    };
    document.addEventListener('mousedown', outside);
    document.addEventListener('touchstart', outside);
    document.addEventListener('keydown', escape);
    return () => {
      document.removeEventListener('mousedown', outside);
      document.removeEventListener('touchstart', outside);
      document.removeEventListener('keydown', escape);
    };
  }, [open]);

  if (vehicles.length < 2) return null;
  const total = vehicles.length;
  const step = (delta: number) => {
    const from = index < 0 ? 0 : index;
    onSelect(vehicles[(from + delta + total) % total].id);
  };
  const pick = (id: string) => {
    setOpen(false);
    setTerm('');
    if (id !== currentId) onSelect(id);
  };

  return (
    <div ref={root} className={styles.switcher}>
      <button type="button" className={styles.arrow} onClick={() => step(-1)} aria-label="Veículo anterior" title="Veículo anterior">
        ‹
      </button>
      {search && (
        <button
          type="button"
          className={styles.position}
          aria-haspopup="dialog"
          aria-expanded={open}
          aria-label={`Trocar de veículo (${index + 1} de ${total})`}
          onClick={() => {
            setOpen(!open);
            setTerm('');
          }}
        >
          {index >= 0 ? `${index + 1} de ${total}` : `${total} veículos`} <span aria-hidden="true">▾</span>
        </button>
      )}
      <button type="button" className={styles.arrow} onClick={() => step(1)} aria-label="Próximo veículo" title="Próximo veículo">
        ›
      </button>

      {open && (
        <div className={styles.panel} role="dialog" aria-label="Trocar de veículo">
          <input
            className={styles.search}
            type="search"
            autoFocus
            placeholder="Buscar por nome, placa ou IMEI"
            aria-label="Buscar veículo"
            value={term}
            onChange={(event) => setTerm(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && matching[0]) pick(matching[0].id);
            }}
          />
          {matching.length === 0 ? (
            <p className={styles.empty}>Nenhum veículo com essa busca.</p>
          ) : (
            <ul className={styles.list} role="listbox" aria-label="Veículos">
              {matching.slice(0, LIMIT).map((v) => {
                const status = v.device?.status ?? 'OFFLINE';
                return (
                  <li key={v.id} role="option" aria-selected={v.id === currentId}>
                    <button
                      type="button"
                      className={`${styles.option} ${v.id === currentId ? styles.current : ''}`}
                      onClick={() => pick(v.id)}
                    >
                      <i className={`${styles.dot} ${styles[status] ?? ''}`} title={formatDeviceStatus(status)} />
                      <span className={styles.name}>{v.name}</span>
                      {v.plate && <span className={styles.plate}>{v.plate}</span>}
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
          {matching.length > LIMIT && (
            <p className={styles.empty}>Busque para achar os outros {matching.length - LIMIT}.</p>
          )}
        </div>
      )}
    </div>
  );
}

import { Fragment, useEffect, useId, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';

import { searchApi } from '@/api/resources';
import { Modal } from '@/components/ui/Modal';
import { formatDeviceStatus, formatICCID } from '@/services/format';
import { formatTaxId } from '@/services/taxid';
import type { SearchResults } from '@/types';

import styles from './GlobalSearch.module.css';

/** Um resultado da busca: o grupo, o que mostrar e para onde vai. */
export interface SearchHit {
  key: string;
  group: string;
  title: string;
  detail: string;
  to: string;
}

/** O texto já busca? Como no servidor: 2 letras, ou 4 dígitos (IMEI, linha, CPF...). */
export function searchReady(q: string): boolean {
  const letters = (q.match(/\p{L}/gu) ?? []).length;
  const digits = (q.match(/\d/g) ?? []).length;
  return letters >= 2 || digits >= 4;
}

/** Os resultados na ordem da lista: clientes, veículos, rastreadores, chips livres. */
export function searchHits(r: SearchResults): SearchHit[] {
  const join = (parts: (string | false | null | undefined)[]) => parts.filter(Boolean).join(' · ');
  return [
    ...r.customers.map((c) => ({
      key: `c-${c.id}`,
      group: 'Clientes',
      title: c.active ? c.name : `${c.name} (desativado)`,
      detail: join([c.email, c.phone, c.document && formatTaxId(c.document)]),
      to: `/clientes/${c.id}`,
    })),
    ...r.vehicles.map((v) => ({
      key: `v-${v.id}`,
      group: 'Veículos',
      title: join([v.name, v.plate]),
      detail: join([v.customerName || 'da central', v.deviceImei ? `IMEI ${v.deviceImei}` : 'sem rastreador']),
      to: `/veiculos/${v.id}`,
    })),
    ...r.devices.map((d) => ({
      key: `d-${d.id}`,
      group: 'Rastreadores',
      title: `IMEI ${d.imei}`,
      detail: join([d.model, formatDeviceStatus(d.status), d.vehicleId ? join([d.vehicleName, d.customerName]) : 'sem veículo']),
      // Com veículo, vai para ele; sem, para a lista dos rastreadores já filtrada.
      to: d.vehicleId ? `/veiculos/${d.vehicleId}` : `/dispositivos?busca=${d.imei}`,
    })),
    ...r.chips.map((c) => ({
      key: `s-${c.id}`,
      group: 'Chips livres',
      title: `ICCID ${formatICCID(c.iccid)}`,
      detail: join([c.phoneNumber, c.carrier]),
      to: `/chips?busca=${c.iccid}`,
    })),
  ];
}

/** O valor depois de uma pausa na digitação. */
function useDebounced<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

/**
 * A busca geral da central, no cabeçalho: o botão e o atalho Ctrl+K (⌘K no
 * Mac) abrem a busca por cliente (nome, e-mail, telefone, CPF/CNPJ), placa,
 * IMEI, linha ou ICCID; Enter abre o resultado marcado.
 */
export function GlobalSearch() {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && !event.altKey && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        setOpen(true);
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, []);

  return (
    <>
      <button
        type="button"
        className={styles.trigger}
        aria-label="Buscar no painel (Ctrl+K)"
        title="Buscar cliente, placa, IMEI ou chip (Ctrl+K)"
        onClick={() => setOpen(true)}
      >
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" aria-hidden="true">
          <circle cx="11" cy="11" r="7" />
          <path d="m20 20-3.5-3.5" />
        </svg>
        <span className={styles.triggerLabel}>Buscar</span>
        <kbd className={styles.kbd}>Ctrl K</kbd>
      </button>
      {open && <SearchDialog onClose={() => setOpen(false)} />}
    </>
  );
}

function SearchDialog({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate();
  const listId = useId();
  const [q, setQ] = useState('');
  const term = useDebounced(q.trim(), 250);
  const ready = searchReady(term);
  const results = useQuery({
    queryKey: ['search', term],
    queryFn: () => searchApi.search(term),
    enabled: ready,
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });
  const hits = ready && results.data ? searchHits(results.data) : [];
  const [active, setActive] = useState(0);
  useEffect(() => setActive(0), [term]);
  const current = Math.min(active, Math.max(0, hits.length - 1));

  const go = (hit: SearchHit) => {
    onClose();
    navigate(hit.to);
  };
  const onKeyDown = (event: ReactKeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (hits.length === 0) return;
      setActive((current + (event.key === 'ArrowDown' ? 1 : hits.length - 1)) % hits.length);
    } else if (event.key === 'Enter' && hits[current]) {
      event.preventDefault();
      go(hits[current]);
    }
  };

  const status = !searchReady(q.trim())
    ? 'Digite 2 letras do nome ou e-mail, ou 4 números (telefone, CPF/CNPJ, placa, IMEI, linha ou ICCID).'
    : !ready || results.isFetching
      ? 'Buscando…'
      : results.isError
        ? 'A busca não respondeu. Tente de novo.'
        : hits.length === 0
          ? 'Nada encontrado.'
          : '';

  return (
    <Modal open title="Buscar no painel" onClose={onClose}>
      <div className={styles.body}>
        <input
          className={styles.input}
          type="search"
          role="combobox"
          aria-expanded={hits.length > 0}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={hits[current] ? `${listId}-${hits[current].key}` : undefined}
          aria-label="Buscar cliente, placa, IMEI ou chip"
          placeholder="Nome, e-mail, telefone, CPF/CNPJ, placa, IMEI ou ICCID"
          autoFocus
          value={q}
          onChange={(event) => setQ(event.target.value)}
          onKeyDown={onKeyDown}
        />
        {status && (
          <p className={styles.status} role="status">
            {status}
          </p>
        )}
        <ul id={listId} role="listbox" aria-label="Resultados" className={styles.list}>
          {hits.map((hit, index) => (
            <Fragment key={hit.key}>
              {(index === 0 || hits[index - 1].group !== hit.group) && (
                <li role="presentation" className={styles.group}>
                  {hit.group}
                </li>
              )}
              <li
                id={`${listId}-${hit.key}`}
                role="option"
                aria-selected={index === current}
                className={`${styles.hit} ${index === current ? styles.hitActive : ''}`}
                onMouseEnter={() => setActive(index)}
                onClick={() => go(hit)}
              >
                <span className={styles.title}>{hit.title}</span>
                {hit.detail && <span className={styles.detail}>{hit.detail}</span>}
              </li>
            </Fragment>
          ))}
        </ul>
        <p className={styles.hint}>↑ ↓ para escolher · Enter abre · Esc fecha</p>
      </div>
    </Modal>
  );
}

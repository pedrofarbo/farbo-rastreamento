import React, { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { publicApi } from '@/api/resources';
import type { PublicRepresentative } from '@/types';

import styles from './RepresentativesSection.module.css';

// Com mais que isso, a busca pela cidade aparece.
const SEARCH_FROM = 6;

/**
 * Os representantes oficiais autorizados: os que a central marcou no
 * afiliado. Cada um com a cidade, a região que atende, o WhatsApp e o botão
 * para contratar pelo link dele (com os preços e a entrega dele). Sem nenhum
 * marcado, a seção não aparece.
 */
export const RepresentativesSection: React.FC = () => {
  const reps = useQuery({ queryKey: ['public', 'representatives'], queryFn: publicApi.representatives, staleTime: 5 * 60_000 });
  const [search, setSearch] = useState('');
  const list = reps.data ?? [];
  const visible = useMemo(() => {
    const term = search.trim().toLowerCase();
    if (!term) return list;
    return list.filter((r) => [r.name, r.city, r.area].some((value) => value.toLowerCase().includes(term)));
  }, [list, search]);

  if (list.length === 0) return null;

  return (
    <section className={styles.section} id="representantes">
      <div className={styles.container}>
        <div className={styles.outerCard}>
          <div className={styles.head}>
            <span className={styles.badge}>
              <Check /> Autorizados pela Farbo
            </span>
            <h2 className={styles.title}>Representantes oficiais</h2>
            <p className={styles.lead}>
              Contrate com um representante perto de você. Os desta lista são autorizados pela Farbo Rastreadores; quem não
              está aqui não fala em nome da Farbo.
            </p>
          </div>

          {list.length > SEARCH_FROM && (
            <input
              className={styles.search}
              type="search"
              placeholder="Buscar por cidade, região ou nome"
              aria-label="Buscar representante por cidade, região ou nome"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          )}

          {visible.length === 0 ? (
            <p className={styles.empty}>
              Nenhum representante com “{search}”. Contrate pelo site: a Farbo envia o rastreador para todo o Brasil.
            </p>
          ) : (
            <div className={styles.grid}>
              {visible.map((r) => (
                <RepresentativeCard key={r.code} rep={r} />
              ))}
            </div>
          )}
        </div>
      </div>
    </section>
  );
};

function RepresentativeCard({ rep }: { rep: PublicRepresentative }) {
  const first = rep.name.split(' ')[0];
  const message = encodeURIComponent(
    `Olá, ${first}! Vi você na lista de representantes oficiais da Farbo Rastreadores e quero saber do rastreador.`,
  );
  return (
    <article className={styles.card}>
      <div className={styles.cardHead}>
        <div>
          <h3>{rep.name}</h3>
          <span className={styles.city}>📍 {rep.city}</span>
        </div>
        <span className={styles.seal} title="Representante oficial autorizado pela Farbo">
          <Check /> Oficial
        </span>
      </div>
      {rep.area && <p className={styles.area}>Atende: {rep.area}</p>}
      {(rep.handDelivery || rep.installs) && (
        <div className={styles.tags}>
          {rep.handDelivery && <span>Entrega em mãos</span>}
          {rep.installs && <span>Faz a instalação</span>}
        </div>
      )}
      {rep.handle && (
        <a
          className={styles.instagram}
          href={`https://instagram.com/${encodeURIComponent(rep.handle)}`}
          target="_blank"
          rel="noopener noreferrer"
        >
          @{rep.handle}
        </a>
      )}
      <div className={styles.cardFoot}>
        <Link className={styles.primary} to={`/indicacao/${rep.code}`} data-analytics={`representante-contratar-${rep.code}`}>
          Contratar com {first}
        </Link>
        {rep.whatsapp && (
          <a
            className={styles.secondary}
            href={`https://wa.me/${rep.whatsapp}?text=${message}`}
            target="_blank"
            rel="noopener noreferrer"
            data-analytics={`representante-whatsapp-${rep.code}`}
          >
            WhatsApp
          </a>
        )}
      </div>
    </article>
  );
}

function Check() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round"
      strokeLinejoin="round" aria-hidden="true">
      <path d="M20 6 9 17l-5-5" />
    </svg>
  );
}

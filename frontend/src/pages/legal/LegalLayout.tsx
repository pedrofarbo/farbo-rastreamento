import React, { useEffect } from 'react';
import { Link } from 'react-router-dom';

import { Footer } from '@/components/landing/Footer';
import { WHATSAPP_NUMBER, whatsappUrl } from '@/config/contact';
import { COMPANY, CONTRACT_PATH, LEGAL_UPDATED_AT, PRIVACY_PATH, TERMS_PATH } from '@/config/legal';

import styles from './Legal.module.css';

export interface LegalSection {
  id: string;
  title: string;
  body: React.ReactNode;
}

/**
 * A moldura dos documentos (Termos de Uso, Política de Privacidade e o
 * Contrato): o topo com a volta para o site, o título, a data da versão, o
 * sumário com links para cada seção e o rodapé da landing.
 */
export function LegalLayout({
  title,
  intro,
  sections,
  updated = `Última atualização: ${LEGAL_UPDATED_AT}`,
}: {
  title: string;
  intro: React.ReactNode;
  sections: LegalSection[];
  /** A linha da versão (o contrato tem a dele). */
  updated?: string;
}) {
  useEffect(() => {
    const previous = document.title;
    document.title = `${title} — ${COMPANY.name}`;
    window.scrollTo(0, 0);
    return () => {
      document.title = previous;
    };
  }, [title]);

  return (
    <div className={styles.page}>
      <header className={styles.topbar}>
        <div className={styles.topbarInner}>
          <Link to="/" className={styles.brand} aria-label={`${COMPANY.name} — voltar ao site`}>
            <img src="/assets/logo-header.png" width={956} height={176} alt={COMPANY.name} className={styles.logo} />
          </Link>
          <nav className={styles.switch} aria-label="Documentos">
            <Link to={TERMS_PATH} className={title === 'Termos de Uso' ? styles.switchActive : undefined}>
              Termos<span className={styles.long}> de Uso</span>
            </Link>
            <Link to={PRIVACY_PATH} className={title === 'Política de Privacidade' ? styles.switchActive : undefined}>
              Privacidade
            </Link>
            <Link to={CONTRACT_PATH} className={title.startsWith('Contrato') ? styles.switchActive : undefined}>
              Contrato
            </Link>
          </nav>
        </div>
      </header>

      <main className={styles.main}>
        <article className={styles.doc}>
          <p className={styles.kicker}>{COMPANY.name}</p>
          <h1 className={styles.title}>{title}</h1>
          <p className={styles.updated}>{updated}</p>
          <div className={styles.intro}>{intro}</div>

          <nav className={styles.toc} aria-label="Sumário">
            <p className={styles.tocTitle}>Sumário</p>
            <ol>
              {sections.map((section) => (
                <li key={section.id}>
                  <a href={`#${section.id}`}>{section.title}</a>
                </li>
              ))}
            </ol>
          </nav>

          {sections.map((section, index) => (
            <section key={section.id} id={section.id} className={styles.section}>
              <h2>
                <span className={styles.number}>{index + 1}.</span> {section.title}
              </h2>
              {section.body}
            </section>
          ))}

          <p className={styles.back}>
            <Link to="/">← Voltar ao site</Link>
          </p>
        </article>
      </main>

      <Footer />
    </div>
  );
}

/** Quem é a empresa, com os dados formais que estiverem preenchidos. */
export function CompanyIdentity() {
  return (
    <>
      <strong>{COMPANY.legalName || COMPANY.name}</strong>
      {COMPANY.legalName && COMPANY.legalName !== COMPANY.name && <> ({COMPANY.name})</>}
      {COMPANY.cnpj && <>, CNPJ {COMPANY.cnpj}</>}
      {COMPANY.address && <>, com sede em {COMPANY.address}</>}
    </>
  );
}

/** Os canais de contato, como links. */
export function ContactChannels() {
  return (
    <>
      pelo e-mail <a href={`mailto:${COMPANY.email}`}>{COMPANY.email}</a>
      {WHATSAPP_NUMBER && (
        <>
          {' '}
          ou pelo WhatsApp{' '}
          <a href={whatsappUrl()} target="_blank" rel="noopener noreferrer">
            {COMPANY.whatsapp}
          </a>
        </>
      )}
    </>
  );
}

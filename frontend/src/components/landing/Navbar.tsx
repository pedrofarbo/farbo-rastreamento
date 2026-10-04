import React, { useState, useEffect } from 'react';

import { LAUNCH_ANCHOR, PRE_LAUNCH, SOCIAL_SECTION } from '@/config/landing';

import { ClientAreaLink } from './ClientAreaLink';
import styles from './Navbar.module.css';

interface NavbarProps {
  onOpenModal: (plan?: string) => void;
}

export const Navbar: React.FC<NavbarProps> = ({ onOpenModal }) => {
  const [scrolled, setScrolled] = useState(false);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [logoError, setLogoError] = useState(false);

  useEffect(() => {
    const handleScroll = () => {
      setScrolled(window.scrollY > 20);
    };
    window.addEventListener('scroll', handleScroll);
    return () => window.removeEventListener('scroll', handleScroll);
  }, []);

  const toggleMobileMenu = () => {
    setMobileMenuOpen(!mobileMenuOpen);
  };

  // Menu aberto: a página atrás não rola e o Esc fecha.
  useEffect(() => {
    if (!mobileMenuOpen) return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMobileMenuOpen(false);
    };
    window.addEventListener('keydown', onKey);
    return () => {
      document.body.style.overflow = previous;
      window.removeEventListener('keydown', onKey);
    };
  }, [mobileMenuOpen]);

  const closeMenu = () => {
    setMobileMenuOpen(false);
  };

  return (
    <header className={`${styles.header} ${scrolled ? styles.headerScrolled : ''}`}>
      <div className={styles.container}>
        <a href="#" className={styles.logoLink} onClick={closeMenu}>
          {logoError ? (
            <div className={styles.logoFallbackText}>
              <span className={styles.brandTitle}>FARBO</span>
              <span className={styles.brandSubtitle}>RASTREADORES</span>
            </div>
          ) : (
            <img
              src="/assets/logo-header.png"
              srcSet="/assets/logo-header-480.png 480w, /assets/logo-header.png 956w"
              sizes="(max-width: 960px) 152px, 196px"
              width={956}
              height={176}
              alt="Farbo Rastreadores"
              className={styles.logoImg}
              onError={() => setLogoError(true)}
            />
          )}
        </a>

        {mobileMenuOpen && <div className={styles.backdrop} onClick={closeMenu} aria-hidden="true" />}
        <nav id="menu-principal" className={`${styles.nav} ${mobileMenuOpen ? styles.navOpen : ''}`}>
          <a href="#beneficios" onClick={closeMenu}>Benefícios</a>
          <a href="#planos" onClick={closeMenu}>Planos</a>
          <a href="#como-funciona" onClick={closeMenu}>Como funciona</a>
          {/* No pré-lançamento o botão do menu já leva à seção da lista. */}
          {!PRE_LAUNCH && (
            <a href={`#${SOCIAL_SECTION.id}`} onClick={closeMenu}>{SOCIAL_SECTION.label}</a>
          )}
          <a href="#contato" onClick={closeMenu}>Contato</a>
          <ClientAreaLink className={styles.mobileLoginLink} onClick={closeMenu}>
            <UserIcon />
            Área do cliente
          </ClientAreaLink>
          {PRE_LAUNCH ? (
            <a className={styles.mobileCtaBtn} href={LAUNCH_ANCHOR} onClick={closeMenu} data-analytics="menu-pre-lancamento">
              Pré-lançamento
            </a>
          ) : (
            <button
              className={styles.mobileCtaBtn}
              onClick={() => {
                closeMenu();
                onOpenModal('Quero meu rastreador');
              }}
            >
              Quero meu rastreador
            </button>
          )}
        </nav>

        <div className={styles.rightActions}>
          <ClientAreaLink className={styles.loginLink} aria-label="Área do cliente">
            <UserIcon />
            <span>Área do cliente</span>
          </ClientAreaLink>

          {PRE_LAUNCH ? (
            <a className={styles.ctaButton} href={LAUNCH_ANCHOR} data-analytics="menu-pre-lancamento">
              Pré-lançamento
            </a>
          ) : (
            <button
              className={styles.ctaButton}
              onClick={() => onOpenModal('Quero meu rastreador')}
            >
              Quero meu rastreador
            </button>
          )}

          <button
            className={styles.hamburger}
            onClick={toggleMobileMenu}
            aria-label={mobileMenuOpen ? 'Fechar menu' : 'Abrir menu'}
            aria-expanded={mobileMenuOpen}
            aria-controls="menu-principal"
          >
            <span className={`${styles.bar} ${mobileMenuOpen ? styles.bar1Open : ''}`}></span>
            <span className={`${styles.bar} ${mobileMenuOpen ? styles.bar2Open : ''}`}></span>
            <span className={`${styles.bar} ${mobileMenuOpen ? styles.bar3Open : ''}`}></span>
          </button>
        </div>
      </div>
    </header>
  );
};

const UserIcon: React.FC = () => (
  <svg
    width="18"
    height="18"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
  >
    <circle cx="12" cy="8" r="4" />
    <path d="M4 21a8 8 0 0 1 16 0" />
  </svg>
);

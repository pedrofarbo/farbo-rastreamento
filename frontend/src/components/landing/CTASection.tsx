import React from 'react';
import styles from './CTASection.module.css';

interface CTASectionProps {
  onOpenModal: (plan?: string) => void;
}

export const CTASection: React.FC<CTASectionProps> = ({ onOpenModal }) => {
  return (
    <section className={styles.ctaSection} id="contato">
      <div className={styles.container}>
        <div className={styles.wrapper}>
          {/* Main Action Button */}
          <button
            className={styles.mainCtaBtn}
            onClick={() => onOpenModal('Quero Proteger Meu Veículo')}
            data-analytics="cta-final"
          >
            <span>QUERO PROTEGER MEU VEÍCULO</span>
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
              <path d="m9 18 6-6-6-6" />
            </svg>
          </button>

          {/* Slogan Badge */}
          <div className={styles.sloganBadge}>
            <div className={styles.shieldIcon}>
              <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
                <circle cx="12" cy="11" r="3" />
              </svg>
            </div>
            <div className={styles.sloganLines}>
              <span>MAIS SEGURANÇA</span>
              <span>MAIS CONTROLE</span>
              <span>MAIS TRANQUILIDADE</span>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
};

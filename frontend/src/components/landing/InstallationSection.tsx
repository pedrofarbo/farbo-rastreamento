import React from 'react';

import { InstallationAnimation } from './InstallationAnimation';
import styles from './InstallationSection.module.css';

interface InstallationSectionProps {
  /** Abre os prestadores recomendados, já filtrados pelo tipo de veículo. */
  onOpenInstallers: (filter?: 'todos' | 'moto' | 'carro') => void;
}

/**
 * Instalação profissional. Os valores são de referência: a instalação é feita
 * por prestadores parceiros e paga direto a eles.
 */
export const InstallationSection: React.FC<InstallationSectionProps> = ({ onOpenInstallers }) => {
  return (
    <section className={styles.installationSection} id="instalacao">
      <div className={styles.container}>
        <div className={styles.outerCard}>
          <h2 className={styles.sectionTitle}>INSTALAÇÃO PROFISSIONAL</h2>

          <div className={styles.contentGrid}>
            {/* Left Options Column */}
            <div className={styles.optionsColumn}>
              {/* Option 1: Moto */}
              <button
                type="button"
                className={styles.optionBox}
                onClick={() => onOpenInstallers('moto')}
              >
                <div className={styles.optionIcon}>
                  <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <circle cx="5.5" cy="17.5" r="3.5" />
                    <circle cx="18.5" cy="17.5" r="3.5" />
                    <path d="M15 6h2l3 7h-6.5L12 8" />
                    <path d="M8 17.5V11l4-3" />
                  </svg>
                </div>
                <div className={styles.optionDetails}>
                  <span className={styles.vehicleType}>Moto</span>
                  <div className={styles.optionPrice}>
                    R$ <strong>120</strong>,00
                  </div>
                  <span className={styles.tagline}>Valor de referência</span>
                </div>
              </button>

              {/* Option 2: Carro */}
              <button
                type="button"
                className={styles.optionBox}
                onClick={() => onOpenInstallers('carro')}
              >
                <div className={styles.optionIcon}>
                  <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M19 17h2c.6 0 1-.4 1-1v-3c0-.9-.7-1.7-1.5-1.9C18.7 10.6 16 10 16 10s-1.3-1.4-2.2-2.3c-.5-.4-1.1-.7-1.8-.7H7c-.7 0-1.3.3-1.8.7C4.3 8.6 3 10 3 10s-2.7.6-4.5 1.1C-2.3 11.3-3 12.1-3 13v3c0 .6.4 1 1 1h2" />
                    <circle cx="7" cy="17" r="2" />
                    <circle cx="17" cy="17" r="2" />
                  </svg>
                </div>
                <div className={styles.optionDetails}>
                  <span className={styles.vehicleType}>Carro</span>
                  <div className={styles.optionPrice}>
                    R$ <strong>180</strong>,00
                  </div>
                  <span className={styles.tagline}>Valor de referência</span>
                </div>
              </button>

              <div className={styles.installersNote}>
                <p>
                  A instalação é feita por <strong>prestadores parceiros</strong> e paga direto a
                  eles — você combina o horário e o valor pelo WhatsApp.
                </p>
                <button
                  type="button"
                  className={styles.installersBtn}
                  onClick={() => onOpenInstallers('todos')}
                >
                  Ver prestadores recomendados
                </button>
              </div>
            </div>

            {/* A instalação em três etapas, animada (no lugar das fotos). */}
            <InstallationAnimation />
          </div>
        </div>
      </div>
    </section>
  );
};

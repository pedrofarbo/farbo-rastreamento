import React from 'react';

import { LAUNCH_ANCHOR, PRE_LAUNCH } from '@/config/landing';

import styles from './HeroSection.module.css';

interface HeroSectionProps {
  onOpenModal: (plan?: string) => void;
}

export const HeroSection: React.FC<HeroSectionProps> = ({ onOpenModal }) => {
  return (
    <section className={styles.heroSection} id="beneficios">
      <div className={styles.container}>
        {/* Left Content */}
        <div className={styles.contentLeft}>
          <div className={styles.subtitleTag}>
            RASTREAMENTO VEICULAR INTELIGENTE
          </div>

          <h1 className={styles.mainTitle}>
            Seu veículo <br />
            <span className={styles.neonText}>sempre mais</span> <br />
            <span className={styles.neonText}>seguro</span>
          </h1>

          <p className={styles.description}>
            Proteção, controle e tranquilidade na palma da sua mão. Rastreamento
            em tempo real para motos e carros.
          </p>

          {/* No pré-lançamento, o convite é entrar na lista (com a promoção). */}
          {PRE_LAUNCH ? (
            <a className={styles.heroCtaBtn} href={LAUNCH_ANCHOR}>
              Entrar no pré-lançamento
            </a>
          ) : (
            <button
              className={styles.heroCtaBtn}
              onClick={() => onOpenModal('Quero meu rastreador')}
            >
              Quero proteger meu veículo
            </button>
          )}

          {/* 5 Circular Feature Highlights Row */}
          <div className={styles.featuresRow}>
            <div className={styles.featureItem}>
              <div className={styles.iconCircle}>
                <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M20 10c0 6-8 12-8 12s-8-6-8-12a8 8 0 0 1 16 0Z" />
                  <circle cx="12" cy="10" r="3" />
                </svg>
              </div>
              <div className={styles.featureText}>
                <strong>Localização</strong>
                <span>em tempo real</span>
              </div>
            </div>

            <div className={styles.featureItem}>
              <div className={styles.iconCircle}>
                <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
                  <path d="m9 12 2 2 4-4" />
                </svg>
              </div>
              <div className={styles.featureText}>
                <strong>Mais segurança</strong>
                <span>contra roubo</span>
              </div>
            </div>

            <div className={styles.featureItem}>
              <div className={styles.iconCircle}>
                <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
                  <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
                </svg>
              </div>
              <div className={styles.featureText}>
                <strong>Alertas</strong>
                <span>inteligentes</span>
              </div>
            </div>

            <div className={styles.featureItem}>
              <div className={styles.iconCircle}>
                <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <circle cx="12" cy="12" r="10" />
                  <polyline points="12 6 12 12 16 14" />
                </svg>
              </div>
              <div className={styles.featureText}>
                <strong>Histórico</strong>
                <span>de rotas</span>
              </div>
            </div>

            <div className={styles.featureItem}>
              <div className={styles.iconCircle}>
                <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <rect width="18" height="12" x="3" y="4" rx="2" />
                  <path d="M2 20h20" />
                </svg>
              </div>
              <div className={styles.featureText}>
                <strong>App e sistema</strong>
                <span>web inclusos</span>
              </div>
            </div>
          </div>
        </div>

        {/* Right Content */}
        <div className={styles.contentRight}>
          <div className={styles.visualWrapper}>
            <div className={styles.scriptTextFloat}>
              Liberdade
              <span>- com mais -</span>
              segurança
            </div>

            <div className={styles.heroGlow}></div>
            <img
              src="/assets/hero-visual.svg"
              alt="Aplicativo e painel web Farbo mostrando a localização de uma moto em tempo real"
              className={styles.heroImg}
            />
          </div>
        </div>
      </div>
    </section>
  );
};

import React from 'react';
import { Link } from 'react-router-dom';

import { CHECKOUT_PATH, installmentsOf, priceLabel, priceParts } from '@/config/landing';
import { useCheckoutOffer } from '@/hooks/useCheckoutOffer';

import styles from './HeroSection.module.css';

interface HeroSectionProps {
  onOpenModal: (plan?: string) => void;
}

/**
 * O topo da landing: a proposta direta (bloquear e desbloquear o veículo pelo
 * celular, pelo melhor preço do mercado, com o relé no kit), o preço da oferta
 * do servidor com o normal riscado, o comodato com a instalação dos técnicos
 * da Farbo e o botão que abre a contratação. O bloqueio tem as condições dos
 * Termos (o relé instalado e o veículo parado), ditas embaixo.
 */
export const HeroSection: React.FC<HeroSectionProps> = ({ onOpenModal }) => {
  const { offer, open } = useCheckoutOffer();
  // Aberta, o preço do site com o normal riscado; fechada, o normal.
  const monthly = offer && (open ? offer.planCents : offer.planRegularCents);
  const regular = offer && open && offer.planRegularCents > offer.planCents ? offer.planRegularCents : undefined;
  const off = regular && monthly !== undefined ? Math.round((1 - monthly / regular) * 100) : 0;
  const equipment = offer && (open ? offer.equipmentCents : offer.equipmentRegularCents);
  const installments =
    offer && equipment !== undefined && offer.maxInstallments > 1 ? installmentsOf(equipment, offer.maxInstallments) : '';
  const parts = monthly !== undefined ? priceParts(monthly) : null;
  return (
    <section className={styles.heroSection} id="beneficios">
      <div className={styles.container}>
        {/* Left Content */}
        <div className={styles.contentLeft}>
          <div className={styles.subtitleTag}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <rect x="4" y="11" width="16" height="10" rx="2" />
              <path d="M8 11V7a4 4 0 0 1 8 0v4" />
            </svg>
            Bloqueio e desbloqueio remoto
          </div>

          <h1 className={styles.mainTitle}>
            Bloqueie seu veículo <br />
            <span className={styles.neonText}>pelo celular.</span>
          </h1>

          <p className={styles.description}>
            Rastreamento em tempo real, alertas no app e <strong>bloqueio e desbloqueio remoto do motor</strong>, pelo{' '}
            <strong className={styles.highlight}>melhor preço do mercado</strong>.
          </p>

          {/* O preço (da oferta do servidor): carregando, só o lugar dele. */}
          <div className={styles.priceBox} aria-busy={!offer}>
            {off > 0 && <span className={styles.offBadge}>{off}% OFF</span>}
            <div className={styles.priceLine}>
              {regular !== undefined && <s className={styles.priceOld}>De {priceLabel(regular)}</s>}
              {parts ? (
                <span className={styles.priceNow}>
                  <span className={styles.priceCurrency}>R$</span>
                  <span className={styles.priceReais}>{parts.reais}</span>
                  <span className={styles.priceCents}>
                    {parts.cents}
                    <small>/mês</small>
                  </span>
                </span>
              ) : (
                <span className={styles.pricePending}>R$ —</span>
              )}
            </div>
            {offer && equipment !== undefined && (
              <p className={styles.priceNote}>
                {open && 'enquanto a assinatura durar · '}rastreador {priceLabel(equipment)}
                {installments && ` ou ${installments} sem juros`}, com o relé de bloqueio
              </p>
            )}
            {/* O comodato: sem comprar o rastreador, instalado pelos técnicos da Farbo. */}
            {open && offer?.loan && (
              <p className={styles.loanNote}>
                <strong>Ou em comodato:</strong> sem comprar o rastreador, instalado pelos nossos técnicos
                {offer.loan.installFeeCents > 0 && ` por ${priceLabel(offer.loan.installFeeCents)}`}
                {offer.loan.surchargeCents > 0 && ` (+ ${priceLabel(offer.loan.surchargeCents)}/mês)`}.
              </p>
            )}
          </div>

          {/* Com a contratação pelo site aberta, o convite é contratar;
              fechada na central, o pré-cadastro. */}
          {open ? (
            <div className={styles.heroCta}>
              <div className={styles.ctaRow}>
                <Link className={styles.heroCtaBtn} to={CHECKOUT_PATH} data-analytics="hero-contratar">
                  Quero proteger meu veículo
                </Link>
                <a className={styles.heroGhostBtn} href="#planos" data-analytics="hero-planos">
                  Ver planos
                </a>
              </div>
              <ul className={styles.trust}>
                <li>Relé de bloqueio incluso</li>
                <li>Pague com Pix</li>
                <li>Receba em casa</li>
                <li>Homologado pela Anatel</li>
              </ul>
            </div>
          ) : (
            <button
              className={styles.heroCtaBtn}
              onClick={() => onOpenModal('Quero meu rastreador')}
            >
              Quero proteger meu veículo
            </button>
          )}
          <p className={styles.finePrint}>
            O relé vem no kit: instale com o seu instalador ou com os nossos técnicos. Por segurança, o bloqueio é feito
            com o veículo parado, e você desbloqueia pelo app.
          </p>

          {/* Os 5 destaques, o bloqueio primeiro. */}
          <div className={styles.featuresRow}>
            <div className={styles.featureItem}>
              <div className={styles.iconCircle}>
                <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <rect x="4" y="11" width="16" height="10" rx="2" />
                  <path d="M8 11V7a4 4 0 0 1 8 0v4" />
                </svg>
              </div>
              <div className={styles.featureText}>
                <strong>Bloqueio</strong>
                <span>pelo celular</span>
              </div>
            </div>
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

            {/* O bloqueio, o que mais vende: o cartão sobre a ilustração. */}
            <div className={styles.lockCard} aria-hidden="true">
              <span className={styles.lockIcon}>
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
                  <rect x="4" y="11" width="16" height="10" rx="2" />
                  <path d="M8 11V7a4 4 0 0 1 8 0v4" />
                </svg>
              </span>
              <span>
                <strong>Bloqueio pelo app</strong>
                <small>confirmado pelo rastreador</small>
              </span>
            </div>
            <div className={styles.heroGlow}></div>
            {/* As telas de verdade do app e do painel (dados de demonstração):
                a moto parada com o motor bloqueado. Ver scripts/landing-images.mjs. */}
            <img
              src="/assets/hero-app.webp"
              width={1200}
              height={1345}
              alt="O app da Farbo no celular e o painel no computador: a moto parada no mapa, com o motor bloqueado pelo app"
              className={styles.heroImg}
            />
          </div>
        </div>
      </div>
    </section>
  );
};

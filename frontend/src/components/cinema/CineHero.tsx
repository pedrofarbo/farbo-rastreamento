import type { CSSProperties } from 'react';
import { Link } from 'react-router-dom';

import { CHECKOUT_PATH, includedLabel, loanOnly, priceLabel, priceParts } from '@/config/landing';
import { useCheckoutOffer } from '@/hooks/useCheckoutOffer';
import { heroPrice, kindPricesLabel } from '@/services/kindPrices';

import { CityMap, Route, VehicleMarker } from './CityMap';
import { Icon } from './icons';
import cine from './cine.module.css';
import styles from './CineHero.module.css';

/** A rota do topo, na grade do mapa (múltiplos de BLOCK caem nas ruas). */
/**
 * A rota do topo, na grade do mapa (múltiplos de BLOCK caem nas ruas):
 * escolhida para, no computador, subir pelo vão entre o texto e o celular,
 * passar por cima dele e descer pela direita, onde dá para ver o veículo.
 */
const ROUTE = 'M420 840V560H490V280H560V210H910V560H840V840';
const rise = (n: number) => ({ '--d': n }) as CSSProperties;

interface CineHeroProps {
  onOpenModal: (plan?: string) => void;
}

/**
 * A abertura: o mapa noturno ao fundo, com a rota neon se desenhando e o
 * veículo andando por ela (e uma moto parada, bloqueada), a proposta direta
 * (bloquear e desbloquear pelo celular), o preço da oferta do servidor (o
 * normal riscado quando há desconto, o rastreador incluso ou o comodato), os
 * botões e as telas de verdade do app. As condições do bloqueio (o relé e o
 * veículo parado) ficam ditas embaixo, como nos Termos.
 */
export function CineHero({ onOpenModal }: CineHeroProps) {
  const { offer, open } = useCheckoutOffer();
  const { monthly, regular, off, table } = heroPrice(offer, open);
  const parts = monthly !== undefined ? priceParts(monthly) : null;

  return (
    <section id="beneficios" className={styles.hero} aria-labelledby="hero-title">
      <div className={styles.backdrop} aria-hidden="true">
        <CityMap className={styles.map} data-still="4.2">
          <Route d={ROUTE} className={styles.route} />
          <g transform="translate(700 630)">
            <VehicleMarker className={styles.parked}>
              <g className={styles.lockBadge} transform="rotate(16) translate(16 -32)">
                <rect x="-12" y="-12" width="24" height="24" rx="7" />
                <path d="M-4.5 -1v-2.5a4.5 4.5 0 0 1 9 0V-1M-6.5 -1h13v8h-13z" />
              </g>
            </VehicleMarker>
          </g>
          <g>
            <VehicleMarker>
              <text className={styles.speed} transform="rotate(16)" y="46" textAnchor="middle">
                42 km/h
              </text>
            </VehicleMarker>
            <animateMotion dur="11s" repeatCount="indefinite" path={ROUTE} />
          </g>
        </CityMap>
        <div className={styles.vignette} />
      </div>

      <div className={styles.inner}>
        <div className={styles.copy}>
          <p className={`${styles.tag} ${styles.rise}`} style={rise(0)}>
            <Icon name="lock" size={14} strokeWidth={2.4} />
            Bloqueio e desbloqueio remoto
          </p>

          <h1 id="hero-title" className={`${styles.title} ${styles.rise}`} style={rise(1)}>
            Bloqueie seu veículo <span>pelo celular.</span>
          </h1>

          <p className={`${styles.lead} ${styles.rise}`} style={rise(2)}>
            Rastreamento em tempo real, alertas no app e <strong>bloqueio e desbloqueio remoto do motor</strong>, pelo{' '}
            <strong className={styles.neon}>melhor preço do mercado</strong>.
          </p>

          {/* O preço (da oferta do servidor): carregando, só o lugar dele. */}
          <div className={`${styles.price} ${styles.rise}`} style={rise(3)} aria-busy={!offer}>
            <div className={styles.priceLine}>
              {off > 0 && <span className={styles.off}>{off}% OFF</span>}
              {regular !== undefined && <s className={styles.old}>De {priceLabel(regular)}</s>}
              {table && <span className={styles.from}>a partir de</span>}
              {parts ? (
                <span className={styles.now}>
                  <span className={styles.currency}>R$</span>
                  <span className={styles.reais}>{parts.reais}</span>
                  <span className={styles.cents}>
                    {parts.cents}
                    <small>/mês</small>
                  </span>
                </span>
              ) : (
                <span className={styles.pending}>R$ —</span>
              )}
            </div>
            {offer && (
              <p className={styles.priceNote}>
                {table && `${kindPricesLabel(table, priceLabel)} · `}
                {open && 'enquanto a assinatura durar · '}
                {loanOnly(offer, open) ? 'rastreador em comodato' : `${includedLabel(offer, open)}: você só paga o frete`}
                , com o relé de bloqueio
              </p>
            )}
            {open && offer?.loan && (
              <p className={styles.loan}>
                <strong>{loanOnly(offer, open) ? 'Em comodato:' : 'Ou em comodato:'}</strong> o rastreador emprestado,
                instalado {offer.installDirect ? 'pelo representante' : 'pelos nossos técnicos'}
                {offer.loan.installFeeCents > 0 && ` por ${priceLabel(offer.loan.installFeeCents)}`}
                {offer.installDirect && ', pago direto a ele'}
                {offer.loan.surchargeCents > 0 && ` (+ ${priceLabel(offer.loan.surchargeCents)}/mês)`}.
              </p>
            )}
          </div>

          {/* Aberta, o convite é contratar; fechada na central, o pré-cadastro. */}
          <div className={`${styles.ctas} ${styles.rise}`} style={rise(4)}>
            {open ? (
              <>
                <Link className={`${cine.btn} ${cine.primary}`} to={CHECKOUT_PATH} data-analytics="hero-contratar">
                  Quero proteger meu veículo
                  <Icon name="arrowRight" size={18} strokeWidth={2.4} />
                </Link>
                <a className={`${cine.btn} ${cine.ghost}`} href="#planos" data-analytics="hero-planos">
                  Ver planos
                </a>
              </>
            ) : (
              <button
                type="button"
                className={`${cine.btn} ${cine.primary}`}
                onClick={() => onOpenModal('Quero meu rastreador')}
              >
                Quero proteger meu veículo
                <Icon name="arrowRight" size={18} strokeWidth={2.4} />
              </button>
            )}
          </div>

          <ul className={`${styles.trust} ${styles.rise}`} style={rise(5)}>
            <li>
              <Icon name="check" size={15} strokeWidth={2.6} />
              Relé de bloqueio incluso
            </li>
            <li>
              <Icon name="check" size={15} strokeWidth={2.6} />
              Pague com Pix
            </li>
            <li>
              <Icon name="check" size={15} strokeWidth={2.6} />
              Receba em casa
            </li>
            <li>
              <Icon name="check" size={15} strokeWidth={2.6} />
              Homologado pela Anatel
            </li>
          </ul>

          <p className={`${styles.fine} ${styles.rise}`} style={rise(6)}>
            O relé vem no kit: instale com o seu instalador ou com os nossos técnicos. Por segurança, o bloqueio é feito
            com o veículo parado, e você desbloqueia pelo app.
          </p>
        </div>

        <div className={styles.visual}>
          <div className={styles.device} data-tilt>
            {/* As telas de verdade do app e do painel (dados de demonstração). */}
            <img
              src="/assets/hero-app.webp"
              width={1200}
              height={1345}
              alt="O app da Farbo no celular e o painel no computador: a moto parada no mapa, com o motor bloqueado pelo app"
              className={styles.img}
            />
          </div>
          <div className={`${styles.chip} ${styles.chipLive}`} aria-hidden="true">
            <i />
            Ao vivo
          </div>
          <div className={`${styles.chip} ${styles.chipLock}`} aria-hidden="true">
            <span className={styles.chipIcon}>
              <Icon name="lock" size={16} strokeWidth={2.4} />
            </span>
            <span>
              <strong>Motor bloqueado</strong>
              <small>confirmado pelo rastreador</small>
            </span>
          </div>
        </div>
      </div>

      <a className={styles.cue} href="#bloqueio" data-analytics="hero-ver-bloqueio">
        Veja o bloqueio acontecer
        <Icon name="arrowDown" size={16} strokeWidth={2.4} />
      </a>
    </section>
  );
}

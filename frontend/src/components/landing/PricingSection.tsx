import React, { useState } from 'react';
import { Link } from 'react-router-dom';

import { WHATSAPP_NUMBER, whatsappUrl } from '@/config/contact';

import {
  ANATEL_HOMOLOGATION,
  CHECKOUT_PATH,
  checkoutPath,
  installmentsOf,
  installmentsPhrase,
  priceLabel,
  priceParts,
} from '@/config/landing';
import { useCheckoutOffer } from '@/hooks/useCheckoutOffer';
import type { CheckoutOffer } from '@/types';

import { FleetFeatures } from './FleetFeatures';
import { WhatsAppIcon } from './WhatsAppFloat';
import styles from './PricingSection.module.css';

/** O tamanho da frota (opcional): vai na mensagem do WhatsApp. */
const FLEET_SIZES = [
  { label: '2 a 5', text: 'de 2 a 5' },
  { label: '6 a 10', text: 'de 6 a 10' },
  { label: '11 a 30', text: 'de 11 a 30' },
  { label: 'Mais de 30', text: 'mais de 30' },
];

/** A mensagem de quem pede o preço de frota pelo WhatsApp. */
export function fleetMessage(size = ''): string {
  const fleet = FLEET_SIZES.find((option) => option.label === size);
  return fleet
    ? `Olá! Vim pelo site da Farbo Rastreadores e quero o preço especial para frotas. Tenho ${fleet.text} veículos para rastrear. Pode me passar um orçamento?`
    : 'Olá! Vim pelo site da Farbo Rastreadores e quero o preço especial para frotas. Pode me passar um orçamento?';
}

interface PricingSectionProps {
  onOpenModal: (plan?: string) => void;
}

export const PricingSection: React.FC<PricingSectionProps> = ({ onOpenModal }) => {
  const [fleetSize, setFleetSize] = useState('');
  // Os preços vêm da oferta do servidor: com a contratação pelo site aberta,
  // os do site (com os normais riscados); fechada, os normais.
  const { offer, open } = useCheckoutOffer();
  const plan = offer && (open ? offer.planCents : offer.planRegularCents);
  const insanos = offer && (open ? offer.insanosCents : offer.insanosRegularCents);
  const equipment = offer && (open ? offer.equipmentCents : offer.equipmentRegularCents);
  const showInsanos = !offer || offer.insanosPlanName !== '';
  return (
    <section className={styles.pricingSection} id="planos">
      <div className={styles.container}>
        {/* A contratação pelo site: o preço em destaque, com o botão que abre
            o funil (Pix na hora). */}
        {open && (
          <div className={styles.launchBanner}>
            <div className={styles.launchBannerText}>
              <span className={styles.launchBannerTag}>Contrate pelo site</span>
              <strong>{bannerHeadline(offer)}</strong>
              <span>
                O preço da mensalidade vale enquanto a assinatura durar
                {offer && showInsanos && ` · integrantes do Insanos MC pagam ${priceLabel(offer.insanosCents)}/mês`}
                {offer?.loan && ' · ou o rastreador em comodato, sem comprar'}.
              </span>
            </div>
            <Link className={styles.launchBannerBtn} to={CHECKOUT_PATH} data-analytics="planos-banner">
              Contratar agora
            </Link>
          </div>
        )}

        <div className={styles.cardsGrid}>
          {/* Card 1: PLANO MENSAL */}
          <div className={`${styles.card} ${open ? styles.launchCard : ''}`}>
            {open && <div className={styles.badgeTop}>PREÇO PROMOCIONAL</div>}
            <div className={styles.cardHeader}>
              <h3 className={styles.cardTitle}>PLANO MENSAL</h3>
              <CardPrice cents={plan} regular={open ? offer?.planRegularCents : undefined} />
              <div className={styles.priceSub}>
                /mês por veículo
                {open && (
                  <>
                    <br />
                    enquanto a assinatura durar
                  </>
                )}
              </div>
            </div>

            <ul className={styles.checkList}>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Rastreamento em tempo real
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                App e sistema web inclusos
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Chip M2M multi operadora com 20 MB/mês incluso
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Histórico de rotas
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Alertas e notificações
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Suporte especializado
              </li>
            </ul>

            {open ? (
              <Link className={styles.neonBtn} to={CHECKOUT_PATH} data-analytics="plano-mensal">
                Contratar agora
              </Link>
            ) : (
              <button className={styles.whiteBtn} onClick={() => onOpenModal('Plano Mensal')} data-analytics="plano-mensal">
                Quero esse plano
              </button>
            )}
          </div>

          {/* Card 2: PREÇO ESPECIAL (MOTOCLUBE INSANOS) */}
          {showInsanos && (
          <div className={`${styles.card} ${styles.featuredCard}`}>
            <div className={styles.badgeTop}>PREÇO ESPECIAL</div>

            <div className={styles.cardHeader}>
              <div className={styles.insanosSubtitle}>
                INTEGRANTES DO MOTOCLUBE INSANOS
              </div>
              <CardPrice cents={insanos} regular={open ? offer?.insanosRegularCents : undefined} featured />
              <div className={styles.priceSubFeatured}>
                /mês por veículo
                {open && (
                  <>
                    <br />
                    enquanto a assinatura durar
                  </>
                )}
              </div>
            </div>

            <ul className={styles.checkList}>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Rastreamento em tempo real
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                App e sistema web inclusos
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Chip M2M multi operadora com 20 MB/mês incluso
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Histórico de rotas
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Alertas e notificações
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Suporte especializado
              </li>
            </ul>

            <div className={styles.partnerFooter}>
              <img
                src="/assets/insanos-escudo.webp"
                width={56}
                height={69}
                loading="lazy"
                decoding="async"
                alt="Escudo do Insanos MC Brasil"
                className={styles.partnerLogo}
              />
              <div className={styles.partnerText}>
                <strong>INSANOS MC</strong>
                <span>
                  FARBO RASTREADORES<br />
                  MOTOCLUBE INSANOS
                </span>
              </div>
            </div>

            {/* A contratação já com o plano do Insanos marcado (quem marca se
                declara integrante); fechada, o pré-cadastro com o plano deles. */}
            {open ? (
              <Link className={styles.neonBtn} to={checkoutPath({ insanos: true })} data-analytics="plano-insanos">
                Contratar com o preço do Insanos
              </Link>
            ) : (
              <button
                className={styles.neonBtn}
                onClick={() => onOpenModal('Preço Especial Insanos MC')}
                data-analytics="plano-insanos"
              >
                Quero meu desconto
              </button>
            )}
          </div>
          )}

          {/* Card 3: FROTAS. Preço sob consulta: o botão abre o WhatsApp com a
              mensagem já escrita (e o tamanho da frota, se escolhido). */}
          <div className={`${styles.card} ${styles.fleetCard}`}>
            <div className={`${styles.badgeTop} ${styles.badgeFleet}`}>PARA EMPRESAS</div>
            <div className={styles.cardHeader}>
              <h3 className={styles.cardTitle}>FROTAS</h3>
              <div className={styles.deviceSubtitle}>Empresas, frotistas e locadoras</div>
              <div className={styles.fleetPrice}>Preço especial</div>
              <div className={styles.priceSub}>
                mensalidade por volume de veículos
                <br />
                condições sob medida para a sua frota
              </div>
            </div>

            <ul className={styles.checkList}>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Toda a frota no mesmo mapa, por grupos
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Relatórios em planilha e no seu e-mail
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Manutenção preventiva por km, horas de motor ou data
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Equipe com acesso por região
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Uma fatura e um pagamento (Pix ou boleto) para a frota toda
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Mensalidade por volume e atendimento dedicado
              </li>
            </ul>
            <a className={styles.fleetMore} href="#frotas" data-analytics="frotas-ver-tudo">
              Ver tudo o que a frota ganha ↓
            </a>

            <div className={styles.fleetSize} role="group" aria-label="Quantos veículos tem a sua frota?">
              <span className={styles.fleetSizeLabel}>Quantos veículos?</span>
              <div className={styles.fleetChips}>
                {FLEET_SIZES.map((option) => (
                  <button
                    key={option.label}
                    type="button"
                    aria-pressed={fleetSize === option.label}
                    className={`${styles.fleetChip} ${fleetSize === option.label ? styles.fleetChipActive : ''}`}
                    onClick={() => setFleetSize(fleetSize === option.label ? '' : option.label)}
                  >
                    {option.label}
                  </button>
                ))}
              </div>
            </div>

            {WHATSAPP_NUMBER ? (
              <a
                className={styles.whatsappBtn}
                href={whatsappUrl(fleetMessage(fleetSize))}
                target="_blank"
                rel="noopener noreferrer"
                data-analytics="plano-frotas"
              >
                <WhatsAppIcon size={20} />
                Pedir orçamento no WhatsApp
              </a>
            ) : (
              <button className={styles.whiteBtn} onClick={() => onOpenModal('Frotas')} data-analytics="plano-frotas">
                Pedir orçamento
              </button>
            )}
          </div>
        </div>

      {/* O rastreador (o equipamento), largo, embaixo dos planos: o preço
          vale para qualquer plano. */}
      <div className={`${styles.card} ${styles.equipmentCard}`} id="rastreador">
          <div className={styles.cardHeader}>
            <h3 className={styles.cardTitle}>EQUIPAMENTO</h3>
            <div className={styles.deviceSubtitle}>Rastreador J16 GT06</div>
            <CardPrice cents={equipment} regular={open ? offer?.equipmentRegularCents : undefined} />
            {offer && equipment !== undefined && (
              <div className={styles.priceSub}>
                {offer.maxInstallments > 1 ? (
                  <>
                    à vista, ou {installmentsOf(equipment, offer.maxInstallments)}
                    <br />
                    sem juros no Pix, direto na mensalidade
                  </>
                ) : (
                  'à vista, no Pix'
                )}
              </div>
            )}
            {/* Quem instala, e o comodato para quem não quer comprar. */}
            {open && offer && (
              <>
                <p className={styles.equipmentNote}>
                  {offer.install
                    ? `Instale com o seu instalador ou com os nossos, por ${priceLabel(offer.install.priceCents)}.`
                    : 'Instale com o instalador da sua preferência.'}
                </p>
                {offer.loan && (
                  <p className={styles.equipmentNote}>
                    <strong>Ou em comodato</strong>, sem comprar: {loanTerms(offer.loan)}.
                  </p>
                )}
              </>
            )}
          </div>

          {/* A foto ocupa o espaço entre o preço e a lista, que fica com a
              largura toda. */}
          <div className={styles.equipmentBody}>
            <img
              src="/assets/rastreador-j16-farbo.webp"
              width={150}
              height={199}
              loading="lazy"
              decoding="async"
              alt="Rastreador J16 com a marca Farbo e o chicote de instalação"
              className={styles.trackerImg}
            />
            <ul className={styles.checkList}>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Alta precisão GPS
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Bloqueio remoto (opcional)
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Suporte a comandos
              </li>
              <li>
                <span className={styles.checkIcon}>✓</span>
                Resistente e confiável
              </li>
              <li className={styles.anatelItem}>
                {/* Selo Anatel: a logo e o número de homologação. */}
                <span className={styles.anatelSeal} title={`Homologação Anatel nº ${ANATEL_HOMOLOGATION}`}>
                  <img src="/assets/anatel-logo.png" width={86} height={23} loading="lazy" decoding="async" alt="Anatel" />
                  <span className={styles.anatelNumber}>{ANATEL_HOMOLOGATION}</span>
                </span>
              </li>
            </ul>
          </div>
        </div>

        {/* Para frotas: as funcionalidades da gestão de frota, com o orçamento. */}
        <FleetFeatures onOpenModal={onOpenModal} />
      </div>
    </section>
  );
};

/** O preço grande do cartão: o normal riscado (se maior) e o valor; carregando, só o lugar dele. */
function CardPrice({ cents, regular, featured }: { cents?: number; regular?: number; featured?: boolean }) {
  if (cents === undefined) {
    return (
      <div className={styles.pricePending} aria-busy="true">
        R$ —
      </div>
    );
  }
  const parts = priceParts(cents);
  return (
    <>
      {regular !== undefined && regular > cents && (
        <div className={styles.priceStrikethrough}>
          De <span>{priceLabel(regular)}</span>
        </div>
      )}
      <div className={featured ? styles.priceMainFeatured : styles.priceMain}>
        <span className={styles.currency}>R$</span>
        <span className={styles.amount}>{parts.reais}</span>
        <span className={styles.cents}>{parts.cents}</span>
      </div>
    </>
  );
}

/** "R$ 34,90/mês e R$ 120 no rastreador, ou 10x de R$ 12,00 ...": o destaque da faixa. */
export function bannerHeadline(offer: CheckoutOffer | undefined): string {
  if (!offer) return 'Escolha o plano, pague com Pix e receba em casa.';
  const installments = installmentsPhrase(offer.equipmentCents, offer.maxInstallments);
  return `${priceLabel(offer.planCents)}/mês e ${priceLabel(offer.equipmentCents)} no rastreador${
    installments ? `, ou ${installments}` : ''
  }. Pague com Pix e receba em casa.`;
}

/** "instalação R$ 150 pelos nossos instaladores e + R$ 3/mês de garantia de troca do rastreador (desinstalação R$ 85 ao encerrar)". */
export function loanTerms(loan: NonNullable<CheckoutOffer['loan']>): string {
  const install = loan.installFeeCents > 0 ? `instalação ${priceLabel(loan.installFeeCents)}` : 'instalação sem custo';
  const guarantee =
    loan.surchargeCents > 0 ? ` e + ${priceLabel(loan.surchargeCents)}/mês de garantia de troca do rastreador` : '';
  const uninstall = loan.uninstallFeeCents > 0 ? ` (desinstalação ${priceLabel(loan.uninstallFeeCents)} ao encerrar)` : '';
  return `${install} pelos nossos instaladores${guarantee}${uninstall}`;
}

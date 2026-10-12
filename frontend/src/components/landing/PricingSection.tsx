import React, { useState } from 'react';

import { WHATSAPP_NUMBER, whatsappUrl } from '@/config/contact';

import {
  ANATEL_HOMOLOGATION,
  INSANOS_MONTHLY,
  LAUNCH_ANCHOR,
  LAUNCH_OFFER,
  PRE_LAUNCH,
  installmentsOf,
  priceParts,
} from '@/config/landing';

import { FleetFeatures } from './FleetFeatures';
import { WhatsAppIcon } from './WhatsAppFloat';
import styles from './PricingSection.module.css';

const monthlyPromo = priceParts(LAUNCH_OFFER.monthlyCents);
const equipmentPromo = priceParts(LAUNCH_OFFER.equipmentCents);
const insanosPromo = priceParts(LAUNCH_OFFER.insanosMonthlyCents);
const insanosRegular = priceParts(INSANOS_MONTHLY.cents);

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
  return (
    <section className={styles.pricingSection} id="planos">
      <div className={styles.container}>
        {/* Pré-lançamento: o preço da lista em destaque, com o convite para
            se cadastrar (a seção da lista fica mais abaixo). */}
        {PRE_LAUNCH && (
          <div className={styles.launchBanner}>
            <div className={styles.launchBannerText}>
              <span className={styles.launchBannerTag}>Preço de pré-lançamento</span>
              <strong>
                Cadastre-se na lista e pague {LAUNCH_OFFER.monthly}/mês nos {LAUNCH_OFFER.months} primeiros meses e{' '}
                {LAUNCH_OFFER.equipment} no rastreador, ou {installmentsOf(LAUNCH_OFFER.equipmentCents)} sem juros.
              </strong>
              <span>
                Só para quem está na lista de pré-lançamento · integrantes do Insanos MC pagam{' '}
                {LAUNCH_OFFER.insanosMonthly}/mês · limitado aos {LAUNCH_OFFER.slots} primeiros clientes · 1 veículo
                por cliente.
              </span>
            </div>
            <a className={styles.launchBannerBtn} href={LAUNCH_ANCHOR} data-analytics="planos-banner">
              Quero me cadastrar
            </a>
          </div>
        )}

        <div className={styles.cardsGrid}>
          {/* Card 1: PLANO MENSAL */}
          <div className={`${styles.card} ${PRE_LAUNCH ? styles.launchCard : ''}`}>
            {PRE_LAUNCH && <div className={styles.badgeTop}>PRÉ-LANÇAMENTO</div>}
            <div className={styles.cardHeader}>
              <h3 className={styles.cardTitle}>PLANO MENSAL</h3>
              {PRE_LAUNCH ? (
                <>
                  <div className={styles.priceStrikethrough}>
                    De <span>{LAUNCH_OFFER.monthlyRegular}</span>
                  </div>
                  <div className={styles.priceMain}>
                    <span className={styles.currency}>R$</span>
                    <span className={styles.amount}>{monthlyPromo.reais}</span>
                    <span className={styles.cents}>{monthlyPromo.cents}</span>
                  </div>
                  <div className={styles.priceSub}>
                    /mês nos {LAUNCH_OFFER.months} primeiros meses
                    <br />
                    depois, {LAUNCH_OFFER.monthlyRegular}/mês por veículo
                  </div>
                </>
              ) : (
                <>
                  <div className={styles.priceStrikethrough}>
                    De R$ <span>119,90</span>
                  </div>
                  <div className={styles.priceMain}>
                    <span className={styles.currency}>R$</span>
                    <span className={styles.amount}>69</span>
                    <span className={styles.cents}>,90</span>
                  </div>
                  <div className={styles.priceSub}>/mês por veículo</div>
                </>
              )}
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

            {PRE_LAUNCH ? (
              <a className={styles.neonBtn} href={LAUNCH_ANCHOR} data-analytics="plano-mensal">
                Garantir preço de pré-lançamento
              </a>
            ) : (
              <button
                className={styles.whiteBtn}
                onClick={() => onOpenModal('Plano Mensal - R$ 69,90')}
              >
                Quero esse plano
              </button>
            )}
          </div>

          {/* Card 2: PREÇO ESPECIAL (MOTOCLUBE INSANOS) */}
          <div className={`${styles.card} ${styles.featuredCard}`}>
            <div className={styles.badgeTop}>PREÇO ESPECIAL</div>

            <div className={styles.cardHeader}>
              <div className={styles.insanosSubtitle}>
                INTEGRANTES DO MOTOCLUBE INSANOS
              </div>
              {/* A promoção de pré-lançamento vale também para o Insanos, com
                  a mensalidade deles; depois dela, o preço especial deles. */}
              {PRE_LAUNCH ? (
                <>
                  <div className={styles.priceStrikethrough}>
                    De <span>{INSANOS_MONTHLY.label}</span>
                  </div>
                  <div className={styles.priceMainFeatured}>
                    <span className={styles.currency}>R$</span>
                    <span className={styles.amount}>{insanosPromo.reais}</span>
                    <span className={styles.cents}>{insanosPromo.cents}</span>
                  </div>
                  <div className={styles.priceSubFeatured}>
                    /mês nos {LAUNCH_OFFER.months} primeiros meses
                    <br />
                    depois, {INSANOS_MONTHLY.label}/mês por veículo
                  </div>
                </>
              ) : (
                <>
                  <div className={styles.priceMainFeatured}>
                    <span className={styles.currency}>R$</span>
                    <span className={styles.amount}>{insanosRegular.reais}</span>
                    <span className={styles.cents}>{insanosRegular.cents}</span>
                  </div>
                  <div className={styles.priceSubFeatured}>/mês por veículo</div>
                </>
              )}
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

            {/* Abre o pré-cadastro com o plano do Insanos (a equipe confere quem
                é integrante); a lista de pré-lançamento já vem marcada lá. */}
            <button
              className={styles.neonBtn}
              onClick={() => onOpenModal('Preço Especial Insanos MC - R$ 39,90')}
              data-analytics="plano-insanos"
            >
              {PRE_LAUNCH ? 'Garantir preço de pré-lançamento' : 'Quero meu desconto'}
            </button>
          </div>

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
            {PRE_LAUNCH ? (
              <>
                <div className={styles.priceStrikethrough}>
                  De <span>{LAUNCH_OFFER.equipmentRegular},00</span>
                </div>
                <div className={styles.priceMain}>
                  <span className={styles.currency}>R$</span>
                  <span className={styles.amount}>{equipmentPromo.reais}</span>
                  <span className={styles.cents}>{equipmentPromo.cents}</span>
                </div>
                <div className={styles.priceSub}>
                  à vista, ou {installmentsOf(LAUNCH_OFFER.equipmentCents)}
                  <br />
                  sem juros no Pix
                </div>
              </>
            ) : (
              <>
                <div className={styles.priceMain}>
                  <span className={styles.currency}>R$</span>
                  <span className={styles.amount}>150</span>
                  <span className={styles.cents}>,00</span>
                </div>
                <div className={styles.priceSub}>
                  à vista, ou {installmentsOf(LAUNCH_OFFER.equipmentRegularCents)}
                  <br />
                  sem juros no Pix
                </div>
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

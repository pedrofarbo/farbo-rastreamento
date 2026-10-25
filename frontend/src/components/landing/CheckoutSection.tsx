import React from 'react';
import { Link } from 'react-router-dom';

import { CHECKOUT_PATH, installmentsPhrase, priceLabel } from '@/config/landing';
import { useCheckoutOffer } from '@/hooks/useCheckoutOffer';

import { loanTerms } from './PricingSection';
import styles from './CheckoutSection.module.css';

const STEPS = [
  { title: 'Escolha o plano e o rastreador', text: 'Comprado (à vista ou parcelado) ou em comodato.' },
  { title: 'Informe o veículo e o endereço', text: 'A sua conta no app é criada na hora.' },
  { title: 'Pague com Pix', text: 'A 1ª mensalidade, o rastreador e o frete, de uma vez.' },
  { title: 'Receba e instale', text: 'Com o seu instalador ou com os nossos, no dia que você escolher.' },
];

interface CheckoutSectionProps {
  onOpenModal: (plan?: string) => void;
}

/**
 * A chamada para a contratação pelo site: a oferta (os preços do servidor),
 * como funciona e o botão do funil; quem prefere conversar deixa o contato no
 * pré-cadastro.
 */
export const CheckoutSection: React.FC<CheckoutSectionProps> = ({ onOpenModal }) => {
  const { offer } = useCheckoutOffer();
  const installments = offer ? installmentsPhrase(offer.equipmentCents, offer.maxInstallments) : '';
  return (
    <section className={styles.section} id="contratar" aria-labelledby="contratar-titulo">
      <div className={styles.container}>
        <div className={styles.card}>
          <div>
            <span className={styles.badge}>Contrate agora</span>
            <h2 className={styles.title} id="contratar-titulo">
              Proteja o seu veículo hoje
            </h2>
            <p className={styles.subtitle}>
              Escolha o plano, informe o veículo e pague com Pix. O rastreador chega no seu endereço e o app já fica
              pronto para usar.
            </p>

            <div className={styles.offer} aria-busy={!offer}>
              <span className={styles.offerTag}>Preço promocional · enquanto a assinatura durar</span>
              <div className={styles.offerPrices}>
                <div className={styles.offerItem}>
                  <span className={styles.offerLabel}>Rastreador</span>
                  <span>
                    <Regular cents={offer?.equipmentRegularCents} than={offer?.equipmentCents} />
                    <strong className={styles.offerPrice}>{offer ? priceLabel(offer.equipmentCents) : 'R$ —'}</strong>
                  </span>
                  {installments && <span className={styles.offerInstallments}>ou {installments}</span>}
                </div>
                <div className={styles.offerItem}>
                  <span className={styles.offerLabel}>Mensalidade</span>
                  <span>
                    <Regular cents={offer?.planRegularCents} than={offer?.planCents} />
                    <strong className={styles.offerPrice}>{offer ? priceLabel(offer.planCents) : 'R$ —'}</strong>
                  </span>
                </div>
              </div>
              {offer && (offer.insanosPlanName || offer.loan) && (
                <p className={styles.offerNote}>
                  {offer.insanosPlanName && (
                    <>
                      Integrantes do Insanos MC pagam <span className={styles.nowrap}>{priceLabel(offer.insanosCents)}</span>
                      /mês.{' '}
                    </>
                  )}
                  {offer.loan && <>Prefere não comprar o rastreador? Em comodato: {loanTerms(offer.loan)}.</>}
                </p>
              )}
            </div>
          </div>

          <div className={styles.panel}>
            <ol className={styles.steps}>
              {STEPS.map((step, i) => (
                <li key={step.title}>
                  <span className={styles.number} aria-hidden="true">
                    {i + 1}
                  </span>
                  <span>
                    <strong>{step.title}</strong>
                    <br />
                    {step.text}
                  </span>
                </li>
              ))}
            </ol>
            <Link className={styles.primary} to={CHECKOUT_PATH} data-analytics="secao-contratar">
              Contratar agora
            </Link>
            <button type="button" className={styles.secondary} onClick={() => onOpenModal()} data-analytics="secao-contato">
              Prefiro que a Farbo fale comigo
            </button>
            <p className={styles.small}>Para empresa (CNPJ) ou frota, fale com a gente: o pedido é pela central.</p>
          </div>
        </div>
      </div>
    </section>
  );
};

/** O preço normal riscado, quando é maior que o do site. */
function Regular({ cents, than }: { cents?: number; than?: number }) {
  if (cents === undefined || than === undefined || cents <= than) return null;
  return (
    <>
      <s className={styles.offerOld}>{priceLabel(cents)}</s>{' '}
    </>
  );
}

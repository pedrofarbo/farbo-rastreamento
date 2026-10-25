import React from 'react';

import { HIRE_VIA_WHATSAPP, WHATSAPP_NUMBER } from '@/config/contact';
import { useCheckoutOffer } from '@/hooks/useCheckoutOffer';

import { HowItWorksArt, type StepArtKind } from './HowItWorksArt';
import styles from './HowItWorksSection.module.css';

type Actor = 'voce' | 'farbo' | 'parceiro';

const ACTOR_LABEL: Record<Actor, string> = {
  voce: 'Você',
  farbo: 'Farbo',
  parceiro: 'Você escolhe',
};

interface Step {
  actor: Actor;
  art: StepArtKind;
  title: string;
  description: string;
}

/**
 * O começo pela contratação no site: a pessoa física escolhe e paga com Pix,
 * e a conta no app já sai pronta.
 */
const SITE_STEPS: Step[] = [
  {
    actor: 'voce',
    art: 'pedido',
    title: 'Contrate pelo site',
    description:
      'Em poucos minutos: escolha o plano e o rastreador — comprado, à vista ou parcelado sem juros, ou em comodato, sem comprar —, informe o veículo e o endereço e pague com Pix a 1ª mensalidade, o rastreador e o frete.',
  },
  {
    actor: 'voce',
    art: 'interesse',
    title: 'Entre no app na hora',
    description:
      'Pago o Pix, a sua conta já está pronta: o app abre com você logado, para acompanhar o pedido e, depois, o veículo. No celular, é só adicionar à tela inicial.',
  },
];

/**
 * O começo com a contratação pelo site fechada na central: o pré-cadastro (ou
 * o WhatsApp, com HIRE_VIA_WHATSAPP), a conta criada pela equipe e o pedido
 * no painel.
 */
const CONTACT_STEPS: Step[] = [
  HIRE_VIA_WHATSAPP && WHATSAPP_NUMBER
    ? {
        actor: 'voce',
        art: 'whatsapp',
        title: 'Contrate pelo WhatsApp',
        description:
          'Escolha o plano e fale com a gente. Criamos a sua conta e você recebe por e-mail o convite para definir a senha do painel.',
      }
    : {
        actor: 'voce',
        art: 'interesse',
        title: 'Deixe seu interesse',
        description:
          WHATSAPP_NUMBER
            ? 'Escolha o plano e preencha o pré-cadastro, ou chame no WhatsApp. A gente responde, cria a sua conta e você recebe por e-mail o convite para definir a senha do painel.'
            : 'Escolha o plano e preencha o pré-cadastro. A gente responde por e-mail, cria a sua conta e você recebe o convite para definir a senha do painel.',
      },
  {
    actor: 'voce',
    art: 'pedido',
    title: 'Peça o rastreador pelo painel',
    description:
      'Cadastre o endereço de entrega e, em "Novo veículo", informe o carro ou a moto: rastreador e assinatura saem no mesmo pedido, pago com Pix no próprio painel.',
  },
];

/** Daqui em diante, o caminho é o mesmo: a base, o envio, a instalação e o app. */
const COMMON_STEPS: Step[] = [
  {
    actor: 'farbo',
    art: 'preparo',
    title: 'Preparamos na nossa base',
    description:
      'Separamos o chip M2M e configuramos o rastreador J16, que vai com o relé de bloqueio no kit. Você acompanha cada etapa do pedido no app.',
  },
  {
    actor: 'farbo',
    art: 'envio',
    title: 'Enviamos até você',
    description:
      'O rastreador sai para o endereço do pedido com código de rastreio da transportadora. Avisamos por e-mail quando ele é enviado e quando chega.',
  },
  {
    actor: 'parceiro',
    art: 'instalacao',
    title: 'Instale do seu jeito',
    description:
      'Com o instalador da sua preferência, pago direto a ele — o app mostra prestadores parceiros —, ou com os técnicos da Farbo, que agendam o dia com você. No comodato, a instalação é sempre com os nossos técnicos.',
  },
  {
    actor: 'voce',
    art: 'acompanhamento',
    title: 'Bloqueie e acompanhe pelo celular',
    description:
      'Instalado, o rastreador se conecta sozinho. No app ou no painel: mapa ao vivo, histórico de trajetos e bloqueio e desbloqueio do motor (por segurança, com o veículo parado). Os alertas — SOS, bateria desconectada, movimento com a ignição desligada, ignição ligada de madrugada e mais — chegam no celular e por e-mail.',
  },
];

/**
 * O processo real, do pedido ao mapa: com a contratação pelo site aberta, a
 * pessoa física contrata e paga com Pix e já entra no app; fechada, o
 * pré-cadastro e o pedido no painel. Depois, o preparo na base, o envio, a
 * instalação (por conta própria ou com os técnicos da Farbo) e o app.
 */
export const HowItWorksSection: React.FC = () => {
  const { open } = useCheckoutOffer();
  const steps = [...(open ? SITE_STEPS : CONTACT_STEPS), ...COMMON_STEPS];
  return (
    <section className={styles.section} id="como-funciona">
      <div className={styles.container}>
        <div className={styles.header}>
          <span className={styles.badge}>PASSO A PASSO</span>
          <h2 className={styles.title}>Como Funciona o Rastreamento Farbo</h2>
          <p className={styles.subtitle}>
            {open
              ? 'Do pedido no site ao mapa no celular, você acompanha cada etapa pelo app.'
              : 'Do primeiro contato à instalação, você acompanha cada etapa pelo painel.'}
          </p>
        </div>

        <ol className={styles.stepsGrid}>
          {steps.map((step, index) => (
            <li key={step.title} className={styles.stepCard}>
              <div className={`${styles.art} ${styles[`art_${step.actor}`]}`}>
                <HowItWorksArt kind={step.art} />
              </div>
              <div className={styles.stepTop}>
                <span className={styles.stepNumber}>{String(index + 1).padStart(2, '0')}</span>
                <span className={`${styles.actor} ${styles[`actor_${step.actor}`]}`}>
                  {ACTOR_LABEL[step.actor]}
                </span>
              </div>
              <h3 className={styles.stepTitle}>{step.title}</h3>
              <p className={styles.stepDesc}>{step.description}</p>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
};
